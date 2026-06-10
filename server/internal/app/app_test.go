package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testAdminEmail    = "admin@relay.io"
	testAdminPassword = "admin123456"
	testUserEmail     = "relay-user@example.com"
	testUserPassword  = "user123456"
)

func testApp(t *testing.T) *App {
	t.Helper()
	return testAppWithConfig(t, nil)
}

func testAppWithConfig(t *testing.T, configure func(*Config)) *App {
	t.Helper()
	cfg := Config{
		Addr:             ":0",
		DatabaseDriver:   "sqlite",
		DatabaseDSN:      filepath.Join(t.TempDir(), "relay-test.db"),
		JWTSecret:        "test-secret",
		AccessTTL:        3600_000_000_000,
		RefreshTTL:       7200_000_000_000,
		AdminEmail:       testAdminEmail,
		AdminPassword:    testAdminPassword,
		SeedData:         true,
		EmailCodeDevMode: true,
	}
	if configure != nil {
		configure(&cfg)
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := app.db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return app
}

func performJSON(app *App, method, path string, token string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)
	return w
}

func unsignedTestJWT(payload map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	body, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body) + ".sig"
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %s: %v", w.Body.String(), err)
	}
	return out
}

func loginToken(t *testing.T, app *App, email, password, role string) string {
	t.Helper()
	w := performJSON(app, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": email, "password": password, "role": role,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	token, _ := body["accessToken"].(string)
	if token == "" {
		t.Fatalf("missing access token: %v", body)
	}
	return token
}

func createTestUser(t *testing.T, app *App) {
	t.Helper()
	var count int64
	if err := app.db.Model(&User{}).Where("email = ?", testUserEmail).Count(&count).Error; err != nil {
		t.Fatalf("count test user: %v", err)
	}
	if count > 0 {
		return
	}
	hash, err := hashPassword(testUserPassword)
	if err != nil {
		t.Fatalf("hash test user password: %v", err)
	}
	if err := app.db.Create(&User{
		Email:        testUserEmail,
		Name:         "Test User",
		PasswordHash: hash,
		Role:         RoleUser,
		Status:       UserStatusNormal,
		MonthlyQuota: 1_000_000,
		WeeklyQuota:  300_000,
		Balance:      100,
	}).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
}

func createThirdPartySource(t *testing.T, app *App, name string) UpstreamSource {
	t.Helper()
	source := UpstreamSource{
		Name:    name,
		Type:    SourceTypeThirdParty,
		BaseURL: "https://api.example.com",
		Status:  SourceStatusDisabled,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create third-party source: %v", err)
	}
	return source
}

func registerEmailCode(t *testing.T, app *App, email string) string {
	t.Helper()
	w := performJSON(app, http.MethodPost, "/api/auth/register/email-code", "", map[string]any{"email": email})
	if w.Code != http.StatusOK {
		t.Fatalf("send register email code: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	data := body["data"].(map[string]any)
	code, _ := data["devCode"].(string)
	if code == "" {
		t.Fatalf("expected dev verification code in test mode, got %v", data)
	}
	return code
}

func createRelayAPIKey(t *testing.T, app *App) string {
	t.Helper()
	createTestUser(t, app)
	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodPost, "/api/user/api-keys", userToken, map[string]any{"name": "test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create api key: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	key := body["data"].(map[string]any)["key"].(string)
	if key == "" || key[:9] != "sk-relay-" {
		t.Fatalf("unexpected key: %q", key)
	}
	return key
}

func createStoredAPIKey(t *testing.T, app *App, userID uint, name string) APIKey {
	t.Helper()
	secret := "sk-relay-test-" + strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	key := APIKey{
		UserID:  userID,
		Name:    name,
		Secret:  secret,
		KeyHash: hashKey(secret),
		Masked:  maskKey(secret),
		Status:  APIKeyStatusValid,
	}
	if err := app.db.Create(&key).Error; err != nil {
		t.Fatalf("create stored api key: %v", err)
	}
	return key
}

func loadTestUser(t *testing.T, app *App) User {
	t.Helper()
	createTestUser(t, app)
	var user User
	if err := app.db.Where("email = ?", testUserEmail).First(&user).Error; err != nil {
		t.Fatalf("load test user: %v", err)
	}
	return user
}

func createUsageLog(t *testing.T, app *App, row UsageLog) UsageLog {
	t.Helper()
	if row.Status == "" {
		row.Status = RequestStatusSuccess
	}
	if err := app.db.Create(&row).Error; err != nil {
		t.Fatalf("create usage log: %v", err)
	}
	return row
}

func TestAdminDashboardReturnsComputedComparisons(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	key := createStoredAPIKey(t, app, user.ID, "dashboard-key")
	now := time.Now()
	today := now
	yesterday := now.AddDate(0, 0, -1)
	lastMonthSameWindow := monthStart(now).AddDate(0, -1, 0)

	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "dashboard-model", TotalTokens: 100, EstimatedCost: 4, CreatedAt: today})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "dashboard-model", TotalTokens: 100, CreatedAt: yesterday})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "dashboard-month", TotalTokens: 100, EstimatedCost: 2, CreatedAt: lastMonthSameWindow})

	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodGet, "/api/admin/dashboard", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard: %d %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]any)
	if got := data["todayRequestsChangePct"].(float64); got != 0 {
		t.Fatalf("todayRequestsChangePct = %v, want 0 when today equals yesterday", got)
	}
	if got := data["monthlySpendPct"].(float64); got != 100 {
		t.Fatalf("monthlySpendPct = %v, want 100", got)
	}
	if _, ok := data["trendChangePct"]; !ok {
		t.Fatalf("expected trendChangePct in dashboard payload")
	}
}

func TestAdminUsageStatsRangeFiltersTotalsAndAggregates(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	key := createStoredAPIKey(t, app, user.ID, "usage-key")
	now := time.Now()
	today := now
	lastWeek := now.AddDate(0, 0, -8)

	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "today-model", PromptTokens: 40, CompletionTokens: 60, TotalTokens: 100, EstimatedCost: 1.25, CreatedAt: today})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "old-model", PromptTokens: 400, CompletionTokens: 500, TotalTokens: 900, EstimatedCost: 9.5, CreatedAt: lastWeek})

	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodGet, "/api/admin/usage/stats?range=day", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("usage stats: %d %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]any)
	if got := int64(data["totalTokens"].(float64)); got != 100 {
		t.Fatalf("totalTokens = %d, want current-day 100", got)
	}
	if got := int64(data["totalRequests"].(float64)); got != 1 {
		t.Fatalf("totalRequests = %d, want current-day 1", got)
	}
	if got := data["granularity"]; got != "hour" {
		t.Fatalf("granularity = %v, want hour", got)
	}
	byModel := data["byModel"].([]any)
	if len(byModel) != 1 || byModel[0].(map[string]any)["model"] != "today-model" {
		t.Fatalf("expected byModel to contain only today-model, got %v", byModel)
	}
	byUser := data["byUser"].([]any)
	if len(byUser) != 1 || byUser[0].(map[string]any)["email"] != testUserEmail {
		t.Fatalf("expected byUser to contain test user, got %v", byUser)
	}
}

func TestUserUsageStatsAPIKeyFilterAffectsTotals(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	keyA := createStoredAPIKey(t, app, user.ID, "key-a")
	keyB := createStoredAPIKey(t, app, user.ID, "key-b")
	now := time.Now()
	today := now

	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: keyA.ID, Model: "key-a-model", PromptTokens: 10, CompletionTokens: 15, CacheReadTokens: 4, CacheWriteTokens: 2, ReasoningTokens: 6, TotalTokens: 25, EstimatedCost: 0.25, CreatedAt: today})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: keyB.ID, Model: "key-b-model", PromptTokens: 20, CompletionTokens: 30, TotalTokens: 50, EstimatedCost: 0.5, CreatedAt: today})

	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodGet, "/api/user/usage?range=day&apiKeyId="+id("k", keyA.ID), userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user usage: %d %s", w.Code, w.Body.String())
	}
	payload := decodeBody(t, w)["data"].(map[string]any)
	stats := payload["stats"].(map[string]any)
	if got := int64(stats["totalTokens"].(float64)); got != 25 {
		t.Fatalf("filtered totalTokens = %d, want 25", got)
	}
	rows := payload["rows"].([]any)
	if len(rows) != 1 || int64(rows[0].(map[string]any)["requests"].(float64)) != 1 {
		t.Fatalf("expected one filtered day row with one request, got %v", rows)
	}
	row := rows[0].(map[string]any)
	if got := int64(row["promptTokens"].(float64)); got != 10 {
		t.Fatalf("promptTokens = %d, want 10", got)
	}
	if got := int64(row["completionTokens"].(float64)); got != 15 {
		t.Fatalf("completionTokens = %d, want 15", got)
	}
	if got := int64(row["cacheReadTokens"].(float64)); got != 4 {
		t.Fatalf("cacheReadTokens = %d, want 4", got)
	}
	if got := int64(row["cacheWriteTokens"].(float64)); got != 2 {
		t.Fatalf("cacheWriteTokens = %d, want 2", got)
	}
	if got := int64(row["reasoningTokens"].(float64)); got != 6 {
		t.Fatalf("reasoningTokens = %d, want 6", got)
	}
	if got := int64(row["totalTokens"].(float64)); got != 25 {
		t.Fatalf("totalTokens = %d, want 25", got)
	}
}

func TestUserLogsAreScopedAndExposeTokenDetails(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	key := createStoredAPIKey(t, app, user.ID, "frontend-key")
	otherHash, err := hashPassword("other123456")
	if err != nil {
		t.Fatalf("hash other password: %v", err)
	}
	other := User{Email: "other-logs@example.com", Name: "Other Logs", PasswordHash: otherHash, Role: RoleUser, Status: UserStatusNormal}
	if err := app.db.Create(&other).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherKey := createStoredAPIKey(t, app, other.ID, "other-key")
	now := time.Now()
	ownLog := createUsageLog(t, app, UsageLog{
		UserID:           user.ID,
		APIKeyID:         key.ID,
		RequestID:        "req-user-visible",
		Protocol:         string(relayProtocolOpenAI),
		Path:             "/v1/chat/completions",
		Stream:           true,
		Model:            "user-log-model",
		UpstreamName:     "internal-upstream",
		PromptTokens:     10,
		CompletionTokens: 20,
		CacheReadTokens:  4,
		CacheWriteTokens: 2,
		ReasoningTokens:  3,
		TotalTokens:      30,
		EstimatedCost:    0.12,
		LatencyMS:        456,
		StatusCode:       http.StatusOK,
		Status:           RequestStatusSuccess,
		RequestHeaders:   jsonString(map[string]any{"Authorization": "<redacted>"}),
		ResponseHeaders:  jsonString(map[string]any{"Content-Type": "application/json"}),
		RequestPayload:   jsonString(map[string]any{"model": "user-log-model"}),
		ResponsePayload:  jsonString(map[string]any{"ok": true}),
		AttemptCount:     1,
		CreatedAt:        now,
	})
	otherLog := createUsageLog(t, app, UsageLog{
		UserID:       other.ID,
		APIKeyID:     otherKey.ID,
		RequestID:    "req-other-hidden",
		Model:        "user-log-model",
		UpstreamName: "other-upstream",
		TotalTokens:  99,
		CreatedAt:    now,
	})
	if err := app.db.Create(&RequestAttempt{
		UsageLogID:   ownLog.ID,
		RequestID:    ownLog.RequestID,
		AttemptIndex: 1,
		SourceID:     99,
		SourceKeyID:  100,
		Model:        ownLog.Model,
		UpstreamName: ownLog.UpstreamName,
		Protocol:     ownLog.Protocol,
		Path:         ownLog.Path,
		StatusCode:   http.StatusOK,
		Status:       RequestStatusSuccess,
		LatencyMS:    456,
		StartedAt:    now,
		EndedAt:      now.Add(456 * time.Millisecond),
	}).Error; err != nil {
		t.Fatalf("create request attempt: %v", err)
	}

	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodGet, "/api/user/logs?page=1&pageSize=10&q=visible", userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user logs: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one scoped user log, got %v", rows)
	}
	row := rows[0].(map[string]any)
	if row["requestId"] != ownLog.RequestID || row["apiKeyName"] != key.Name || row["userEmail"] != testUserEmail {
		t.Fatalf("unexpected user log row identity: %v", row)
	}
	if row["sourceId"] != "" || row["sourceKeyId"] != "" || row["sourceKeyAlias"] != "" {
		t.Fatalf("user log should not expose source internals: %v", row)
	}
	if int64(row["tokensPrompt"].(float64)) != 10 ||
		int64(row["tokensCompletion"].(float64)) != 20 ||
		int64(row["tokensCacheRead"].(float64)) != 4 ||
		int64(row["tokensCacheWrite"].(float64)) != 2 ||
		int64(row["tokensReasoning"].(float64)) != 3 {
		t.Fatalf("unexpected token details: %v", row)
	}
	requestPayload := row["requestPayload"].(map[string]any)
	if requestPayload["model"] != "user-log-model" {
		t.Fatalf("unexpected request payload: %v", requestPayload)
	}
	pagination := body["pagination"].(map[string]any)
	if pagination["total"].(float64) != 1 {
		t.Fatalf("expected other user's logs to be hidden, pagination=%v", pagination)
	}

	attemptResp := performJSON(app, http.MethodGet, "/api/user/logs/"+id("log", ownLog.ID)+"/attempts", userToken, nil)
	if attemptResp.Code != http.StatusOK {
		t.Fatalf("user log attempts: %d %s", attemptResp.Code, attemptResp.Body.String())
	}
	attempts := decodeBody(t, attemptResp)["data"].([]any)
	if len(attempts) != 1 {
		t.Fatalf("expected one attempt, got %v", attempts)
	}
	attempt := attempts[0].(map[string]any)
	if attempt["sourceId"] != "" || attempt["sourceKeyId"] != "" {
		t.Fatalf("user attempts should not expose source internals: %v", attempt)
	}
	otherAttemptResp := performJSON(app, http.MethodGet, "/api/user/logs/"+id("log", otherLog.ID)+"/attempts", userToken, nil)
	if otherAttemptResp.Code != http.StatusNotFound {
		t.Fatalf("expected other user's log attempts to be hidden, got %d %s", otherAttemptResp.Code, otherAttemptResp.Body.String())
	}
}

