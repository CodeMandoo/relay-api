package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errInviteCodeExists = errors.New("invite code exists")

func (a *App) adminUsers(c *gin.Context) {
	var users []User
	query := a.db.Order("created_at desc")
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		like := "%" + q + "%"
		query = query.Where("email LIKE ? OR name LIKE ? OR invite_code LIKE ?", like, like, like)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" && role != "all" {
		query = query.Where("role = ?", role)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&users).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	since := monthStart(time.Now())
	out := make([]UserDTO, 0, len(users))
	for _, user := range users {
		out = append(out, userDTO(user, a.userTokenUsage(user.ID, since)))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateUser(c *gin.Context) {
	var req struct {
		Email        string  `json:"email"`
		Name         string  `json:"name"`
		Password     string  `json:"password"`
		Role         string  `json:"role"`
		Status       string  `json:"status"`
		MonthlyQuota int64   `json:"monthlyQuota"`
		WeeklyQuota  int64   `json:"weeklyQuota"`
		Balance      float64 `json:"balance"`
	}
	if !bindJSON(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || strings.TrimSpace(req.Password) == "" {
		errorJSON(c, http.StatusBadRequest, "email and password are required")
		return
	}
	role := req.Role
	if role == "" {
		role = RoleUser
	}
	status := req.Status
	if status == "" {
		status = UserStatusNormal
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "password hashing failed")
		return
	}
	user := User{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         role,
		Status:       status,
		MonthlyQuota: req.MonthlyQuota,
		WeeklyQuota:  req.WeeklyQuota,
		Balance:      req.Balance,
	}
	if err := a.db.Create(&user).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create user failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": userDTO(user, 0)})
}

func (a *App) adminUpdateUser(c *gin.Context) {
	userID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	for _, key := range []string{"name", "role", "status"} {
		if value, ok := req[key].(string); ok && strings.TrimSpace(value) != "" {
			updates[toSnake(key)] = strings.TrimSpace(value)
		}
	}
	if email, ok := req["email"].(string); ok && strings.TrimSpace(email) != "" {
		updates["email"] = strings.ToLower(strings.TrimSpace(email))
	}
	if value, ok := numberFromMap(req, "monthlyQuota"); ok {
		updates["monthly_quota"] = int64(value)
	}
	if value, ok := numberFromMap(req, "weeklyQuota"); ok {
		updates["weekly_quota"] = int64(value)
	}
	if value, ok := numberFromMap(req, "balance"); ok {
		updates["balance"] = value
	}
	if password, ok := req["password"].(string); ok && strings.TrimSpace(password) != "" {
		hash, err := hashPassword(password)
		if err != nil {
			errorJSON(c, http.StatusInternalServerError, "password hashing failed")
			return
		}
		updates["password_hash"] = hash
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update user failed")
		return
	}
	var user User
	if err := a.db.First(&user, userID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "user not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": userDTO(user, a.userTokenUsage(user.ID, monthStart(time.Now())))})
}

func (a *App) adminUpdateUserQuota(c *gin.Context) {
	userID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		MonthlyQuota int64   `json:"monthlyQuota"`
		WeeklyQuota  int64   `json:"weeklyQuota"`
		Balance      float64 `json:"balance"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if err := a.db.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"monthly_quota": req.MonthlyQuota,
		"weekly_quota":  req.WeeklyQuota,
		"balance":       req.Balance,
	}).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update quota failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminDeleteUser(c *gin.Context) {
	userID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Model(&User{}).Where("id = ?", userID).Update("status", UserStatusDisabled).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "disable user failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminSources(c *gin.Context) {
	var sources []UpstreamSource
	query := a.db.Order("priority asc, id asc")
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("name LIKE ? OR base_url LIKE ?", "%"+q+"%", "%"+q+"%")
	}
	if err := query.Find(&sources).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]SourceDTO, 0, len(sources))
	for _, source := range sources {
		out = append(out, sourceDTO(source, false))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateSource(c *gin.Context) {
	var req struct {
		Name             string `json:"name"`
		Type             string `json:"type"`
		APIBase          string `json:"apiBase"`
		OpenAIBaseURL    string `json:"openaiBaseUrl"`
		AnthropicBaseURL string `json:"anthropicBaseUrl"`
		APIKey           string `json:"apiKey"`
		Priority         int    `json:"priority"`
		Status           string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.APIBase) == "" {
		errorJSON(c, http.StatusBadRequest, "name and apiBase are required")
		return
	}
	sourceType := normalizeSourceType(req.Type)
	if sourceType == "" {
		sourceType = SourceTypeThirdParty
	}
	if sourceType != SourceTypeThirdParty {
		errorJSON(c, http.StatusBadRequest, "only third-party provider sources can be created")
		return
	}
	if req.Status == "" {
		req.Status = SourceStatusOffline
	}
	source := UpstreamSource{
		Name:             strings.TrimSpace(req.Name),
		Type:             sourceType,
		BaseURL:          normalizeSourceBaseURL(req.APIBase),
		OpenAIBaseURL:    normalizeBaseURL(req.OpenAIBaseURL),
		AnthropicBaseURL: normalizeBaseURL(req.AnthropicBaseURL),
		APIKey:           strings.TrimSpace(req.APIKey),
		Priority:         req.Priority,
		Status:           req.Status,
	}
	if source.Priority == 0 {
		source.Priority = 100
	}
	if err := a.db.Create(&source).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create source failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": sourceDTO(source, false)})
}

func (a *App) adminUpdateSource(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	var current UpstreamSource
	if err := a.db.First(&current, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if strings.EqualFold(current.Type, SourceTypeCLIProxyAPI) {
		for _, key := range []string{"type", "apiBase", "openaiBaseUrl", "anthropicBaseUrl", "apiKey", "managementKey"} {
			if _, ok := req[key]; ok {
				errorJSON(c, http.StatusBadRequest, "built-in CLIProxyAPI connection is configured by RELAY_CLIPROXYAPI_* environment variables")
				return
			}
		}
	}
	updates := map[string]any{}
	resetSourceHealth := false
	stringFields := map[string]string{
		"name":             "name",
		"type":             "type",
		"apiBase":          "base_url",
		"openaiBaseUrl":    "open_ai_base_url",
		"anthropicBaseUrl": "anthropic_base_url",
		"apiKey":           "api_key",
		"status":           "status",
	}
	for jsonKey, dbKey := range stringFields {
		if value, ok := req[jsonKey].(string); ok {
			switch jsonKey {
			case "apiBase":
				value = normalizeSourceBaseURL(value)
			case "type":
				value = normalizeSourceType(value)
				if value == "" {
					errorJSON(c, http.StatusBadRequest, "invalid source type")
					return
				}
				if value == SourceTypeCLIProxyAPI {
					errorJSON(c, http.StatusBadRequest, "CLIProxyAPI is a built-in source")
					return
				}
			case "openaiBaseUrl", "anthropicBaseUrl":
				value = normalizeBaseURL(value)
			}
			updates[dbKey] = strings.TrimSpace(value)
			switch jsonKey {
			case "apiBase", "openaiBaseUrl", "anthropicBaseUrl", "apiKey":
				resetSourceHealth = true
			case "status":
				resetSourceHealth = resetSourceHealth || strings.TrimSpace(value) == SourceStatusOnline
			}
		}
	}
	if value, ok := numberFromMap(req, "priority"); ok {
		updates["priority"] = int(value)
	}
	if value, ok := numberFromMap(req, "load"); ok {
		updates["load"] = int(value)
	}
	if value, ok := numberFromMap(req, "latencyMs"); ok {
		updates["latency_ms"] = int(value)
	}
	if value, ok := numberFromMap(req, "failureCount"); ok {
		updates["failure_count"] = int(value)
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if resetSourceHealth {
		updates["failure_count"] = 0
		updates["cooldown_until"] = nil
		updates["last_failure_at"] = nil
	}
	if err := a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update source failed")
		return
	}
	if resetSourceHealth {
		if err := a.recoverSourceBindings(sourceID); err != nil {
			errorJSON(c, http.StatusBadRequest, "recover source bindings failed")
			return
		}
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sourceDTO(source, false)})
}

func (a *App) adminCheckSource(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	status, latencyMS, checkErr := a.checkSourceReachability(c, source)
	updates := map[string]any{
		"status":     status,
		"latency_ms": latencyMS,
	}
	if status == SourceStatusOnline {
		updates["failure_count"] = 0
		updates["cooldown_until"] = nil
		updates["last_failure_at"] = nil
	}
	if status == SourceStatusOnline {
		if err := a.db.Exec(
			"UPDATE upstream_sources SET status = ?, latency_ms = ?, failure_count = 0, cooldown_until = NULL, last_failure_at = NULL WHERE id = ?",
			status,
			latencyMS,
			sourceID,
		).Error; err != nil {
			errorJSON(c, http.StatusBadRequest, "update source health failed")
			return
		}
		if err := a.recoverSourceBindings(sourceID); err != nil {
			errorJSON(c, http.StatusBadRequest, "recover source bindings failed")
			return
		}
	} else if err := a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update source health failed")
		return
	}
	var refreshed UpstreamSource
	_ = a.db.First(&refreshed, sourceID).Error
	out := gin.H{"data": sourceDTO(source, false)}
	if refreshed.ID != 0 {
		out["data"] = sourceDTO(refreshed, false)
	}
	if checkErr != nil {
		out["error"] = checkErr.Error()
	}
	c.JSON(http.StatusOK, out)
}

func (a *App) adminRecoverSource(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if source.Status == SourceStatusDisabled {
		errorJSON(c, http.StatusBadRequest, "disabled source cannot be recovered")
		return
	}
	if err := a.db.Exec(
		"UPDATE upstream_sources SET status = ?, failure_count = 0, cooldown_until = NULL, last_failure_at = NULL WHERE id = ?",
		SourceStatusOnline,
		sourceID,
	).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "recover source failed")
		return
	}
	if err := a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Update("cooldown_until", nil).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "recover source failed")
		return
	}
	if err := a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Update("last_failure_at", nil).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "recover source failed")
		return
	}
	if err := a.recoverSourceBindings(sourceID); err != nil {
		errorJSON(c, http.StatusBadRequest, "recover source bindings failed")
		return
	}
	var refreshed UpstreamSource
	_ = a.db.First(&refreshed, sourceID).Error
	c.JSON(http.StatusOK, gin.H{"data": sourceDTO(refreshed, false)})
}

func (a *App) adminDeleteSource(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if strings.EqualFold(source.Type, SourceTypeCLIProxyAPI) {
		errorJSON(c, http.StatusBadRequest, "built-in CLIProxyAPI source cannot be deleted")
		return
	}
	if err := a.db.Delete(&UpstreamSource{}, sourceID).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete source failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) checkSourceReachability(c *gin.Context, source UpstreamSource) (string, int, error) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, normalizeBaseURL(source.BaseURL), nil)
	if err != nil {
		return SourceStatusOffline, 0, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return SourceStatusOffline, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return SourceStatusOnline, latency, nil
}

func (a *App) adminSourceAccounts(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !sourceSupportsAccountPool(source) {
		c.JSON(http.StatusOK, gin.H{"data": []SourceAccountDTO{}})
		return
	}
	var accounts []SourceAccount
	if err := a.db.Where("source_id = ?", sourceID).Order("id desc").Find(&accounts).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]SourceAccountDTO, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, sourceAccountDTO(account))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateSourceAccount(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !sourceSupportsAccountPool(source) {
		errorJSON(c, http.StatusBadRequest, "account management is only supported for CLIProxyAPI sources")
		return
	}
	var req SourceAccountDTO
	if !bindJSON(c, &req) {
		return
	}
	now := time.Now()
	account := SourceAccount{
		SourceID:         sourceID,
		Identifier:       req.Identifier,
		Provider:         cliProxyProviderLabel(req.Provider),
		PlanType:         normalizePlanType(req.PlanType),
		SubscriptionPlan: normalizePlanType(req.SubscriptionPlan),
		HasSubscription:  req.HasSubscription,
		Status:           req.Status,
		Balance:          req.Balance,
		BalanceLimit:     req.BalanceLimit,
		Used5h:           req.Used5h,
		Limit5h:          req.Limit5h,
		Used7d:           req.Used7d,
		Limit7d:          req.Limit7d,
		LastRefreshed:    now,
	}
	if account.Status == "" {
		account.Status = "valid"
	}
	if err := a.db.Create(&account).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create account failed")
		return
	}
	_ = a.recountSourceAccounts(sourceID)
	c.JSON(http.StatusCreated, gin.H{"data": sourceAccountDTO(account)})
}

func (a *App) adminCreateOAuthSession(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Provider string `json:"provider"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = "ChatGPT"
	}
	req.Provider = cliProxyProviderLabel(req.Provider)
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !strings.EqualFold(source.Type, "CLIProxyAPI") {
		errorJSON(c, http.StatusBadRequest, "OAuth account flow is only supported for CLIProxyAPI sources")
		return
	}
	if strings.TrimSpace(source.ManagementKey) == "" {
		errorJSON(c, http.StatusBadRequest, cliProxyManagementKeyMissingMessage)
		return
	}
	payload, err := a.startCLIProxyOAuth(c, source, req.Provider)
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	payload["sourceId"] = id("s", sourceID)
	payload["provider"] = req.Provider
	c.JSON(http.StatusAccepted, payload)
}

