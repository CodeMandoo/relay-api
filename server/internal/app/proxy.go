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

type usageTokens struct {
	Prompt                   int64 // input tokens
	Completion               int64 // output tokens
	CacheRead                int64 // cache read tokens
	CacheWrite               int64 // cache creation tokens
	Reasoning                int64 // reasoning/thinking tokens
	Total                    int64
	PromptIncludesCacheRead  bool
	PromptIncludesCacheWrite bool
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
	_, key, ok := currentAPIIdentity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "invalid api key")
		return
	}
	modelGroupID, ok := a.requireAPIKeyModelGroup(c, key)
	if !ok {
		return
	}
	var models []ModelConfig
	query := a.db.Where("status = ?", ModelStatusActive)
	query = a.applyModelGroupFilter(query, modelGroupID)
	if err := query.Order("name asc").Find(&models).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	data := make([]gin.H, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		if !a.modelProtocolAllowed(model, relayProtocolOpenAI) {
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
	_, key, ok := currentAPIIdentity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "invalid api key")
		return
	}
	modelGroupID, ok := a.requireAPIKeyModelGroup(c, key)
	if !ok {
		return
	}
	name := strings.TrimSpace(c.Param("model"))
	var model ModelConfig
	query := a.db.Where("name = ? AND status = ?", name, ModelStatusActive)
	query = a.applyModelGroupFilter(query, modelGroupID)
	if err := query.First(&model).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	if !a.modelProtocolAllowed(model, relayProtocolOpenAI) {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": model.Name, "object": "model", "created": model.CreatedAt.Unix(), "owned_by": model.Provider})
}

func (a *App) proxyChatCompletions(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/chat/completions", true)
}

func (a *App) proxyCompletions(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/completions", false)
}

func (a *App) proxyResponses(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolOpenAI, "/v1/responses", true)
}

func (a *App) proxyAnthropicMessages(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolAnthropic, requestPathWithQuery(c, "/v1/messages"), true)
}

func (a *App) proxyAnthropicCountTokens(c *gin.Context) {
	a.proxyJSONBody(c, relayProtocolAnthropic, requestPathWithQuery(c, "/v1/messages/count_tokens"), false)
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
	a.proxyUpstream(c, relayProtocolGemini, requestPathWithQuery(c, c.Request.URL.Path), body, modelName, stream, false)
}

