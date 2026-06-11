package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const cliProxyManagementKeyMissingMessage = "CLIProxyAPI management key is empty; set RELAY_CLIPROXYAPI_MANAGEMENT_KEY to match CLIProxyAPI remote-management.secret-key"

func (a *App) startCLIProxyOAuth(c *gin.Context, source UpstreamSource, provider string) (gin.H, error) {
	endpoint, err := cliProxyOAuthEndpoint(provider)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := a.cliProxyJSON(c.Request.Context(), source, http.MethodGet, endpoint+"?is_webui=1", nil, &payload); err != nil {
		return nil, err
	}
	out := gin.H{}
	for key, value := range payload {
		out[key] = value
	}
	if rawURL, _ := payload["url"].(string); rawURL != "" {
		out["authUrl"] = rawURL
	}
	if state, _ := payload["state"].(string); state != "" {
		out["sessionId"] = state
		out["statusUrl"] = cliProxyManagementBase(source.BaseURL) + "/v0/management/get-auth-status?state=" + url.QueryEscape(state)
	}
	return out, nil
}

func (a *App) submitCLIProxyOAuthCallback(ctx context.Context, source UpstreamSource, provider string, redirectURL string) (bool, error) {
	providerID, err := cliProxyOAuthProviderID(provider)
	if err != nil {
		return false, err
	}
	redirectURL = strings.TrimSpace(redirectURL)
	if redirectURL == "" {
		return false, fmt.Errorf("redirect URL is required")
	}
	payload, err := json.Marshal(map[string]string{
		"provider":     providerID,
		"redirect_url": redirectURL,
	})
	if err != nil {
		return false, err
	}
	if err := a.cliProxyJSON(ctx, source, http.MethodPost, "/v0/management/oauth-callback", strings.NewReader(string(payload)), nil); err != nil {
		return false, err
	}
	state := oauthStateFromRedirectURL(redirectURL)
	if state == "" {
		return false, nil
	}
	return a.waitCLIProxyOAuthStatus(ctx, source, state, 15*time.Second)
}

func (a *App) submitCLIProxyManualToken(ctx context.Context, source UpstreamSource, provider string, identifier string, tokenInput string) error {
	authFileProvider, err := cliProxyManualAuthFileProvider(provider)
	if err != nil {
		return err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return fmt.Errorf("identifier is required")
	}
	token, err := parseManualRefreshToken(tokenInput)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	payload := map[string]any{
		"type":          authFileProvider,
		"email":         identifier,
		"account":       identifier,
		"label":         identifier,
		"auth_kind":     "oauth",
		"refresh_token": token.RefreshToken,
		"expired":       time.Unix(0, 0).UTC().Format(time.RFC3339),
		"last_refresh":  now.Format(time.RFC3339),
	}
	if token.AccessToken != "" {
		payload["access_token"] = token.AccessToken
	}
	if token.IDToken != "" {
		payload["id_token"] = token.IDToken
	}
	name := manualAuthFileName(authFileProvider, identifier, now)
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := "/v0/management/auth-files?name=" + url.QueryEscape(name)
	return a.cliProxyJSON(ctx, source, http.MethodPost, endpoint, strings.NewReader(string(raw)), nil)
}

func (a *App) deleteCLIProxyAuthFile(ctx context.Context, source UpstreamSource, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("auth file name is required")
	}
	endpoint := "/v0/management/auth-files?name=" + url.QueryEscape(name)
	return a.cliProxyJSON(ctx, source, http.MethodDelete, endpoint, nil, nil)
}

type manualTokenPayload struct {
	RefreshToken string
	AccessToken  string
	IDToken      string
}

func parseManualRefreshToken(raw string) (manualTokenPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return manualTokenPayload{}, fmt.Errorf("refresh_token is required")
	}
	if strings.HasPrefix(raw, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return manualTokenPayload{}, fmt.Errorf("token JSON is invalid")
		}
		token := manualTokenPayload{
			RefreshToken: firstString(payload, "refresh_token", "refreshToken"),
			AccessToken:  firstString(payload, "access_token", "accessToken"),
			IDToken:      firstString(payload, "id_token", "idToken"),
		}
		if nested, ok := payload["token"].(map[string]any); ok {
			if token.RefreshToken == "" {
				token.RefreshToken = firstString(nested, "refresh_token", "refreshToken")
			}
			if token.AccessToken == "" {
				token.AccessToken = firstString(nested, "access_token", "accessToken")
			}
			if token.IDToken == "" {
				token.IDToken = firstString(nested, "id_token", "idToken")
			}
		}
		if token.RefreshToken == "" {
			return manualTokenPayload{}, fmt.Errorf("token JSON must contain refresh_token")
		}
		return token, nil
	}
	return manualTokenPayload{RefreshToken: raw}, nil
}