func (a *App) adminSubmitOAuthCallback(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Provider    string `json:"provider"`
		RedirectURL string `json:"redirectUrl"`
	}
	if !bindJSON(c, &req) {
		return
	}
	req.Provider = cliProxyProviderLabel(req.Provider)
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = "ChatGPT"
	}
	if strings.TrimSpace(req.RedirectURL) == "" {
		errorJSON(c, http.StatusBadRequest, "redirectUrl is required")
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !strings.EqualFold(source.Type, "CLIProxyAPI") {
		errorJSON(c, http.StatusBadRequest, "OAuth account flow is only supported for CLIProxyAPI sources")
		return
	}
	if strings.TrimSpace(source.ManagementKey) == "" {
		errorJSON(c, http.StatusBadRequest, cliProxyManagementKeyMissingMessage)
		return
	}
	complete, err := a.submitCLIProxyOAuthCallback(c.Request.Context(), source, req.Provider, req.RedirectURL)
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}

	out := []SourceAccountDTO{}
	if complete {
		accounts, err := a.syncCLIProxyAccounts(c, source)
		if err != nil {
			errorJSON(c, http.StatusBadGateway, err.Error())
			return
		}
		out = make([]SourceAccountDTO, 0, len(accounts))
		for _, account := range accounts {
			out = append(out, sourceAccountDTO(account))
		}
	}
	status := http.StatusOK
	if !complete {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{"ok": true, "pending": !complete, "data": out})
}

func (a *App) adminSubmitSourceAccountToken(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Provider     string `json:"provider"`
		Identifier   string `json:"identifier"`
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	if !bindJSON(c, &req) {
		return
	}
	req.Provider = cliProxyProviderLabel(req.Provider)
	if strings.TrimSpace(req.Provider) == "" || req.Provider == "unknown" {
		req.Provider = "ChatGPT"
	}
	token := strings.TrimSpace(req.RefreshToken)
	if token == "" {
		token = strings.TrimSpace(req.Token)
	}
	if strings.TrimSpace(req.Identifier) == "" {
		errorJSON(c, http.StatusBadRequest, "identifier is required")
		return
	}
	if token == "" {
		errorJSON(c, http.StatusBadRequest, "refreshToken is required")
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !strings.EqualFold(source.Type, "CLIProxyAPI") {
		errorJSON(c, http.StatusBadRequest, "manual token login is only supported for CLIProxyAPI sources")
		return
	}
	if strings.TrimSpace(source.ManagementKey) == "" {
		errorJSON(c, http.StatusBadRequest, cliProxyManagementKeyMissingMessage)
		return
	}
	if err := a.submitCLIProxyManualToken(c.Request.Context(), source, req.Provider, req.Identifier, token); err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	accounts, err := a.syncCLIProxyAccounts(c, source)
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]SourceAccountDTO, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, sourceAccountDTO(account))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": out})
}

func (a *App) adminSyncSourceAccounts(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !strings.EqualFold(source.Type, "CLIProxyAPI") {
		errorJSON(c, http.StatusBadRequest, "account sync is only supported for CLIProxyAPI sources")
		return
	}
	if strings.TrimSpace(source.ManagementKey) == "" {
		errorJSON(c, http.StatusBadRequest, cliProxyManagementKeyMissingMessage)
		return
	}
	accounts, err := a.syncCLIProxyAccounts(c, source)
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]SourceAccountDTO, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, sourceAccountDTO(account))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminUpdateSourceAccount(c *gin.Context) {
	accountID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	for _, key := range []string{"identifier", "provider", "planType", "subscriptionPlan", "status"} {
		if value, ok := req[key].(string); ok {
			dbKey := key
			nextValue := strings.TrimSpace(value)
			if key == "provider" {
				nextValue = cliProxyProviderLabel(nextValue)
			}
			if key == "planType" {
				dbKey = "plan_type"
				nextValue = normalizePlanType(nextValue)
			}
			if key == "subscriptionPlan" {
				dbKey = "subscription_plan"
				nextValue = normalizePlanType(nextValue)
			}
			updates[dbKey] = nextValue
		}
	}
	for jsonKey, dbKey := range map[string]string{"balance": "balance", "balanceLimit": "balance_limit", "used5h": "used5h", "limit5h": "limit5h", "used7d": "used7d", "limit7d": "limit7d"} {
		if value, ok := numberFromMap(req, jsonKey); ok {
			updates[dbKey] = value
		}
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	var account SourceAccount
	if err := a.db.First(&account, accountID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "account not found")
		return
	}
	if err := a.db.Model(&account).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update account failed")
		return
	}
	_ = a.db.First(&account, accountID).Error
	c.JSON(http.StatusOK, gin.H{"data": sourceAccountDTO(account)})
}