func TestAdminLogsAreServerPaginated(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	key := createStoredAPIKey(t, app, user.ID, "log-key")
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 25; i++ {
		createUsageLog(t, app, UsageLog{
			UserID:       user.ID,
			APIKeyID:     key.ID,
			Model:        "paged-model",
			UpstreamName: "paged-upstream",
			TotalTokens:  int64(i + 1),
			CreatedAt:    base.Add(time.Duration(i) * time.Minute),
		})
	}

	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodGet, "/api/admin/logs?page=2&pageSize=10&model=paged-model", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin logs: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 10 {
		t.Fatalf("page row count = %d, want 10", len(rows))
	}
	pagination := body["pagination"].(map[string]any)
	if pagination["page"].(float64) != 2 || pagination["pageSize"].(float64) != 10 || pagination["total"].(float64) != 25 || pagination["totalPages"].(float64) != 3 {
		t.Fatalf("unexpected pagination: %v", pagination)
	}
}

func TestAdminLogsTimeFilters(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	key := createStoredAPIKey(t, app, user.ID, "time-filter-key")
	base := time.Date(2026, 5, 20, 8, 0, 0, 0, time.Local)
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "time-filter-old", TotalTokens: 10, CreatedAt: base})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "time-filter-in", TotalTokens: 20, CreatedAt: base.Add(2 * time.Hour)})
	createUsageLog(t, app, UsageLog{UserID: user.ID, APIKeyID: key.ID, Model: "time-filter-new", TotalTokens: 30, CreatedAt: base.Add(4 * time.Hour)})

	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodGet, "/api/admin/logs?from=2026-05-20T09:00&to=2026-05-20T11:00&q=time-filter", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin logs time filter: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one filtered log, got %v", rows)
	}
	if rows[0].(map[string]any)["model"] != "time-filter-in" {
		t.Fatalf("unexpected filtered log: %v", rows[0])
	}
}

func TestAdminModelsExposeRoutingCandidatesAndControls(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	cooldownUntil := time.Now().Add(10 * time.Minute)
	sourceA := UpstreamSource{Name: "Route_Primary", Type: SourceTypeThirdParty, BaseURL: "https://primary.example.com/v1", APIKey: "a-key", Priority: 1, Status: SourceStatusOnline, FailureCount: 2, CooldownUntil: &cooldownUntil}
	sourceB := UpstreamSource{Name: "Route_Backup", Type: SourceTypeThirdParty, BaseURL: "https://backup.example.com/v1", APIKey: "b-key", Priority: 2, Status: SourceStatusOnline}
	if err := app.db.Create(&sourceA).Error; err != nil {
		t.Fatalf("create route primary: %v", err)
	}
	if err := app.db.Create(&sourceB).Error; err != nil {
		t.Fatalf("create route backup: %v", err)
	}
	models := []ModelConfig{
		{SourceID: sourceA.ID, Name: "routed-admin-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 3, RoutingEnabled: true},
		{SourceID: sourceB.ID, Name: "routed-admin-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 1, RoutingEnabled: true},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create routed models: %v", err)
	}
	if err := migrateModelRouteBindings(app.db); err != nil {
		t.Fatalf("migrate routed models: %v", err)
	}

	w := performJSON(app, http.MethodGet, "/api/admin/models?q=routed-admin-model", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin models: %d %s", w.Code, w.Body.String())
	}
	rows := decodeBody(t, w)["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one logical routed model row, got %v", rows)
	}
	first := rows[0].(map[string]any)
	if first["candidateCount"] != float64(2) {
		t.Fatalf("candidateCount = %v, want 2 in %v", first["candidateCount"], first)
	}
	candidates := first["routingCandidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("expected two routing candidates, got %v", candidates)
	}
	primary := candidates[0].(map[string]any)
	if primary["sourceName"] != "Route_Primary" || primary["sourcePriority"] != float64(1) || primary["routingWeight"] != float64(3) || primary["coolingDown"] != true {
		t.Fatalf("unexpected primary candidate: %v", primary)
	}

	update := performJSON(app, http.MethodPut, "/api/admin/models/"+id("m", models[0].ID), adminToken, map[string]any{
		"routingEnabled": false,
		"routingWeight":  7,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update routing controls: %d %s", update.Code, update.Body.String())
	}
	updated := decodeBody(t, update)["data"].(map[string]any)
	if updated["routingEnabled"] != false || updated["routingWeight"] != float64(7) {
		t.Fatalf("unexpected updated routing controls: %v", updated)
	}
}

func TestAdminModelDuplicateSourceBindingsCanBeRemoved(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	source := UpstreamSource{Name: "Duplicate_Source_Remove", Type: SourceTypeThirdParty, BaseURL: "https://dup.example.com/v1", APIKey: "dup-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	create := performJSON(app, http.MethodPost, "/api/admin/models", adminToken, map[string]any{
		"name":     "duplicate-source-model",
		"sourceId": id("s", source.ID),
		"provider": "OpenAI",
		"formats":  []string{ModelFormatOpenAI},
		"bindings": []map[string]any{
			{"sourceId": id("s", source.ID), "routingWeight": 1},
			{"sourceId": id("s", source.ID), "routingWeight": 5},
		},
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create duplicate bindings model: %d %s", create.Code, create.Body.String())
	}
	created := decodeBody(t, create)["data"].(map[string]any)
	modelID := created["id"].(string)
	candidates := created["routingCandidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2 in %v", len(candidates), created)
	}
	kept := candidates[1].(map[string]any)

	update := performJSON(app, http.MethodPut, "/api/admin/models/"+modelID, adminToken, map[string]any{
		"bindings": []map[string]any{
			{
				"id":            kept["id"],
				"sourceId":      kept["sourceId"],
				"sourceKeyId":   "default",
				"routingWeight": kept["routingWeight"],
			},
		},
	})
	if update.Code != http.StatusOK {
		t.Fatalf("remove duplicate binding: %d %s", update.Code, update.Body.String())
	}
	updated := decodeBody(t, update)["data"].(map[string]any)
	updatedCandidates := updated["routingCandidates"].([]any)
	if len(updatedCandidates) != 1 {
		t.Fatalf("candidate count after delete = %d, want 1 in %v", len(updatedCandidates), updated)
	}
	var bindingCount int64
	if err := app.db.Model(&ModelRouteBinding{}).Where("model_id = ?", publicIDNumber(modelID)).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count route bindings: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("route binding count = %d, want 1", bindingCount)
	}
}

