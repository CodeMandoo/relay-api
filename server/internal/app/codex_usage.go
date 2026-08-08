package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const codexUsageDefaultBaseURL = "https://chatgpt.com"

type codexUsageSnapshot struct {
	PrimaryUsedPercent   *float64
	PrimaryResetAt       *time.Time
	SecondaryUsedPercent *float64
	SecondaryResetAt     *time.Time
}

type codexSubscriptionSnapshot struct {
	HasSubscription  bool
	AccountPlanType  string
	SubscriptionPlan string
	ExpiresAt        *time.Time
	RenewsAt         *time.Time
}

func (s codexSubscriptionSnapshot) HasData() bool {
	return s.AccountPlanType != "" || s.SubscriptionPlan != "" || s.ExpiresAt != nil || s.RenewsAt != nil
}

func (a *App) refreshCodexUsageForAccount(ctx context.Context, source UpstreamSource, account *SourceAccount, listed map[string]any) error {
	if account == nil || !isCodexPlatform(account.Provider) {
		return nil
	}

	tokenPayload := listed
	if tokenStringFromPayload(tokenPayload, "access_token") == "" {
		downloaded, err := a.downloadCLIProxyAuthFile(ctx, source, account.AuthFileName)
		if err != nil {
			return err
		}
		if downloaded != nil {
			tokenPayload = downloaded
		}
	}

	accessToken := tokenStringFromPayload(tokenPayload, "access_token")
	if accessToken == "" {
		return nil
	}

	chatgptAccountID, workspaceID := codexAccountScopeFromPayload(tokenPayload)
	if account.ChatGPTAccountID != "" {
		chatgptAccountID = account.ChatGPTAccountID
	}
	if account.WorkspaceID != "" {
		workspaceID = account.WorkspaceID
	}
	if workspaceID == "" {
		workspaceID = chatgptAccountID
	}
	account.PlanType = choosePlanType(account.PlanType, extractPlanTypeFromPayload(tokenPayload))
	account.SubscriptionPlan = choosePlanType(account.SubscriptionPlan)
	account.HasSubscription = isPaidPlanType(account.PlanType) || isPaidPlanType(account.SubscriptionPlan)
	if account.PlanType == "" {
		account.PlanType = extractPlanTypeFromPayload(tokenPayload)
	}
	if account.SubscriptionPlan == "" {
		account.SubscriptionPlan = account.PlanType
	}
	account.HasSubscription = account.HasSubscription || isPaidPlanType(account.PlanType) || isPaidPlanType(account.SubscriptionPlan)

	if subscription, err := fetchCodexSubscriptionSnapshot(ctx, codexUsageBaseURL(), accessToken, firstNonEmpty(chatgptAccountID, workspaceID)); err == nil && subscription.HasData() {
		applyCodexSubscriptionSnapshot(account, subscription)
	}

	snapshot, err := fetchCodexUsageSnapshot(ctx, codexUsageBaseURL(), accessToken, workspaceID)
	if err != nil {
		return err
	}
	now := time.Now()
	applyCodexUsageSnapshot(account, snapshot)
	account.ChatGPTAccountID = chatgptAccountID
	account.WorkspaceID = workspaceID
	account.LastRefreshed = now

	return a.db.Model(account).Select(
		"ChatGPTAccountID",
		"WorkspaceID",
		"PlanType",
		"SubscriptionPlan",
		"HasSubscription",
		"SubscriptionExpiresAt",
		"SubscriptionRenewsAt",
		"Used5h",
		"Limit5h",
		"Used7d",
		"Limit7d",
		"NextRefresh5h",
		"NextRefresh7d",
		"LastRefreshed",
	).Updates(account).Error
}

func (a *App) downloadCLIProxyAuthFile(ctx context.Context, source UpstreamSource, name string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(strings.ToLower(name), ".json") {
		return nil, nil
	}
	managementKey := strings.TrimSpace(source.ManagementKey)
	if managementKey == "" {
		return nil, fmt.Errorf(cliProxyManagementKeyMissingMessage)
	}
	base := cliProxyManagementBase(source.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("CLIProxyAPI base URL is empty")
	}
	endpoint := base + "/v0/management/auth-files/download?name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("CLIProxyAPI management key is invalid: %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CLIProxyAPI auth file download failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode CLIProxyAPI auth file: %w", err)
	}
	return payload, nil
}

