package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type routeTarget struct {
	Model        ModelConfig
	Binding      ModelRouteBinding
	Source       UpstreamSource
	SourceKey    *SourceKey
	SingleSource bool
}

type relayProtocol string

const (
	relayProtocolOpenAI    relayProtocol = "openai"
	relayProtocolAnthropic relayProtocol = "anthropic"
	relayProtocolGemini    relayProtocol = "gemini"
)

const (
	sourceFailureCooldownThreshold = 1
	sourceFailureCooldownDuration  = 5 * time.Minute
)

type usageTokens struct {
	Prompt     int64 // input tokens
	Completion int64 // output tokens
	CacheRead  int64 // cache read tokens
	CacheWrite int64 // cache creation tokens
	Reasoning  int64 // reasoning/thinking tokens
	Total      int64
}

type usageRecordMeta struct {
	RequestID       string
	Protocol        relayProtocol
	Path            string
	Stream          bool
	ResponseHeaders http.Header
	Attempts        []RequestAttempt
}

func (a *App) openAIModels(c *gin.Context) {
	var models []ModelConfig
	if err := a.db.Where("status = ?", ModelStatusActive).Order("name asc").Find(&models).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	data := make([]gin.H, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		if !modelSupportsRelayProtocol(model, relayProtocolOpenAI) {
			continue
		}
		if seen[model.Name] {
			continue
		}
		seen[model.Name] = true
		data = append(data, gin.H{
			"id":       model.Name,
			"object":   "model",
			"created":  model.CreatedAt.Unix(),
			"owned_by": model.Provider,
		})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

func (a *App) openAIModel(c *gin.Context) {
	name := strings.TrimSpace(c.Param("model"))
	var model ModelConfig
	if err := a.db.Where("name = ? AND status = ?", name, ModelStatusActive).First(&model).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	if !modelSupportsRelayProtocol(model, relayProtocolOpenAI) {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": model.Name, "object": "model", "created": model.CreatedAt.Unix(), "owned_by": model.Provider})
}

func (a *App) proxyChatCompletions(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/chat/completions")
}

func (a *App) proxyCompletions(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/completions")
}

func (a *App) proxyResponses(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/responses")
}

func (a *App) proxyAnthropicMessages(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolAnthropic, requestPathWithQuery(c, "/v1/messages"))
}

func (a *App) proxyAnthropicCountTokens(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolAnthropic, requestPathWithQuery(c, "/v1/messages/count_tokens"))
}

func (a *App) proxyGeminiGenerate(c *gin.Context) {
	modelName := geminiModelFromPath(c.Request.URL.Path)
	if modelName == "" {
		errorJSON(c, http.StatusBadRequest, "model is required")
		return
	}
	body, err := readLimitedBody(c.Request.Body, 16<<20)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "read request failed")
		return
	}
	stream := strings.Contains(strings.ToLower(c.Request.URL.Path), ":stream")
	a.proxyUpstream(c, relayProtocolGemini, requestPathWithQuery(c, c.Request.URL.Path), body, modelName, stream)
}

func (a *App) geminiModels(c *gin.Context) {
	var models []ModelConfig
	if err := a.db.Where("status = ? AND provider = ?", ModelStatusActive, "Google").Order("name asc").Find(&models).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]gin.H, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		if seen[model.Name] {
			continue
		}
		seen[model.Name] = true
		out = append(out, gin.H{
			"name":                       "models/" + model.Name,
			"displayName":                firstNonEmpty(model.DisplayName, model.Name),
			"version":                    "",
			"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"},
		})
	}
	c.JSON(http.StatusOK, gin.H{"models": out})
}

func (a *App) proxyJSONBody(c *gin.Context, protocol relayProtocol, upstreamPath string) {
	body, err := readLimitedBody(c.Request.Body, 16<<20)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "read request failed")
		return
	}
	payload := mustJSONMap(body)
	modelName := getString(payload, "model")
	if modelName == "" {
		errorJSON(c, http.StatusBadRequest, "model is required")
		return
	}
	a.proxyUpstream(c, protocol, upstreamPath, body, modelName, getBool(payload, "stream"))
}