func (a *App) adminDeleteSourceAccount(c *gin.Context) {
	accountID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var account SourceAccount
	if err := a.db.First(&account, accountID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "account not found")
		return
	}
	if err := a.db.Delete(&account).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete account failed")
		return
	}
	_ = a.recountSourceAccounts(account.SourceID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminRefreshSourceAccount(c *gin.Context) {
	accountID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var account SourceAccount
	if err := a.db.First(&account, accountID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "account not found")
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, account.SourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if sourceSupportsAccountPool(source) {
		if strings.TrimSpace(source.ManagementKey) == "" {
			errorJSON(c, http.StatusBadRequest, cliProxyManagementKeyMissingMessage)
			return
		}
		accounts, err := a.syncCLIProxyAccounts(c, source)
		if err != nil {
			errorJSON(c, http.StatusBadGateway, err.Error())
			return
		}
		for _, refreshed := range accounts {
			if refreshed.ID == account.ID || (refreshed.Identifier == account.Identifier && refreshed.Provider == account.Provider) {
				c.JSON(http.StatusOK, gin.H{"data": sourceAccountDTO(refreshed)})
				return
			}
		}
	}
	now := time.Now()
	if err := a.db.Model(&SourceAccount{}).Where("id = ?", accountID).Update("last_refreshed", now).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "refresh account failed")
		return
	}
	account.LastRefreshed = now
	c.JSON(http.StatusOK, gin.H{"data": sourceAccountDTO(account)})
}

func (a *App) recountSourceAccounts(sourceID uint) error {
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		return err
	}
	if !sourceSupportsAccountPool(source) {
		return a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Update("account_count", 0).Error
	}
	var count int64
	if err := a.db.Model(&SourceAccount{}).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
		return err
	}
	return a.db.Model(&UpstreamSource{}).Where("id = ?", sourceID).Update("account_count", int(count)).Error
}

func sourceSupportsAccountPool(source UpstreamSource) bool {
	return strings.EqualFold(source.Type, SourceTypeCLIProxyAPI)
}

func sourceSupportsCredentialKeys(source UpstreamSource) bool {
	return !sourceSupportsAccountPool(source)
}

