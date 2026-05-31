package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func errorJSON(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func parseNumericID(raw string) (uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("missing id")
	}
	if idx := strings.LastIndex(raw, "_"); idx >= 0 {
		raw = raw[idx+1:]
	}
	raw = strings.TrimLeft(raw, "0")
	if raw == "" {
		raw = "0"
	}
	id64, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id64 == 0 {
		return 0, errors.New("invalid id")
	}
	return uint(id64), nil
}

func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		errorJSON(c, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func randomToken(prefix string, bytesLen int) (string, error) {
	bytes := make([]byte, bytesLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func randomInviteCode() (string, error) {
	token, err := randomToken("", 8)
	if err != nil {
		return "", err
	}
	token = strings.ToUpper(strings.ReplaceAll(token, "_", "X"))
	if len(token) > 12 {
		token = token[:12]
	}
	return token[:4] + "-" + token[4:8] + "-" + token[8:], nil
}

func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = 2 << 20
	}
	return io.ReadAll(io.LimitReader(body, limit))
}

func writeRawJSON(c *gin.Context, status int, payload []byte) {
	c.Data(status, "application/json; charset=utf-8", payload)
}

func jsonString(value any) string {
	if value == nil {
		return ""
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(bytes)
}

func mustJSONMap(body []byte) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func getString(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func getBool(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func requestPathWithQuery(c *gin.Context, path string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return path
	}
	query := c.Request.URL.Query()
	query.Del("key")
	if encoded := query.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

func prefixPathPreservingQuery(prefix, path string) string {
	pathOnly, query, hasQuery := strings.Cut(path, "?")
	prefix = "/" + strings.Trim(prefix, "/")
	pathOnly = "/" + strings.TrimLeft(pathOnly, "/")
	if strings.HasPrefix(pathOnly, prefix+"/") || pathOnly == prefix {
		if hasQuery {
			return pathOnly + "?" + query
		}
		return pathOnly
	}
	out := prefix + pathOnly
	if hasQuery {
		out += "?" + query
	}
	return out
}

func geminiModelFromPath(path string) string {
	path = strings.TrimSpace(path)
	const marker = "/models/"
	index := strings.Index(path, marker)
	if index < 0 {
		return ""
	}
	model := path[index+len(marker):]
	if cut := strings.IndexAny(model, ":/?#"); cut >= 0 {
		model = model[:cut]
	}
	return strings.TrimSpace(strings.TrimPrefix(model, "models/"))
}

func monthStart(now time.Time) time.Time {
	year, month, _ := now.Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
}

func weekStart(now time.Time) time.Time {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	start := now.AddDate(0, 0, -(weekday - 1))
	y, m, d := start.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func nonZeroFloat(value float64, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func nonZeroInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func finalBillingPrice(price float64, multiple float64) float64 {
	return price * nonZeroFloat(multiple, 1)
}

func estimateCost(totalTokens int64, inputRate, outputRate float64) float64 {
	rate := (inputRate + outputRate) / 2
	if rate <= 0 {
		rate = 1
	}
	return round2(float64(totalTokens) / 1_000_000 * rate * 12)
}

// estimateCostDetailed calculates cost using per-token-type billing rates.
// Input tokens that are cache hits are billed at the cache read rate instead of the full input rate.
// Reasoning tokens are billed at the output rate (same as tokscale).
// Returns 0 if all billing rates are zero (free model).
func estimateCostDetailed(usage usageTokens, model ModelConfig) float64 {
	inputRate := model.BillingInput
	outputRate := model.BillingOutput
	cacheReadRate := model.BillingCacheRead
	cacheWriteRate := model.BillingCacheWrite

	// If all billing rates are zero, apply the legacy fallback
	if inputRate <= 0 && outputRate <= 0 && cacheReadRate <= 0 && cacheWriteRate <= 0 {
		return estimateCost(usage.Total, inputRate, outputRate)
	}

	// Non-cache input tokens = total input - cache read tokens
	nonCacheInput := usage.Prompt - usage.CacheRead
	if nonCacheInput < 0 {
		nonCacheInput = 0
	}

	// Output tokens include reasoning tokens (billed at output rate)
	outputTotal := usage.Completion + usage.Reasoning

	cost := float64(nonCacheInput) / 1_000_000 * inputRate
	cost += float64(outputTotal) / 1_000_000 * outputRate
	cost += float64(usage.CacheRead) / 1_000_000 * cacheReadRate
	cost += float64(usage.CacheWrite) / 1_000_000 * cacheWriteRate

	return round2(cost)
}

func normalizeBaseURL(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/")
}

const (
	SourceTypeCLIProxyAPI = "CLIProxyAPI"
	SourceTypeThirdParty  = "Third-party Provider"
	ModelFormatOpenAI     = "openai"
	ModelFormatAnthropic  = "anthropic"
)

func normalizeSourceType(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, SourceTypeCLIProxyAPI) {
		return SourceTypeCLIProxyAPI
	}
	if strings.EqualFold(value, SourceTypeThirdParty) {
		return SourceTypeThirdParty
	}
	return ""
}

func normalizeSourceBaseURL(base string) string {
	base = normalizeBaseURL(base)
	lower := strings.ToLower(base)
	for _, suffix := range []string{"/api/provider/openai/v1", "/api/provider/anthropic/v1", "/api/provider/google/v1beta", "/v1beta", "/v1"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimRight(base[:len(base)-len(suffix)], "/")
		}
	}
	return base
}

func joinUpstreamPath(base, path string) string {
	base = normalizeBaseURL(base)
	path = "/" + strings.TrimLeft(path, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		return base + strings.TrimPrefix(path, "/v1")
	}
	if strings.HasSuffix(base, "/v1beta") && strings.HasPrefix(path, "/v1beta/") {
		return base + strings.TrimPrefix(path, "/v1beta")
	}
	return base + path
}

func normalizeModelFormats(input []string, provider string) string {
	seen := map[string]bool{}
	formats := make([]string, 0, 2)
	for _, raw := range input {
		format := strings.ToLower(strings.TrimSpace(raw))
		if format != ModelFormatOpenAI && format != ModelFormatAnthropic {
			continue
		}
		if !seen[format] {
			seen[format] = true
			formats = append(formats, format)
		}
	}
	if len(formats) == 0 {
		formats = inferredModelFormats(provider)
	}
	return strings.Join(formats, ",")
}

func modelFormatList(model ModelConfig) []string {
	return parseModelFormats(model.Formats)
}

func parseModelFormats(value string) []string {
	seen := map[string]bool{}
	formats := make([]string, 0, 2)
	for _, raw := range strings.Split(value, ",") {
		format := strings.ToLower(strings.TrimSpace(raw))
		if format != ModelFormatOpenAI && format != ModelFormatAnthropic {
			continue
		}
		if !seen[format] {
			seen[format] = true
			formats = append(formats, format)
		}
	}
	return formats
}

func inferredModelFormats(provider string) []string {
	if strings.EqualFold(strings.TrimSpace(provider), "Anthropic") {
		return []string{ModelFormatAnthropic}
	}
	return []string{ModelFormatOpenAI}
}

func modelHasFormat(model ModelConfig, format string) bool {
	for _, item := range modelFormatList(model) {
		if item == format {
			return true
		}
	}
	return false
}

func modelSupportsRelayProtocol(model ModelConfig, protocol relayProtocol) bool {
	switch protocol {
	case relayProtocolAnthropic:
		return modelHasFormat(model, ModelFormatAnthropic)
	case relayProtocolOpenAI:
		return modelHasFormat(model, ModelFormatOpenAI)
	case relayProtocolGemini:
		return strings.EqualFold(strings.TrimSpace(model.Provider), "Google")
	default:
		return true
	}
}