func (a *App) proxyUpstream(c *gin.Context, protocol relayProtocol, upstreamPath string, body []byte, modelName string, stream bool) {
	user, key, ok := currentAPIIdentity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "invalid api key")
		return
	}
	requestID := newRelayRequestID()
	if err := a.checkQuota(user); err != nil {
		errorJSON(c, http.StatusTooManyRequests, err.Error())
		return
	}
	targets, err := a.routeTargets(modelName, protocol)
	if err != nil {
		a.recordUsage(c, user, key, routeTarget{Model: ModelConfig{Name: modelName}}, usageTokens{}, http.StatusBadGateway, RequestStatusError, err.Error(), body, nil, 0, usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream})
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	settings, _ := a.getSettings()
	attempts := settings.MaxRetries
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > len(targets) {
		attempts = len(targets)
	}
	var lastErr error
	lastStatus := http.StatusBadGateway
	attemptRows := make([]RequestAttempt, 0, attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		target := targets[attempt]
		start := time.Now()
		resp, err := a.callUpstream(c, target, protocol, upstreamPath, body, stream)
		ended := time.Now()
		latency := ended.Sub(start).Milliseconds()
		if err != nil {
			lastErr = err
			statusCode := upstreamRequestErrorStatus(c, err)
			lastStatus = statusCode
			attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, protocol, upstreamPath, statusCode, RequestStatusError, err.Error(), latency, start, ended))
			if statusCode == 499 {
				a.recordUsage(c, user, key, target, usageTokens{}, statusCode, RequestStatusError, err.Error(), body, nil, latency, usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream, Attempts: attemptRows})
				return
			}
			a.markTargetFailure(target, statusCode)
			continue
		}
		if isRetryableUpstreamStatus(resp.StatusCode) && attempt < attempts-1 {
			_, _ = readLimitedBody(resp.Body, 1<<20)
			_ = resp.Body.Close()
			lastErr = errors.New(resp.Status)
			lastStatus = resp.StatusCode
			attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, protocol, upstreamPath, resp.StatusCode, RequestStatusError, resp.Status, latency, start, ended))
			a.markTargetFailure(target, resp.StatusCode)
			continue
		}
		status := RequestStatusSuccess
		errMsg := ""
		if resp.StatusCode >= 400 {
			status = RequestStatusError
			errMsg = resp.Status
		}
		attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, protocol, upstreamPath, resp.StatusCode, status, errMsg, latency, start, ended))
		a.markTargetResult(target, resp.StatusCode)
		meta := usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream, Attempts: attemptRows}
		if stream || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			a.proxyStreamResponse(c, resp, user, key, target, body, latency, meta)
			return
		}
		a.proxyBufferedResponse(c, resp, user, key, target, body, latency, meta)
		return
	}
	message := "upstream unavailable"
	if lastErr != nil {
		message = lastErr.Error()
	}
	target := routeTarget{}
	if len(targets) > 0 {
		target = targets[len(targets)-1]
	}
	a.recordUsage(c, user, key, target, usageTokens{}, lastStatus, RequestStatusError, message, body, nil, 0, usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream, Attempts: attemptRows})
	errorJSON(c, lastStatus, message)
}