func TestAdminModelUpdateRemovesDuplicateSiblingRows(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	source := UpstreamSource{Name: "Duplicate_Sibling_Source", Type: SourceTypeThirdParty, BaseURL: "https://sibling.example.com/v1", APIKey: "sibling-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	models := []ModelConfig{
		{SourceID: source.ID, Name: "duplicate-sibling-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 1},
		{SourceID: source.ID, Name: "duplicate-sibling-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 2},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create duplicate model rows: %v", err)
	}
	if err := app.replaceModelBindings(models[0].ID, []modelBindingRequest{{SourceID: id("s", source.ID), SourceKeyID: "default", RoutingWeight: 1}}); err != nil {
		t.Fatalf("create first binding: %v", err)
	}
	if err := app.replaceModelBindings(models[1].ID, []modelBindingRequest{{SourceID: id("s", source.ID), SourceKeyID: "default", RoutingWeight: 2}}); err != nil {
		t.Fatalf("create sibling binding: %v", err)
	}

	list := performJSON(app, http.MethodGet, "/api/admin/models?q=duplicate-sibling-model", adminToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list duplicate sibling model: %d %s", list.Code, list.Body.String())
	}
	rows := decodeBody(t, list)["data"].([]any)
	first := rows[0].(map[string]any)
	candidates := first["routingCandidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("expected dirty state with two duplicate candidates, got %v", first)
	}
	kept := candidates[0].(map[string]any)

	update := performJSON(app, http.MethodPut, "/api/admin/models/"+first["id"].(string), adminToken, map[string]any{
		"bindings": []map[string]any{
			{
				"id":            kept["id"],
				"sourceId":      kept["sourceId"],
				"sourceKeyId":   "default",
				"routingWeight": kept["routingWeight"],
			},
		},
	})
	if update.Code != http.StatusOK {
		t.Fatalf("remove duplicate sibling binding: %d %s", update.Code, update.Body.String())
	}
	var modelCount int64
	if err := app.db.Model(&ModelConfig{}).Where("name = ?", "duplicate-sibling-model").Count(&modelCount).Error; err != nil {
		t.Fatalf("count model rows: %v", err)
	}
	if modelCount != 1 {
		t.Fatalf("model row count = %d, want 1", modelCount)
	}
	var bindingCount int64
	if err := app.db.Model(&ModelRouteBinding{}).Where("model_id = ?", models[0].ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count route bindings: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("route binding count = %d, want 1", bindingCount)
	}
}

func TestModelBindingMigrationMergesLegacyDuplicateModels(t *testing.T) {
	app := testApp(t)
	sourceA := UpstreamSource{Name: "Legacy_Source_A", Type: SourceTypeThirdParty, BaseURL: "https://legacy-a.example.com/v1", APIKey: "a-key", Priority: 2, Status: SourceStatusOnline}
	sourceB := UpstreamSource{Name: "Legacy_Source_B", Type: SourceTypeThirdParty, BaseURL: "https://legacy-b.example.com/v1", APIKey: "b-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&sourceA).Error; err != nil {
		t.Fatalf("create source A: %v", err)
	}
	if err := app.db.Create(&sourceB).Error; err != nil {
		t.Fatalf("create source B: %v", err)
	}
	models := []ModelConfig{
		{SourceID: sourceA.ID, Name: "legacy-merged-model", DisplayName: "Legacy Merged Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 5},
		{SourceID: sourceB.ID, Name: "legacy-merged-model", DisplayName: "Legacy Merged Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 20},
		{SourceID: sourceA.ID, Name: "legacy-single-model", DisplayName: "Legacy Single Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 3},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create legacy models: %v", err)
	}
	if err := migrateModelRouteBindings(app.db); err != nil {
		t.Fatalf("migrate model bindings: %v", err)
	}

	var mergedCount int64
	if err := app.db.Model(&ModelConfig{}).Where("name = ?", "legacy-merged-model").Count(&mergedCount).Error; err != nil {
		t.Fatalf("count merged models: %v", err)
	}
	if mergedCount != 1 {
		t.Fatalf("legacy-merged-model count = %d, want 1", mergedCount)
	}
	var merged ModelConfig
	if err := app.db.Where("name = ?", "legacy-merged-model").First(&merged).Error; err != nil {
		t.Fatalf("load merged model: %v", err)
	}
	var bindings []ModelRouteBinding
	if err := app.db.Where("model_id = ?", merged.ID).Order("routing_weight desc").Find(&bindings).Error; err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("binding count = %d, want 2", len(bindings))
	}
	if bindings[0].SourceID != sourceB.ID || bindings[0].RoutingWeight != 20 {
		t.Fatalf("expected source B to keep higher route weight, got %+v", bindings[0])
	}

	var single ModelConfig
	if err := app.db.Where("name = ?", "legacy-single-model").First(&single).Error; err != nil {
		t.Fatalf("load single model: %v", err)
	}
	var singleBindings int64
	if err := app.db.Model(&ModelRouteBinding{}).Where("model_id = ?", single.ID).Count(&singleBindings).Error; err != nil {
		t.Fatalf("count single bindings: %v", err)
	}
	if singleBindings != 1 {
		t.Fatalf("single binding count = %d, want 1", singleBindings)
	}
}

func TestAdminSameModelNameCanExistInDifferentGroups(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	source := UpstreamSource{Name: "Grouped_Model_Source", Type: SourceTypeThirdParty, BaseURL: "https://grouped.example.com/v1", APIKey: "key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	groupResp := performJSON(app, http.MethodPost, "/api/admin/model-groups", adminToken, map[string]any{
		"name": "premium",
	})
	if groupResp.Code != http.StatusCreated {
		t.Fatalf("create model group: %d %s", groupResp.Code, groupResp.Body.String())
	}
	groupID := decodeBody(t, groupResp)["data"].(map[string]any)["id"].(string)

	first := performJSON(app, http.MethodPost, "/api/admin/models", adminToken, map[string]any{
		"name":     "same-name-grouped-model",
		"sourceId": id("s", source.ID),
		"provider": "OpenAI",
		"formats":  []string{ModelFormatOpenAI},
		"enabled":  true,
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("create default group model: %d %s", first.Code, first.Body.String())
	}
	second := performJSON(app, http.MethodPost, "/api/admin/models", adminToken, map[string]any{
		"name":         "same-name-grouped-model",
		"modelGroupId": groupID,
		"sourceId":     id("s", source.ID),
		"provider":     "OpenAI",
		"formats":      []string{ModelFormatOpenAI},
		"enabled":      true,
	})
	if second.Code != http.StatusCreated {
		t.Fatalf("create custom group model: %d %s", second.Code, second.Body.String())
	}

	list := performJSON(app, http.MethodGet, "/api/admin/models?q=same-name-grouped-model", adminToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list grouped models: %d %s", list.Code, list.Body.String())
	}
	rows := decodeBody(t, list)["data"].([]any)
	seenGroups := map[string]bool{}
	for _, row := range rows {
		item := row.(map[string]any)
		if item["name"] == "same-name-grouped-model" {
			seenGroups[item["modelGroupId"].(string)] = true
		}
	}
	if len(seenGroups) != 2 || !seenGroups[groupID] {
		t.Fatalf("expected same model name in two groups, got %v from rows %v", seenGroups, rows)
	}
}

func TestAPIKeyModelGroupControlsRouteResolution(t *testing.T) {
	defaultCalls := 0
	customCalls := 0
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_default_group",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "default"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			"model":   "group-routed-model",
		})
	}))
	defer defaultUpstream.Close()
	customUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_custom_group",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "custom"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
			"model":   "group-routed-model",
		})
	}))
	defer customUpstream.Close()

	app := testApp(t)
	user := loadTestUser(t, app)
	defaultGroup, err := defaultPlatformModelGroup(app.db)
	if err != nil {
		t.Fatalf("default group: %v", err)
	}
	customGroup := ModelGroup{Name: "custom-route"}
	if err := app.db.Create(&customGroup).Error; err != nil {
		t.Fatalf("create custom group: %v", err)
	}
	defaultSource := UpstreamSource{Name: "Default_Group_Route_Source", Type: SourceTypeThirdParty, BaseURL: defaultUpstream.URL + "/v1", APIKey: "default-key", Priority: 1, Status: SourceStatusOnline}
	customSource := UpstreamSource{Name: "Custom_Group_Route_Source", Type: SourceTypeThirdParty, BaseURL: customUpstream.URL + "/v1", APIKey: "custom-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&defaultSource).Error; err != nil {
		t.Fatalf("create default source: %v", err)
	}
	if err := app.db.Create(&customSource).Error; err != nil {
		t.Fatalf("create custom source: %v", err)
	}
	models := []ModelConfig{
		{ModelGroupID: defaultGroup.ID, SourceID: defaultSource.ID, Name: "group-routed-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
		{ModelGroupID: customGroup.ID, SourceID: customSource.ID, Name: "group-routed-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create grouped route models: %v", err)
	}
	defaultSecret := "sk-relay-route-default"
	customSecret := "sk-relay-route-custom"
	keys := []APIKey{
		{UserID: user.ID, ModelGroupID: defaultGroup.ID, Name: "default-route", Secret: defaultSecret, KeyHash: hashKey(defaultSecret), Masked: maskKey(defaultSecret), Status: APIKeyStatusValid},
		{UserID: user.ID, ModelGroupID: customGroup.ID, Name: "custom-route", Secret: customSecret, KeyHash: hashKey(customSecret), Masked: maskKey(customSecret), Status: APIKeyStatusValid},
	}
	if err := app.db.Create(&keys).Error; err != nil {
		t.Fatalf("create grouped api keys: %v", err)
	}

	payload := map[string]any{
		"model":    "group-routed-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	w := performJSON(app, http.MethodPost, "/v1/chat/completions", defaultSecret, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("default group proxy: %d %s", w.Code, w.Body.String())
	}
	w = performJSON(app, http.MethodPost, "/v1/chat/completions", customSecret, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("custom group proxy: %d %s", w.Code, w.Body.String())
	}
	if defaultCalls != 1 || customCalls != 1 {
		t.Fatalf("expected one call per group, default=%d custom=%d", defaultCalls, customCalls)
	}
}

func TestAPIKeyDeletedModelGroupReturnsClearError(t *testing.T) {
	app := testApp(t)
	user := loadTestUser(t, app)
	group := ModelGroup{Name: "deleted-route"}
	if err := app.db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	secret := "sk-relay-deleted-group"
	key := APIKey{
		UserID:       user.ID,
		ModelGroupID: group.ID,
		Name:         "deleted-group-key",
		Secret:       secret,
		KeyHash:      hashKey(secret),
		Masked:       maskKey(secret),
		Status:       APIKeyStatusValid,
	}
	if err := app.db.Create(&key).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := app.db.Delete(&group).Error; err != nil {
		t.Fatalf("delete group: %v", err)
	}

	payload := map[string]any{
		"model":    "deleted-group-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	w := performJSON(app, http.MethodPost, "/v1/chat/completions", secret, payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("deleted group proxy: %d %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w)["error"]; got != "当前分组已删除" {
		t.Fatalf("expected deleted group error, got %v", got)
	}

	w = performJSON(app, http.MethodGet, "/v1/models", secret, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("deleted group model list: %d %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w)["error"]; got != "当前分组已删除" {
		t.Fatalf("expected deleted group model list error, got %v", got)
	}
}

func TestAdminRecoverSourceClearsCooldown(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	lastFailure := time.Now().Add(-time.Minute)
	cooldownUntil := time.Now().Add(10 * time.Minute)
	source := UpstreamSource{Name: "Recover_Source", Type: SourceTypeThirdParty, BaseURL: "https://recover.example.com/v1", APIKey: "key", Priority: 1, Status: SourceStatusOnline, FailureCount: 4, LastFailureAt: &lastFailure, CooldownUntil: &cooldownUntil}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create recover source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "recover-binding-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create recover binding model: %v", err)
	}
	if err := app.replaceModelBindings(model.ID, []modelBindingRequest{{SourceID: id("s", source.ID), SourceKeyID: "default", RoutingWeight: 1}}); err != nil {
		t.Fatalf("create recover binding: %v", err)
	}
	var binding ModelRouteBinding
	if err := app.db.Where("model_id = ?", model.ID).First(&binding).Error; err != nil {
		t.Fatalf("load recover binding: %v", err)
	}
	if err := app.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(map[string]any{
		"scheduler_state": schedulerStateOpen,
		"failure_count":   4,
		"cooldown_until":  cooldownUntil,
		"last_failure_at": lastFailure,
	}).Error; err != nil {
		t.Fatalf("cool recover binding: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/"+id("s", source.ID)+"/recover", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("recover source: %d %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]any)
	if data["failureCount"] != float64(0) || data["coolingDown"] != false {
		t.Fatalf("unexpected recovered source dto: %v", data)
	}
	var refreshed UpstreamSource
	if err := app.db.First(&refreshed, source.ID).Error; err != nil {
		t.Fatalf("load recovered source: %v", err)
	}
	if refreshed.FailureCount != 0 || refreshed.CooldownUntil != nil || refreshed.LastFailureAt != nil {
		t.Fatalf("expected cooldown fields cleared, got %+v", refreshed)
	}
	var recoveredBinding ModelRouteBinding
	if err := app.db.First(&recoveredBinding, binding.ID).Error; err != nil {
		t.Fatalf("load recovered binding: %v", err)
	}
	if schedulerBindingState(recoveredBinding) != schedulerStateClosed || recoveredBinding.FailureCount != 0 || recoveredBinding.CooldownUntil != nil || recoveredBinding.LastFailureAt != nil {
		t.Fatalf("expected binding cooldown fields cleared, got %+v", recoveredBinding)
	}
}

func TestSchedulerSmoothWeightedRoundRobinUsesRoutingWeight(t *testing.T) {
	app := testApp(t)
	primary, backup, _ := createSchedulerModel(t, app, "weighted-scheduler-model", 3, 1)

	hits := map[uint]int{}
	for i := 0; i < 4; i++ {
		targets, err := app.routeTargets("weighted-scheduler-model", relayProtocolOpenAI)
		if err != nil {
			t.Fatalf("route targets: %v", err)
		}
		if len(targets) == 0 {
			t.Fatalf("expected route targets")
		}
		hits[targets[0].Source.ID]++
	}
	if hits[primary.ID] != 3 || hits[backup.ID] != 1 {
		t.Fatalf("expected 3:1 weighted schedule, got primary=%d backup=%d", hits[primary.ID], hits[backup.ID])
	}
}

func TestSchedulerFailureLadderAndHalfOpenProbe(t *testing.T) {
	app := testApp(t)
	primary, _, _ := createSchedulerModel(t, app, "failure-ladder-model", 5, 1)
	target := firstSchedulerTarget(t, app, "failure-ladder-model")
	if target.Source.ID != primary.ID {
		t.Fatalf("expected primary target first, got %+v", target.Source)
	}

	app.markTargetFailure(target, http.StatusBadGateway)
	binding := loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateClosed || binding.FailureCount != 1 || binding.CooldownUntil != nil {
		t.Fatalf("first failure should stay closed without cooldown, got %+v", binding)
	}

	app.markTargetFailure(target, http.StatusBadGateway)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateOpen || binding.FailureCount != 2 {
		t.Fatalf("second failure should open breaker, got %+v", binding)
	}
	assertCooldownNear(t, binding.CooldownUntil, schedulerShortCooldown)

	app.markTargetFailure(target, http.StatusBadGateway)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	assertCooldownNear(t, binding.CooldownUntil, schedulerMediumCooldown)

	app.markTargetFailure(target, http.StatusBadGateway)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	assertCooldownNear(t, binding.CooldownUntil, schedulerLongCooldown)

	past := time.Now().Add(-time.Second)
	if err := app.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID).Updates(map[string]any{
		"scheduler_state": schedulerStateOpen,
		"cooldown_until":  past,
	}).Error; err != nil {
		t.Fatalf("expire cooldown: %v", err)
	}
	app.resetSchedulerMemory()
	targets, err := app.routeTargets("failure-ladder-model", relayProtocolOpenAI)
	if err != nil {
		t.Fatalf("route half-open target: %v", err)
	}
	if len(targets) == 0 || targets[0].Binding.ID != target.Binding.ID {
		t.Fatalf("expected expired binding to be probed first, got %+v", targets)
	}
	binding = loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateHalfOpen {
		t.Fatalf("expected expired open binding to become half-open, got %+v", binding)
	}

	app.markTargetFailure(targets[0], http.StatusBadGateway)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateOpen {
		t.Fatalf("half-open failure should reopen breaker, got %+v", binding)
	}
	assertCooldownNear(t, binding.CooldownUntil, schedulerLongCooldown)
}

func TestSchedulerRecoveringSuccessAndFailure(t *testing.T) {
	app := testApp(t)
	_, _, _ = createSchedulerModel(t, app, "recovering-scheduler-model", 5, 1)
	target := firstSchedulerTarget(t, app, "recovering-scheduler-model")
	if err := app.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID).Updates(map[string]any{
		"scheduler_state": schedulerStateHalfOpen,
		"failure_count":   4,
		"success_streak":  0,
		"cooldown_until":  nil,
	}).Error; err != nil {
		t.Fatalf("set half-open binding: %v", err)
	}

	app.markTargetSuccess(target)
	binding := loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateRecovering || binding.SuccessStreak != 1 || binding.CooldownUntil != nil {
		t.Fatalf("half-open success should enter recovering, got %+v", binding)
	}
	app.markTargetSuccess(target)
	app.markTargetSuccess(target)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateClosed || binding.FailureCount != 0 || binding.SuccessStreak != 0 || binding.CooldownUntil != nil {
		t.Fatalf("three recovering successes should close breaker, got %+v", binding)
	}

	if err := app.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID).Updates(map[string]any{
		"scheduler_state": schedulerStateRecovering,
		"failure_count":   2,
		"success_streak":  1,
		"cooldown_until":  nil,
	}).Error; err != nil {
		t.Fatalf("set recovering binding: %v", err)
	}
	app.markTargetFailure(target, http.StatusBadGateway)
	binding = loadRouteBinding(t, app, target.Binding.ID)
	if schedulerBindingState(binding) != schedulerStateOpen || binding.SuccessStreak != 0 {
		t.Fatalf("recovering failure should reopen breaker, got %+v", binding)
	}
	assertCooldownNear(t, binding.CooldownUntil, schedulerLongCooldown)
}