func (a *App) geminiModels(c *gin.Context) {
	_, key, ok := currentAPIIdentity(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "invalid api key")
		return
	}
	modelGroupID, ok := a.requireAPIKeyModelGroup(c, key)
	if !ok {
		return
	}
	var models []ModelConfig
	query := a.db.Where("status = ? AND provider = ?", ModelStatusActive, "Google")
	query = a.applyModelGroupFilter(query, modelGroupID)
	if err := query.Order("name asc").Find(&models).Error; err != nil {
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

func (a *App) proxyJSONBody(c *gin.Context, protocol relayProtocol, upstreamPath string, allowConversion bool) {
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
	a.proxyUpstream(c, protocol, upstreamPath, body, modelName, getBool(payload, "stream"), allowConversion)
}

func (a *App) proxyUpstream(c *gin.Context, protocol relayProtocol, upstreamPath string, body []byte, modelName string, stream bool, allowConversion bool) {
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
	modelGroupID, ok := a.requireAPIKeyModelGroup(c, key)
	if !ok {
		return
	}
	targets, err := a.scheduledRouteTargets(modelName, protocol, allowConversion, modelGroupID)
	if err != nil {
		a.recordUsage(c, user, key, routeTarget{Model: ModelConfig{Name: modelName}}, usageTokens{}, http.StatusBadGateway, RequestStatusError, err.Error(), body, nil, 0, usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream})
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	settings, _ := a.getSettings()
	attempts := settings.MaxRetries
	group, _ := a.modelGroupForRouting(modelGroupID)
	if !group.DynamicRouting {
		attempts = 1
	}
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
		// Resolve the protocol the upstream actually speaks; when it differs
		// from the client protocol, convert the request on the way out and
		// the response on the way back.
		targetProtocol := protocol
		targetPath := upstreamPath
		targetBody := body
		converted := false
		if allowConversion && a.protocolConversionEnabled() {
			if native := modelNativeProtocol(target.Model, protocol); native != protocol {
				convertedBody, convErr := convertRequestBody(protocol, native, body, upstreamPath)
				if convErr != nil {
					lastErr = convErr
					attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, native, targetPath, http.StatusBadRequest, RequestStatusError, convErr.Error(), 0, time.Now(), time.Now()))
					continue
				}
				targetProtocol = native
				targetPath = convertedUpstreamPathForTarget(target, native)
				targetBody = convertedBody
				converted = true
			}
		}
		start := time.Now()
		resp, err := a.callUpstream(c, target, targetProtocol, targetPath, targetBody, stream)
		ended := time.Now()
		latency := ended.Sub(start).Milliseconds()
		if err != nil {
			lastErr = err
			statusCode := upstreamRequestErrorStatus(c, err)
			lastStatus = statusCode
			attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, targetProtocol, targetPath, statusCode, RequestStatusError, err.Error(), latency, start, ended))
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
			attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, targetProtocol, targetPath, resp.StatusCode, RequestStatusError, resp.Status, latency, start, ended))
			a.markTargetFailure(target, resp.StatusCode)
			continue
		}
		status := RequestStatusSuccess
		errMsg := ""
		if resp.StatusCode >= 400 {
			status = RequestStatusError
			errMsg = resp.Status
		}
		attemptRows = append(attemptRows, requestAttemptRow(requestID, attempt+1, target, targetProtocol, targetPath, resp.StatusCode, status, errMsg, latency, start, ended))
		a.markTargetResult(target, resp.StatusCode)
		meta := usageRecordMeta{RequestID: requestID, Protocol: protocol, Path: upstreamPath, Stream: stream, Attempts: attemptRows}
		isEventStream := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
		if converted {
			// Decide by the actual upstream content type: some upstreams answer
			// a streaming request with a plain JSON body.
			if isEventStream {
				a.proxyConvertedStreamResponse(c, resp, targetProtocol, protocol, user, key, target, body, latency, meta)
				return
			}
			a.proxyConvertedBufferedResponse(c, resp, targetProtocol, protocol, user, key, target, body, latency, meta)
			return
		}
		if stream || isEventStream {
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
		// 流式请求同样启用环境变量代理（HTTP_PROXY/HTTPS_PROXY/NO_PROXY），
		// 否则会漏配代理导致直连上游。
		client.Transport = &http.Transport{
			ResponseHeaderTimeout: timeout,
			Proxy:                 http.ProxyFromEnvironment,
		}
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

// proxyConvertedBufferedResponse reads a non-streaming upstream response,
// converts it from the upstream protocol to the client protocol and writes it.
func (a *App) proxyConvertedBufferedResponse(c *gin.Context, resp *http.Response, from, to relayProtocol, user User, key APIKey, target routeTarget, requestBody []byte, latency int64, meta usageRecordMeta) {
	defer resp.Body.Close()
	meta.ResponseHeaders = resp.Header.Clone()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		a.recordUsage(c, user, key, target, usageTokens{}, http.StatusBadGateway, RequestStatusError, err.Error(), requestBody, nil, latency, meta)
		errorJSON(c, http.StatusBadGateway, "read upstream response failed")
		return
	}
	var out []byte
	if resp.StatusCode >= 400 {
		out = convertErrorBody(to, responseBody)
	} else {
		converted, convErr := convertResponseBody(from, to, responseBody, target.Model.Name, meta.Path)
		if convErr != nil {
			a.recordUsage(c, user, key, target, usageTokens{}, http.StatusBadGateway, RequestStatusError, convErr.Error(), requestBody, responseBody, latency, meta)
			errorJSON(c, http.StatusBadGateway, "convert upstream response failed")
			return
		}
		out = converted
	}
	usage := extractUsage(out)
	status := RequestStatusSuccess
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = RequestStatusError
		errMsg = resp.Status
	}
	a.recordUsage(c, user, key, target, usage, resp.StatusCode, status, errMsg, requestBody, out, latency, meta)
	c.Data(resp.StatusCode, "application/json; charset=utf-8", out)
}