func (a *App) callUpstream(c *gin.Context, target routeTarget, protocol relayProtocol, path string, body []byte, stream bool) (*http.Response, error) {
	source := target.Source
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, upstreamURL(target, protocol, path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyProxyHeaders(req.Header, c.Request.Header)
	applyUpstreamAuth(req.Header, source, effectiveUpstreamAPIKey(target), protocol)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if protocol == relayProtocolAnthropic && req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	timeout := 120 * time.Second
	if settings, err := a.getSettings(); err == nil && settings.DefaultTimeout > 0 {
		timeout = time.Duration(settings.DefaultTimeout) * time.Second
	}
	client := &http.Client{Timeout: timeout}
	if stream {
		client.Timeout = 0
		client.Transport = &http.Transport{ResponseHeaderTimeout: timeout}
	}
	return client.Do(req)
}

func upstreamRequestErrorStatus(c *gin.Context, err error) int {
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return 499
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func copyProxyHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "x-api-key" || lower == "x-goog-api-key" || lower == "host" || lower == "content-length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func applyUpstreamAuth(header http.Header, source UpstreamSource, apiKey string, protocol relayProtocol) {
	header.Del("Authorization")
	header.Del("X-API-Key")
	header.Del("X-Goog-Api-Key")
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	if strings.EqualFold(source.Type, "CLIProxyAPI") {
		header.Set("Authorization", "Bearer "+apiKey)
		return
	}
	switch protocol {
	case relayProtocolAnthropic:
		header.Set("x-api-key", apiKey)
	case relayProtocolGemini:
		header.Set("x-goog-api-key", apiKey)
	default:
		header.Set("Authorization", "Bearer "+apiKey)
	}
}

func effectiveUpstreamAPIKey(target routeTarget) string {
	if target.SourceKey != nil && strings.TrimSpace(target.SourceKey.APIKey) != "" {
		return target.SourceKey.APIKey
	}
	return target.Source.APIKey
}

func upstreamURL(target routeTarget, protocol relayProtocol, path string) string {
	source := target.Source
	if strings.EqualFold(source.Type, SourceTypeCLIProxyAPI) {
		if providerPath := cliProxyProviderRelayPath(protocol, path); providerPath != "" {
			return joinUpstreamPath(cliProxyManagementBase(source.BaseURL), providerPath)
		}
	}
	return joinUpstreamPath(sourceProtocolBaseURL(source, protocol), path)
}

func sourceProtocolBaseURL(source UpstreamSource, protocol relayProtocol) string {
	switch protocol {
	case relayProtocolOpenAI:
		if strings.TrimSpace(source.OpenAIBaseURL) != "" {
			return source.OpenAIBaseURL
		}
		base := normalizeBaseURL(source.BaseURL)
		if strings.HasSuffix(strings.ToLower(base), "/v1") {
			return base
		}
		return joinUpstreamPath(base, "/v1")
	case relayProtocolAnthropic:
		if strings.TrimSpace(source.AnthropicBaseURL) != "" {
			return source.AnthropicBaseURL
		}
		return source.BaseURL
	default:
		return source.BaseURL
	}
}

func cliProxyProviderRelayPath(protocol relayProtocol, path string) string {
	switch protocol {
	case relayProtocolAnthropic:
		return prefixPathPreservingQuery("/api/provider/anthropic", path)
	case relayProtocolGemini:
		return prefixPathPreservingQuery("/api/provider/google", path)
	default:
		return ""
	}
}

func (a *App) proxyBufferedResponse(c *gin.Context, resp *http.Response, user User, key APIKey, target routeTarget, requestBody []byte, latency int64, meta usageRecordMeta) {
	defer resp.Body.Close()
	meta.ResponseHeaders = resp.Header.Clone()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		a.recordUsage(c, user, key, target, usageTokens{}, http.StatusBadGateway, RequestStatusError, err.Error(), requestBody, nil, latency, meta)
		errorJSON(c, http.StatusBadGateway, "read upstream response failed")
		return
	}
	usage := extractUsage(responseBody)
	status := RequestStatusSuccess
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = RequestStatusError
		errMsg = resp.Status
	}
	a.recordUsage(c, user, key, target, usage, resp.StatusCode, status, errMsg, requestBody, responseBody, latency, meta)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(resp.StatusCode, contentType, responseBody)
}