func TestModelBindingUpdateResetsSchedulerCooldownAndAppliesWeight(t *testing.T) {
	app := testApp(t)
	primary, backup, model := createSchedulerModel(t, app, "binding-update-scheduler-model", 10, 1)
	var bindings []ModelRouteBinding
	if err := app.db.Where("model_id = ?", model.ID).Order("id asc").Find(&bindings).Error; err != nil {
		t.Fatalf("load scheduler bindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("binding count = %d, want 2", len(bindings))
	}
	cooldownUntil := time.Now().Add(10 * time.Minute)
	if err := app.db.Model(&ModelRouteBinding{}).Where("id = ?", bindings[0].ID).Updates(map[string]any{
		"scheduler_state": schedulerStateOpen,
		"failure_count":   4,
		"cooldown_until":  cooldownUntil,
	}).Error; err != nil {
		t.Fatalf("cool primary binding: %v", err)
	}

	if err := app.replaceModelBindings(model.ID, []modelBindingRequest{
		{ID: id("mb", bindings[0].ID), SourceID: id("s", primary.ID), SourceKeyID: "default", RoutingWeight: 1},
		{ID: id("mb", bindings[1].ID), SourceID: id("s", backup.ID), SourceKeyID: "default", RoutingWeight: 10},
	}); err != nil {
		t.Fatalf("update scheduler bindings: %v", err)
	}

	refreshed := loadRouteBinding(t, app, bindings[0].ID)
	if schedulerBindingState(refreshed) != schedulerStateClosed || refreshed.FailureCount != 0 || refreshed.CooldownUntil != nil {
		t.Fatalf("expected binding update to reset scheduler state, got %+v", refreshed)
	}
	target := firstSchedulerTarget(t, app, "binding-update-scheduler-model")
	if target.Source.ID != backup.ID {
		t.Fatalf("expected updated higher weight backup to route first, got %+v", target.Source)
	}
}

func createSchedulerModel(t *testing.T, app *App, name string, primaryWeight int, backupWeight int) (UpstreamSource, UpstreamSource, ModelConfig) {
	t.Helper()
	primary := UpstreamSource{Name: name + "_Primary", Type: SourceTypeThirdParty, BaseURL: "https://primary.example.com/v1", APIKey: "primary-key", Priority: 1, Status: SourceStatusOnline}
	backup := UpstreamSource{Name: name + "_Backup", Type: SourceTypeThirdParty, BaseURL: "https://backup.example.com/v1", APIKey: "backup-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&primary).Error; err != nil {
		t.Fatalf("create primary source: %v", err)
	}
	if err := app.db.Create(&backup).Error; err != nil {
		t.Fatalf("create backup source: %v", err)
	}
	model := ModelConfig{SourceID: primary.ID, Name: name, Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: primaryWeight, RoutingEnabled: true}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create scheduler model: %v", err)
	}
	if err := app.replaceModelBindings(model.ID, []modelBindingRequest{
		{SourceID: id("s", primary.ID), SourceKeyID: "default", RoutingWeight: primaryWeight},
		{SourceID: id("s", backup.ID), SourceKeyID: "default", RoutingWeight: backupWeight},
	}); err != nil {
		t.Fatalf("create scheduler bindings: %v", err)
	}
	return primary, backup, model
}

func firstSchedulerTarget(t *testing.T, app *App, modelName string) routeTarget {
	t.Helper()
	targets, err := app.routeTargets(modelName, relayProtocolOpenAI)
	if err != nil {
		t.Fatalf("route targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatalf("expected route targets")
	}
	return targets[0]
}

func loadRouteBinding(t *testing.T, app *App, bindingID uint) ModelRouteBinding {
	t.Helper()
	var binding ModelRouteBinding
	if err := app.db.First(&binding, bindingID).Error; err != nil {
		t.Fatalf("load route binding: %v", err)
	}
	return binding
}

func assertCooldownNear(t *testing.T, until *time.Time, want time.Duration) {
	t.Helper()
	if until == nil {
		t.Fatalf("expected cooldown near %s, got nil", want)
	}
	remaining := time.Until(*until)
	if remaining < want-5*time.Second || remaining > want+5*time.Second {
		t.Fatalf("cooldown remaining = %s, want near %s", remaining, want)
	}
}

func TestUserModelsHideUpstreamNameWhenConfigured(t *testing.T) {
	app := testApp(t)
	if err := app.db.Model(&PlatformSettings{}).Where("id > 0").Update("hide_upstream_name_from_users", true).Error; err != nil {
		t.Fatalf("enable hide upstream setting: %v", err)
	}
	source := UpstreamSource{Name: "Private_Upstream_Name", Type: SourceTypeThirdParty, BaseURL: "https://private.example.com/v1", APIKey: "key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create private source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "private-source-model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create private model: %v", err)
	}

	createTestUser(t, app)
	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodGet, "/api/user/models", userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user models: %d %s", w.Code, w.Body.String())
	}
	rows := decodeBody(t, w)["data"].([]any)
	for _, row := range rows {
		item := row.(map[string]any)
		if item["name"] != "private-source-model" {
			continue
		}
		if item["sourceName"] == "Private_Upstream_Name" || item["source"] == "Private_Upstream_Name" {
			t.Fatalf("expected upstream name to be hidden, got %v", item)
		}
		if item["sourceName"] != "平台中转源" {
			t.Fatalf("unexpected hidden source name: %v", item)
		}
		return
	}
	t.Fatalf("private-source-model not found in %v", rows)
}

