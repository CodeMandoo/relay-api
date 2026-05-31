package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	cliProxyUsageQueueBatchSize  = 500
	cliProxyUsageQueueMaxBatches = 5
)

type cliProxyUsageQueueRecord struct {
	Timestamp       time.Time                    `json:"timestamp"`
	Provider        string                       `json:"provider"`
	Model           string                       `json:"model"`
	Alias           string                       `json:"alias"`
	Endpoint        string                       `json:"endpoint"`
	Source          string                       `json:"source"`
	AuthIndex       string                       `json:"auth_index"`
	AuthType        string                       `json:"auth_type"`
	APIKey          string                       `json:"api_key"`
	RequestID       string                       `json:"request_id"`
	ReasoningEffort string                       `json:"reasoning_effort"`
	LatencyMS       int64                        `json:"latency_ms"`
	Failed          bool                         `json:"failed"`
	Fail            cliProxyUsageQueueFail       `json:"fail"`
	Tokens          cliProxyUsageQueueTokenStats `json:"tokens"`
}

type cliProxyUsageQueueFail struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type cliProxyUsageQueueTokenStats struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

func (a *App) adminSourceAccountTokenUsage(c *gin.Context) {
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

	syncedCount := 0
	syncError := ""
	if sourceSupportsAccountPool(source) && strings.TrimSpace(source.ManagementKey) != "" {
		count, err := a.collectCLIProxyUsageQueue(c.Request.Context(), source)
		if err != nil {
			syncError = err.Error()
		} else {
			syncedCount = count
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": a.sourceAccountTokenUsageDTO(account, syncedCount, syncError)})
}

func (a *App) sourceAccountTokenUsageDTO(account SourceAccount, syncedCount int, syncError string) SourceAccountTokenUsageDTO {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := weekStart(now)
	month := monthStart(now)
	return SourceAccountTokenUsageDTO{
		AccountID:   id("a", account.ID),
		DayTokens:   a.sourceAccountTokensSince(account.ID, &dayStart),
		WeekTokens:  a.sourceAccountTokensSince(account.ID, &week),
		MonthTokens: a.sourceAccountTokensSince(account.ID, &month),
		TotalTokens: a.sourceAccountTokensSince(account.ID, nil),
		SyncedCount: syncedCount,
		SyncError:   syncError,
	}
}

func (a *App) sourceAccountTokensSince(accountID uint, since *time.Time) int64 {
	query := a.db.Model(&SourceAccountUsageLog{}).Where("source_account_id = ?", accountID)
	if since != nil {
		query = query.Where("created_at >= ?", *since)
	}
	var total int64
	query.Select("COALESCE(sum(total_tokens), 0)").Scan(&total)
	return total
}

func (a *App) collectCLIProxyUsageQueue(ctx context.Context, source UpstreamSource) (int, error) {
	total := 0
	for batch := 0; batch < cliProxyUsageQueueMaxBatches; batch++ {
		var payload []json.RawMessage
		endpoint := fmt.Sprintf("/v0/management/usage-queue?count=%d", cliProxyUsageQueueBatchSize)
		if err := a.cliProxyJSON(ctx, source, http.MethodGet, endpoint, nil, &payload); err != nil {
			return total, err
		}
		if len(payload) == 0 {
			return total, nil
		}
		for _, raw := range payload {
			record, ok := parseCLIProxyUsageQueueRecord(raw)
			if !ok {
				continue
			}
			saved, err := a.storeCLIProxyUsageRecord(source.ID, record)
			if err != nil {
				return total, err
			}
			if saved {
				total++
			}
		}
		if len(payload) < cliProxyUsageQueueBatchSize {
			return total, nil
		}
	}
	return total, nil
}

func (a *App) collectCLIProxyUsageQueueAsync(source UpstreamSource) {
	if !sourceSupportsAccountPool(source) || strings.TrimSpace(source.ManagementKey) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.collectCLIProxyUsageQueue(ctx, source)
	}()
}

func parseCLIProxyUsageQueueRecord(raw json.RawMessage) (cliProxyUsageQueueRecord, bool) {
	var record cliProxyUsageQueueRecord
	if len(raw) == 0 {
		return record, false
	}
	if err := json.Unmarshal(raw, &record); err == nil && record.hasData() {
		return record, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || strings.TrimSpace(text) == "" {
		return record, false
	}
	if err := json.Unmarshal([]byte(text), &record); err != nil || !record.hasData() {
		return record, false
	}
	return record, true
}

func (r cliProxyUsageQueueRecord) hasData() bool {
	return strings.TrimSpace(r.Source) != "" ||
		strings.TrimSpace(r.AuthIndex) != "" ||
		strings.TrimSpace(r.Provider) != "" ||
		strings.TrimSpace(r.Model) != "" ||
		r.Tokens.TotalTokens != 0 ||
		r.Tokens.InputTokens != 0 ||
		r.Tokens.OutputTokens != 0
}

func (a *App) storeCLIProxyUsageRecord(sourceID uint, record cliProxyUsageQueueRecord) (bool, error) {
	account, ok := a.matchSourceAccountForUsage(sourceID, record)
	if !ok {
		return false, nil
	}
	tokens := record.Tokens
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	createdAt := record.Timestamp
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	row := SourceAccountUsageLog{
		SourceID:            sourceID,
		SourceAccountID:     account.ID,
		Provider:            cliProxyProviderLabel(record.Provider),
		Identifier:          firstNonEmpty(strings.TrimSpace(record.Source), account.Identifier),
		AuthIndex:           strings.TrimSpace(record.AuthIndex),
		Model:               firstNonEmpty(record.Alias, record.Model),
		Endpoint:            strings.TrimSpace(record.Endpoint),
		RequestID:           strings.TrimSpace(record.RequestID),
		InputTokens:         tokens.InputTokens,
		OutputTokens:        tokens.OutputTokens,
		ReasoningTokens:     tokens.ReasoningTokens,
		CachedTokens:        tokens.CachedTokens,
		CacheReadTokens:     tokens.CacheReadTokens,
		CacheCreationTokens: tokens.CacheCreationTokens,
		TotalTokens:         tokens.TotalTokens,
		Failed:              record.Failed,
		StatusCode:          record.Fail.StatusCode,
		LatencyMS:           record.LatencyMS,
		CreatedAt:           createdAt,
	}
	return true, a.db.Create(&row).Error
}

func (a *App) matchSourceAccountForUsage(sourceID uint, record cliProxyUsageQueueRecord) (SourceAccount, bool) {
	var account SourceAccount
	authIndex := strings.TrimSpace(record.AuthIndex)
	if authIndex != "" {
		if err := a.db.Where("source_id = ? AND auth_index = ?", sourceID, authIndex).First(&account).Error; err == nil {
			return account, true
		}
	}

	sourceValue := strings.TrimSpace(record.Source)
	if sourceValue == "" {
		return SourceAccount{}, false
	}
	provider := cliProxyProviderLabel(record.Provider)
	if provider != "" && provider != "unknown" {
		if err := a.db.Where("source_id = ? AND provider = ? AND lower(identifier) = lower(?)", sourceID, provider, sourceValue).First(&account).Error; err == nil {
			return account, true
		}
	}
	if err := a.db.Where("source_id = ? AND lower(identifier) = lower(?)", sourceID, sourceValue).First(&account).Error; err == nil {
		return account, true
	}
	if err := a.db.Where("source_id = ? AND auth_file_name = ?", sourceID, sourceValue).First(&account).Error; err == nil {
		return account, true
	}
	return SourceAccount{}, false
}