func (a *App) proxyStreamResponse(c *gin.Context, resp *http.Response, user User, key APIKey, target routeTarget, requestBody []byte, latency int64, meta usageRecordMeta) {
	defer resp.Body.Close()
	meta.ResponseHeaders = resp.Header.Clone()
	for header, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(header, value)
		}
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
	}
	c.Status(resp.StatusCode)
	flusher, _ := c.Writer.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	capture := &limitCapture{limit: 1 << 20}
	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = capture.Write(chunk)
			if _, writeErr := c.Writer.Write(chunk); writeErr != nil {
				a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), 499, RequestStatusError, writeErr.Error(), requestBody, capture.Bytes(), latency, meta)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), http.StatusBadGateway, RequestStatusError, readErr.Error(), requestBody, capture.Bytes(), latency, meta)
			return
		}
	}
	status := RequestStatusSuccess
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = RequestStatusError
		errMsg = resp.Status
	}
	a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), resp.StatusCode, status, errMsg, requestBody, capture.Bytes(), latency, meta)
}

func (a *App) routeTargets(modelName string, protocol relayProtocol) ([]routeTarget, error) {
	var models []ModelConfig
	if err := a.db.Where("name = ? AND status = ?", modelName, ModelStatusActive).Find(&models).Error; err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, errors.New("model not configured")
	}
	candidates := make([]routeTarget, 0, len(models))
	now := time.Now()
	for _, model := range models {
		if !modelSupportsRelayProtocol(model, protocol) {
			continue
		}
		bindings, err := a.modelBindings(model)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if !binding.Enabled {
				continue
			}
			var source UpstreamSource
			if err := a.db.First(&source, binding.SourceID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return nil, err
			}
			if source.Status == SourceStatusDisabled {
				continue
			}
			target := routeTarget{Model: model, Binding: binding, Source: source}
			if binding.SourceKeyID != nil {
				var sourceKey SourceKey
				err := a.db.Where("id = ? AND source_id = ? AND status = ?", *binding.SourceKeyID, source.ID, APIKeyStatusValid).First(&sourceKey).Error
				if err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return nil, err
				}
				target.SourceKey = &sourceKey
			}
			candidates = append(candidates, target)
		}
	}
	if len(candidates) == 1 {
		// A single upstream is a direct route, not a schedulable pool.
		candidates[0].SingleSource = true
		return candidates, nil
	}
	targets := make([]routeTarget, 0, len(candidates))
	for _, target := range candidates {
		if target.Source.Status != SourceStatusOnline {
			continue
		}
		if target.Source.CooldownUntil != nil && target.Source.CooldownUntil.After(now) {
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, errors.New("no online source for model")
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Source.Priority != targets[j].Source.Priority {
			return targets[i].Source.Priority < targets[j].Source.Priority
		}
		leftWeight := nonZeroInt(targets[i].Binding.RoutingWeight, 1)
		rightWeight := nonZeroInt(targets[j].Binding.RoutingWeight, 1)
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		if targets[i].Binding.ID != targets[j].Binding.ID {
			return targets[i].Binding.ID < targets[j].Binding.ID
		}
		return targets[i].Model.ID < targets[j].Model.ID
	})
	return targets, nil
}

func (a *App) checkQuota(user User) error {
	if user.MonthlyQuota > 0 && a.userTokenUsage(user.ID, monthStart(time.Now())) >= user.MonthlyQuota {
		return errors.New("monthly quota exceeded")
	}
	if user.WeeklyQuota > 0 && a.userTokenUsage(user.ID, weekStart(time.Now())) >= user.WeeklyQuota {
		return errors.New("weekly quota exceeded")
	}
	return nil
}