func TestUserModelsExposeCurrentScheduledModelAndCandidateCount(t *testing.T) {
	app := testApp(t)
	sourceA := UpstreamSource{Name: "OpenRouter_A", Type: SourceTypeThirdParty, BaseURL: "https://a.example.com/v1", APIKey: "a-key", Priority: 2, Status: SourceStatusOnline}
	sourceB := UpstreamSource{Name: "OpenRouter_B", Type: SourceTypeThirdParty, BaseURL: "https://b.example.com/v1", APIKey: "b-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&sourceA).Error; err != nil {
		t.Fatalf("create source A: %v", err)
	}
	if err := app.db.Create(&sourceB).Error; err != nil {
		t.Fatalf("create source B: %v", err)
	}
	models := []ModelConfig{
		{SourceID: sourceA.ID, Name: "shared-openai-model", DisplayName: "Shared OpenAI Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 100},
		{SourceID: sourceB.ID, Name: "shared-openai-model", DisplayName: "Shared OpenAI Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 1},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create models: %v", err)
	}

	createTestUser(t, app)
	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodGet, "/api/user/models", userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user models: %d %s", w.Code, w.Body.String())
	}
	rows := decodeBody(t, w)["data"].([]any)
	var found []map[string]any
	for _, row := range rows {
		item := row.(map[string]any)
		if item["name"] == "shared-openai-model" {
			found = append(found, item)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one deduplicated shared-openai-model row, got %v", found)
	}
	if found[0]["sourceName"] != "OpenRouter_A" {
		t.Fatalf("expected current scheduled source OpenRouter_A by routing weight, got %v", found[0])
	}
	if found[0]["sourceType"] != SourceTypeThirdParty || found[0]["sourceStatus"] != SourceStatusOnline {
		t.Fatalf("unexpected source metadata: %v", found[0])
	}
	if got := int64(found[0]["routingCandidates"].(float64)); got != 2 {
		t.Fatalf("routingCandidates = %d, want 2", got)
	}
}

func TestRegisterRequiresInviteCode(t *testing.T) {
	app := testApp(t)
	w := performJSON(app, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": "new@example.com", "password": "newpass123",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected missing invite to fail, got %d: %s", w.Code, w.Body.String())
	}

	w = performJSON(app, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": "new@example.com", "password": "newpass123", "inviteCode": "TEAM-DEV-2026",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected invite registration to pass, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	user := body["user"].(map[string]any)
	if user["role"] != RoleUser {
		t.Fatalf("expected user role, got %v", user["role"])
	}

	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w = performJSON(app, http.MethodGet, "/api/admin/users?q=TEAM-DEV-2026", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("search users by invite: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	users := body["data"].([]any)
	if len(users) != 1 {
		t.Fatalf("expected one user for invite search, got %d", len(users))
	}
	created := users[0].(map[string]any)
	if created["email"] != "new@example.com" || created["inviteCode"] != "TEAM-DEV-2026" {
		t.Fatalf("expected registered user invite code in admin list, got %v", created)
	}
}

func TestRegisterRequiresEmailCodeWhenEnabled(t *testing.T) {
	app := testAppWithConfig(t, func(cfg *Config) {
		cfg.RequireEmailVerification = true
	})
	w := performJSON(app, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": "verified@example.com", "password": "newpass123", "inviteCode": "TEAM-DEV-2026",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected missing email code to fail, got %d: %s", w.Code, w.Body.String())
	}

	emailCode := registerEmailCode(t, app, "verified@example.com")
	w = performJSON(app, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": "verified@example.com", "password": "newpass123", "inviteCode": "TEAM-DEV-2026", "emailCode": emailCode,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected invite registration to pass, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDefaultPlatformTimeoutIs120Seconds(t *testing.T) {
	app := testApp(t)
	var settings PlatformSettings
	if err := app.db.First(&settings).Error; err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings.DefaultTimeout != 120 {
		t.Fatalf("expected default timeout 120 seconds, got %d", settings.DefaultTimeout)
	}
}

func TestAdminCreateInviteSupportsCustomUniqueCode(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodPost, "/api/admin/invite-codes", adminToken, map[string]any{
		"code":  "emp-10001",
		"limit": 1,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create custom invite: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	invite := body["data"].(map[string]any)
	if invite["code"] != "EMP-10001" {
		t.Fatalf("expected normalized custom code, got %v", invite["code"])
	}

	w = performJSON(app, http.MethodPost, "/api/admin/invite-codes", adminToken, map[string]any{
		"code":  "EMP-10001",
		"limit": 1,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected duplicate invite to conflict, got %d %s", w.Code, w.Body.String())
	}
}

func TestDeletedInviteCodeCanBeRecreatedAndUsed(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodPost, "/api/admin/invite-codes", adminToken, map[string]any{
		"code":  "emp-reuse",
		"limit": 1,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create invite: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	invite := body["data"].(map[string]any)
	inviteID := invite["id"].(string)

	w = performJSON(app, http.MethodDelete, "/api/admin/invite-codes/"+inviteID, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete invite: %d %s", w.Code, w.Body.String())
	}

	w = performJSON(app, http.MethodPost, "/api/admin/invite-codes", adminToken, map[string]any{
		"code":  "EMP-REUSE",
		"limit": 1,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("recreate invite: %d %s", w.Code, w.Body.String())
	}

	w = performJSON(app, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": "reuse@example.com", "password": "newpass123", "inviteCode": "emp-reuse",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("register with recreated invite: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminCanLoginThroughUserEntry(t *testing.T) {
	app := testApp(t)
	w := performJSON(app, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": testAdminEmail, "password": testAdminPassword, "role": RoleUser,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("admin login through user entry failed: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	user := body["user"].(map[string]any)
	if user["role"] != RoleAdmin {
		t.Fatalf("expected admin role, got %v", user["role"])
	}
}

func TestUserCannotLoginThroughAdminEntry(t *testing.T) {
	app := testApp(t)
	createTestUser(t, app)
	w := performJSON(app, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": testUserEmail, "password": testUserPassword, "role": RoleAdmin,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected user admin-entry login to fail, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSourceAccountTokenUsageCollectsCLIProxyQueue(t *testing.T) {
	usageQueueCalls := 0
	usageTimestamp := time.Now().Format(time.RFC3339)
	cliProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/usage-queue" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mgmt-secret" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		usageQueueCalls++
		if usageQueueCalls > 1 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = fmt.Fprintf(w, `[
			{
				"timestamp":%q,
				"provider":"codex",
				"model":"gpt-5.4",
				"alias":"gpt-5.4",
				"source":"user@example.com",
				"auth_index":"auth-001",
				"request_id":"req-001",
				"tokens":{"input_tokens":1200,"output_tokens":300,"total_tokens":1500},
				"fail":{"status_code":200}
			}
		]`, usageTimestamp)
	}))
	defer cliProxy.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	source := UpstreamSource{
		Name:          "CLIProxyAPI_Test",
		Type:          "CLIProxyAPI",
		BaseURL:       cliProxy.URL,
		ManagementKey: "mgmt-secret",
		Status:        SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	account := SourceAccount{
		SourceID:      source.ID,
		Identifier:    "user@example.com",
		Provider:      "ChatGPT",
		AuthIndex:     "auth-001",
		Status:        "valid",
		LastRefreshed: time.Now(),
	}
	if err := app.db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	w := performJSON(app, http.MethodGet, "/api/admin/source-accounts/"+id("a", account.ID)+"/token-usage", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token usage status = %d body=%s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	data := body["data"].(map[string]any)
	if got := int64(data["totalTokens"].(float64)); got != 1500 {
		t.Fatalf("totalTokens = %d, want 1500", got)
	}
	if got := int64(data["monthTokens"].(float64)); got != 1500 {
		t.Fatalf("monthTokens = %d, want 1500", got)
	}
	if got := int64(data["syncedCount"].(float64)); got != 1 {
		t.Fatalf("syncedCount = %d, want 1", got)
	}
}

func TestAPIKeyProxyRecordsUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{map[string]any{"id": "gpt-4o"}}})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl_test", "object": "chat.completion",
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
				"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10},
				"model":   "gpt-4o",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	app := testApp(t)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url": upstream.URL + "/v1",
		"status":   SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	key := createRelayAPIKey(t, app)

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy request: %d %s", w.Code, w.Body.String())
	}
	var count int64
	if err := app.db.Model(&UsageLog{}).Where("model = ? AND total_tokens = ?", "gpt-4o", 10).Count(&count).Error; err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one usage row, got %d", count)
	}
}

func TestExtractUsageRecordsCacheTokenVariants(t *testing.T) {
	cases := []struct {
		name                        string
		body                        string
		wantPrompt                  int64
		wantCompletion              int64
		wantCacheRead               int64
		wantCacheWrite              int64
		wantReasoning               int64
		wantTotal                   int64
		wantPromptIncludesCacheRead bool
	}{
		{
			name:                        "OpenAI prompt token details",
			body:                        `{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":7}}}`,
			wantPrompt:                  100,
			wantCompletion:              20,
			wantCacheRead:               40,
			wantReasoning:               7,
			wantTotal:                   120,
			wantPromptIncludesCacheRead: true,
		},
		{
			name:                        "OpenAI responses nested input details",
			body:                        `{"response":{"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":5}}}}`,
			wantPrompt:                  100,
			wantCompletion:              20,
			wantCacheRead:               30,
			wantReasoning:               5,
			wantTotal:                   120,
			wantPromptIncludesCacheRead: true,
		},
		{
			name:           "Anthropic native cache read and write",
			body:           `{"usage":{"input_tokens":11,"output_tokens":5,"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}`,
			wantPrompt:     11,
			wantCompletion: 5,
			wantCacheRead:  7,
			wantCacheWrite: 3,
			wantTotal:      16,
		},
		{
			name:                        "Claude OpenAI style cache creation split",
			body:                        `{"usage":{"prompt_tokens":130,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":30,"cached_creation_tokens":50},"claude_cache_creation_5_m_tokens":20,"claude_cache_creation_1_h_tokens":40}}`,
			wantPrompt:                  130,
			wantCompletion:              8,
			wantCacheRead:               30,
			wantCacheWrite:              60,
			wantTotal:                   138,
			wantPromptIncludesCacheRead: true,
		},
		{
			name: "Anthropic stream split usage",
			body: "event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"cache_read_input_tokens\":7,\"cache_creation_input_tokens\":3}}}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n",
			wantPrompt:     11,
			wantCompletion: 5,
			wantCacheRead:  7,
			wantCacheWrite: 3,
			wantTotal:      16,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractUsage([]byte(tc.body))
			if got.Prompt != tc.wantPrompt {
				t.Fatalf("Prompt = %d, want %d", got.Prompt, tc.wantPrompt)
			}
			if got.Completion != tc.wantCompletion {
				t.Fatalf("Completion = %d, want %d", got.Completion, tc.wantCompletion)
			}
			if got.CacheRead != tc.wantCacheRead {
				t.Fatalf("CacheRead = %d, want %d", got.CacheRead, tc.wantCacheRead)
			}
			if got.CacheWrite != tc.wantCacheWrite {
				t.Fatalf("CacheWrite = %d, want %d", got.CacheWrite, tc.wantCacheWrite)
			}
			if got.Reasoning != tc.wantReasoning {
				t.Fatalf("Reasoning = %d, want %d", got.Reasoning, tc.wantReasoning)
			}
			if got.Total != tc.wantTotal {
				t.Fatalf("Total = %d, want %d", got.Total, tc.wantTotal)
			}
			if got.PromptIncludesCacheRead != tc.wantPromptIncludesCacheRead {
				t.Fatalf("PromptIncludesCacheRead = %v, want %v", got.PromptIncludesCacheRead, tc.wantPromptIncludesCacheRead)
			}
		})
	}
}

func TestEstimateCostDetailedDoesNotSubtractSeparateAnthropicCache(t *testing.T) {
	model := ModelConfig{BillingInput: 10, BillingOutput: 20, BillingCacheRead: 1, BillingCacheWrite: 2}
	openAIUsage := usageTokens{Prompt: 1_000_000, Completion: 100_000, CacheRead: 400_000, PromptIncludesCacheRead: true, Total: 1_100_000}
	anthropicUsage := usageTokens{Prompt: 1_000_000, Completion: 100_000, CacheRead: 400_000, Total: 1_100_000}

	if got := estimateCostDetailed(openAIUsage, model); got != 8.4 {
		t.Fatalf("OpenAI-style cost = %.2f, want 8.40", got)
	}
	if got := estimateCostDetailed(anthropicUsage, model); got != 12.4 {
		t.Fatalf("Anthropic-style cost = %.2f, want 12.40", got)
	}
}

func TestAnthropicMessagesUseNativeDirectSource(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "anthropic-secret" {
			t.Fatalf("missing anthropic api key header: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("authorization should not be forwarded to anthropic source")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("unexpected anthropic version: %q", r.Header.Get("anthropic-version"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"model":   "claude-direct-test",
			"usage":   map[string]any{"input_tokens": 11, "output_tokens": 5, "cache_read_input_tokens": 7, "cache_creation_input_tokens": 3},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "Anthropic_Direct_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL, APIKey: "anthropic-secret", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "claude-direct-test", DisplayName: "Claude Direct Test", Provider: "Anthropic", Formats: ModelFormatAnthropic, BillingInput: 1, BillingOutput: 1, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	key := createRelayAPIKey(t, app)
	w := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "claude-direct-test",
		"max_tokens": 8,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("anthropic proxy request: %d %s", w.Code, w.Body.String())
	}
	var count int64
	if err := app.db.Model(&UsageLog{}).Where("model = ? AND total_tokens = ?", "claude-direct-test", 16).Count(&count).Error; err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one usage row, got %d", count)
	}
	var log UsageLog
	if err := app.db.Where("model = ?", "claude-direct-test").First(&log).Error; err != nil {
		t.Fatalf("load usage row: %v", err)
	}
	if log.CacheReadTokens != 7 {
		t.Fatalf("cache read tokens = %d, want 7", log.CacheReadTokens)
	}
	if log.CacheWriteTokens != 3 {
		t.Fatalf("cache write tokens = %d, want 3", log.CacheWriteTokens)
	}
}

func TestCLIProxyAnthropicUsesProviderSpecificPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/provider/anthropic/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cliproxy-secret" {
			t.Fatalf("unexpected authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 4, "output_tokens": 2},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url": upstream.URL + "/v1",
		"api_key":  "cliproxy-secret",
		"status":   SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	key := createRelayAPIKey(t, app)
	w := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "claude-3-5-sonnet",
		"max_tokens": 8,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("cliproxy anthropic proxy request: %d %s", w.Code, w.Body.String())
	}
}

func TestUserInvokeTestModelCallsCLIProxyAnthropicModel(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/provider/anthropic/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cliproxy-secret" {
			t.Fatalf("unexpected authorization: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("unexpected anthropic version: %q", r.Header.Get("anthropic-version"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "claude-3-5-sonnet" {
			t.Fatalf("unexpected model: %v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"model":   "claude-3-5-sonnet",
			"usage":   map[string]any{"input_tokens": 2, "output_tokens": 1},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url": upstream.URL + "/v1",
		"api_key":  "cliproxy-secret",
		"status":   SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}
	var model ModelConfig
	if err := app.db.Where("name = ?", "claude-3-5-sonnet").First(&model).Error; err != nil {
		t.Fatalf("load model: %v", err)
	}

	createTestUser(t, app)
	userToken := loginToken(t, app, testUserEmail, testUserPassword, RoleUser)
	w := performJSON(app, http.MethodPost, "/api/user/models/"+id("m", model.ID)+"/invoke-test", userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("invoke test request: %d %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatalf("expected upstream to be called")
	}
	body := decodeBody(t, w)
	data := body["data"].(map[string]any)
	if got := int64(data["totalTokens"].(float64)); got != 3 {
		t.Fatalf("totalTokens = %d, want 3", got)
	}
}

func TestGeminiNativeUsesGeminiAuthAndStripsClientKeyQuery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-direct-test:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("expected alt query to be preserved, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("key") != "" {
			t.Fatalf("client key query leaked upstream: %q", r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != "gemini-secret" {
			t.Fatalf("missing gemini api key header: %q", r.Header.Get("x-goog-api-key"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("authorization should not be forwarded to gemini source")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "ok"}}}}},
			"usageMetadata": map[string]any{"promptTokenCount": 13, "candidatesTokenCount": 7, "totalTokenCount": 20},
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "Gemini_Direct_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1beta", APIKey: "gemini-secret", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "gemini-direct-test", DisplayName: "Gemini Direct Test", Provider: "Google", Formats: ModelFormatOpenAI, BillingInput: 1, BillingOutput: 1, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	key := createRelayAPIKey(t, app)
	w := performJSON(app, http.MethodPost, "/v1beta/models/gemini-direct-test:generateContent?alt=sse&key="+key, "", map[string]any{
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("gemini proxy request: %d %s", w.Code, w.Body.String())
	}
	var count int64
	if err := app.db.Model(&UsageLog{}).Where("model = ? AND total_tokens = ?", "gemini-direct-test", 20).Count(&count).Error; err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one usage row, got %d", count)
	}
}

func TestNormalizeSourceBaseURLStripsProtocolSuffix(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:8081/v1":                         "http://127.0.0.1:8081",
		"https://openrouter.ai/api/v1":                     "https://openrouter.ai/api",
		"https://relay.example/api/provider/anthropic/v1":  "https://relay.example",
		"https://relay.example/api/provider/google/v1beta": "https://relay.example",
	}
	for input, expected := range cases {
		if got := normalizeSourceBaseURL(input); got != expected {
			t.Fatalf("normalizeSourceBaseURL(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestSourceHealthCheckPingsConfiguredURLOnly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected health path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("health check should not send authorization, got %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{}})
	}))
	defer upstream.Close()

	app := testAppWithConfig(t, func(cfg *Config) {
		cfg.CLIProxyAPIBaseURL = upstream.URL + "/v1"
		cfg.CLIProxyAPIAPIKey = "relay-env-key"
		cfg.CLIProxyAPIManagementKey = "management-env-key"
	})
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	var builtIn UpstreamSource
	if err := app.db.Where("type = ?", SourceTypeCLIProxyAPI).First(&builtIn).Error; err != nil {
		t.Fatalf("load built-in cliproxy source: %v", err)
	}
	if builtIn.BaseURL != upstream.URL {
		t.Fatalf("expected env-configured root apiBase, got %v", builtIn.BaseURL)
	}
	if builtIn.APIKey != "relay-env-key" || builtIn.ManagementKey != "management-env-key" {
		t.Fatalf("expected env-configured CLIProxyAPI keys, got api=%q management=%q", builtIn.APIKey, builtIn.ManagementKey)
	}
	w := performJSON(app, http.MethodPost, "/api/admin/sources/"+id("s", builtIn.ID)+"/check", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("check source: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	source := body["data"].(map[string]any)
	if source["status"] != SourceStatusOnline {
		t.Fatalf("expected online health status, got %v", source["status"])
	}
}

func TestAdminCannotCreateCLIProxyAPISource(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodPost, "/api/admin/sources", adminToken, map[string]any{
		"name":    "CLIProxyAPI_Extra",
		"type":    SourceTypeCLIProxyAPI,
		"apiBase": "http://127.0.0.1:8317",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected CLIProxyAPI source create to fail, got %d %s", w.Code, w.Body.String())
	}
}

func TestAdminCannotUpdateBuiltInCLIProxyAPISecrets(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	var builtIn UpstreamSource
	if err := app.db.Where("type = ?", SourceTypeCLIProxyAPI).First(&builtIn).Error; err != nil {
		t.Fatalf("load built-in cliproxy source: %v", err)
	}
	path := "/api/admin/sources/" + id("s", builtIn.ID)
	for _, payload := range []map[string]any{
		{"apiKey": "relay-secret"},
		{"managementKey": "mgmt-secret"},
	} {
		w := performJSON(app, http.MethodPut, path, adminToken, payload)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected built-in CLIProxyAPI secret update to fail, got %d %s", w.Code, w.Body.String())
		}
	}
}

func TestSourceHealthCheckTreatsHTTPErrorAsReachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic" {
			t.Fatalf("unexpected health path: %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	w := performJSON(app, http.MethodPost, "/api/admin/sources", adminToken, map[string]any{
		"name":    "DeepSeek_Anthropic_Health_Test",
		"type":    SourceTypeThirdParty,
		"apiBase": upstream.URL + "/anthropic",
		"apiKey":  "health-secret",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create source: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	sourceID := body["data"].(map[string]any)["id"].(string)
	w = performJSON(app, http.MethodPost, "/api/admin/sources/"+sourceID+"/check", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("check source: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	source := body["data"].(map[string]any)
	if source["status"] != SourceStatusOnline {
		t.Fatalf("expected online health status for reachable 404 URL, got %v", source["status"])
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("expected no health error for reachable 404 URL, got %v", body["error"])
	}
}

func TestModelBoundSourceKeyUsesSelectedCredential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer bound-key" {
			t.Fatalf("expected bound source key, got authorization %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_bound_key",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
			"model":   "openrouter-bound-test",
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "OpenRouter_Key_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "default-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	sourceKey := SourceKey{SourceID: source.ID, Alias: "team-a", APIKey: "bound-key", Status: APIKeyStatusValid}
	if err := app.db.Create(&sourceKey).Error; err != nil {
		t.Fatalf("create source key: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, SourceKeyID: &sourceKey.ID, Name: "openrouter-bound-test", DisplayName: "OpenRouter Bound Test", Provider: "OpenAI", Formats: ModelFormatOpenAI, BillingInput: 1, BillingOutput: 1, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	key := createRelayAPIKey(t, app)
	w := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model": "openrouter-bound-test",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy request: %d %s", w.Code, w.Body.String())
	}
	var refreshed SourceKey
	if err := app.db.First(&refreshed, sourceKey.ID).Error; err != nil {
		t.Fatalf("load source key: %v", err)
	}
	if refreshed.LastUsedAt == nil {
		t.Fatalf("expected source key last used timestamp")
	}
}

func TestOpenAIResponsesProxy(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			t.Fatalf("expected upstream key, got authorization %q", r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if payload["model"] != "responses-test" {
			t.Fatalf("unexpected model: %v", payload["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_test",
			"object": "response",
			"model":  "responses-test",
			"output": []any{map[string]any{"type": "message", "role": "assistant"}},
			"usage":  map[string]any{"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
			"status": "completed",
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "OpenAI_Responses_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "upstream-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "responses-test", DisplayName: "Responses Test", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	key := createRelayAPIKey(t, app)
	w := performJSON(app, http.MethodPost, "/v1/responses", key, map[string]any{
		"model": "responses-test",
		"input": "Reply with ok.",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy responses request: %d %s", w.Code, w.Body.String())
	}
	if !upstreamCalled {
		t.Fatalf("expected upstream to be called")
	}

	var log UsageLog
	if err := app.db.Where("model = ?", "responses-test").First(&log).Error; err != nil {
		t.Fatalf("load usage log: %v", err)
	}
	if log.PromptTokens != 5 || log.CompletionTokens != 2 || log.TotalTokens != 7 {
		t.Fatalf("unexpected usage: prompt=%d completion=%d total=%d", log.PromptTokens, log.CompletionTokens, log.TotalTokens)
	}
}

func TestOpenAIResponsesProxyAcceptsDoubleSlashPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_double_slash",
			"object": "response",
			"model":  "responses-double-slash-test",
			"usage":  map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
			"status": "completed",
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "OpenAI_Responses_Double_Slash_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "upstream-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "responses-double-slash-test", DisplayName: "Responses Double Slash Test", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"model": "responses-double-slash-test",
		"input": "Reply with ok.",
	})
	req := httptest.NewRequest(http.MethodPost, "http://relay.test//v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+createRelayAPIKey(t, app))
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("proxy double slash responses request: %d %s", w.Code, w.Body.String())
	}
}

func TestProxyFailoverRecordsSingleUsageAndAttempts(t *testing.T) {
	var firstCalls int
	var secondCalls int
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, "temporary upstream failure", http.StatusInternalServerError)
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_failover",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
			"model":   "failover-model",
		})
	}))
	defer secondUpstream.Close()

	app := testApp(t)
	firstSource := UpstreamSource{Name: "Failover_Primary", Type: SourceTypeThirdParty, BaseURL: firstUpstream.URL + "/v1", APIKey: "primary-key", Priority: 1, Status: SourceStatusOnline}
	secondSource := UpstreamSource{Name: "Failover_Backup", Type: SourceTypeThirdParty, BaseURL: secondUpstream.URL + "/v1", APIKey: "backup-key", Priority: 2, Status: SourceStatusOnline}
	if err := app.db.Create(&firstSource).Error; err != nil {
		t.Fatalf("create first source: %v", err)
	}
	if err := app.db.Create(&secondSource).Error; err != nil {
		t.Fatalf("create second source: %v", err)
	}
	models := []ModelConfig{
		{SourceID: firstSource.ID, Name: "failover-model", DisplayName: "Failover Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
		{SourceID: secondSource.ID, Name: "failover-model", DisplayName: "Failover Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create models: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "failover-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy failover request: %d %s", w.Code, w.Body.String())
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("expected both upstreams to be attempted once, primary=%d backup=%d", firstCalls, secondCalls)
	}
	var logs []UsageLog
	if err := app.db.Where("model = ?", "failover-model").Find(&logs).Error; err != nil {
		t.Fatalf("load usage logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one final usage log, got %d", len(logs))
	}
	if logs[0].SourceID != secondSource.ID || logs[0].Status != RequestStatusSuccess || logs[0].TotalTokens != 5 {
		t.Fatalf("unexpected final usage log: %+v", logs[0])
	}
	var attempts []RequestAttempt
	if err := app.db.Where("usage_log_id = ?", logs[0].ID).Order("attempt_index asc").Find(&attempts).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected two attempts, got %d", len(attempts))
	}
	if attempts[0].SourceID != firstSource.ID || attempts[0].StatusCode != http.StatusInternalServerError || attempts[0].Status != RequestStatusError {
		t.Fatalf("unexpected first attempt: %+v", attempts[0])
	}
	if attempts[1].SourceID != secondSource.ID || attempts[1].StatusCode != http.StatusOK || attempts[1].Status != RequestStatusSuccess {
		t.Fatalf("unexpected second attempt: %+v", attempts[1])
	}
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	attemptResp := performJSON(app, http.MethodGet, "/api/admin/logs/"+id("log", logs[0].ID)+"/attempts", adminToken, nil)
	if attemptResp.Code != http.StatusOK {
		t.Fatalf("log attempts endpoint: %d %s", attemptResp.Code, attemptResp.Body.String())
	}
	attemptPayload := decodeBody(t, attemptResp)["data"].([]any)
	if len(attemptPayload) != 2 {
		t.Fatalf("expected attempts endpoint to return two attempts, got %v", attemptPayload)
	}
	if attemptPayload[0].(map[string]any)["sourceId"] != id("s", firstSource.ID) || attemptPayload[1].(map[string]any)["sourceId"] != id("s", secondSource.ID) {
		t.Fatalf("unexpected attempts endpoint source order: %v", attemptPayload)
	}
	logResp := performJSON(app, http.MethodGet, "/api/admin/logs?page=1&pageSize=1&model=failover-model", adminToken, nil)
	if logResp.Code != http.StatusOK {
		t.Fatalf("admin log list: %d %s", logResp.Code, logResp.Body.String())
	}
	logPayload := decodeBody(t, logResp)["data"].([]any)
	if len(logPayload) != 1 {
		t.Fatalf("expected one log row, got %v", logPayload)
	}
	logRow := logPayload[0].(map[string]any)
	if logRow["protocol"] != string(relayProtocolOpenAI) || logRow["path"] != "/v1/chat/completions" || logRow["attemptCount"] != float64(2) {
		t.Fatalf("unexpected log detail fields: %v", logRow)
	}
	requestHeaders := logRow["requestHeaders"].(map[string]any)
	if requestHeaders["Authorization"] != "<redacted>" {
		t.Fatalf("expected authorization header to be redacted, got %v", requestHeaders["Authorization"])
	}
	var refreshed UpstreamSource
	if err := app.db.First(&refreshed, firstSource.ID).Error; err != nil {
		t.Fatalf("load primary source: %v", err)
	}
	if refreshed.FailureCount == 0 || refreshed.CooldownUntil != nil {
		t.Fatalf("expected primary source failure count without source-level cooldown, got failure=%d cooldown=%v", refreshed.FailureCount, refreshed.CooldownUntil)
	}
}

func TestProxyRoutesEnabledModelWhenRoutingFlagIsFalse(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_enabled_routing_false",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4},
			"model":   "enabled-routing-false-model",
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "Enabled_Routing_False_Source", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "upstream-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "enabled-routing-false-model", DisplayName: "Enabled Routing False Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive, RoutingWeight: 1, RoutingEnabled: true}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}
	if err := app.db.Model(&ModelConfig{}).Where("id = ?", model.ID).Update("routing_enabled", false).Error; err != nil {
		t.Fatalf("disable legacy routing flag: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "enabled-routing-false-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy request: %d %s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected upstream to be called once, got %d", calls)
	}
	var log UsageLog
	if err := app.db.Where("model = ?", "enabled-routing-false-model").First(&log).Error; err != nil {
		t.Fatalf("load usage log: %v", err)
	}
	if log.SourceID != source.ID || log.TotalTokens != 4 || log.Status != RequestStatusSuccess {
		t.Fatalf("unexpected usage log: %+v", log)
	}
}

func TestProxySkipsCoolingSource(t *testing.T) {
	var coolingCalls int
	var backupCalls int
	coolingUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coolingCalls++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer coolingUpstream.Close()
	backupUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_cooldown",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			"model":   "cooldown-model",
		})
	}))
	defer backupUpstream.Close()

	app := testApp(t)
	cooldownUntil := time.Now().Add(10 * time.Minute)
	coolingSource := UpstreamSource{Name: "Cooling_Primary", Type: SourceTypeThirdParty, BaseURL: coolingUpstream.URL + "/v1", APIKey: "cooling-key", Priority: 1, Status: SourceStatusOnline, FailureCount: 3, CooldownUntil: &cooldownUntil}
	backupSource := UpstreamSource{Name: "Cooling_Backup", Type: SourceTypeThirdParty, BaseURL: backupUpstream.URL + "/v1", APIKey: "backup-key", Priority: 2, Status: SourceStatusOnline}
	if err := app.db.Create(&coolingSource).Error; err != nil {
		t.Fatalf("create cooling source: %v", err)
	}
	if err := app.db.Create(&backupSource).Error; err != nil {
		t.Fatalf("create backup source: %v", err)
	}
	models := []ModelConfig{
		{SourceID: coolingSource.ID, Name: "cooldown-model", DisplayName: "Cooldown Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
		{SourceID: backupSource.ID, Name: "cooldown-model", DisplayName: "Cooldown Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create models: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "cooldown-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy cooldown request: %d %s", w.Code, w.Body.String())
	}
	if coolingCalls != 0 || backupCalls != 1 {
		t.Fatalf("expected cooling source to be skipped and backup called once, cooling=%d backup=%d", coolingCalls, backupCalls)
	}
	var log UsageLog
	if err := app.db.Where("model = ?", "cooldown-model").First(&log).Error; err != nil {
		t.Fatalf("load usage log: %v", err)
	}
	var attempts []RequestAttempt
	if err := app.db.Where("usage_log_id = ?", log.ID).Find(&attempts).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].SourceID != backupSource.ID {
		t.Fatalf("expected one backup attempt, got %+v", attempts)
	}
}