// proxyConvertedStreamResponse converts an upstream SSE stream event-by-event
// into the client protocol, flushing converted events as they are produced.
func (a *App) proxyConvertedStreamResponse(c *gin.Context, resp *http.Response, from, to relayProtocol, user User, key APIKey, target routeTarget, requestBody []byte, latency int64, meta usageRecordMeta) {
	defer resp.Body.Close()
	meta.ResponseHeaders = resp.Header.Clone()
	if resp.StatusCode >= 400 {
		// Upstream errors arrive as plain JSON even for streaming requests.
		body, _ := readLimitedBody(resp.Body, 1<<20)
		out := convertErrorBody(to, body)
		a.recordUsage(c, user, key, target, usageTokens{}, resp.StatusCode, RequestStatusError, resp.Status, requestBody, out, latency, meta)
		c.Data(resp.StatusCode, "application/json; charset=utf-8", out)
		return
	}
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	c.Status(resp.StatusCode)
	flusher, _ := c.Writer.(http.Flusher)
	converter := newStreamConverter(from, to, meta.Path, target.Model.Name)
	capture := &limitCapture{limit: 1 << 20}
	reader := bufio.NewReader(resp.Body)
	emit := func(events []sseEvent) error {
		for _, ev := range events {
			chunk := writeSSEEvent(to, ev)
			_, _ = capture.Write([]byte(chunk))
			if _, writeErr := c.Writer.Write([]byte(chunk)); writeErr != nil {
				return writeErr
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	for {
		ev, readErr := readSSEEvent(reader)
		if readErr == nil || ev.data != "" || ev.event != "" {
			if writeErr := emit(converter.push(ev)); writeErr != nil {
				a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), 499, RequestStatusError, writeErr.Error(), requestBody, capture.Bytes(), latency, meta)
				return
			}
		}
		if readErr != nil {
			break
		}
	}
	if writeErr := emit(converter.finish()); writeErr != nil {
		a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), 499, RequestStatusError, writeErr.Error(), requestBody, capture.Bytes(), latency, meta)
		return
	}
	a.recordUsage(c, user, key, target, extractUsage(capture.Bytes()), resp.StatusCode, RequestStatusSuccess, "", requestBody, capture.Bytes(), latency, meta)
}

func (a *App) scheduledRouteTargets(modelName string, protocol relayProtocol, allowConversion bool, modelGroupIDs ...uint) ([]routeTarget, error) {
	return a.routeTargetsWithScheduler(modelName, protocol, true, allowConversion, modelGroupIDs...)
}

func (a *App) routeTargets(modelName string, protocol relayProtocol, modelGroupIDs ...uint) ([]routeTarget, error) {
	return a.routeTargetsWithScheduler(modelName, protocol, false, false, modelGroupIDs...)
}

func (a *App) routeTargetsWithScheduler(modelName string, protocol relayProtocol, advanceScheduler bool, allowConversion bool, modelGroupIDs ...uint) ([]routeTarget, error) {
	modelGroupID := uint(0)
	if len(modelGroupIDs) > 0 {
		modelGroupID = modelGroupIDs[0]
	}
	var models []ModelConfig
	query := a.db.Where("name = ? AND status = ?", modelName, ModelStatusActive)
	query = a.applyModelGroupFilter(query, modelGroupID)
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, errors.New("model not configured")
	}
	candidates := make([]routeTarget, 0, len(models))
	now := time.Now()
	for _, model := range models {
		allowed := modelSupportsRelayProtocol(model, protocol)
		if !allowed && allowConversion {
			allowed = a.modelProtocolAllowed(model, protocol)
		}
		if !allowed {
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
			if advanceScheduler {
				target.Binding = a.refreshSchedulerState(target.Binding, now)
			}
			candidates = append(candidates, target)
		}
	}
	if len(candidates) == 1 {
		candidates[0].SingleSource = true
	}
	targets := make([]routeTarget, 0, len(candidates))
	for _, target := range candidates {
		if target.Source.Status != SourceStatusOnline {
			continue
		}
		if target.Source.CooldownUntil != nil && target.Source.CooldownUntil.After(now) {
			continue
		}
		if effectiveRoutingWeight(target, now) <= 0 {
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		if len(candidates) > 0 && anyCandidateFrozen(candidates, now) {
			return nil, errors.New("当前模型所有上游源冷却中")
		}
		return nil, errors.New("no online source for model")
	}
	group, err := a.modelGroupForRouting(modelGroupID)
	if err != nil {
		return nil, err
	}
	if !group.DynamicRouting {
		fixed, err := a.fixedRouteTarget(targets, group)
		if err != nil {
			return nil, err
		}
		return []routeTarget{fixed}, nil
	}
	if advanceScheduler {
		return a.scheduleTargets(targets, now), nil
	}
	return previewRouteTargets(targets, now), nil
}

func previewRouteTargets(targets []routeTarget, now time.Time) []routeTarget {
	if len(targets) <= 1 {
		return targets
	}
	out := append([]routeTarget(nil), targets...)
	sort.SliceStable(out, func(i, j int) bool {
		return routeTargetLess(out[i], out[j], now)
	})
	return out
}