func (a *App) recordUsage(c *gin.Context, user User, key APIKey, target routeTarget, usage usageTokens, statusCode int, status string, errMsg string, requestBody []byte, responseBody []byte, latency int64, metas ...usageRecordMeta) UsageLog {
	if usage.Total == 0 {
		usage.Total = usage.Prompt + usage.Completion
	}
	meta := usageRecordMeta{}
	if len(metas) > 0 {
		meta = metas[0]
	}
	cost := estimateCostDetailed(usage, target.Model)
	sourceKeyID := sourceKeyIDFromTarget(target)
	modelName := target.Model.Name
	upstreamName := target.Source.Name
	if len(meta.Attempts) > 0 {
		final := meta.Attempts[len(meta.Attempts)-1]
		if modelName == "" {
			modelName = final.Model
		}
		if upstreamName == "" {
			upstreamName = final.UpstreamName
		}
		if sourceKeyID == 0 {
			sourceKeyID = final.SourceKeyID
		}
	}
	logRow := UsageLog{
		UserID:           user.ID,
		APIKeyID:         key.ID,
		SourceID:         target.Source.ID,
		SourceKeyID:      sourceKeyID,
		RequestID:        meta.RequestID,
		Protocol:         string(meta.Protocol),
		Path:             meta.Path,
		Stream:           meta.Stream,
		Model:            modelName,
		UpstreamName:     upstreamName,
		PromptTokens:     usage.Prompt,
		CompletionTokens: usage.Completion,
		CacheReadTokens:  usage.CacheRead,
		CacheWriteTokens: usage.CacheWrite,
		ReasoningTokens:  usage.Reasoning,
		TotalTokens:      usage.Total,
		EstimatedCost:    cost,
		LatencyMS:        latency,
		StatusCode:       statusCode,
		Status:           status,
		ErrorMessage:     errMsg,
		RequestPayload:   truncateString(string(requestBody), 64<<10),
		ResponsePayload:  truncateString(string(responseBody), 64<<10),
		RequestHeaders:   sanitizedHeaderJSON(requestHeadersFromContext(c)),
		ResponseHeaders:  sanitizedHeaderJSON(meta.ResponseHeaders),
		AttemptCount:     len(meta.Attempts),
	}
	_ = a.db.Create(&logRow).Error
	if logRow.ID != 0 && len(meta.Attempts) > 0 {
		attemptRows := make([]RequestAttempt, len(meta.Attempts))
		for i := range meta.Attempts {
			attemptRows[i] = meta.Attempts[i]
			attemptRows[i].ID = 0
			attemptRows[i].UsageLogID = logRow.ID
			if attemptRows[i].RequestID == "" {
				attemptRows[i].RequestID = meta.RequestID
			}
			if attemptRows[i].AttemptIndex == 0 {
				attemptRows[i].AttemptIndex = i + 1
			}
		}
		if err := a.db.Create(&attemptRows).Error; err == nil {
			logRow.AttemptCount = len(attemptRows)
			logRow.FinalAttemptID = attemptRows[len(attemptRows)-1].ID
			_ = a.db.Model(&UsageLog{}).Where("id = ?", logRow.ID).Updates(map[string]any{
				"attempt_count":    logRow.AttemptCount,
				"final_attempt_id": logRow.FinalAttemptID,
			}).Error
		}
	}
	now := time.Now()
	_ = a.db.Model(&APIKey{}).Where("id = ?", key.ID).Updates(map[string]any{
		"last_used_at": now,
		"spent_usd":    gorm.Expr("spent_usd + ?", cost),
	}).Error
	if cost > 0 {
		_ = a.db.Model(&User{}).Where("id = ?", user.ID).Update("balance", gorm.Expr("balance - ?", cost)).Error
	}
	if target.Source.ID != 0 && latency > 0 {
		updates := map[string]any{"latency_ms": int(latency)}
		if status == RequestStatusSuccess && target.Source.Status != SourceStatusDisabled {
			updates["status"] = SourceStatusOnline
		}
		_ = a.db.Model(&UpstreamSource{}).Where("id = ?", target.Source.ID).Updates(updates).Error
		_ = a.db.Model(&ModelConfig{}).Where("id = ?", target.Model.ID).Update("latency_ms", int(latency)).Error
		if target.Binding.ID != 0 {
			_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID).Update("latency_ms", int(latency)).Error
		}
	}
	if target.SourceKey != nil && target.SourceKey.ID != 0 {
		_ = a.db.Model(&SourceKey{}).Where("id = ?", target.SourceKey.ID).Update("last_used_at", now).Error
	}
	a.collectCLIProxyUsageQueueAsync(target.Source)
	return logRow
}