func manualAuthFileName(provider string, identifier string, now time.Time) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, strings.ToLower(strings.TrimSpace(identifier)))
	clean = strings.Trim(clean, "-_.")
	if clean == "" {
		clean = "account"
	}
	if len(clean) > 48 {
		clean = strings.Trim(clean[:48], "-_.")
	}
	return fmt.Sprintf("relay-%s-%s-%d.json", strings.ToLower(provider), clean, now.Unix())
}

func (a *App) waitCLIProxyOAuthStatus(ctx context.Context, source UpstreamSource, state string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var payload map[string]any
		endpoint := "/v0/management/get-auth-status?state=" + url.QueryEscape(state)
		if err := a.cliProxyJSON(ctx, source, http.MethodGet, endpoint, nil, &payload); err != nil {
			return false, err
		}
		status := strings.ToLower(firstString(payload, "status"))
		switch status {
		case "", "ok":
			return true, nil
		case "error":
			message := firstString(payload, "error")
			if message == "" {
				message = "OAuth callback failed"
			}
			return false, fmt.Errorf("%s", message)
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func oauthStateFromRedirectURL(redirectURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(redirectURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("state"))
}

func (a *App) syncCLIProxyAccounts(c *gin.Context, source UpstreamSource) ([]SourceAccount, error) {
	var payload struct {
		Files []map[string]any `json:"files"`
	}
	if err := a.cliProxyJSON(c.Request.Context(), source, http.MethodGet, "/v0/management/auth-files", nil, &payload); err != nil {
		return nil, err
	}
	now := time.Now()
	accounts := make([]SourceAccount, 0, len(payload.Files))
	for _, file := range payload.Files {
		account := cliProxyFileToAccount(source.ID, file, now)
		if strings.TrimSpace(account.Identifier) == "" {
			continue
		}
		existing, err := a.findExistingSourceAccount(account)
		if err == nil {
			account.ID = existing.ID
			account.CreatedAt = existing.CreatedAt
			if account.Balance == 0 {
				account.Balance = existing.Balance
			}
			if account.BalanceLimit == 0 {
				account.BalanceLimit = existing.BalanceLimit
			}
			if err := a.db.Model(&existing).Updates(account).Error; err != nil {
				return nil, err
			}
			account.ID = existing.ID
		} else if err := a.db.Create(&account).Error; err != nil {
			return nil, err
		}
		_ = a.refreshCodexUsageForAccount(c.Request.Context(), source, &account, file)
		accounts = append(accounts, account)
	}
	if err := a.recountSourceAccounts(source.ID); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (a *App) findExistingSourceAccount(account SourceAccount) (SourceAccount, error) {
	var existing SourceAccount
	if strings.TrimSpace(account.AuthFileName) != "" {
		err := a.db.Where("source_id = ? AND auth_file_name = ?", account.SourceID, account.AuthFileName).First(&existing).Error
		if err == nil {
			return existing, nil
		}
	}
	if strings.TrimSpace(account.AuthIndex) != "" {
		err := a.db.Where("source_id = ? AND auth_index = ?", account.SourceID, account.AuthIndex).First(&existing).Error
		if err == nil {
			return existing, nil
		}
	}
	err := a.db.Where("source_id = ? AND identifier = ? AND provider = ?", account.SourceID, account.Identifier, account.Provider).First(&existing).Error
	if err == nil {
		return existing, nil
	}
	if strings.TrimSpace(account.Identifier) != "" {
		err = a.db.Where("source_id = ? AND identifier = ?", account.SourceID, account.Identifier).First(&existing).Error
	}
	return existing, err
}

func (a *App) cliProxyJSON(ctx context.Context, source UpstreamSource, method string, endpoint string, body io.Reader, target any) error {
	managementKey := strings.TrimSpace(source.ManagementKey)
	if managementKey == "" {
		return fmt.Errorf(cliProxyManagementKeyMissingMessage)
	}
	base := cliProxyManagementBase(source.BaseURL)
	if base == "" {
		return fmt.Errorf("CLIProxyAPI base URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := cliProxyErrorMessage(raw)
		if strings.EqualFold(message, "unknown or expired state") {
			return fmt.Errorf("OAuth authorization session expired; start authorization again and submit the new callback URL")
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("CLIProxyAPI management API is unavailable (404); set remote-management.secret-key in CLIProxyAPI config.yaml and restart CLIProxyAPI")
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("CLIProxyAPI management key is invalid: %s", resp.Status)
		}
		if message != "" {
			return fmt.Errorf("CLIProxyAPI management request failed: %s", message)
		}
		return fmt.Errorf("CLIProxyAPI management request failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode CLIProxyAPI response: %w", err)
	}
	return nil
}

func cliProxyErrorMessage(raw []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if message := firstString(payload, "error", "message"); message != "" {
			return message
		}
	}
	return strings.TrimSpace(string(raw))
}

func cliProxyManagementBase(baseURL string) string {
	base := normalizeBaseURL(baseURL)
	for _, suffix := range []string{"/v1", "/v1beta", "/api/provider/openai/v1", "/api/provider/anthropic/v1", "/api/provider/google/v1beta"} {
		if strings.HasSuffix(strings.ToLower(base), suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return base
}

func cliProxyOAuthEndpoint(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex", "openai":
		return "/v0/management/codex-auth-url", nil
	case "claude":
		return "/v0/management/anthropic-auth-url", nil
	case "gemini":
		return "/v0/management/gemini-cli-auth-url", nil
	case "grok":
		return "/v0/management/xai-auth-url", nil
	case "antigravity":
		return "/v0/management/antigravity-auth-url", nil
	default:
		return "", fmt.Errorf("unsupported CLIProxyAPI OAuth provider: %s", provider)
	}
}

func cliProxyOAuthProviderID(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex", "openai":
		return "codex", nil
	case "claude", "anthropic":
		return "anthropic", nil
	case "gemini", "google":
		return "gemini", nil
	case "grok", "xai", "x-ai", "x.ai":
		return "xai", nil
	case "antigravity", "anti-gravity":
		return "antigravity", nil
	default:
		return "", fmt.Errorf("unsupported CLIProxyAPI OAuth provider: %s", provider)
	}
}

func cliProxyManualAuthFileProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex", "openai":
		return "codex", nil
	case "claude", "anthropic":
		return "claude", nil
	default:
		return "", fmt.Errorf("manual token login currently supports ChatGPT and Claude only")
	}
}

func cliProxyFileToAccount(sourceID uint, file map[string]any, now time.Time) SourceAccount {
	provider := firstString(file, "provider", "type")
	if provider == "" {
		provider = "unknown"
	}
	authFileName := firstString(file, "name")
	identifier := firstString(file, "email", "account", "label", "name", "id", "auth_index")
	lastRefresh := firstTime(file, now, "last_refresh", "lastRefresh", "updated_at", "modtime", "created_at")
	chatgptAccountID, workspaceID := cliProxyAccountScope(file)
	planType, subscriptionPlan := cliProxyAccountPlans(file)
	status := "valid"
	if disabled, _ := file["disabled"].(bool); disabled {
		status = "expired"
	}
	if rawStatus := strings.ToLower(firstString(file, "status")); rawStatus != "" {
		switch {
		case strings.Contains(rawStatus, "disabled"), strings.Contains(rawStatus, "expired"):
			status = "expired"
		case strings.Contains(rawStatus, "cooldown"), strings.Contains(rawStatus, "unavailable"), strings.Contains(rawStatus, "retry"):
			status = "cooldown"
		}
	}
	return SourceAccount{
		SourceID:         sourceID,
		Identifier:       identifier,
		Provider:         cliProxyProviderLabel(provider),
		AuthFileName:     authFileName,
		AuthIndex:        firstString(file, "auth_index", "authIndex"),
		ChatGPTAccountID: chatgptAccountID,
		WorkspaceID:      workspaceID,
		PlanType:         planType,
		SubscriptionPlan: subscriptionPlan,
		HasSubscription:  isPaidPlanType(subscriptionPlan) || isPaidPlanType(planType),
		Status:           status,
		Balance:          numberFromKeys(file, "balance", "remaining", "available"),
		BalanceLimit:     numberFromKeys(file, "balance_limit", "balanceLimit", "limit", "quota_limit", "quotaLimit"),
		Used5h:           int64(numberFromKeys(file, "used5h", "used_5h", "used_5_hour", "used5Hour")),
		Limit5h:          int64(numberFromKeys(file, "limit5h", "limit_5h", "limit_5_hour", "limit5Hour")),
		Used7d:           int64(numberFromKeys(file, "used7d", "used_7d", "used_7_day", "used7Day")),
		Limit7d:          int64(numberFromKeys(file, "limit7d", "limit_7d", "limit_7_day", "limit7Day")),
		SuccessCount:     int64(numberFromKeys(file, "success", "success_count", "successCount")),
		FailedCount:      int64(numberFromKeys(file, "failed", "failed_count", "failedCount")),
		RecentRequests:   recentRequestCount(file["recent_requests"]),
		LastRefreshed:    lastRefresh,
	}
}

func cliProxyAccountScope(file map[string]any) (string, string) {
	chatgptAccountID := firstString(file, "chatgpt_account_id", "chatgptAccountId")
	workspaceID := firstString(file, "workspace_id", "workspaceId")
	if claims, ok := file["id_token"].(map[string]any); ok {
		if chatgptAccountID == "" {
			chatgptAccountID = firstString(claims, "chatgpt_account_id", "chatgptAccountId")
		}
		if workspaceID == "" {
			workspaceID = firstString(claims, "workspace_id", "workspaceId", "chatgpt_account_id", "chatgptAccountId")
		}
	}
	if workspaceID == "" {
		workspaceID = chatgptAccountID
	}
	return normalizeScopedIdentity(chatgptAccountID, "cgpt="), normalizeScopedIdentity(workspaceID, "ws=")
}

func cliProxyAccountPlans(file map[string]any) (string, string) {
	planType := normalizePlanType(firstString(file, "plan_type", "planType"))
	subscriptionPlan := normalizePlanType(firstString(file, "subscription_plan", "subscriptionPlan", "plan_type", "planType"))
	if claims, ok := file["id_token"].(map[string]any); ok {
		if planType == "" {
			planType = normalizePlanType(firstString(claims, "chatgpt_plan_type", "chatgptPlanType", "plan_type", "planType"))
		}
		if auth, ok := asMap(claims["https://api.openai.com/auth"]); ok && planType == "" {
			planType = normalizePlanType(firstString(auth, "chatgpt_plan_type", "chatgptPlanType", "plan_type", "planType"))
		}
	}
	if subscriptionPlan == "" {
		subscriptionPlan = planType
	}
	return planType, subscriptionPlan
}

func cliProxyProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex", "openai", "chatgpt":
		return "ChatGPT"
	case "anthropic", "claude":
		return "Claude"
	case "gemini", "google", "gemini-cli":
		return "Gemini"
	case "xai", "grok":
		return "Grok"
	default:
		if strings.TrimSpace(provider) == "" {
			return "unknown"
		}
		return provider
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value, ok := m[key].(float64); ok && value > 0 {
			return fmt.Sprintf("%.0f", value)
		}
	}
	return ""
}

func numberFromKeys(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if number := numberAnyFlexible(value); number != 0 {
				return number
			}
		}
	}
	return 0
}

func numberAnyFlexible(value any) float64 {
	switch v := value.(type) {
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return parsed
		}
	}
	return numberAny(value)
}

func recentRequestCount(value any) int64 {
	rows, ok := value.([]any)
	if !ok {
		return 0
	}
	var total int64
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		total += int64(numberFromKeys(m, "success", "success_count", "successCount"))
		total += int64(numberFromKeys(m, "failed", "failed_count", "failedCount"))
	}
	return total
}

func firstTime(m map[string]any, fallback time.Time, keys ...string) time.Time {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
				if parsed, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
					return parsed
				}
			}
		case float64:
			if v > 0 {
				return time.Unix(int64(v), 0)
			}
		}
	}
	return fallback
}