func codexUsageBaseURL() string {
	return env("RELAY_CODEX_USAGE_BASE_URL", codexUsageDefaultBaseURL)
}

func codexUsageEndpoint(baseURL string) string {
	base := normalizeBaseURL(baseURL)
	if base == "" {
		base = codexUsageDefaultBaseURL
	}
	lower := strings.ToLower(base)
	if (lower == "https://chatgpt.com" || lower == "https://chat.openai.com") && !strings.Contains(lower, "/backend-api") {
		base += "/backend-api"
		lower = strings.ToLower(base)
	}
	if strings.Contains(lower, "/backend-api") {
		return base + "/wham/usage"
	}
	return base + "/api/codex/usage"
}

func codexAccountsCheckEndpoint(baseURL string) string {
	base := normalizeBaseURL(baseURL)
	if base == "" {
		base = codexUsageDefaultBaseURL
	}
	lower := strings.ToLower(base)
	if (lower == "https://chatgpt.com" || lower == "https://chat.openai.com") && !strings.Contains(lower, "/backend-api") {
		base += "/backend-api"
	}
	return base + "/accounts/check/v4-2023-04-27"
}

func fetchCodexUsageSnapshot(ctx context.Context, baseURL, bearer, workspaceID string) (codexUsageSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageEndpoint(baseURL), nil)
	if err != nil {
		return codexUsageSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
		req.Header.Set("ChatGPT-Account-ID", workspaceID)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return codexUsageSnapshot{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexUsageSnapshot{}, fmt.Errorf("codex usage endpoint failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return codexUsageSnapshot{}, fmt.Errorf("decode codex usage response: %w", err)
	}
	return parseCodexUsageSnapshot(payload), nil
}

func parseCodexUsageSnapshot(payload map[string]any) codexUsageSnapshot {
	rateLimit, _ := asMap(payload["rate_limit"])
	primary, _ := asMap(rateLimit["primary_window"])
	secondary, _ := asMap(rateLimit["secondary_window"])
	return codexUsageSnapshot{
		PrimaryUsedPercent:   jsonNumberPtr(primary, "used_percent"),
		PrimaryResetAt:       unixTimePtr(primary, "reset_at"),
		SecondaryUsedPercent: jsonNumberPtr(secondary, "used_percent"),
		SecondaryResetAt:     unixTimePtr(secondary, "reset_at"),
	}
}

func fetchCodexSubscriptionSnapshot(ctx context.Context, baseURL, bearer, accountID string) (codexSubscriptionSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexAccountsCheckEndpoint(baseURL), nil)
	if err != nil {
		return codexSubscriptionSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Origin", "https://chatgpt.com")
	req.Header.Set("Referer", "https://chatgpt.com/")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return codexSubscriptionSnapshot{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return codexSubscriptionSnapshot{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexSubscriptionSnapshot{}, fmt.Errorf("codex accounts/check endpoint failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return codexSubscriptionSnapshot{}, fmt.Errorf("decode codex accounts/check response: %w", err)
	}
	accounts, _ := asMap(payload["accounts"])
	return selectCodexSubscriptionSnapshot(accounts, accountID), nil
}

func selectCodexSubscriptionSnapshot(accounts map[string]any, accountID string) codexSubscriptionSnapshot {
	if len(accounts) == 0 {
		return codexSubscriptionSnapshot{}
	}
	if accountID = strings.TrimSpace(accountID); accountID != "" {
		if entry, ok := asMap(accounts[accountID]); ok {
			return parseCodexSubscriptionSnapshot(entry)
		}
	}
	var first *codexSubscriptionSnapshot
	var defaultAccount *codexSubscriptionSnapshot
	var paid *codexSubscriptionSnapshot
	for _, raw := range accounts {
		entry, ok := asMap(raw)
		if !ok {
			continue
		}
		snapshot := parseCodexSubscriptionSnapshot(entry)
		if first == nil {
			copy := snapshot
			first = &copy
		}
		if defaultAccount == nil {
			if accountMap, ok := asMap(entry["account"]); ok {
				if isDefault, _ := accountMap["is_default"].(bool); isDefault {
					copy := snapshot
					defaultAccount = &copy
				}
			}
		}
		if paid == nil && isPaidPlanType(snapshot.AccountPlanType) {
			copy := snapshot
			paid = &copy
		}
	}
	for _, candidate := range []*codexSubscriptionSnapshot{defaultAccount, paid, first} {
		if candidate != nil {
			return *candidate
		}
	}
	return codexSubscriptionSnapshot{}
}

func parseCodexSubscriptionSnapshot(entry map[string]any) codexSubscriptionSnapshot {
	account, _ := asMap(entry["account"])
	entitlement, _ := asMap(entry["entitlement"])
	accountPlanType := choosePlanType(
		firstString(account, "plan_type", "planType", "account_plan_type", "accountPlanType"),
		firstPlanStringRecursive(account),
		firstString(entitlement, "subscription_plan", "subscriptionPlan"),
		firstPlanStringRecursive(entitlement),
	)
	subscriptionPlan := choosePlanType(
		firstString(entitlement, "subscription_plan", "subscriptionPlan"),
		firstPlanStringRecursive(entitlement),
		firstString(account, "plan_type", "planType", "account_plan_type", "accountPlanType"),
		firstPlanStringRecursive(account),
	)
	expiresAt := parseSubscriptionTimestamp(firstString(entitlement, "expires_at", "expiresAt"))
	renewsAt := parseSubscriptionTimestamp(firstNonEmpty(
		firstString(entitlement, "renews_at", "renewsAt"),
		firstString(entitlement, "next_renewal_at", "nextRenewalAt"),
		firstString(entitlement, "next_credit_grant_update", "nextCreditGrantUpdate"),
		firstString(entitlement, "renewal_date", "renewalDate"),
	))
	if renewsAt == nil {
		if willRenew, _ := entitlement["will_renew"].(bool); willRenew {
			renewsAt = expiresAt
		}
	}
	hasSubscription, ok := boolFromAny(entitlement["has_active_subscription"])
	if !ok {
		hasSubscription, ok = boolFromAny(account["has_subscription"])
	}
	if !ok {
		hasSubscription, ok = boolFromAny(account["has_active_subscription"])
	}
	if !ok {
		hasSubscription, ok = boolFromAny(account["is_paid_subscription_active"])
	}
	if !ok {
		hasSubscription = isPaidPlanType(accountPlanType) ||
			isPaidPlanType(subscriptionPlan) ||
			expiresAt != nil || renewsAt != nil
	}
	return codexSubscriptionSnapshot{
		HasSubscription:  hasSubscription,
		AccountPlanType:  accountPlanType,
		SubscriptionPlan: subscriptionPlan,
		ExpiresAt:        expiresAt,
		RenewsAt:         renewsAt,
	}
}

func applyCodexSubscriptionSnapshot(account *SourceAccount, snapshot codexSubscriptionSnapshot) {
	if snapshot.AccountPlanType != "" {
		account.PlanType = snapshot.AccountPlanType
	}
	if snapshot.SubscriptionPlan != "" {
		account.SubscriptionPlan = snapshot.SubscriptionPlan
	}
	account.HasSubscription = snapshot.HasSubscription
	account.SubscriptionExpiresAt = snapshot.ExpiresAt
	account.SubscriptionRenewsAt = snapshot.RenewsAt
}

func applyCodexUsageSnapshot(account *SourceAccount, snapshot codexUsageSnapshot) {
	if snapshot.PrimaryUsedPercent != nil {
		account.Used5h = percentToInt(*snapshot.PrimaryUsedPercent)
		account.Limit5h = 100
		account.NextRefresh5h = snapshot.PrimaryResetAt
	}
	if snapshot.SecondaryUsedPercent != nil {
		account.Used7d = percentToInt(*snapshot.SecondaryUsedPercent)
		account.Limit7d = 100
		account.NextRefresh7d = snapshot.SecondaryResetAt
	}
}

func extractPlanTypeFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if value := choosePlanType(
		firstString(payload, "account_plan_type", "accountPlanType", "plan_type", "planType", "subscription_plan", "subscriptionPlan"),
		firstPlanStringRecursive(payload),
	); value != "" {
		return value
	}
	for _, token := range []string{tokenStringFromPayload(payload, "access_token"), tokenStringFromPayload(payload, "id_token")} {
		if value := extractPlanTypeFromJWT(token); value != "" {
			return value
		}
	}
	return ""
}

func extractPlanTypeFromJWT(token string) string {
	payload := jwtPayload(token)
	if payload == nil {
		return ""
	}
	if value := choosePlanType(
		firstString(payload, "chatgpt_plan_type", "chatgptPlanType", "plan_type", "planType"),
		firstPlanStringRecursive(payload),
	); value != "" {
		return value
	}
	if auth, ok := asMap(payload["https://api.openai.com/auth"]); ok {
		if value := choosePlanType(
			firstString(auth, "chatgpt_plan_type", "chatgptPlanType", "plan_type", "planType"),
			firstPlanStringRecursive(auth),
		); value != "" {
			return value
		}
	}
	return ""
}

func choosePlanType(candidates ...string) string {
	selected := ""
	for _, candidate := range candidates {
		next := normalizePlanType(candidate)
		if next == "" {
			continue
		}
		if selected == "" {
			selected = next
			continue
		}
		if selected == "pro" && isDetailedProPlan(next) {
			selected = next
		}
		if selected == "pro_20x" && next == "pro_5x" {
			selected = next
		}
	}
	return selected
}

func firstPlanStringRecursive(value any) string {
	return firstPlanStringRecursiveSeen(value, 0)
}

func firstPlanStringRecursiveSeen(value any, depth int) string {
	if depth > 8 {
		return ""
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{
			"account_plan_type", "accountPlanType",
			"subscription_plan", "subscriptionPlan",
			"plan_type", "planType",
			"chatgpt_plan_type", "chatgptPlanType",
			"subscription_tier", "subscriptionTier",
			"plan", "tier", "sku",
			"plan_name", "planName",
			"subscription_name", "subscriptionName",
			"product_name", "productName",
			"display_name", "displayName",
			"title",
		} {
			if text := firstString(typed, key); text != "" && normalizePlanType(text) != "" {
				return text
			}
		}
		for _, child := range typed {
			if text := firstPlanStringRecursiveSeen(child, depth+1); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := firstPlanStringRecursiveSeen(child, depth+1); text != "" {
				return text
			}
		}
	}
	return ""
}

func normalizePlanType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	switch normalized {
	case "oauth", "browser_oauth", "api_key", "apikey", "key", "unknown":
		return ""
	}
	if strings.Contains(normalized, "prolite") || strings.Contains(normalized, "pro_lite") {
		return "pro_5x"
	}
	if strings.Contains(normalized, "pro") {
		switch {
		case strings.Contains(normalized, "20x"), strings.Contains(normalized, "20_x"):
			return "pro_20x"
		case strings.Contains(normalized, "5x"), strings.Contains(normalized, "5_x"):
			return "pro_5x"
		}
	}
	switch {
	case strings.Contains(normalized, "free"):
		return "free"
	case normalized == "go" || strings.HasSuffix(normalized, "_go") || strings.Contains(normalized, "chatgpt_go"):
		return "go"
	case strings.Contains(normalized, "plus"):
		return "plus"
	case strings.Contains(normalized, "business"):
		return "business"
	case strings.Contains(normalized, "team"):
		return "team"
	case strings.Contains(normalized, "enterprise"):
		return "enterprise"
	case normalized == "edu", strings.Contains(normalized, "education"), strings.Contains(normalized, "chatgpt_edu"):
		return "edu"
	case normalized == "pro", normalized == "chatgpt_pro", strings.Contains(normalized, "pro"):
		return "pro_20x"
	default:
		return normalized
	}
}

func isDetailedProPlan(value string) bool {
	switch normalizePlanType(value) {
	case "pro_5x", "pro_20x":
		return true
	default:
		return false
	}
}

func isPaidPlanType(value string) bool {
	plan := normalizePlanType(value)
	return plan != "" && plan != "free"
}

func percentToInt(value float64) int64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return int64(math.Round(value))
}

