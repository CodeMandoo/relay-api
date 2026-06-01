package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *App) userDashboard(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	now := time.Now()
	monthUsed := a.userTokenUsage(user.ID, monthStart(now))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var todayRequests int64
	var todayTokens int64
	a.db.Model(&UsageLog{}).Where("user_id = ? AND created_at >= ?", user.ID, today).Count(&todayRequests)
	a.db.Model(&UsageLog{}).Where("user_id = ? AND created_at >= ?", user.ID, today).Select("COALESCE(sum(total_tokens), 0)").Scan(&todayTokens)
	var monthRequests int64
	a.db.Model(&UsageLog{}).Where("user_id = ? AND created_at >= ?", user.ID, monthStart(now)).Count(&monthRequests)
	total := user.MonthlyQuota
	remaining := total - monthUsed
	if remaining < 0 {
		remaining = 0
	}
	percentage := 0.0
	if total > 0 {
		percentage = round2(float64(monthUsed) / float64(total) * 100)
	}
	periodEnd := monthStart(now).AddDate(0, 1, 0).Add(-time.Second)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"quota": gin.H{
			"used":               monthUsed,
			"total":              total,
			"remaining":          remaining,
			"percentageUsed":     percentage,
			"billingPeriodStart": monthStart(now).UTC().Format(time.RFC3339),
			"billingPeriodEnd":   periodEnd.UTC().Format(time.RFC3339),
			"todayRequests":      todayRequests,
			"todayTokens":        todayTokens,
			"monthRequests":      monthRequests,
			"monthTokens":        monthUsed,
		},
		"usage": a.usageStats(&user.ID, "week", nil),
	}})
}

func (a *App) userUsage(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	apiKeyID := userAPIKeyFilter(c.Query("apiKeyId"))
	stats := a.usageStats(&user.ID, c.Query("range"), apiKeyID)
	rows := a.usageRows(user.ID, c.Query("range"), c.Query("apiKeyId"))
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"stats": stats, "rows": rows}})
}