func newRelayRequestID() string {
	token, err := randomToken("req_", 12)
	if err == nil {
		return token
	}
	return "req_" + time.Now().UTC().Format("20060102150405.000000000")
}

func isRetryableUpstreamStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func requestAttemptRow(requestID string, index int, target routeTarget, protocol relayProtocol, path string, statusCode int, status string, errMsg string, latency int64, started time.Time, ended time.Time) RequestAttempt {
	if started.IsZero() {
		started = time.Now()
	}
	if ended.IsZero() {
		ended = started
	}
	return RequestAttempt{
		RequestID:     requestID,
		AttemptIndex:  index,
		ModelConfigID: target.Model.ID,
		SourceID:      target.Source.ID,
		SourceKeyID:   sourceKeyIDFromTarget(target),
		Model:         target.Model.Name,
		UpstreamName:  target.Source.Name,
		Protocol:      string(protocol),
		Path:          path,
		StatusCode:    statusCode,
		Status:        status,
		ErrorMessage:  errMsg,
		LatencyMS:     latency,
		StartedAt:     started,
		EndedAt:       ended,
	}
}

func sourceKeyIDFromTarget(target routeTarget) uint {
	if target.SourceKey == nil {
		return sourceKeyIDValueFromBinding(target.Binding)
	}
	if target.SourceKey.ID == 0 {
		return 0
	}
	return target.SourceKey.ID
}

func requestHeadersFromContext(c *gin.Context) http.Header {
	if c == nil || c.Request == nil {
		return nil
	}
	return c.Request.Header
}

func sanitizedHeaderJSON(header http.Header) string {
	if len(header) == 0 {
		return "{}"
	}
	out := make(map[string]string, len(header))
	for key, values := range header {
		if sensitiveHeaderName(key) {
			out[key] = "<redacted>"
			continue
		}
		out[key] = truncateString(strings.Join(values, ", "), 2048)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func sensitiveHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "cookie", "set-cookie", "x-api-key", "x-goog-api-key", "proxy-authorization":
		return true
	default:
		return strings.Contains(strings.ToLower(name), "secret") || strings.Contains(strings.ToLower(name), "token")
	}
}

func (a *App) markTargetResult(target routeTarget, statusCode int) {
	if isRetryableUpstreamStatus(statusCode) {
		a.markTargetFailure(target, statusCode)
		return
	}
	a.markTargetSuccess(target)
}

func (a *App) markTargetSuccess(target routeTarget) {
	if target.Source.ID == 0 {
		return
	}
	now := time.Now()
	_ = a.db.Model(&UpstreamSource{}).Where("id = ?", target.Source.ID).Updates(map[string]any{
		"failure_count":   0,
		"cooldown_until":  nil,
		"status":          SourceStatusOnline,
		"success_count":   gorm.Expr("success_count + ?", 1),
		"last_success_at": now,
	}).Error
}

func (a *App) markTargetFailure(target routeTarget, statusCode int) {
	if target.Source.ID == 0 || statusCode == 499 {
		return
	}
	now := time.Now()
	updates := map[string]any{
		"failure_count":   gorm.Expr("failure_count + ?", 1),
		"last_failure_at": now,
	}
	if !target.SingleSource && target.Source.FailureCount+1 >= sourceFailureCooldownThreshold {
		updates["cooldown_until"] = now.Add(sourceFailureCooldownDuration)
	}
	_ = a.db.Model(&UpstreamSource{}).Where("id = ?", target.Source.ID).Updates(updates).Error
}