func jsonNumberPtr(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	value, ok := m[key]
	if !ok {
		return nil
	}
	number := numberAnyFlexible(value)
	return &number
}

func unixTimePtr(m map[string]any, key string) *time.Time {
	number := jsonNumberPtr(m, key)
	if number == nil || *number <= 0 {
		return nil
	}
	t := time.Unix(int64(*number), 0)
	return &t
}

func parseSubscriptionTimestamp(value string) *time.Time {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return &parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return &parsed
	}
	return nil
}

func tokenStringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	for _, candidate := range []map[string]any{payload} {
		if value := firstString(candidate, key); value != "" {
			return value
		}
	}
	for _, nestedKey := range []string{"token_data", "token", "metadata"} {
		nested, ok := asMap(payload[nestedKey])
		if !ok {
			continue
		}
		if value := firstString(nested, key); value != "" {
			return value
		}
	}
	return ""
}

func codexAccountScopeFromPayload(payload map[string]any) (string, string) {
	chatgptAccountID := normalizeScopedIdentity(firstString(payload, "chatgpt_account_id", "chatgptAccountId"), "cgpt=")
	workspaceID := normalizeScopedIdentity(firstString(payload, "workspace_id", "workspaceId", "organization_id", "org_id"), "ws=")
	for _, token := range []string{tokenStringFromPayload(payload, "id_token"), tokenStringFromPayload(payload, "access_token")} {
		if token == "" {
			continue
		}
		if chatgptAccountID == "" {
			chatgptAccountID = extractChatGPTAccountIDFromJWT(token)
		}
		if workspaceID == "" {
			workspaceID = extractWorkspaceIDFromJWT(token)
		}
	}
	if workspaceID == "" {
		workspaceID = chatgptAccountID
	}
	return chatgptAccountID, workspaceID
}