func (a *App) userModels(c *gin.Context) {
	var models []ModelConfig
	if err := a.db.Where("status = ?", ModelStatusActive).Order("name asc").Find(&models).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	candidateCounts := map[string]int{}
	for _, model := range models {
		bindings, err := a.modelBindings(model)
		if err != nil || len(bindings) == 0 {
			candidateCounts[model.Name]++
			continue
		}
		candidateCounts[model.Name] += len(bindings)
	}
	sourceMap := map[uint]UpstreamSource{}
	var sources []UpstreamSource
	a.db.Find(&sources)
	for _, source := range sources {
		sourceMap[source.ID] = source
	}
	sourceKeyMap := map[uint]SourceKey{}
	sourceKeyIDs := make([]uint, 0)
	for _, model := range models {
		if model.SourceKeyID != nil {
			sourceKeyIDs = append(sourceKeyIDs, *model.SourceKeyID)
		}
	}
	if len(sourceKeyIDs) > 0 {
		var sourceKeys []SourceKey
		a.db.Where("id IN ?", sourceKeyIDs).Find(&sourceKeys)
		for _, key := range sourceKeys {
			sourceKeyMap[key.ID] = key
		}
	}
	models = currentUserModels(models, sourceMap, sourceKeyMap, time.Now())
	settings, _ := a.getSettings()
	out := make([]gin.H, 0, len(models))
	for _, model := range models {
		source := sourceMap[model.SourceID]
		latency := model.LatencyMS
		if targets, err := a.routeTargets(model.Name, modelTestProtocol(model)); err == nil && len(targets) > 0 {
			source = targets[0].Source
			latency = nonZeroInt(targets[0].Binding.LatencyMS, targets[0].Model.LatencyMS)
		}
		sourceName := source.Name
		if settings.HideUpstreamNameFromUsers {
			sourceName = "平台中转源"
		}
		status := "online"
		if source.Status != SourceStatusOnline {
			status = "offline"
		}
		out = append(out, gin.H{
			"id":                id("m", model.ID),
			"name":              model.Name,
			"provider":          model.Provider,
			"formats":           modelFormatList(model),
			"status":            status,
			"latencyMs":         latency,
			"sourceId":          id("s", source.ID),
			"source":            sourceName,
			"sourceName":        sourceName,
			"sourceType":        source.Type,
			"sourceStatus":      source.Status,
			"routingCandidates": candidateCounts[model.Name],
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func currentUserModels(models []ModelConfig, sources map[uint]UpstreamSource, sourceKeys map[uint]SourceKey, now time.Time) []ModelConfig {
	groups := map[string][]ModelConfig{}
	names := make([]string, 0)
	for _, model := range models {
		if _, ok := groups[model.Name]; !ok {
			names = append(names, model.Name)
		}
		groups[model.Name] = append(groups[model.Name], model)
	}
	sort.Strings(names)
	out := make([]ModelConfig, 0, len(names))
	for _, name := range names {
		group := groups[name]
		sort.SliceStable(group, func(i, j int) bool {
			leftUsable := userModelSchedulable(group[i], sources, sourceKeys, now)
			rightUsable := userModelSchedulable(group[j], sources, sourceKeys, now)
			if leftUsable != rightUsable {
				return leftUsable
			}
			leftSource := sources[group[i].SourceID]
			rightSource := sources[group[j].SourceID]
			leftWeight := group[i].RoutingWeight
			if leftWeight <= 0 {
				leftWeight = 1
			}
			rightWeight := group[j].RoutingWeight
			if rightWeight <= 0 {
				rightWeight = 1
			}
			if leftWeight != rightWeight {
				return leftWeight > rightWeight
			}
			if leftSource.Priority != rightSource.Priority {
				return leftSource.Priority < rightSource.Priority
			}
			return group[i].ID < group[j].ID
		})
		if len(group) > 0 {
			out = append(out, group[0])
		}
	}
	return out
}

func userModelSchedulable(model ModelConfig, sources map[uint]UpstreamSource, sourceKeys map[uint]SourceKey, now time.Time) bool {
	source, ok := sources[model.SourceID]
	if !ok || source.Status != SourceStatusOnline {
		return false
	}
	if source.CooldownUntil != nil && source.CooldownUntil.After(now) {
		return false
	}
	if model.SourceKeyID == nil {
		return true
	}
	key, ok := sourceKeys[*model.SourceKeyID]
	return ok && key.SourceID == model.SourceID && key.Status == APIKeyStatusValid
}

func (a *App) userTestModel(c *gin.Context) {
	modelID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := a.userModelTestTarget(c, modelID)
	if !ok {
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	start := time.Now()
	protocol := modelTestProtocol(target.Model)
	path := "/v1/models"
	if protocol == relayProtocolGemini {
		path = "/v1beta/models"
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstreamURL(target, protocol, path), nil)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "invalid source url")
		return
	}
	applyUpstreamAuth(req.Header, target.Source, effectiveUpstreamAPIKey(target), protocol)
	if protocol == relayProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		_ = a.db.Model(&target.Model).Update("latency_ms", 0).Error
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	_ = a.db.Model(&target.Model).Update("latency_ms", latency).Error
	_ = a.db.Model(&target.Source).Updates(map[string]any{"latency_ms": latency, "status": SourceStatusOnline}).Error
	if target.SourceKey != nil {
		_ = a.db.Model(&SourceKey{}).Where("id = ?", target.SourceKey.ID).Update("last_used_at", time.Now()).Error
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"latencyMs": latency, "statusCode": resp.StatusCode, "online": resp.StatusCode < 500}})
}

func (a *App) userInvokeTestModel(c *gin.Context) {
	modelID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := a.userModelTestTarget(c, modelID)
	if !ok {
		return
	}
	protocol := modelTestProtocol(target.Model)
	path, body, err := modelInvokeTestPayload(protocol, target.Model.Name)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "invalid model test payload")
		return
	}
	timeout := 120 * time.Second
	if settings, err := a.getSettings(); err == nil && settings.DefaultTimeout > 0 {
		timeout = time.Duration(settings.DefaultTimeout) * time.Second
	}
	client := &http.Client{Timeout: timeout}
	start := time.Now()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL(target, protocol, path), bytes.NewReader(body))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, "invalid source url")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(req.Header, target.Source, effectiveUpstreamAPIKey(target), protocol)
	if protocol == relayProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	responseBody, _ := readLimitedBody(resp.Body, 1<<20)
	if resp.StatusCode >= http.StatusBadRequest {
		errorJSON(c, http.StatusBadGateway, upstreamInvokeError(resp.Status, responseBody))
		return
	}
	usage := extractUsage(responseBody)
	_ = a.db.Model(&target.Model).Update("latency_ms", latency).Error
	_ = a.db.Model(&target.Source).Updates(map[string]any{"latency_ms": latency, "status": SourceStatusOnline}).Error
	if target.SourceKey != nil {
		_ = a.db.Model(&SourceKey{}).Where("id = ?", target.SourceKey.ID).Update("last_used_at", time.Now()).Error
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"latencyMs":        latency,
		"statusCode":       resp.StatusCode,
		"ok":               true,
		"promptTokens":     usage.Prompt,
		"completionTokens": usage.Completion,
		"totalTokens":      usage.Total,
	}})
}