func (a *App) adminSourceKeys(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !sourceSupportsCredentialKeys(source) {
		c.JSON(http.StatusOK, gin.H{"data": []SourceKeyDTO{}})
		return
	}
	var keys []SourceKey
	if err := a.db.Where("source_id = ?", sourceID).Order("id asc").Find(&keys).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]SourceKeyDTO, 0, len(keys))
	for _, key := range keys {
		out = append(out, sourceKeyDTO(key, false))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateSourceKey(c *gin.Context) {
	sourceID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source not found")
		return
	}
	if !sourceSupportsCredentialKeys(source) {
		errorJSON(c, http.StatusBadRequest, "API key pool is only supported for direct third-party sources")
		return
	}
	var req struct {
		Alias  string `json:"alias"`
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	alias := strings.TrimSpace(req.Alias)
	secret := strings.TrimSpace(req.Key)
	if alias == "" {
		errorJSON(c, http.StatusBadRequest, "alias is required")
		return
	}
	if secret == "" {
		errorJSON(c, http.StatusBadRequest, "key is required")
		return
	}
	key := SourceKey{
		SourceID: sourceID,
		Alias:    alias,
		APIKey:   secret,
		Status:   strings.TrimSpace(req.Status),
	}
	if key.Status == "" {
		key.Status = APIKeyStatusValid
	}
	if err := a.db.Create(&key).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create source key failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": sourceKeyDTO(key, false)})
}

func (a *App) adminUpdateSourceKey(c *gin.Context) {
	keyID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var key SourceKey
	if err := a.db.First(&key, keyID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source key not found")
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	if alias, ok := req["alias"].(string); ok && strings.TrimSpace(alias) != "" {
		updates["alias"] = strings.TrimSpace(alias)
	}
	if secret, ok := req["key"].(string); ok && strings.TrimSpace(secret) != "" {
		updates["api_key"] = strings.TrimSpace(secret)
	}
	if status, ok := req["status"].(string); ok && strings.TrimSpace(status) != "" {
		updates["status"] = strings.TrimSpace(status)
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&key).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update source key failed")
		return
	}
	_ = a.db.First(&key, keyID).Error
	c.JSON(http.StatusOK, gin.H{"data": sourceKeyDTO(key, false)})
}

func (a *App) adminDeleteSourceKey(c *gin.Context) {
	keyID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var key SourceKey
	if err := a.db.First(&key, keyID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "source key not found")
		return
	}
	if err := a.db.Model(&ModelConfig{}).Where("source_key_id = ?", key.ID).Update("source_key_id", gorm.Expr("NULL")).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "clear model key bindings failed")
		return
	}
	if err := a.db.Model(&ModelRouteBinding{}).Where("source_key_id = ?", key.ID).Update("source_key_id", gorm.Expr("NULL")).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "clear model route key bindings failed")
		return
	}
	if err := a.db.Delete(&key).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete source key failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminModels(c *gin.Context) {
	var models []ModelConfig
	query := a.db.Order("id asc")
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		query = query.Where("name LIKE ? OR display_name LIKE ?", "%"+q+"%", "%"+q+"%")
	}
	if err := query.Find(&models).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	sources := a.sourceMap()
	sourceKeys := a.sourceKeyAliasMap()
	groupNames := a.modelGroupNameMap()
	defaultGroupID := a.defaultModelGroupID()
	candidates := a.modelRouteCandidatesByGroup(models, sources, sourceKeys, defaultGroupID)
	out := make([]ModelDTO, 0, len(models))
	for _, model := range models {
		model.ModelGroupID = modelGroupBucketID(model, defaultGroupID)
		dto := modelDTO(model, sources[model.SourceID].Name, sourceKeys[sourceKeyIDValue(model.SourceKeyID)])
		dto.ModelGroupName = groupNames[model.ModelGroupID]
		dto.RoutingCandidates = candidates[modelGroupBucketKey(model.Name, model.ModelGroupID)]
		dto.CandidateCount = len(dto.RoutingCandidates)
		out = append(out, dto)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateModel(c *gin.Context) {
	var req struct {
		Name               string                `json:"name"`
		SourceID           string                `json:"sourceId"`
		SourceKeyID        string                `json:"sourceKeyId"`
		ModelGroupID       string                `json:"modelGroupId"`
		Provider           string                `json:"provider"`
		Formats            []string              `json:"formats"`
		InputPrice         float64               `json:"inputPrice"`
		OutputPrice        float64               `json:"outputPrice"`
		CacheWritePrice    float64               `json:"cacheWritePrice"`
		CacheReadPrice     float64               `json:"cacheReadPrice"`
		InputMultiple      float64               `json:"inputMultiple"`
		OutputMultiple     float64               `json:"outputMultiple"`
		CacheWriteMultiple float64               `json:"cacheWriteMultiple"`
		CacheReadMultiple  float64               `json:"cacheReadMultiple"`
		BillingInput       float64               `json:"billingInput"`
		BillingOutput      float64               `json:"billingOutput"`
		Enabled            *bool                 `json:"enabled"`
		RoutingWeight      int                   `json:"routingWeight"`
		RoutingEnabled     *bool                 `json:"routingEnabled"`
		Bindings           []modelBindingRequest `json:"bindings"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		errorJSON(c, http.StatusBadRequest, "model name is required")
		return
	}
	group, err := a.platformModelGroupFromRequest(req.ModelGroupID)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	bindings := normalizeBindingRequests(req.Bindings, modelBindingRequest{
		SourceID:       req.SourceID,
		SourceKeyID:    req.SourceKeyID,
		RoutingWeight:  req.RoutingWeight,
		RoutingEnabled: req.RoutingEnabled,
	})
	parsedBindings, err := a.validateModelBindingRequests(bindings)
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	firstBinding := parsedBindings[0]
	firstSourceID, _ := parseNumericID(firstBinding.SourceID)
	firstSourceKeyID, _ := a.resolveSourceKeyID(firstSourceID, firstBinding.SourceKeyID)
	firstRoutingEnabled := true
	if firstBinding.RoutingEnabled != nil {
		firstRoutingEnabled = *firstBinding.RoutingEnabled
	}
	status := ModelStatusActive
	if req.Enabled != nil && !*req.Enabled {
		status = ModelStatusDisabled
	}
	inputPrice := nonZeroFloat(req.InputPrice, req.BillingInput)
	outputPrice := nonZeroFloat(req.OutputPrice, req.BillingOutput)
	inputMultiple := nonZeroFloat(req.InputMultiple, 1)
	outputMultiple := nonZeroFloat(req.OutputMultiple, 1)
	cacheWriteMultiple := nonZeroFloat(req.CacheWriteMultiple, 1)
	cacheReadMultiple := nonZeroFloat(req.CacheReadMultiple, 1)
	model := ModelConfig{
		ModelGroupID:       group.ID,
		SourceID:           firstSourceID,
		SourceKeyID:        firstSourceKeyID,
		Name:               strings.TrimSpace(req.Name),
		DisplayName:        strings.TrimSpace(req.Name),
		Provider:           strings.TrimSpace(req.Provider),
		Formats:            normalizeModelFormats(req.Formats, req.Provider),
		InputPrice:         inputPrice,
		OutputPrice:        outputPrice,
		CacheWritePrice:    req.CacheWritePrice,
		CacheReadPrice:     req.CacheReadPrice,
		InputMultiple:      inputMultiple,
		OutputMultiple:     outputMultiple,
		CacheWriteMultiple: cacheWriteMultiple,
		CacheReadMultiple:  cacheReadMultiple,
		BillingInput:       finalBillingPrice(inputPrice, inputMultiple),
		BillingOutput:      finalBillingPrice(outputPrice, outputMultiple),
		BillingCacheWrite:  finalBillingPrice(req.CacheWritePrice, cacheWriteMultiple),
		BillingCacheRead:   finalBillingPrice(req.CacheReadPrice, cacheReadMultiple),
		Status:             status,
		RoutingWeight:      firstBinding.RoutingWeight,
		RoutingEnabled:     firstRoutingEnabled,
	}
	if model.Provider == "" {
		model.Provider = "OpenAI"
		model.Formats = normalizeModelFormats(req.Formats, model.Provider)
	}
	var existing ModelConfig
	existingQuery := a.db.Where("name = ?", model.Name)
	if model.ModelGroupID == a.defaultModelGroupID() {
		existingQuery = existingQuery.Where("model_group_id = ? OR model_group_id = 0", model.ModelGroupID)
	} else {
		existingQuery = existingQuery.Where("model_group_id = ?", model.ModelGroupID)
	}
	if err := existingQuery.Order("id asc").First(&existing).Error; err == nil {
		merged, err := a.appendModelBindingsToExisting(existing, model, parsedBindings)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, "merge model bindings failed")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": a.modelDTOWithRouting(merged)})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		errorJSON(c, http.StatusBadRequest, "load model failed")
		return
	}
	if err := a.db.Create(&model).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create model failed")
		return
	}
	if err := a.replaceModelBindings(model.ID, parsedBindings); err != nil {
		errorJSON(c, http.StatusBadRequest, "create model bindings failed")
		return
	}
	if err := a.syncModelLegacyBindingFields(model.ID, a.primaryModelBindingRequest(parsedBindings)); err != nil {
		errorJSON(c, http.StatusBadRequest, "create model binding mirror failed")
		return
	}
	if err := a.db.First(&model, model.ID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": a.modelDTOWithRouting(model)})
}

func (a *App) adminUpdateModel(c *gin.Context) {
	modelID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	var existing ModelConfig
	if err := a.db.First(&existing, modelID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	bindingRequests, hasBindingRequests := parseBindingRequests(req["bindings"])
	var parsedBindings []modelBindingRequest
	if hasBindingRequests {
		var err error
		parsedBindings, err = a.validateModelBindingRequests(bindingRequests)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	updates := map[string]any{}
	for _, key := range []string{"name", "provider"} {
		if value, ok := req[key].(string); ok && strings.TrimSpace(value) != "" {
			updates[key] = strings.TrimSpace(value)
		}
	}
	if value, ok := req["modelGroupId"].(string); ok && strings.TrimSpace(value) != "" {
		group, err := a.platformModelGroupFromRequest(value)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
		updates["model_group_id"] = group.ID
	}
	providerForFormats := existing.Provider
	if provider, ok := updates["provider"].(string); ok {
		providerForFormats = provider
	}
	if rawFormats, ok := req["formats"].([]any); ok {
		formats := make([]string, 0, len(rawFormats))
		for _, raw := range rawFormats {
			if value, ok := raw.(string); ok {
				formats = append(formats, value)
			}
		}
		updates["formats"] = normalizeModelFormats(formats, providerForFormats)
	}
	targetSourceID := existing.SourceID
	sourceChanged := false
	if sourceID, ok := req["sourceId"].(string); ok && strings.TrimSpace(sourceID) != "" {
		parsed, err := parseNumericID(sourceID)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, "invalid sourceId")
			return
		}
		if _, err := a.getSourceForModel(parsed); err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
		targetSourceID = parsed
		sourceChanged = parsed != existing.SourceID
		updates["source_id"] = parsed
	}
	if raw, ok := req["sourceKeyId"].(string); ok {
		sourceKeyID, err := a.resolveSourceKeyID(targetSourceID, raw)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
		if sourceKeyID == nil {
			updates["source_key_id"] = gorm.Expr("NULL")
		} else {
			updates["source_key_id"] = *sourceKeyID
		}
	} else if sourceChanged {
		updates["source_key_id"] = gorm.Expr("NULL")
	}
	if pricingUpdates := modelPricingUpdates(req, existing); len(pricingUpdates) > 0 {
		for key, value := range pricingUpdates {
			updates[key] = value
		}
	}
	if enabled, ok := req["enabled"].(bool); ok {
		if enabled {
			updates["status"] = ModelStatusActive
		} else {
			updates["status"] = ModelStatusDisabled
		}
	}
	if routingEnabled, ok := req["routingEnabled"].(bool); ok {
		updates["routing_enabled"] = routingEnabled
	}
	if value, ok := numberFromMap(req, "routingWeight"); ok {
		weight := int(value)
		if weight <= 0 {
			weight = 1
		}
		updates["routing_weight"] = weight
	}
	if len(updates) == 0 {
		if !hasBindingRequests {
			errorJSON(c, http.StatusBadRequest, "no fields to update")
			return
		}
	} else {
		if err := a.db.Model(&ModelConfig{}).Where("id = ?", modelID).Updates(updates).Error; err != nil {
			errorJSON(c, http.StatusBadRequest, "update model failed")
			return
		}
	}
	var model ModelConfig
	if err := a.db.First(&model, modelID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model not found")
		return
	}
	model.ModelGroupID = a.normalizeModelGroupID(model.ModelGroupID)
	if hasBindingRequests {
		if err := a.deleteModelSiblings(model.Name, model.ModelGroupID, model.ID); err != nil {
			errorJSON(c, http.StatusBadRequest, "delete duplicate model rows failed")
			return
		}
		if err := a.replaceModelBindings(model.ID, parsedBindings); err != nil {
			errorJSON(c, http.StatusBadRequest, "update model bindings failed")
			return
		}
		if err := a.syncModelLegacyBindingFields(model.ID, a.primaryModelBindingRequest(parsedBindings)); err != nil {
			errorJSON(c, http.StatusBadRequest, "update model binding mirror failed")
			return
		}
		if err := a.db.First(&model, modelID).Error; err != nil {
			errorJSON(c, http.StatusNotFound, "model not found")
			return
		}
	} else if sourceChanged || req["sourceKeyId"] != nil || req["routingWeight"] != nil || req["routingEnabled"] != nil {
		fallback := modelBindingRequest{
			SourceID:       id("s", model.SourceID),
			SourceKeyID:    optionalPublicID("sk", sourceKeyIDValue(model.SourceKeyID)),
			RoutingWeight:  nonZeroInt(model.RoutingWeight, 1),
			RoutingEnabled: &model.RoutingEnabled,
		}
		parsed, err := a.validateModelBindingRequests([]modelBindingRequest{fallback})
		if err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
		if err := a.deleteModelSiblings(model.Name, model.ModelGroupID, model.ID); err != nil {
			errorJSON(c, http.StatusBadRequest, "delete duplicate model rows failed")
			return
		}
		if err := a.replaceModelBindings(model.ID, parsed); err != nil {
			errorJSON(c, http.StatusBadRequest, "update model bindings failed")
			return
		}
		if err := a.syncModelLegacyBindingFields(model.ID, a.primaryModelBindingRequest(parsed)); err != nil {
			errorJSON(c, http.StatusBadRequest, "update model binding mirror failed")
			return
		}
		if err := a.db.First(&model, modelID).Error; err != nil {
			errorJSON(c, http.StatusNotFound, "model not found")
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": a.modelDTOWithRouting(model)})
}

func (a *App) appendModelBindingsToExisting(existing ModelConfig, incoming ModelConfig, bindings []modelBindingRequest) (ModelConfig, error) {
	var group []ModelConfig
	groupID := a.normalizeModelGroupID(existing.ModelGroupID)
	query := a.db.Where("name = ?", existing.Name)
	if groupID == a.defaultModelGroupID() {
		query = query.Where("model_group_id = ? OR model_group_id = 0", groupID)
	} else {
		query = query.Where("model_group_id = ?", groupID)
	}
	if err := query.Order("id asc").Find(&group).Error; err != nil {
		return ModelConfig{}, err
	}
	currentBindings, err := a.modelBindingRequestsForGroup(existing.ID, group)
	if err != nil {
		return ModelConfig{}, err
	}
	nextBindings := append(currentBindings, bindings...)
	updates := modelCreateMergeUpdates(incoming)
	if len(updates) > 0 {
		if err := a.db.Model(&ModelConfig{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return ModelConfig{}, err
		}
	}
	if err := a.deleteModelSiblings(existing.Name, groupID, existing.ID); err != nil {
		return ModelConfig{}, err
	}
	if err := a.replaceModelBindings(existing.ID, nextBindings); err != nil {
		return ModelConfig{}, err
	}
	if err := a.syncModelLegacyBindingFields(existing.ID, a.primaryModelBindingRequest(nextBindings)); err != nil {
		return ModelConfig{}, err
	}
	var refreshed ModelConfig
	if err := a.db.First(&refreshed, existing.ID).Error; err != nil {
		return ModelConfig{}, err
	}
	return refreshed, nil
}

func modelCreateMergeUpdates(model ModelConfig) map[string]any {
	updates := map[string]any{
		"provider": model.Provider,
		"formats":  model.Formats,
		"status":   model.Status,
	}
	if model.InputPrice != 0 || model.BillingInput != 0 {
		updates["input_price"] = model.InputPrice
		updates["input_multiple"] = nonZeroFloat(model.InputMultiple, 1)
		updates["billing_input"] = model.BillingInput
	}
	if model.OutputPrice != 0 || model.BillingOutput != 0 {
		updates["output_price"] = model.OutputPrice
		updates["output_multiple"] = nonZeroFloat(model.OutputMultiple, 1)
		updates["billing_output"] = model.BillingOutput
	}
	if model.CacheWritePrice != 0 || model.BillingCacheWrite != 0 {
		updates["cache_write_price"] = model.CacheWritePrice
		updates["cache_write_multiple"] = nonZeroFloat(model.CacheWriteMultiple, 1)
		updates["billing_cache_write"] = model.BillingCacheWrite
	}
	if model.CacheReadPrice != 0 || model.BillingCacheRead != 0 {
		updates["cache_read_price"] = model.CacheReadPrice
		updates["cache_read_multiple"] = nonZeroFloat(model.CacheReadMultiple, 1)
		updates["billing_cache_read"] = model.BillingCacheRead
	}
	return updates
}

func modelPricingUpdates(req map[string]any, existing ModelConfig) map[string]any {
	inputPrice := existing.InputPrice
	if inputPrice == 0 && existing.BillingInput > 0 {
		inputPrice = existing.BillingInput
	}
	outputPrice := existing.OutputPrice
	if outputPrice == 0 && existing.BillingOutput > 0 {
		outputPrice = existing.BillingOutput
	}
	cacheWritePrice := existing.CacheWritePrice
	if cacheWritePrice == 0 && existing.BillingCacheWrite > 0 {
		cacheWritePrice = existing.BillingCacheWrite
	}
	cacheReadPrice := existing.CacheReadPrice
	if cacheReadPrice == 0 && existing.BillingCacheRead > 0 {
		cacheReadPrice = existing.BillingCacheRead
	}
	inputMultiple := nonZeroFloat(existing.InputMultiple, 1)
	outputMultiple := nonZeroFloat(existing.OutputMultiple, 1)
	cacheWriteMultiple := nonZeroFloat(existing.CacheWriteMultiple, 1)
	cacheReadMultiple := nonZeroFloat(existing.CacheReadMultiple, 1)

	changed := false
	if value, ok := numberFromMap(req, "inputPrice"); ok {
		inputPrice = value
		changed = true
	}
	if value, ok := numberFromMap(req, "outputPrice"); ok {
		outputPrice = value
		changed = true
	}
	if value, ok := numberFromMap(req, "cacheWritePrice"); ok {
		cacheWritePrice = value
		changed = true
	}
	if value, ok := numberFromMap(req, "cacheReadPrice"); ok {
		cacheReadPrice = value
		changed = true
	}
	if value, ok := numberFromMap(req, "inputMultiple"); ok {
		inputMultiple = nonZeroFloat(value, 1)
		changed = true
	}
	if value, ok := numberFromMap(req, "outputMultiple"); ok {
		outputMultiple = nonZeroFloat(value, 1)
		changed = true
	}
	if value, ok := numberFromMap(req, "cacheWriteMultiple"); ok {
		cacheWriteMultiple = nonZeroFloat(value, 1)
		changed = true
	}
	if value, ok := numberFromMap(req, "cacheReadMultiple"); ok {
		cacheReadMultiple = nonZeroFloat(value, 1)
		changed = true
	}
	if value, ok := numberFromMap(req, "billingInput"); ok && !changed {
		inputPrice = value
		inputMultiple = 1
		changed = true
	}
	if value, ok := numberFromMap(req, "billingOutput"); ok && !changed {
		outputPrice = value
		outputMultiple = 1
		changed = true
	}
	if !changed {
		return nil
	}
	return map[string]any{
		"input_price":          inputPrice,
		"output_price":         outputPrice,
		"cache_write_price":    cacheWritePrice,
		"cache_read_price":     cacheReadPrice,
		"input_multiple":       inputMultiple,
		"output_multiple":      outputMultiple,
		"cache_write_multiple": cacheWriteMultiple,
		"cache_read_multiple":  cacheReadMultiple,
		"billing_input":        finalBillingPrice(inputPrice, inputMultiple),
		"billing_output":       finalBillingPrice(outputPrice, outputMultiple),
		"billing_cache_write":  finalBillingPrice(cacheWritePrice, cacheWriteMultiple),
		"billing_cache_read":   finalBillingPrice(cacheReadPrice, cacheReadMultiple),
	}
}

func (a *App) adminDeleteModel(c *gin.Context) {
	modelID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Delete(&ModelConfig{}, modelID).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete model failed")
		return
	}
	_ = a.db.Where("model_id = ?", modelID).Delete(&ModelRouteBinding{}).Error
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminBatchModels(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
	}
	if !bindJSON(c, &req) {
		return
	}
	ids := make([]uint, 0, len(req.IDs))
	for _, raw := range req.IDs {
		parsed, err := parseNumericID(raw)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, "invalid model id")
			return
		}
		ids = append(ids, parsed)
	}
	switch req.Action {
	case "enable":
		a.db.Model(&ModelConfig{}).Where("id IN ?", ids).Update("status", ModelStatusActive)
	case "disable":
		a.db.Model(&ModelConfig{}).Where("id IN ?", ids).Update("status", ModelStatusDisabled)
	case "delete":
		a.db.Where("model_id IN ?", ids).Delete(&ModelRouteBinding{})
		a.db.Delete(&ModelConfig{}, ids)
	default:
		errorJSON(c, http.StatusBadRequest, "unsupported action")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminInvites(c *gin.Context) {
	var invites []InviteCode
	if err := a.db.Order("created_at desc").Find(&invites).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	now := time.Now()
	out := make([]InviteDTO, 0, len(invites))
	for _, invite := range invites {
		out = append(out, inviteDTO(invite, now))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateInvite(c *gin.Context) {
	user, _ := currentUser(c)
	var req struct {
		Code      string `json:"code"`
		Limit     int    `json:"limit"`
		MaxUse    int    `json:"maxUse"`
		ExpiresAt string `json:"expiresAt"`
		Remark    string `json:"remark"`
	}
	if !bindJSON(c, &req) {
		return
	}
	code, errMessage := normalizeCustomInviteCode(req.Code)
	if errMessage != "" {
		errorJSON(c, http.StatusBadRequest, errMessage)
		return
	}
	if code == "" {
		generated, err := randomInviteCode()
		if err != nil {
			errorJSON(c, http.StatusInternalServerError, "generate invite failed")
			return
		}
		code = generated
	}
	maxUse := req.Limit
	if maxUse == 0 {
		maxUse = req.MaxUse
	}
	if maxUse == 0 {
		maxUse = 1
	}
	var expires *time.Time
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, "invalid expiresAt")
			return
		}
		expires = &parsed
	}
	invite := InviteCode{Code: code, MaxUse: maxUse, CreatedBy: user.ID, ExpiresAt: expires, Remark: req.Remark, Status: InviteStatusActive}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		var existing InviteCode
		err := tx.Unscoped().Where("code = ?", code).First(&existing).Error
		if err == nil {
			if existing.DeletedAt.Valid {
				if err := tx.Unscoped().Delete(&existing).Error; err != nil {
					return err
				}
			} else {
				return errInviteCodeExists
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&invite).Error
	}); err != nil {
		if errors.Is(err, errInviteCodeExists) {
			errorJSON(c, http.StatusConflict, "邀请码已存在")
			return
		}
		errorJSON(c, http.StatusBadRequest, "create invite failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": inviteDTO(invite, time.Now()), "link": "/register?code=" + invite.Code})
}

func normalizeCustomInviteCode(raw string) (string, string) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", ""
	}
	if len(code) < 3 || len(code) > 64 {
		return "", "邀请码长度必须为 3-64 个字符"
	}
	for _, ch := range code {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return "", "邀请码只能包含字母、数字、连字符和下划线"
	}
	return code, ""
}

func (a *App) adminUpdateInvite(c *gin.Context) {
	inviteID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	if value, ok := req["remark"].(string); ok {
		updates["remark"] = value
	}
	if value, ok := req["status"].(string); ok {
		updates["status"] = value
	}
	if value, ok := numberFromMap(req, "limit"); ok {
		updates["max_use"] = int(value)
	}
	if value, ok := numberFromMap(req, "maxUse"); ok {
		updates["max_use"] = int(value)
	}
	if value, ok := req["expiresAt"].(string); ok {
		if strings.TrimSpace(value) == "" {
			updates["expires_at"] = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				errorJSON(c, http.StatusBadRequest, "invalid expiresAt")
				return
			}
			updates["expires_at"] = &parsed
		}
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&InviteCode{}).Where("id = ?", inviteID).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update invite failed")
		return
	}
	var invite InviteCode
	_ = a.db.First(&invite, inviteID).Error
	c.JSON(http.StatusOK, gin.H{"data": inviteDTO(invite, time.Now())})
}

func (a *App) adminDeleteInvite(c *gin.Context) {
	inviteID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Unscoped().Delete(&InviteCode{}, inviteID).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete invite failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminLogs(c *gin.Context) {
	var logs []UsageLog
	page := parseBoundedInt(c.Query("page"), 1, 1, 1000000)
	pageSize := parseBoundedInt(c.Query("pageSize"), 20, 1, 100)
	query := a.db.Model(&UsageLog{})
	if model := strings.TrimSpace(c.Query("model")); model != "" && model != "all" {
		query = query.Where("model = ?", model)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if sourceID, ok := optionalNumericQuery(c, "sourceId"); ok {
		query = query.Where("source_id = ?", sourceID)
	}
	if userID, ok := optionalNumericQuery(c, "userId"); ok {
		query = query.Where("user_id = ?", userID)
	}
	if apiKeyID, ok := optionalNumericQuery(c, "apiKeyId"); ok {
		query = query.Where("api_key_id = ?", apiKeyID)
	}
	if from, ok := parseQueryTime(c.Query("from")); ok {
		query = query.Where("created_at >= ?", from)
	}
	if to, ok := parseQueryTime(c.Query("to")); ok {
		query = query.Where("created_at < ?", to)
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		like := "%" + q + "%"
		userIDs := []uint{0}
		keyIDs := []uint{0}
		var matchedUsers []User
		if err := a.db.Where("email LIKE ? OR name LIKE ?", like, like).Find(&matchedUsers).Error; err == nil {
			for _, user := range matchedUsers {
				userIDs = append(userIDs, user.ID)
			}
		}
		var matchedKeys []APIKey
		if err := a.db.Where("name LIKE ? OR masked LIKE ?", like, like).Find(&matchedKeys).Error; err == nil {
			for _, key := range matchedKeys {
				keyIDs = append(keyIDs, key.ID)
			}
		}
		query = query.Where(
			"model LIKE ? OR upstream_name LIKE ? OR error_message LIKE ? OR user_id IN ? OR api_key_id IN ?",
			like,
			like,
			like,
			userIDs,
			keyIDs,
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if err := query.Order("created_at desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&logs).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{
		"data": a.requestLogDTOs(logs),
		"pagination": gin.H{
			"page":       page,
			"pageSize":   pageSize,
			"total":      total,
			"totalPages": totalPages,
		},
	})
}

func (a *App) adminLogDetail(c *gin.Context) {
	logID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var row UsageLog
	if err := a.db.First(&row, logID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "log not found")
		return
	}
	dto := a.requestLogDTOs([]UsageLog{row})
	if len(dto) == 0 {
		errorJSON(c, http.StatusNotFound, "log not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dto[0]})
}

func (a *App) adminLogAttempts(c *gin.Context) {
	logID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var log UsageLog
	if err := a.db.First(&log, logID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "log not found")
		return
	}
	var attempts []RequestAttempt
	if err := a.db.Where("usage_log_id = ?", logID).Order("attempt_index asc").Find(&attempts).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": requestAttemptDTOs(attempts)})
}

func (a *App) adminClearLogs(c *gin.Context) {
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&RequestAttempt{}).Error; err != nil {
			return err
		}
		return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&UsageLog{}).Error
	}); err != nil {
		errorJSON(c, http.StatusInternalServerError, "clear logs failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminUsageStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": a.usageStats(nil, c.Query("range"), nil)})
}

func (a *App) adminResetUsage(c *gin.Context) {
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&UsageLog{}).Error; err != nil {
			return err
		}
		return tx.Model(&APIKey{}).Where("spent_usd <> 0 OR last_used_at IS NOT NULL").Updates(map[string]any{
			"spent_usd":    0,
			"last_used_at": nil,
		}).Error
	}); err != nil {
		errorJSON(c, http.StatusInternalServerError, "reset usage failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) adminDashboard(c *gin.Context) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	var todayRequests int64
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", today, now).Count(&todayRequests)
	var yesterdayRequests int64
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", yesterday, today).Count(&yesterdayRequests)
	var activeUsers int64
	a.db.Model(&User{}).Where("status = ?", UserStatusNormal).Count(&activeUsers)
	var onlineSources int64
	var totalSources int64
	a.db.Model(&UpstreamSource{}).Count(&totalSources)
	a.db.Model(&UpstreamSource{}).Where("status = ?", SourceStatusOnline).Count(&onlineSources)
	var monthlySpend float64
	currentMonthStart := monthStart(now)
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", currentMonthStart, now).Select("COALESCE(sum(estimated_cost), 0)").Scan(&monthlySpend)
	lastMonthStart := currentMonthStart.AddDate(0, -1, 0)
	lastMonthEnd := lastMonthStart.Add(now.Sub(currentMonthStart))
	if lastMonthEnd.After(currentMonthStart) {
		lastMonthEnd = currentMonthStart
	}
	var previousMonthlySpend float64
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", lastMonthStart, lastMonthEnd).Select("COALESCE(sum(estimated_cost), 0)").Scan(&previousMonthlySpend)

	var trend []gin.H
	for i := 6; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		next := day.AddDate(0, 0, 1)
		var count int64
		a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", day, next).Count(&count)
		trend = append(trend, gin.H{"day": day.Format("01-02"), "value": count, "isToday": i == 0})
	}
	currentTrendStart := today.AddDate(0, 0, -6)
	previousTrendStart := currentTrendStart.AddDate(0, 0, -7)
	var currentTrendRequests int64
	var previousTrendRequests int64
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", currentTrendStart, now).Count(&currentTrendRequests)
	a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", previousTrendStart, currentTrendStart).Count(&previousTrendRequests)

	var sources []UpstreamSource
	a.db.Order("priority asc").Find(&sources)
	statuses := make([]gin.H, 0, len(sources))
	for _, source := range sources {
		statuses = append(statuses, gin.H{"name": source.Name, "status": source.Status, "load": source.Load, "latencyMs": source.LatencyMS})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"todayRequests":          todayRequests,
		"todayRequestsChangePct": percentChange(float64(todayRequests), float64(yesterdayRequests)),
		"activeUsers":            activeUsers,
		"activeUsersChange":      0,
		"upstreamOnline":         onlineSources,
		"upstreamTotal":          totalSources,
		"monthlySpend":           round2(monthlySpend),
		"monthlySpendPct":        percentChange(monthlySpend, previousMonthlySpend),
		"trendChangePct":         percentChange(float64(currentTrendRequests), float64(previousTrendRequests)),
		"trend7d":                trend,
		"upstreamStatuses":       statuses,
	}})
}

func (a *App) adminSettings(c *gin.Context) {
	settings, err := a.getSettings()
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "settings not initialized")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": settingsDTO(settings)})
}

func (a *App) adminUpdateSettings(c *gin.Context) {
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	settings, err := a.getSettings()
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "settings not initialized")
		return
	}
	updates := map[string]any{}
	for jsonKey, dbKey := range map[string]string{"platformName": "platform_name", "supportEmail": "support_email"} {
		if value, ok := req[jsonKey].(string); ok {
			updates[dbKey] = strings.TrimSpace(value)
		}
	}
	for jsonKey, dbKey := range map[string]string{
		"openRegistration":          "open_registration",
		"requireInviteCode":         "require_invite_code",
		"streamingEnabled":          "streaming_enabled",
		"hideUpstreamNameFromUsers": "hide_upstream_name_from_users",
	} {
		if value, ok := req[jsonKey].(bool); ok {
			updates[dbKey] = value
		}
	}
	if value, ok := numberFromMap(req, "defaultUserBalance"); ok {
		updates["default_user_balance"] = value
	}
	if value, ok := numberFromMap(req, "maxRetries"); ok {
		updates["max_retries"] = int(value)
	}
	if value, ok := numberFromMap(req, "defaultTimeout"); ok {
		updates["default_timeout"] = int(value)
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&settings).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update settings failed")
		return
	}
	settings, _ = a.getSettings()
	c.JSON(http.StatusOK, gin.H{"data": settingsDTO(settings)})
}

func (a *App) getSettings() (PlatformSettings, error) {
	var settings PlatformSettings
	err := a.db.First(&settings).Error
	return settings, err
}

func settingsDTO(settings PlatformSettings) gin.H {
	return gin.H{
		"platformName":              settings.PlatformName,
		"supportEmail":              settings.SupportEmail,
		"openRegistration":          settings.OpenRegistration,
		"requireInviteCode":         settings.RequireInviteCode,
		"defaultUserBalance":        settings.DefaultUserBalance,
		"maxRetries":                settings.MaxRetries,
		"defaultTimeout":            settings.DefaultTimeout,
		"streamingEnabled":          settings.StreamingEnabled,
		"hideUpstreamNameFromUsers": settings.HideUpstreamNameFromUsers,
	}
}

func (a *App) userTokenUsage(userID uint, since time.Time) int64 {
	var total int64
	a.db.Model(&UsageLog{}).Where("user_id = ? AND created_at >= ?", userID, since).Select("COALESCE(sum(total_tokens), 0)").Scan(&total)
	return total
}

func (a *App) sourceNameMap() map[uint]string {
	var sources []UpstreamSource
	a.db.Find(&sources)
	out := map[uint]string{}
	for _, source := range sources {
		out[source.ID] = source.Name
	}
	return out
}

func (a *App) sourceMap() map[uint]UpstreamSource {
	var sources []UpstreamSource
	a.db.Find(&sources)
	out := map[uint]UpstreamSource{}
	for _, source := range sources {
		out[source.ID] = source
	}
	return out
}

func (a *App) sourceKeyAliasMap() map[uint]string {
	var keys []SourceKey
	a.db.Find(&keys)
	out := map[uint]string{}
	for _, key := range keys {
		out[key.ID] = key.Alias
	}
	return out
}

func (a *App) modelDTOWithRouting(model ModelConfig) ModelDTO {
	sources := a.sourceMap()
	sourceKeys := a.sourceKeyAliasMap()
	groupNames := a.modelGroupNameMap()
	model.ModelGroupID = a.normalizeModelGroupID(model.ModelGroupID)
	dto := modelDTO(model, sources[model.SourceID].Name, sourceKeys[sourceKeyIDValue(model.SourceKeyID)])
	dto.ModelGroupName = groupNames[model.ModelGroupID]
	candidates := a.modelRouteCandidates(model.Name, model.ModelGroupID, sources, sourceKeys)
	dto.RoutingCandidates = candidates
	dto.CandidateCount = len(candidates)
	return dto
}

func (a *App) modelRouteCandidates(name string, groupID uint, sources map[uint]UpstreamSource, sourceKeys map[uint]string) []ModelRouteCandidateDTO {
	var models []ModelConfig
	defaultGroupID := a.defaultModelGroupID()
	query := a.db.Where("name = ?", name)
	if groupID == 0 || groupID == defaultGroupID {
		query = query.Where("model_group_id = ? OR model_group_id = 0", defaultGroupID)
	} else {
		query = query.Where("model_group_id = ?", groupID)
	}
	if err := query.Order("id asc").Find(&models).Error; err != nil {
		return []ModelRouteCandidateDTO{}
	}
	normalizedGroupID := groupID
	if normalizedGroupID == 0 {
		normalizedGroupID = defaultGroupID
	}
	return a.modelRouteCandidatesByGroup(models, sources, sourceKeys, defaultGroupID)[modelGroupBucketKey(name, normalizedGroupID)]
}

func (a *App) modelRouteCandidatesByGroup(models []ModelConfig, sources map[uint]UpstreamSource, sourceKeys map[uint]string, defaultGroupID uint) map[string][]ModelRouteCandidateDTO {
	out := map[string][]ModelRouteCandidateDTO{}
	for _, model := range models {
		model.ModelGroupID = modelGroupBucketID(model, defaultGroupID)
		bindings, err := a.modelBindings(model)
		if err != nil {
			continue
		}
		for _, binding := range bindings {
			source := sources[binding.SourceID]
			key := modelGroupBucketKey(model.Name, model.ModelGroupID)
			out[key] = append(out[key], modelRouteCandidateDTOFromBinding(model, binding, source, sourceKeys[sourceKeyIDValueFromBinding(binding)]))
		}
	}
	for key := range out {
		sort.SliceStable(out[key], func(i, j int) bool {
			left := out[key][i]
			right := out[key][j]
			if left.RoutingWeight != right.RoutingWeight {
				return left.RoutingWeight > right.RoutingWeight
			}
			if left.SourcePriority != right.SourcePriority {
				return left.SourcePriority < right.SourcePriority
			}
			return publicIDNumber(left.ID) < publicIDNumber(right.ID)
		})
	}
	return out
}

func sourceKeyIDValue(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}

func (a *App) getSourceForModel(sourceID uint) (UpstreamSource, error) {
	var source UpstreamSource
	if err := a.db.First(&source, sourceID).Error; err != nil {
		return UpstreamSource{}, fmt.Errorf("source not found")
	}
	return source, nil
}

func (a *App) resolveSourceKeyID(sourceID uint, raw string) (*uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "default") || strings.EqualFold(raw, "none") {
		return nil, nil
	}
	keyID, err := parseNumericID(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid sourceKeyId")
	}
	var key SourceKey
	if err := a.db.Where("id = ? AND source_id = ?", keyID, sourceID).First(&key).Error; err != nil {
		return nil, fmt.Errorf("source key not found")
	}
	if key.Status != APIKeyStatusValid {
		return nil, fmt.Errorf("source key is disabled")
	}
	return &key.ID, nil
}

func (a *App) requestLogDTOs(logs []UsageLog) []gin.H {
	userIDs := make([]uint, 0, len(logs))
	keyIDs := make([]uint, 0, len(logs))
	sourceKeyIDs := make([]uint, 0, len(logs))
	for _, row := range logs {
		userIDs = append(userIDs, row.UserID)
		keyIDs = append(keyIDs, row.APIKeyID)
		if row.SourceKeyID > 0 {
			sourceKeyIDs = append(sourceKeyIDs, row.SourceKeyID)
		}
	}
	var users []User
	a.db.Where("id IN ?", userIDs).Find(&users)
	userEmail := map[uint]string{}
	for _, user := range users {
		userEmail[user.ID] = user.Email
	}
	var keys []APIKey
	a.db.Where("id IN ?", keyIDs).Find(&keys)
	keyName := map[uint]string{}
	for _, key := range keys {
		keyName[key.ID] = key.Name
	}
	var sourceKeys []SourceKey
	a.db.Where("id IN ?", sourceKeyIDs).Find(&sourceKeys)
	sourceKeyAlias := map[uint]string{}
	for _, key := range sourceKeys {
		sourceKeyAlias[key.ID] = key.Alias
	}
	out := make([]gin.H, 0, len(logs))
	for _, row := range logs {
		out = append(out, gin.H{
			"id":               id("log", row.ID),
			"timestamp":        row.CreatedAt.UTC().Format(time.RFC3339),
			"requestId":        row.RequestID,
			"userEmail":        userEmail[row.UserID],
			"apiKeyName":       keyName[row.APIKeyID],
			"apiKeyId":         optionalPublicID("k", row.APIKeyID),
			"sourceId":         optionalPublicID("s", row.SourceID),
			"sourceKeyId":      optionalPublicID("sk", row.SourceKeyID),
			"sourceKeyAlias":   sourceKeyAlias[row.SourceKeyID],
			"protocol":         row.Protocol,
			"path":             row.Path,
			"stream":           row.Stream,
			"model":            row.Model,
			"upstreamName":     row.UpstreamName,
			"tokensPrompt":     row.PromptTokens,
			"tokensCompletion": row.CompletionTokens,
			"tokensCacheRead":  row.CacheReadTokens,
			"tokensCacheWrite": row.CacheWriteTokens,
			"tokensReasoning":  row.ReasoningTokens,
			"tokensTotal":      row.TotalTokens,
			"estimatedCost":    row.EstimatedCost,
			"latencyMs":        row.LatencyMS,
			"statusCode":       row.StatusCode,
			"statusText":       row.Status,
			"errorMessage":     row.ErrorMessage,
			"requestHeaders":   mustJSONMap([]byte(row.RequestHeaders)),
			"responseHeaders":  mustJSONMap([]byte(row.ResponseHeaders)),
			"requestPayload":   mustJSONMap([]byte(row.RequestPayload)),
			"responsePayload":  mustJSONMap([]byte(row.ResponsePayload)),
			"attemptCount":     row.AttemptCount,
			"finalAttemptId":   optionalPublicID("att", row.FinalAttemptID),
		})
	}
	return out
}

func requestAttemptDTOs(attempts []RequestAttempt) []gin.H {
	out := make([]gin.H, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, gin.H{
			"id":            optionalPublicID("att", attempt.ID),
			"usageLogId":    optionalPublicID("log", attempt.UsageLogID),
			"requestId":     attempt.RequestID,
			"attemptIndex":  attempt.AttemptIndex,
			"modelConfigId": optionalPublicID("m", attempt.ModelConfigID),
			"sourceId":      optionalPublicID("s", attempt.SourceID),
			"sourceKeyId":   optionalPublicID("sk", attempt.SourceKeyID),
			"model":         attempt.Model,
			"upstreamName":  attempt.UpstreamName,
			"protocol":      attempt.Protocol,
			"path":          attempt.Path,
			"statusCode":    attempt.StatusCode,
			"statusText":    attempt.Status,
			"errorMessage":  attempt.ErrorMessage,
			"latencyMs":     attempt.LatencyMS,
			"startedAt":     attempt.StartedAt.UTC().Format(time.RFC3339),
			"endedAt":       attempt.EndedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func optionalPublicID(prefix string, value uint) string {
	if value == 0 {
		return ""
	}
	return id(prefix, value)
}

type usageWindow struct {
	Range       string
	Granularity string
	Start       time.Time
	End         time.Time
}

func (a *App) usageStats(userID *uint, rawRange string, apiKeyID *uint) gin.H {
	window := usageWindowForRange(rawRange, time.Now())
	base := func() *gorm.DB {
		query := a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", window.Start, window.End)
		if userID != nil {
			query = query.Where("user_id = ?", *userID)
		}
		if apiKeyID != nil {
			query = query.Where("api_key_id = ?", *apiKeyID)
		}
		return query
	}
	var totalTokens int64
	var totalCost float64
	var totalRequests int64
	base().Select("COALESCE(sum(total_tokens), 0)").Scan(&totalTokens)
	base().Select("COALESCE(sum(estimated_cost), 0)").Scan(&totalCost)
	base().Count(&totalRequests)

	trend := make([]gin.H, 0)
	for _, bucket := range usageBuckets(window) {
		q := a.db.Model(&UsageLog{}).Where("created_at >= ? AND created_at < ?", bucket.Start, bucket.End)
		if userID != nil {
			q = q.Where("user_id = ?", *userID)
		}
		if apiKeyID != nil {
			q = q.Where("api_key_id = ?", *apiKeyID)
		}
		var tokens int64
		var cost float64
		var requests int64
		q.Select("COALESCE(sum(total_tokens), 0)").Scan(&tokens)
		q.Select("COALESCE(sum(estimated_cost), 0)").Scan(&cost)
		q.Count(&requests)
		trend = append(trend, gin.H{"date": bucket.Start.Format("2006-01-02"), "label": bucket.Label, "tokens": tokens, "cost": round2(cost), "requests": requests})
	}

	type modelAgg struct {
		Model  string
		Tokens int64
	}
	var rows []modelAgg
	modelQuery := base().Select("model, COALESCE(sum(total_tokens), 0) as tokens").Group("model").Order("tokens desc").Limit(8)
	if userID != nil {
		modelQuery = modelQuery.Where("user_id = ?", *userID)
	}
	modelQuery.Scan(&rows)
	colors := []string{"hsl(221, 83%, 53%)", "hsl(262, 70%, 60%)", "hsl(160, 60%, 45%)", "hsl(215, 16%, 47%)"}
	byModel := make([]gin.H, 0, len(rows))
	for i, row := range rows {
		pct := 0.0
		if totalTokens > 0 {
			pct = round2(float64(row.Tokens) / float64(totalTokens) * 100)
		}
		byModel = append(byModel, gin.H{"model": row.Model, "tokens": row.Tokens, "percentage": pct, "color": colors[i%len(colors)]})
	}
	byUser := make([]gin.H, 0)
	if userID == nil {
		type userAgg struct {
			UserID   uint
			Email    string
			Tokens   int64
			Requests int64
			Cost     float64
		}
		var users []userAgg
		userQuery := a.db.Table("usage_logs").
			Select("usage_logs.user_id as user_id, users.email as email, COALESCE(sum(usage_logs.total_tokens), 0) as tokens, count(*) as requests, COALESCE(sum(usage_logs.estimated_cost), 0) as cost").
			Joins("LEFT JOIN users ON users.id = usage_logs.user_id").
			Where("usage_logs.created_at >= ? AND usage_logs.created_at < ?", window.Start, window.End).
			Group("usage_logs.user_id, users.email").
			Order("tokens desc").
			Limit(5)
		if apiKeyID != nil {
			userQuery = userQuery.Where("usage_logs.api_key_id = ?", *apiKeyID)
		}
		userQuery.Scan(&users)
		for _, row := range users {
			pct := 0.0
			if totalTokens > 0 {
				pct = round2(float64(row.Tokens) / float64(totalTokens) * 100)
			}
			byUser = append(byUser, gin.H{"userId": id("u", row.UserID), "email": row.Email, "tokens": row.Tokens, "requests": row.Requests, "cost": round2(row.Cost), "percentage": pct})
		}
	}
	return gin.H{
		"totalTokens":   totalTokens,
		"totalCost":     round2(totalCost),
		"totalRequests": totalRequests,
		"trend":         trend,
		"byModel":       byModel,
		"byUser":        byUser,
		"range":         window.Range,
		"granularity":   window.Granularity,
	}
}

type usageBucket struct {
	Start time.Time
	End   time.Time
	Label string
}

func usageWindowForRange(raw string, now time.Time) usageWindow {
	rangeName := strings.ToLower(strings.TrimSpace(raw))
	switch rangeName {
	case "day":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return usageWindow{Range: "day", Granularity: "hour", Start: start, End: now}
	case "month":
		return usageWindow{Range: "month", Granularity: "day", Start: monthStart(now), End: now}
	default:
		return usageWindow{Range: "week", Granularity: "day", Start: weekStart(now), End: now}
	}
}

func usageBuckets(window usageWindow) []usageBucket {
	buckets := make([]usageBucket, 0)
	switch window.Granularity {
	case "hour":
		for i := 0; i < 24; i++ {
			start := window.Start.Add(time.Duration(i) * time.Hour)
			buckets = append(buckets, usageBucket{Start: start, End: start.Add(time.Hour), Label: start.Format("15:00")})
		}
	default:
		if window.Range == "week" {
			for i := 0; i < 7; i++ {
				start := window.Start.AddDate(0, 0, i)
				buckets = append(buckets, usageBucket{Start: start, End: start.AddDate(0, 0, 1), Label: start.Format("01-02")})
			}
			return buckets
		}
		for start := window.Start; !start.After(window.End); start = start.AddDate(0, 0, 1) {
			buckets = append(buckets, usageBucket{Start: start, End: start.AddDate(0, 0, 1), Label: start.Format("01-02")})
			if start.Year() == window.End.Year() && start.YearDay() == window.End.YearDay() {
				break
			}
		}
	}
	return buckets
}

func percentChange(current float64, previous float64) float64 {
	if previous == 0 {
		return 0
	}
	return round2((current - previous) / previous * 100)
}

func parseBoundedInt(raw string, fallback int, min int, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		value = fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func optionalNumericQuery(c *gin.Context, key string) (uint, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" || raw == "all" {
		return 0, false
	}
	value, err := parseNumericID(raw)
	return value, err == nil
}

func parseQueryTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func numberFromMap(m map[string]any, key string) (float64, bool) {
	switch value := m[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case jsonNumber:
		f, err := value.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}

func toSnake(value string) string {
	switch value {
	case "monthlyQuota":
		return "monthly_quota"
	case "weeklyQuota":
		return "weekly_quota"
	default:
		return value
	}
}

func dbErrNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

func (a *App) adminSyncPricing(c *gin.Context) {
	result, err := a.syncModelPricing()
	if err != nil {
		errorJSON(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
}

func (a *App) adminPricingStatus(c *gin.Context) {
	c.JSON(http.StatusOK, globalPricingCache.status())
}