func TestProxySingleSourceRoutesDirectlyWithStaleCooldown(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_single_cooldown",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			"model":   "single-cooldown-model",
		})
	}))
	defer upstream.Close()

	app := testApp(t)
	cooldownUntil := time.Now().Add(10 * time.Minute)
	source := UpstreamSource{Name: "Single_Cooling_Source", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "single-key", Priority: 1, Status: SourceStatusOnline, FailureCount: 2, CooldownUntil: &cooldownUntil}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "single-cooldown-model", DisplayName: "Single Cooldown Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "single-cooldown-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy single cooldown request: %d %s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected single direct source to be called once, got %d", calls)
	}
	var refreshed UpstreamSource
	if err := app.db.First(&refreshed, source.ID).Error; err != nil {
		t.Fatalf("load source: %v", err)
	}
	if refreshed.CooldownUntil != nil || refreshed.FailureCount != 0 || refreshed.Status != SourceStatusOnline {
		t.Fatalf("expected single direct source success to clear stale cooldown, got status=%s failure=%d cooldown=%v", refreshed.Status, refreshed.FailureCount, refreshed.CooldownUntil)
	}
}

func TestProxySingleSourceFailureDoesNotCooldown(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "temporary single upstream failure", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{Name: "Single_Failing_Source", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "single-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	model := ModelConfig{SourceID: source.ID, Name: "single-failure-model", DisplayName: "Single Failure Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}
	if err := app.db.Create(&model).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "single-failure-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected upstream 500 to pass through, got %d %s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected single source to be called once, got %d", calls)
	}
	var refreshed UpstreamSource
	if err := app.db.First(&refreshed, source.ID).Error; err != nil {
		t.Fatalf("load source: %v", err)
	}
	if refreshed.FailureCount == 0 || refreshed.CooldownUntil != nil {
		t.Fatalf("expected failure count without cooldown, got failure=%d cooldown=%v", refreshed.FailureCount, refreshed.CooldownUntil)
	}
}