func isCodexPlatform(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex":
		return true
	default:
		return false
	}
}

func extractChatGPTAccountIDFromJWT(token string) string {
	payload := jwtPayload(token)
	if payload == nil {
		return ""
	}
	if value := normalizeScopedIdentity(firstString(payload, "chatgpt_account_id", "chatgptAccountId"), "cgpt="); value != "" {
		return value
	}
	if auth, ok := asMap(payload["https://api.openai.com/auth"]); ok {
		return normalizeScopedIdentity(firstString(auth, "chatgpt_account_id", "chatgptAccountId"), "cgpt=")
	}
	return ""
}

func extractWorkspaceIDFromJWT(token string) string {
	payload := jwtPayload(token)
	if payload == nil {
		return ""
	}
	for _, key := range []string{"workspace_id", "workspaceId", "chatgpt_account_id", "chatgptAccountId", "organization_id", "org_id"} {
		if value := normalizeScopedIdentity(firstString(payload, key), "ws="); value != "" {
			return value
		}
	}
	auth, ok := asMap(payload["https://api.openai.com/auth"])
	if !ok {
		return ""
	}
	if orgs, ok := auth["organizations"].([]any); ok {
		for _, item := range orgs {
			org, ok := asMap(item)
			if !ok {
				continue
			}
			isDefault, _ := org["is_default"].(bool)
			if isDefault {
				if value := normalizeScopedIdentity(firstString(org, "id"), "ws="); value != "" {
					return value
				}
			}
		}
		for _, item := range orgs {
			org, ok := asMap(item)
			if !ok {
				continue
			}
			if value := normalizeScopedIdentity(firstString(org, "id"), "ws="); value != "" {
				return value
			}
		}
	}
	for _, key := range []string{"workspace_id", "workspaceId", "chatgpt_account_id", "chatgptAccountId", "organization_id", "org_id"} {
		if value := normalizeScopedIdentity(firstString(auth, key), "ws="); value != "" {
			return value
		}
	}
	return ""
}

func jwtPayload(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil
	}
	return payload
}

func normalizeScopedIdentity(value, marker string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	scoped := raw
	if idx := strings.LastIndex(scoped, "::"); idx >= 0 {
		scoped = scoped[idx+2:]
	}
	for _, segment := range strings.Split(scoped, "|") {
		segment = strings.TrimSpace(segment)
		if strings.HasPrefix(segment, marker) {
			return strings.TrimSpace(strings.TrimPrefix(segment, marker))
		}
	}
	if strings.Contains(raw, "::") || strings.Contains(raw, "|") || strings.Contains(raw, "=") || strings.HasPrefix(raw, "import-sub-") {
		return ""
	}
	return raw
}

func boolFromAny(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" || normalized == "yes" {
			return true, true
		}
		if normalized == "false" || normalized == "0" || normalized == "no" {
			return false, true
		}
	default:
		return false, false
	}
	return false, false
}

func asMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}