func (a *App) requireAPIKeyModelGroup(c *gin.Context, key APIKey) (uint, bool) {
	groupID, err := a.modelGroupIDForAPIKey(key)
	if err == nil {
		return groupID, true
	}
	if errors.Is(err, errModelGroupDeleted) {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return 0, false
	}
	errorJSON(c, http.StatusInternalServerError, "database error")
	return 0, false
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
	if target.Source.ID != 0 {
		// 转发延迟（latency_ms）只由测速接口更新，真实请求耗时含模型执行时间，不覆盖。
		if status == RequestStatusSuccess && target.Source.Status != SourceStatusDisabled {
			_ = a.db.Model(&UpstreamSource{}).Where("id = ?", target.Source.ID).Update("status", SourceStatusOnline).Error
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
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
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
	if statusCode < http.StatusBadRequest {
		a.markTargetSuccess(target)
	}
}

func (a *App) markTargetSuccess(target routeTarget) {
	if target.Source.ID == 0 {
		return
	}
	now := time.Now()
	a.markBindingSuccess(target, now)
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
	// 单上游源模型不启用熔断冷却：失败只累计源级统计，不写入绑定冷却状态，
	// 后续请求继续直接转发该源。
	if !target.SingleSource {
		a.markBindingFailure(target, statusCode, now)
	}
	updates := map[string]any{
		"failure_count":   gorm.Expr("failure_count + ?", 1),
		"last_failure_at": now,
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
		if hasUsageTokens(next) {
			usage = mergeUsageTokens(usage, next)
		}
	}
	if computed := usage.Prompt + usage.Completion; computed > usage.Total {
		usage.Total = computed
	}
	return usage
}

func extractJSONUsage(body []byte) usageTokens {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return usageTokens{}
	}
	var usage usageTokens
	if raw, ok := payload["usage"].(map[string]any); ok {
		usage = mergeUsageTokens(usage, extractOpenAIUsage(raw))
	}
	if raw, ok := payload["usageMetadata"].(map[string]any); ok {
		usage = mergeUsageTokens(usage, extractGeminiUsage(raw))
	}
	if message, ok := payload["message"].(map[string]any); ok {
		if raw, ok := message["usage"].(map[string]any); ok {
			usage = mergeUsageTokens(usage, extractOpenAIUsage(raw))
		}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if raw, ok := response["usage"].(map[string]any); ok {
			usage = mergeUsageTokens(usage, extractOpenAIUsage(raw))
		}
	}
	usage = mergeUsageTokens(usage, extractChoiceUsage(payload))
	usage = mergeUsageTokens(usage, extractTimingUsage(payload))
	if usage.Total == 0 {
		usage.Total = usage.Prompt + usage.Completion
	}
	return usage
}

// extractOpenAIUsage extracts token usage from OpenAI-style response.
// It also accepts OpenAI-compatible usage variants returned by Anthropic,
// Responses API, and common third-party providers.
func extractOpenAIUsage(raw map[string]any) usageTokens {
	prompt := int64(numberAny(raw["prompt_tokens"]))
	promptSource := ""
	if prompt > 0 {
		promptSource = "prompt_tokens"
	}
	completion := int64(numberAny(raw["completion_tokens"]))
	if prompt == 0 {
		prompt = int64(numberAny(raw["input_tokens"]))
		if prompt > 0 {
			promptSource = "input_tokens"
		}
	}
	if completion == 0 {
		completion = int64(numberAny(raw["output_tokens"]))
	}
	total := int64(numberAny(raw["total_tokens"]))
	if total == 0 {
		total = prompt + completion
	}

	var cacheRead int64
	var cacheWrite int64
	var promptIncludesCacheRead bool
	var promptIncludesCacheWrite bool
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		if value := int64(numberAny(details["cached_tokens"])); value > cacheRead {
			cacheRead = value
			promptIncludesCacheRead = promptSource != ""
		}
		if value := int64(numberAny(details["cached_creation_tokens"])); value > cacheWrite {
			cacheWrite = value
			promptIncludesCacheWrite = promptSource != ""
		}
	}
	if details, ok := raw["input_tokens_details"].(map[string]any); ok {
		if value := int64(numberAny(details["cached_tokens"])); value > cacheRead {
			cacheRead = value
			promptIncludesCacheRead = promptSource != ""
		}
		if value := int64(numberAny(details["cached_creation_tokens"])); value > cacheWrite {
			cacheWrite = value
			promptIncludesCacheWrite = promptSource != ""
		}
	}
	if value := int64(numberAny(raw["cached_tokens"])); value > cacheRead {
		cacheRead = value
		promptIncludesCacheRead = promptSource != ""
	}
	if value := int64(numberAny(raw["prompt_cache_hit_tokens"])); value > cacheRead {
		cacheRead = value
		promptIncludesCacheRead = promptSource != ""
	}
	if value := int64(numberAny(raw["cache_read_input_tokens"])); value > cacheRead {
		cacheRead = value
		promptIncludesCacheRead = false
	}
	if value := int64(numberAny(raw["cache_creation_input_tokens"])); value > cacheWrite {
		cacheWrite = value
		promptIncludesCacheWrite = false
	}
	if value := int64(numberAny(raw["cache_creation_tokens"])); value > cacheWrite {
		cacheWrite = value
	}
	if split := int64(numberAny(raw["claude_cache_creation_5_m_tokens"])) + int64(numberAny(raw["claude_cache_creation_1_h_tokens"])); split > cacheWrite {
		cacheWrite = split
		promptIncludesCacheWrite = promptSource != ""
	}
	if creation, ok := raw["cache_creation"].(map[string]any); ok {
		split := int64(numberAny(creation["ephemeral_5m_input_tokens"])) + int64(numberAny(creation["ephemeral_1h_input_tokens"]))
		if split > cacheWrite {
			cacheWrite = split
			promptIncludesCacheWrite = false
		}
	}

	var reasoning int64
	if details, ok := raw["completion_tokens_details"].(map[string]any); ok {
		reasoning = int64(numberAny(details["reasoning_tokens"]))
	}
	if details, ok := raw["output_tokens_details"].(map[string]any); ok {
		if value := int64(numberAny(details["reasoning_tokens"])); value > reasoning {
			reasoning = value
		}
	}
	if value := int64(numberAny(raw["reasoning_tokens"])); value > reasoning {
		reasoning = value
	}

	return usageTokens{
		Prompt:                   prompt,
		Completion:               completion,
		CacheRead:                cacheRead,
		CacheWrite:               cacheWrite,
		Reasoning:                reasoning,
		Total:                    total,
		PromptIncludesCacheRead:  promptIncludesCacheRead,
		PromptIncludesCacheWrite: promptIncludesCacheWrite,
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
		Prompt:                  prompt,
		Completion:              completion,
		CacheRead:               cacheRead,
		Total:                   total,
		PromptIncludesCacheRead: cacheRead > 0,
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

func extractChoiceUsage(payload map[string]any) usageTokens {
	choices, ok := payload["choices"].([]any)
	if !ok {
		return usageTokens{}
	}
	var usage usageTokens
	for _, choice := range choices {
		choiceMap, ok := choice.(map[string]any)
		if !ok {
			continue
		}
		raw, ok := choiceMap["usage"].(map[string]any)
		if !ok {
			continue
		}
		usage = mergeUsageTokens(usage, extractOpenAIUsage(raw))
	}
	return usage
}

func extractTimingUsage(payload map[string]any) usageTokens {
	timings, ok := payload["timings"].(map[string]any)
	if !ok {
		return usageTokens{}
	}
	cacheRead := int64(numberAny(timings["cache_n"]))
	if cacheRead == 0 {
		return usageTokens{}
	}
	return usageTokens{CacheRead: cacheRead}
}

func hasUsageTokens(usage usageTokens) bool {
	return usage.Prompt > 0 ||
		usage.Completion > 0 ||
		usage.CacheRead > 0 ||
		usage.CacheWrite > 0 ||
		usage.Reasoning > 0 ||
		usage.Total > 0
}

func mergeUsageTokens(current usageTokens, next usageTokens) usageTokens {
	if !hasUsageTokens(next) {
		return current
	}
	if next.Prompt > 0 {
		current.Prompt = next.Prompt
		current.PromptIncludesCacheRead = next.PromptIncludesCacheRead
		current.PromptIncludesCacheWrite = next.PromptIncludesCacheWrite
	} else {
		current.PromptIncludesCacheRead = current.PromptIncludesCacheRead || next.PromptIncludesCacheRead
		current.PromptIncludesCacheWrite = current.PromptIncludesCacheWrite || next.PromptIncludesCacheWrite
	}
	if next.Completion > 0 {
		current.Completion = next.Completion
	}
	if next.CacheRead > 0 {
		current.CacheRead = next.CacheRead
	}
	if next.CacheWrite > 0 {
		current.CacheWrite = next.CacheWrite
	}
	if next.Reasoning > 0 {
		current.Reasoning = next.Reasoning
	}
	if next.Total > 0 {
		current.Total = next.Total
	}
	return current
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