func TestProxyTimeoutFailoverCoolsPrimary(t *testing.T) {
	var primaryCalls int
	var backupCalls int
	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		time.Sleep(1500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":"late"}`))
	}))
	defer primaryUpstream.Close()
	backupUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_timeout_failover",
			"object":  "chat.completion",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
			"model":   "timeout-failover-model",
		})
	}))
	defer backupUpstream.Close()

	app := testApp(t)
	if err := app.db.Model(&PlatformSettings{}).Where("id > 0").Update("default_timeout", 1).Error; err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	primarySource := UpstreamSource{Name: "Timeout_Primary", Type: SourceTypeThirdParty, BaseURL: primaryUpstream.URL + "/v1", APIKey: "primary-key", Priority: 1, Status: SourceStatusOnline}
	backupSource := UpstreamSource{Name: "Timeout_Backup", Type: SourceTypeThirdParty, BaseURL: backupUpstream.URL + "/v1", APIKey: "backup-key", Priority: 2, Status: SourceStatusOnline}
	if err := app.db.Create(&primarySource).Error; err != nil {
		t.Fatalf("create primary source: %v", err)
	}
	if err := app.db.Create(&backupSource).Error; err != nil {
		t.Fatalf("create backup source: %v", err)
	}
	models := []ModelConfig{
		{SourceID: primarySource.ID, Name: "timeout-failover-model", DisplayName: "Timeout Failover Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
		{SourceID: backupSource.ID, Name: "timeout-failover-model", DisplayName: "Timeout Failover Model", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create models: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":    "timeout-failover-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy timeout failover request: %d %s", w.Code, w.Body.String())
	}
	if primaryCalls != 1 || backupCalls != 1 {
		t.Fatalf("expected primary timeout and backup success, primary=%d backup=%d", primaryCalls, backupCalls)
	}
	var refreshed UpstreamSource
	if err := app.db.First(&refreshed, primarySource.ID).Error; err != nil {
		t.Fatalf("load primary source: %v", err)
	}
	if refreshed.FailureCount == 0 || refreshed.CooldownUntil != nil {
		t.Fatalf("expected primary timeout to update source failure count without source-level cooldown, got failure=%d cooldown=%v", refreshed.FailureCount, refreshed.CooldownUntil)
	}
	var log UsageLog
	if err := app.db.Where("model = ?", "timeout-failover-model").First(&log).Error; err != nil {
		t.Fatalf("load timeout usage log: %v", err)
	}
	if log.AttemptCount != 2 || log.SourceID != backupSource.ID {
		t.Fatalf("expected final log to record two attempts and backup source, got %+v", log)
	}
}

func TestStreamingProxyDoesNotUseWholeRequestTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(1100 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	app := testApp(t)
	if err := app.db.Model(&PlatformSettings{}).Where("id > 0").Update("default_timeout", 1).Error; err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	source := UpstreamSource{Name: "OpenAI_Stream_Timeout_Test", Type: SourceTypeThirdParty, BaseURL: upstream.URL + "/v1", APIKey: "upstream-key", Priority: 1, Status: SourceStatusOnline}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := app.db.Create(&ModelConfig{SourceID: source.ID, Name: "stream-timeout-test", DisplayName: "Stream Timeout Test", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive}).Error; err != nil {
		t.Fatalf("create model: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/v1/chat/completions", createRelayAPIKey(t, app), map[string]any{
		"model":  "stream-timeout-test",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("proxy stream request: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "second") {
		t.Fatalf("expected delayed stream chunk to pass through, got %q", w.Body.String())
	}
}

func TestThirdPartySourceUsesConfiguredProtocolBaseURLs(t *testing.T) {
	seenOpenAI := false
	seenAnthropic := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openai/v1/chat/completions":
			seenOpenAI = true
			if r.Header.Get("Authorization") != "Bearer third-party-key" {
				t.Fatalf("expected openai authorization, got %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl_third_party",
				"object":  "chat.completion",
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
				"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
				"model":   "third-party-openai-test",
			})
		case "/anthropic/v1/messages":
			seenAnthropic = true
			if r.Header.Get("x-api-key") != "third-party-key" {
				t.Fatalf("expected anthropic api key, got %q", r.Header.Get("x-api-key"))
			}
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("authorization should not be forwarded to anthropic source")
			}
			if r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Fatalf("unexpected anthropic version: %q", r.Header.Get("anthropic-version"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "msg_third_party",
				"type":    "message",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "text", "text": "ok"}},
				"model":   "third-party-anthropic-test",
				"usage":   map[string]any{"input_tokens": 5, "output_tokens": 4},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	app := testApp(t)
	source := UpstreamSource{
		Name:             "Third_Party_Protocol_Test",
		Type:             SourceTypeThirdParty,
		BaseURL:          upstream.URL + "/base",
		OpenAIBaseURL:    upstream.URL + "/openai/v1",
		AnthropicBaseURL: upstream.URL + "/anthropic",
		APIKey:           "third-party-key",
		Priority:         1,
		Status:           SourceStatusOnline,
	}
	if err := app.db.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	models := []ModelConfig{
		{SourceID: source.ID, Name: "third-party-openai-test", DisplayName: "Third Party OpenAI Test", Provider: "OpenAI", Formats: ModelFormatOpenAI, Status: ModelStatusActive},
		{SourceID: source.ID, Name: "third-party-anthropic-test", DisplayName: "Third Party Anthropic Test", Provider: "Anthropic", Formats: ModelFormatAnthropic, Status: ModelStatusActive},
	}
	if err := app.db.Create(&models).Error; err != nil {
		t.Fatalf("create models: %v", err)
	}

	key := createRelayAPIKey(t, app)
	openAIResp := performJSON(app, http.MethodPost, "/v1/chat/completions", key, map[string]any{
		"model":    "third-party-openai-test",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if openAIResp.Code != http.StatusOK {
		t.Fatalf("openai proxy request: %d %s", openAIResp.Code, openAIResp.Body.String())
	}
	anthropicResp := performJSON(app, http.MethodPost, "/v1/messages", key, map[string]any{
		"model":      "third-party-anthropic-test",
		"max_tokens": 8,
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if anthropicResp.Code != http.StatusOK {
		t.Fatalf("anthropic proxy request: %d %s", anthropicResp.Code, anthropicResp.Body.String())
	}
	if !seenOpenAI || !seenAnthropic {
		t.Fatalf("expected both protocol endpoints to be called, openai=%v anthropic=%v", seenOpenAI, seenAnthropic)
	}
}

func TestCLIProxyAccountSyncAndOAuth(t *testing.T) {
	var seenUsageAccountID string
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer codex-access-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accounts": map[string]any{
					"cgpt-usage": map[string]any{
						"account": map[string]any{
							"plan_type":        "pro",
							"has_subscription": true,
							"is_default":       true,
						},
						"entitlement": map[string]any{
							"subscription_plan": "ChatGPT Pro 5x",
							"expires_at":        "2026-06-01T00:00:00Z",
							"renews_at":         "2026-06-01T00:00:00Z",
						},
					},
				},
			})
		case "/backend-api/wham/usage":
			seenUsageAccountID = r.Header.Get("ChatGPT-Account-ID")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rate_limit": map[string]any{
					"primary_window": map[string]any{
						"used_percent":         25.4,
						"limit_window_seconds": 18_000,
						"reset_at":             1_800_000_000,
					},
					"secondary_window": map[string]any{
						"used_percent":         60.2,
						"limit_window_seconds": 604_800,
						"reset_at":             1_800_360_000,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer usage.Close()
	t.Setenv("RELAY_CODEX_USAGE_BASE_URL", usage.URL+"/backend-api")

	idToken := unsignedTestJWT(map[string]any{
		"workspace_id": "org-usage",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "google-oauth2|user::cgpt=cgpt-usage|ws=org-usage",
		},
	})
	var callbackProvider string
	var callbackRedirect string
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mgmt-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []any{
					map[string]any{
						"name":     "codex.json",
						"provider": "codex",
						"email":    "codex@example.com",
						"status":   "ok",
						"success":  12,
						"failed":   1,
						"recent_requests": []any{
							map[string]any{"time": "12:00-12:10", "success": 3, "failed": 0},
							map[string]any{"time": "12:10-12:20", "success": 1, "failed": 1},
						},
						"last_refresh": "2026-05-24T00:00:00Z",
					},
				},
			})
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") != "codex.json" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":          "codex",
				"email":         "codex@example.com",
				"account_type":  "oauth",
				"id_token":      idToken,
				"access_token":  "codex-access-token",
				"refresh_token": "codex-refresh-token",
			})
		case "/v0/management/codex-auth-url":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "url": "https://auth.example.test", "state": "state123"})
		case "/v0/management/oauth-callback":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if strings.Contains(req["redirect_url"], "state=expired") {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": "unknown or expired state"})
				return
			}
			callbackProvider = req["provider"]
			callbackRedirect = req["redirect_url"]
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "/v0/management/get-auth-status":
			if r.URL.Query().Get("state") != "state123" {
				http.Error(w, "bad state", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer management.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url":       management.URL + "/v1",
		"api_key":        "relay-secret",
		"management_key": "mgmt-secret",
		"status":         SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/sync", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("sync accounts: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one synced account, got %d", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["successCount"] != float64(12) || row["failedCount"] != float64(1) || row["recentRequests"] != float64(5) {
		t.Fatalf("unexpected request counters: %v", row)
	}
	if row["used5h"] != float64(25) || row["limit5h"] != float64(100) || row["used7d"] != float64(60) || row["limit7d"] != float64(100) {
		t.Fatalf("expected codex usage quota, got %v", row)
	}
	if row["provider"] != "ChatGPT" || row["planType"] != "pro_5x" || row["openaiPlanType"] != "pro" || row["subscriptionPlan"] != "pro_5x" || row["hasSubscription"] != true {
		t.Fatalf("expected platform and subscription fields, got %v", row)
	}
	if seenUsageAccountID != "org-usage" {
		t.Fatalf("expected ChatGPT-Account-ID org-usage, got %q", seenUsageAccountID)
	}

	w = performJSON(app, http.MethodPost, "/api/admin/source-accounts/"+row["id"].(string)+"/refresh", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh account: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	refreshed := body["data"].(map[string]any)
	if refreshed["successCount"] != float64(12) || refreshed["recentRequests"] != float64(5) {
		t.Fatalf("expected refresh to sync CLIProxy account stats, got %v", refreshed)
	}
	if refreshed["used5h"] != float64(25) || refreshed["used7d"] != float64(60) {
		t.Fatalf("expected refresh to keep codex quota, got %v", refreshed)
	}

	w = performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/oauth", adminToken, map[string]any{"provider": "ChatGPT"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("oauth: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	if body["authUrl"] != "https://auth.example.test" || body["sessionId"] != "state123" {
		t.Fatalf("unexpected oauth payload: %v", body)
	}

	callbackURL := "http://localhost:1455/auth/callback?code=code123&state=state123"
	w = performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/oauth/callback", adminToken, map[string]any{
		"provider":    "ChatGPT",
		"redirectUrl": callbackURL,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("oauth callback: %d %s", w.Code, w.Body.String())
	}
	if callbackProvider != "codex" || callbackRedirect != callbackURL {
		t.Fatalf("unexpected callback payload: provider=%q redirect=%q", callbackProvider, callbackRedirect)
	}

	expiredURL := "http://localhost:1455/auth/callback?code=code123&state=expired"
	w = performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/oauth/callback", adminToken, map[string]any{
		"provider":    "ChatGPT",
		"redirectUrl": expiredURL,
	})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expired oauth callback: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	if !strings.Contains(body["error"].(string), "session expired") {
		t.Fatalf("expected expired session error, got %v", body)
	}
}