func extractUsage(body []byte) usageTokens {
	if len(body) == 0 {
		return usageTokens{}
	}
	text := string(body)
	if strings.Contains(text, "\ndata:") || strings.HasPrefix(strings.TrimSpace(text), "data:") {
		return extractSSEUsage(text)
	}
	return extractJSONUsage(body)
}

func extractSSEUsage(text string) usageTokens {
	var usage usageTokens
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		next := extractJSONUsage([]byte(data))
		if next.Total > 0 || next.Prompt > 0 || next.Completion > 0 {
			usage = next
		}
	}
	return usage
}

func extractJSONUsage(body []byte) usageTokens {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return usageTokens{}
	}
	if raw, ok := payload["usage"].(map[string]any); ok {
		return extractOpenAIUsage(raw)
	}
	if raw, ok := payload["usageMetadata"].(map[string]any); ok {
		return extractGeminiUsage(raw)
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if raw, ok := response["usage"].(map[string]any); ok {
			return extractAnthropicUsage(raw)
		}
	}
	return usageTokens{}
}

// extractOpenAIUsage extracts token usage from OpenAI-style response.
func extractOpenAIUsage(raw map[string]any) usageTokens {
	prompt := int64(numberAny(raw["prompt_tokens"]))
	completion := int64(numberAny(raw["completion_tokens"]))
	if prompt == 0 {
		prompt = int64(numberAny(raw["input_tokens"]))
	}
	if completion == 0 {
		completion = int64(numberAny(raw["output_tokens"]))
	}
	total := int64(numberAny(raw["total_tokens"]))
	if total == 0 {
		total = prompt + completion
	}

	// Cache tokens from prompt_tokens_details
	var cacheRead int64
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		cacheRead = int64(numberAny(details["cached_tokens"]))
	}

	// Reasoning tokens from completion_tokens_details
	var reasoning int64
	if details, ok := raw["completion_tokens_details"].(map[string]any); ok {
		reasoning = int64(numberAny(details["reasoning_tokens"]))
	}

	return usageTokens{
		Prompt:     prompt,
		Completion: completion,
		CacheRead:  cacheRead,
		Reasoning:  reasoning,
		Total:      total,
	}
}

// extractGeminiUsage extracts token usage from Gemini-style response.
func extractGeminiUsage(raw map[string]any) usageTokens {
	prompt := int64(numberAny(raw["promptTokenCount"]))
	completion := int64(numberAny(raw["candidatesTokenCount"]))
	total := int64(numberAny(raw["totalTokenCount"]))
	if total == 0 {
		total = prompt + completion
	}
	cacheRead := int64(numberAny(raw["cachedContentTokenCount"]))
	return usageTokens{
		Prompt:     prompt,
		Completion: completion,
		CacheRead:  cacheRead,
		Total:      total,
	}
}

// extractAnthropicUsage extracts token usage from Anthropic-style response.
func extractAnthropicUsage(raw map[string]any) usageTokens {
	prompt := int64(numberAny(raw["input_tokens"]))
	completion := int64(numberAny(raw["output_tokens"]))
	total := int64(numberAny(raw["total_tokens"]))
	if total == 0 {
		total = prompt + completion
	}
	cacheRead := int64(numberAny(raw["cache_read_input_tokens"]))
	cacheWrite := int64(numberAny(raw["cache_creation_input_tokens"]))
	reasoning := int64(numberAny(raw["reasoning_tokens"]))
	return usageTokens{
		Prompt:     prompt,
		Completion: completion,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Reasoning:  reasoning,
		Total:      total,
	}
}

func numberAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

type limitCapture struct {
	limit int
	buf   bytes.Buffer
}

func (w *limitCapture) Write(p []byte) (int, error) {
	if w.limit <= 0 || w.buf.Len() >= w.limit {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *limitCapture) Bytes() []byte {
	return w.buf.Bytes()
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