func (a *App) userModelTestTarget(c *gin.Context, modelID uint) (routeTarget, bool) {
	var model ModelConfig
	if err := a.db.First(&model, modelID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return routeTarget{}, false
	}
	targets, err := a.routeTargets(model.Name, modelTestProtocol(model))
	if err != nil || len(targets) == 0 {
		errorJSON(c, http.StatusBadGateway, "source is not online")
		return routeTarget{}, false
	}
	return targets[0], true
}

func modelTestProtocol(model ModelConfig) relayProtocol {
	if strings.EqualFold(model.Provider, "Anthropic") && modelHasFormat(model, ModelFormatAnthropic) {
		return relayProtocolAnthropic
	}
	if modelHasFormat(model, ModelFormatOpenAI) {
		return relayProtocolOpenAI
	}
	if modelHasFormat(model, ModelFormatAnthropic) {
		return relayProtocolAnthropic
	}
	if strings.EqualFold(model.Provider, "Google") {
		return relayProtocolGemini
	}
	return relayProtocolOpenAI
}

func modelInvokeTestPayload(protocol relayProtocol, modelName string) (string, []byte, error) {
	var path string
	var payload map[string]any
	switch protocol {
	case relayProtocolAnthropic:
		path = "/v1/messages"
		payload = map[string]any{
			"model":      modelName,
			"max_tokens": 1,
			"messages": []any{
				map[string]any{"role": "user", "content": "Reply with ok."},
			},
		}
	case relayProtocolGemini:
		geminiName := strings.TrimPrefix(modelName, "models/")
		path = "/v1beta/models/" + url.PathEscape(geminiName) + ":generateContent"
		payload = map[string]any{
			"contents": []any{
				map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Reply with ok."}}},
			},
			"generationConfig": map[string]any{
				"maxOutputTokens": 1,
				"temperature":     0,
			},
		}
	default:
		path = "/v1/chat/completions"
		payload = map[string]any{
			"model":       modelName,
			"max_tokens":  1,
			"temperature": 0,
			"messages": []any{
				map[string]any{"role": "user", "content": "Reply with ok."},
			},
		}
	}
	body, err := json.Marshal(payload)
	return path, body, err
}

func upstreamInvokeError(status string, body []byte) string {
	message := strings.TrimSpace(string(body))
	if len(message) > 300 {
		message = message[:300] + "..."
	}
	if message == "" {
		return fmt.Sprintf("upstream model invocation failed: %s", status)
	}
	return fmt.Sprintf("upstream model invocation failed: %s %s", status, message)
}

func (a *App) userAPIKeys(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var keys []APIKey
	if err := a.db.Where("user_id = ?", user.ID).Order("created_at desc").Find(&keys).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]APIKeyDTO, 0, len(keys))
	for _, key := range keys {
		out = append(out, apiKeyDTO(key, false))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) userCreateAPIKey(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Name  string   `json:"name"`
		Limit *float64 `json:"limit"`
	}
	if !bindJSON(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "default"
	}
	secret, err := randomToken("sk-relay-", 32)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "generate api key failed")
		return
	}
	key := APIKey{
		UserID:   user.ID,
		Name:     name,
		Secret:   secret,
		KeyHash:  hashKey(secret),
		Masked:   maskKey(secret),
		Status:   APIKeyStatusValid,
		LimitUSD: req.Limit,
	}
	if err := a.db.Create(&key).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create api key failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": apiKeyDTO(key, true)})
}