func TestPlanExtractionIgnoresOAuthAccountType(t *testing.T) {
	planType, subscriptionPlan := cliProxyAccountPlans(map[string]any{
		"account_type": "oauth",
		"id_token": map[string]any{
			"account_type": "oauth",
			"https://api.openai.com/auth": map[string]any{
				"account_type": "oauth",
			},
		},
	})
	if planType != "" || subscriptionPlan != "" {
		t.Fatalf("expected oauth account type to be ignored, got plan=%q subscription=%q", planType, subscriptionPlan)
	}
	if normalized := normalizePlanType("oauth"); normalized != "" {
		t.Fatalf("expected unknown plan value to normalize empty, got %q", normalized)
	}
}

func TestAdminSubmitSourceAccountTokenUploadsAuthFile(t *testing.T) {
	var uploadName string
	var uploaded map[string]any
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mgmt-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v0/management/auth-files" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost:
			uploadName = r.URL.Query().Get("name")
			if !strings.HasSuffix(uploadName, ".json") {
				t.Fatalf("expected json upload name, got %q", uploadName)
			}
			if err := json.NewDecoder(r.Body).Decode(&uploaded); err != nil {
				t.Fatalf("decode uploaded auth file: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case http.MethodGet:
			if uploaded == nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []any{
					map[string]any{
						"name":         uploadName,
						"provider":     uploaded["type"],
						"email":        uploaded["email"],
						"status":       "ok",
						"last_refresh": uploaded["last_refresh"],
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer management.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url":       management.URL + "/v1",
		"api_key":        "relay-secret",
		"management_key": "mgmt-secret",
		"status":         SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/token", adminToken, map[string]any{
		"provider":     "Claude",
		"identifier":   "claude@example.com",
		"refreshToken": "claude-refresh-token",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("manual token login: %d %s", w.Code, w.Body.String())
	}
	if uploaded["type"] != "claude" || uploaded["email"] != "claude@example.com" || uploaded["refresh_token"] != "claude-refresh-token" {
		t.Fatalf("unexpected uploaded auth file: %v", uploaded)
	}
	if uploaded["access_token"] != nil || uploaded["id_token"] != nil {
		t.Fatalf("access/id token should not be required for manual refresh token login: %v", uploaded)
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one synced account, got %d", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["provider"] != "Claude" || row["identifier"] != "claude@example.com" {
		t.Fatalf("unexpected synced account: %v", row)
	}
}

func TestAdminSubmitChatGPTTokenUsesAccessTokenForSubscription(t *testing.T) {
	var seenAccountsCheck bool
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer manual-access-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			seenAccountsCheck = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accounts": map[string]any{
					"default": map[string]any{
						"account": map[string]any{
							"plan_type":        "plus",
							"has_subscription": true,
							"is_default":       true,
						},
						"entitlement": map[string]any{
							"subscription_plan":       "ChatGPT Plus",
							"has_active_subscription": true,
						},
					},
				},
			})
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer usage.Close()
	t.Setenv("RELAY_CODEX_USAGE_BASE_URL", usage.URL+"/backend-api")

	var uploaded map[string]any
	var uploadName string
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mgmt-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v0/management/auth-files" && r.Method == http.MethodPost:
			uploadName = r.URL.Query().Get("name")
			if err := json.NewDecoder(r.Body).Decode(&uploaded); err != nil {
				t.Fatalf("decode uploaded auth file: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case r.URL.Path == "/v0/management/auth-files" && r.Method == http.MethodGet:
			if uploaded == nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []any{map[string]any{
					"name":         uploadName,
					"provider":     uploaded["type"],
					"email":        uploaded["email"],
					"status":       "ok",
					"last_refresh": uploaded["last_refresh"],
				}},
			})
		case r.URL.Path == "/v0/management/auth-files/download" && r.Method == http.MethodGet:
			if r.URL.Query().Get("name") != uploadName {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(uploaded)
		default:
			http.NotFound(w, r)
		}
	}))
	defer management.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url":       management.URL + "/v1",
		"api_key":        "relay-secret",
		"management_key": "mgmt-secret",
		"status":         SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/token", adminToken, map[string]any{
		"provider":     "ChatGPT",
		"identifier":   "chatgpt@example.com",
		"refreshToken": "manual-refresh-token",
		"accessToken":  "manual-access-token",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("manual token login: %d %s", w.Code, w.Body.String())
	}
	if uploaded["access_token"] != nil || uploaded["refresh_token"] != "manual-refresh-token" {
		t.Fatalf("expected access token to stay out of CLIProxy auth file, got %v", uploaded)
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one synced account, got %d", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["provider"] != "ChatGPT" || row["openaiPlanType"] != "plus" {
		t.Fatalf("expected official plan from manual access token, got %v", row)
	}
	if row["planType"] != nil || row["subscriptionPlan"] != nil || row["hasSubscription"] != false {
		t.Fatalf("manual access token should not change normalized subscription fields, got %v", row)
	}
	if !seenAccountsCheck {
		t.Fatalf("expected OpenAI accounts/check to be called")
	}
}

func TestPlanExtractionKeepsOfficialChatGPTPlanNames(t *testing.T) {
	tests := map[string]string{
		"ChatGPT Free":       "free",
		"chatgpt_go":         "go",
		"ChatGPT Plus":       "plus",
		"ChatGPT Pro":        "pro_20x",
		"ChatGPT Pro 5x":     "pro_5x",
		"prolite":            "pro_5x",
		"chatgpt-pro-20x":    "pro_20x",
		"ChatGPT Team":       "team",
		"ChatGPT Business":   "business",
		"ChatGPT Enterprise": "enterprise",
		"ChatGPT Edu":        "edu",
		"education":          "edu",
	}
	for input, want := range tests {
		if got := normalizePlanType(input); got != want {
			t.Fatalf("normalizePlanType(%q) = %q, want %q", input, got, want)
		}
	}

	snapshot := parseCodexSubscriptionSnapshot(map[string]any{
		"account": map[string]any{
			"plan_type": "pro",
		},
		"entitlement": map[string]any{
			"product": map[string]any{
				"display_name": "ChatGPT Pro 20x",
			},
			"has_active_subscription": true,
		},
	})
	if snapshot.AccountPlanType != "pro_20x" || snapshot.OpenAIPlanType != "pro" || snapshot.SubscriptionPlan != "pro_20x" {
		t.Fatalf("expected nested official plan name to win, got %+v", snapshot)
	}
}

func TestCLIProxyOAuthRequiresManagementKey(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url":       "http://127.0.0.1:8317",
		"api_key":        "relay-secret",
		"management_key": "",
		"status":         SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/oauth", adminToken, map[string]any{"provider": "ChatGPT"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected oauth to fail without management key, got %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if !strings.Contains(body["error"].(string), "management key is empty") {
		t.Fatalf("unexpected error: %v", body["error"])
	}
}

func TestCLIProxyOAuthReportsUnavailableManagementAPI(t *testing.T) {
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer management.Close()

	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)
	if err := app.db.Model(&UpstreamSource{}).Where("type = ?", SourceTypeCLIProxyAPI).Updates(map[string]any{
		"base_url":       management.URL,
		"api_key":        "relay-secret",
		"management_key": "mgmt-secret",
		"status":         SourceStatusOnline,
	}).Error; err != nil {
		t.Fatalf("update source: %v", err)
	}

	w := performJSON(app, http.MethodPost, "/api/admin/sources/s_001/accounts/oauth", adminToken, map[string]any{"provider": "ChatGPT"})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected unavailable management API to fail as gateway error, got %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if !strings.Contains(body["error"].(string), "management API is unavailable") {
		t.Fatalf("unexpected error: %v", body["error"])
	}
}

func TestDirectSourceKeyAdminLifecycleAndModelBinding(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)

	source := createThirdPartySource(t, app, "OpenRouter_Key_Admin_Test")
	sourceKeyPath := "/api/admin/sources/" + id("s", source.ID) + "/keys"
	w := performJSON(app, http.MethodPost, sourceKeyPath, adminToken, map[string]any{
		"alias": "prod-group-a",
		"key":   "openrouter-prod-key",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create source key: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	created := body["data"].(map[string]any)
	sourceKeyID := created["id"].(string)
	if created["alias"] != "prod-group-a" || created["key"] != nil {
		t.Fatalf("unexpected source key payload: %v", created)
	}

	w = performJSON(app, http.MethodGet, sourceKeyPath, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list source keys: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	if len(body["data"].([]any)) != 1 {
		t.Fatalf("expected one source key, got %v", body["data"])
	}

	w = performJSON(app, http.MethodPost, "/api/admin/models", adminToken, map[string]any{
		"name":          "admin-bound-model",
		"sourceId":      id("s", source.ID),
		"sourceKeyId":   sourceKeyID,
		"provider":      "OpenAI",
		"billingInput":  1,
		"billingOutput": 1,
		"enabled":       true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create bound model: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	model := body["data"].(map[string]any)
	modelID := model["id"].(string)
	if model["sourceKeyAlias"] != "prod-group-a" {
		t.Fatalf("expected source key alias on model, got %v", model)
	}

	w = performJSON(app, http.MethodPut, "/api/admin/models/"+modelID, adminToken, map[string]any{
		"sourceKeyId": "default",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("clear model source key: %d %s", w.Code, w.Body.String())
	}
	body = decodeBody(t, w)
	model = body["data"].(map[string]any)
	if _, ok := model["sourceKeyId"]; ok {
		t.Fatalf("expected source key binding to be cleared, got %v", model)
	}
}

func TestNonCLIProxySourceDoesNotExposeAccountPool(t *testing.T) {
	app := testApp(t)
	adminToken := loginToken(t, app, testAdminEmail, testAdminPassword, RoleAdmin)

	source := createThirdPartySource(t, app, "OpenRouter_No_Account_Pool_Test")
	if source.AccountCount != 0 {
		t.Fatalf("expected OpenRouter account count to be zero, got %d", source.AccountCount)
	}

	sourcePath := "/api/admin/sources/" + id("s", source.ID) + "/accounts"
	w := performJSON(app, http.MethodGet, sourcePath, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	rows := body["data"].([]any)
	if len(rows) != 0 {
		t.Fatalf("expected no accounts for OpenRouter, got %d", len(rows))
	}

	w = performJSON(app, http.MethodPost, sourcePath, adminToken, map[string]any{
		"identifier": "openrouter-key",
		"provider":   "ChatGPT",
		"status":     "valid",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected manual account create to fail, got %d %s", w.Code, w.Body.String())
	}

	w = performJSON(app, http.MethodPost, sourcePath+"/sync", adminToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected account sync to fail, got %d %s", w.Code, w.Body.String())
	}

	w = performJSON(app, http.MethodPost, sourcePath+"/oauth", adminToken, map[string]any{"provider": "ChatGPT"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected oauth to fail, got %d %s", w.Code, w.Body.String())
	}
}