func (a *App) userUpdateAPIKey(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	keyID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	if value, ok := req["name"].(string); ok && strings.TrimSpace(value) != "" {
		updates["name"] = strings.TrimSpace(value)
	}
	if value, ok := req["status"].(string); ok && strings.TrimSpace(value) != "" {
		updates["status"] = strings.TrimSpace(value)
	}
	if value, ok := req["enabled"].(bool); ok {
		if value {
			updates["status"] = APIKeyStatusValid
		} else {
			updates["status"] = APIKeyStatusDisabled
		}
	}
	if value, ok := numberFromMap(req, "limit"); ok {
		updates["limit_usd"] = value
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&APIKey{}).Where("id = ? AND user_id = ?", keyID, user.ID).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update api key failed")
		return
	}
	var key APIKey
	if err := a.db.Where("id = ? AND user_id = ?", keyID, user.ID).First(&key).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "api key not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apiKeyDTO(key, false)})
}

func (a *App) userDeleteAPIKey(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	keyID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Where("id = ? AND user_id = ?", keyID, user.ID).Delete(&APIKey{}).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete api key failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) userRevealAPIKey(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	keyID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var key APIKey
	if err := a.db.Where("id = ? AND user_id = ?", keyID, user.ID).First(&key).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "api key not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apiKeyDTO(key, true)})
}

func (a *App) usageRows(userID uint, rawRange string, apiKeyRaw string) []gin.H {
	window := usageWindowForRange(rawRange, time.Now())
	var apiKeyID uint
	if strings.TrimSpace(apiKeyRaw) != "" && apiKeyRaw != "all" {
		parsed, err := parseNumericID(apiKeyRaw)
		if err == nil {
			apiKeyID = parsed
		}
	}
	days := make([]time.Time, 0)
	if window.Range == "week" {
		for i := 0; i < 7; i++ {
			days = append(days, window.Start.AddDate(0, 0, i))
		}
	} else {
		for day := window.Start; !day.After(window.End); day = day.AddDate(0, 0, 1) {
			days = append(days, day)
			if day.Year() == window.End.Year() && day.YearDay() == window.End.YearDay() {
				break
			}
		}
	}
	rows := make([]gin.H, 0, len(days))
	for _, day := range days {
		next := day.AddDate(0, 0, 1)
		q := a.db.Model(&UsageLog{}).Where("user_id = ? AND created_at >= ? AND created_at < ?", userID, day, next)
		if apiKeyID > 0 {
			q = q.Where("api_key_id = ?", apiKeyID)
		}
		var usage struct {
			Requests         int64
			PromptTokens     int64
			CompletionTokens int64
			CacheReadTokens  int64
			CacheWriteTokens int64
			ReasoningTokens  int64
			TotalTokens      int64
			EstimatedCost    float64
		}
		q.Select(`
			count(*) as requests,
			COALESCE(sum(prompt_tokens), 0) as prompt_tokens,
			COALESCE(sum(completion_tokens), 0) as completion_tokens,
			COALESCE(sum(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(sum(cache_write_tokens), 0) as cache_write_tokens,
			COALESCE(sum(reasoning_tokens), 0) as reasoning_tokens,
			COALESCE(sum(total_tokens), 0) as total_tokens,
			COALESCE(sum(estimated_cost), 0) as estimated_cost
		`).Scan(&usage)
		if usage.TotalTokens == 0 {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		rows = append(rows, gin.H{
			"date":             day.Format("2006-01-02"),
			"requests":         usage.Requests,
			"promptTokens":     usage.PromptTokens,
			"completionTokens": usage.CompletionTokens,
			"cacheReadTokens":  usage.CacheReadTokens,
			"cacheWriteTokens": usage.CacheWriteTokens,
			"reasoningTokens":  usage.ReasoningTokens,
			"totalTokens":      usage.TotalTokens,
			"estimatedCost":    round2(usage.EstimatedCost),
		})
	}
	return rows
}

func userAPIKeyFilter(raw string) *uint {
	if strings.TrimSpace(raw) == "" || raw == "all" {
		return nil
	}
	parsed, err := parseNumericID(raw)
	if err != nil {
		return nil
	}
	return &parsed
}
