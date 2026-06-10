package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	UserStatusNormal   = "normal"
	UserStatusDisabled = "disabled"

	SourceStatusOnline   = "online"
	SourceStatusOffline  = "offline"
	SourceStatusDisabled = "disabled"

	ModelStatusActive   = "active"
	ModelStatusDisabled = "disabled"

	APIKeyStatusValid    = "valid"
	APIKeyStatusDisabled = "disabled"

	InviteStatusActive   = "active"
	InviteStatusDisabled = "disabled"

	EmailCodePurposeRegister = "register"

	RequestStatusSuccess = "success"
	RequestStatusError   = "error"

	DefaultModelGroupName = "默认分组"
)

type User struct {
	ID           uint   `gorm:"primaryKey"`
	Email        string `gorm:"size:255;uniqueIndex;not null"`
	Name         string `gorm:"size:255;not null"`
	PasswordHash string `gorm:"size:255;not null"`
	Role         string `gorm:"size:20;index;not null"`
	Status       string `gorm:"size:20;index;not null;default:normal"`
	InviteCode   string `gorm:"size:64;index"`
	MonthlyQuota int64  `gorm:"not null;default:0"`
	WeeklyQuota  int64  `gorm:"not null;default:0"`
	Balance      float64
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

type InviteCode struct {
	ID        uint   `gorm:"primaryKey"`
	Code      string `gorm:"size:64;uniqueIndex;not null"`
	MaxUse    int    `gorm:"not null;default:1"`
	UsedCount int    `gorm:"not null;default:0"`
	CreatedBy uint
	ExpiresAt *time.Time
	Remark    string `gorm:"size:255"`
	Status    string `gorm:"size:20;index;not null;default:active"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type EmailVerificationCode struct {
	ID        uint      `gorm:"primaryKey"`
	Email     string    `gorm:"size:255;index;not null"`
	Purpose   string    `gorm:"size:50;index;not null"`
	CodeHash  string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	UsedAt    *time.Time
	Attempts  int       `gorm:"not null;default:0"`
	SentAt    time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type ModelGroup struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"size:120;index;not null"`
	Description  string `gorm:"size:255"`
	BindingsJSON string `gorm:"type:text"`
	IsDefault    bool   `gorm:"not null;default:false"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

type UpstreamSource struct {
	ID               uint   `gorm:"primaryKey"`
	Name             string `gorm:"size:255;not null;index"`
	Type             string `gorm:"size:50;not null;index"`
	BaseURL          string `gorm:"size:512;not null"`
	OpenAIBaseURL    string `gorm:"size:512"`
	AnthropicBaseURL string `gorm:"size:512"`
	APIKey           string `gorm:"type:text"`
	ManagementKey    string `gorm:"type:text"`
	Priority         int    `gorm:"not null;default:100"`
	Status           string `gorm:"size:20;index;not null;default:online"`
	Load             int
	LatencyMS        int
	AccountCount     int
	FailureCount     int
	LastFailureAt    *time.Time
	CooldownUntil    *time.Time `gorm:"index"`
	SuccessCount     int64
	LastSuccessAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt `gorm:"index"`
}

type SourceAccount struct {
	ID                    uint   `gorm:"primaryKey"`
	SourceID              uint   `gorm:"index;not null"`
	Identifier            string `gorm:"size:255;not null"`
	Provider              string `gorm:"size:50;not null"`
	AuthFileName          string `gorm:"size:255;index"`
	AuthIndex             string `gorm:"size:255;index"`
	ChatGPTAccountID      string `gorm:"size:255"`
	WorkspaceID           string `gorm:"size:255"`
	PlanType              string `gorm:"size:50"`
	OpenAIPlanType        string `gorm:"column:openai_plan_type;size:100"`
	SubscriptionPlan      string `gorm:"size:50"`
	HasSubscription       bool
	SubscriptionExpiresAt *time.Time
	SubscriptionRenewsAt  *time.Time
	Status                string `gorm:"size:20;index;not null;default:valid"`
	Balance               float64
	BalanceLimit          float64
	Used5h                int64
	Limit5h               int64
	Used7d                int64
	Limit7d               int64
	SuccessCount          int64
	FailedCount           int64
	RecentRequests        int64
	NextRefresh5h         *time.Time
	NextRefresh7d         *time.Time
	LastRefreshed         time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             gorm.DeletedAt `gorm:"index"`
}

type SourceAccountUsageLog struct {
	ID                  uint   `gorm:"primaryKey"`
	SourceID            uint   `gorm:"index;not null"`
	SourceAccountID     uint   `gorm:"index;not null"`
	Provider            string `gorm:"size:50;index"`
	Identifier          string `gorm:"size:255;index"`
	AuthIndex           string `gorm:"size:255;index"`
	Model               string `gorm:"size:255;index"`
	Endpoint            string `gorm:"size:255"`
	RequestID           string `gorm:"size:255;index"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64 `gorm:"index"`
	Failed              bool
	StatusCode          int
	LatencyMS           int64
	CreatedAt           time.Time `gorm:"index"`
}

type SourceKey struct {
	ID         uint   `gorm:"primaryKey"`
	SourceID   uint   `gorm:"index;not null"`
	Alias      string `gorm:"size:120;not null"`
	APIKey     string `gorm:"type:text;not null"`
	Status     string `gorm:"size:20;index;not null;default:valid"`
	LastUsedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

type ModelConfig struct {
	ID                 uint   `gorm:"primaryKey"`
	ModelGroupID       uint   `gorm:"index;not null;default:0"`
	SourceID           uint   `gorm:"index;not null"`
	SourceKeyID        *uint  `gorm:"index"`
	Name               string `gorm:"size:255;index;not null"`
	DisplayName        string `gorm:"size:255"`
	Provider           string `gorm:"size:50;not null"`
	Formats            string `gorm:"size:80"`
	InputPrice         float64
	OutputPrice        float64
	CacheWritePrice    float64
	CacheReadPrice     float64
	InputMultiple      float64 `gorm:"not null;default:1"`
	OutputMultiple     float64 `gorm:"not null;default:1"`
	CacheWriteMultiple float64 `gorm:"not null;default:1"`
	CacheReadMultiple  float64 `gorm:"not null;default:1"`
	BillingInput       float64
	BillingOutput      float64
	BillingCacheWrite  float64
	BillingCacheRead   float64
	Status             string `gorm:"size:20;index;not null;default:active"`
	LatencyMS          int
	RoutingWeight      int  `gorm:"not null;default:1"`
	RoutingEnabled     bool `gorm:"not null;default:true"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

type ModelRouteBinding struct {
	ID             uint `gorm:"primaryKey"`
	ModelID        uint `gorm:"index;not null"`
	SourceID       uint `gorm:"index;not null"`
	SourceKeyID    *uint
	RoutingWeight  int  `gorm:"not null;default:1"`
	RoutingEnabled bool `gorm:"not null;default:true"`
	Enabled        bool `gorm:"not null;default:true"`
	LatencyMS      int
	SchedulerState string `gorm:"size:20;index;not null;default:closed"`
	FailureCount   int
	SuccessStreak  int
	CooldownUntil  *time.Time `gorm:"index"`
	LastFailureAt  *time.Time
	LastSuccessAt  *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

type APIKey struct {
	ID           uint   `gorm:"primaryKey"`
	UserID       uint   `gorm:"index;not null"`
	ModelGroupID uint   `gorm:"index;not null;default:0"`
	Name         string `gorm:"size:255;not null"`
	Secret       string `gorm:"size:160;uniqueIndex;not null"`
	KeyHash      string `gorm:"size:64;uniqueIndex;not null"`
	Masked       string `gorm:"size:80;not null"`
	Status       string `gorm:"size:20;index;not null;default:valid"`
	LimitUSD     *float64
	SpentUSD     float64
	LastUsedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

type UsageLog struct {
	ID               uint   `gorm:"primaryKey"`
	UserID           uint   `gorm:"index;not null"`
	APIKeyID         uint   `gorm:"index"`
	SourceID         uint   `gorm:"index"`
	SourceKeyID      uint   `gorm:"index"`
	RequestID        string `gorm:"size:255;index"`
	Protocol         string `gorm:"size:30;index"`
	Path             string `gorm:"size:255"`
	Stream           bool
	Model            string `gorm:"size:255;index"`
	UpstreamName     string `gorm:"size:255"`
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	TotalTokens      int64 `gorm:"index"`
	EstimatedCost    float64
	LatencyMS        int64
	StatusCode       int
	Status           string `gorm:"size:20;index;not null"`
	ErrorMessage     string `gorm:"type:text"`
	RequestHeaders   string `gorm:"type:text"`
	ResponseHeaders  string `gorm:"type:text"`
	RequestPayload   string `gorm:"type:text"`
	ResponsePayload  string `gorm:"type:text"`
	AttemptCount     int
	FinalAttemptID   uint
	CreatedAt        time.Time `gorm:"index"`
}

type RequestAttempt struct {
	ID            uint   `gorm:"primaryKey"`
	UsageLogID    uint   `gorm:"index;not null"`
	RequestID     string `gorm:"size:255;index"`
	AttemptIndex  int    `gorm:"index;not null"`
	ModelConfigID uint   `gorm:"index"`
	SourceID      uint   `gorm:"index"`
	SourceKeyID   uint   `gorm:"index"`
	Model         string `gorm:"size:255;index"`
	UpstreamName  string `gorm:"size:255"`
	Protocol      string `gorm:"size:30;index"`
	Path          string `gorm:"size:255"`
	StatusCode    int
	Status        string `gorm:"size:20;index;not null"`
	ErrorMessage  string `gorm:"type:text"`
	LatencyMS     int64
	StartedAt     time.Time `gorm:"index"`
	EndedAt       time.Time `gorm:"index"`
	CreatedAt     time.Time
}

type PlatformSettings struct {
	ID                        uint `gorm:"primaryKey"`
	PlatformName              string
	SupportEmail              string
	OpenRegistration          bool
	RequireInviteCode         bool
	DefaultUserBalance        float64
	MaxRetries                int
	DefaultTimeout            int
	StreamingEnabled          bool
	HideUpstreamNameFromUsers bool
	UpdatedAt                 time.Time
}

type UserDTO struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	Name          string  `json:"name"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	InviteCode    string  `json:"inviteCode,omitempty"`
	RegisteredAt  string  `json:"registeredAt"`
	MonthlyQuota  int64   `json:"monthlyQuota"`
	WeeklyQuota   int64   `json:"weeklyQuota"`
	UsedThisMonth int64   `json:"usedThisMonth"`
	Balance       float64 `json:"balance"`
	AvatarText    string  `json:"avatarText,omitempty"`
}

type AuthUserDTO struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	AvatarText string `json:"avatarText"`
}

type InviteDTO struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	UsedCount int    `json:"usedCount"`
	Limit     int    `json:"limit"`
	Remark    string `json:"remark"`
	Status    string `json:"status"`
}

type SourceDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	APIBase          string `json:"apiBase"`
	OpenAIBaseURL    string `json:"openaiBaseUrl,omitempty"`
	AnthropicBaseURL string `json:"anthropicBaseUrl,omitempty"`
	APIKey           string `json:"apiKey,omitempty"`
	MaskedKey        string `json:"maskedKey,omitempty"`
	ManagementKey    string `json:"managementKey,omitempty"`
	HasManagementKey bool   `json:"hasManagementKey"`
	AccountCount     int    `json:"accountCount"`
	Priority         int    `json:"priority"`
	Status           string `json:"status"`
	Load             int    `json:"load"`
	LatencyMS        int    `json:"latencyMs"`
	FailureCount     int    `json:"failureCount"`
	SuccessCount     int64  `json:"successCount"`
	LastFailureAt    string `json:"lastFailureAt,omitempty"`
	LastSuccessAt    string `json:"lastSuccessAt,omitempty"`
	CooldownUntil    string `json:"cooldownUntil,omitempty"`
	CoolingDown      bool   `json:"coolingDown"`
	CreatedAt        string `json:"createdAt,omitempty"`
}

type SourceAccountDTO struct {
	ID                    string  `json:"id"`
	SourceID              string  `json:"sourceId"`
	Identifier            string  `json:"identifier"`
	Provider              string  `json:"provider"`
	AuthIndex             string  `json:"authIndex,omitempty"`
	PlanType              string  `json:"planType,omitempty"`
	OpenAIPlanType        string  `json:"openaiPlanType"`
	SubscriptionPlan      string  `json:"subscriptionPlan,omitempty"`
	HasSubscription       bool    `json:"hasSubscription"`
	SubscriptionExpiresAt string  `json:"subscriptionExpiresAt,omitempty"`
	SubscriptionRenewsAt  string  `json:"subscriptionRenewsAt,omitempty"`
	Status                string  `json:"status"`
	Balance               float64 `json:"balance"`
	BalanceLimit          float64 `json:"balanceLimit"`
	Used5h                int64   `json:"used5h"`
	Limit5h               int64   `json:"limit5h"`
	Used7d                int64   `json:"used7d"`
	Limit7d               int64   `json:"limit7d"`
	SuccessCount          int64   `json:"successCount"`
	FailedCount           int64   `json:"failedCount"`
	RecentRequests        int64   `json:"recentRequests"`
	NextRefresh5h         string  `json:"nextRefresh5h,omitempty"`
	NextRefresh7d         string  `json:"nextRefresh7d,omitempty"`
	LastRefreshed         string  `json:"lastRefreshed"`
}

type SourceAccountTokenUsageDTO struct {
	AccountID   string `json:"accountId"`
	DayTokens   int64  `json:"dayTokens"`
	WeekTokens  int64  `json:"weekTokens"`
	MonthTokens int64  `json:"monthTokens"`
	TotalTokens int64  `json:"totalTokens"`
	SyncedCount int    `json:"syncedCount"`
	SyncError   string `json:"syncError,omitempty"`
}

type SourceKeyDTO struct {
	ID         string  `json:"id"`
	SourceID   string  `json:"sourceId"`
	Alias      string  `json:"alias"`
	Key        string  `json:"key,omitempty"`
	Masked     string  `json:"masked"`
	Status     string  `json:"status"`
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

type ModelDTO struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	ModelGroupID       string                   `json:"modelGroupId,omitempty"`
	ModelGroupName     string                   `json:"modelGroupName,omitempty"`
	SourceID           string                   `json:"sourceId"`
	SourceName         string                   `json:"sourceName"`
	SourceKeyID        string                   `json:"sourceKeyId,omitempty"`
	SourceKeyAlias     string                   `json:"sourceKeyAlias,omitempty"`
	Provider           string                   `json:"provider"`
	Formats            []string                 `json:"formats"`
	InputPrice         float64                  `json:"inputPrice"`
	OutputPrice        float64                  `json:"outputPrice"`
	CacheWritePrice    float64                  `json:"cacheWritePrice"`
	CacheReadPrice     float64                  `json:"cacheReadPrice"`
	InputMultiple      float64                  `json:"inputMultiple"`
	OutputMultiple     float64                  `json:"outputMultiple"`
	CacheWriteMultiple float64                  `json:"cacheWriteMultiple"`
	CacheReadMultiple  float64                  `json:"cacheReadMultiple"`
	BillingInput       float64                  `json:"billingInput"`
	BillingOutput      float64                  `json:"billingOutput"`
	BillingCacheWrite  float64                  `json:"billingCacheWrite"`
	BillingCacheRead   float64                  `json:"billingCacheRead"`
	Enabled            bool                     `json:"enabled"`
	RoutingWeight      int                      `json:"routingWeight"`
	RoutingEnabled     bool                     `json:"routingEnabled"`
	CandidateCount     int                      `json:"candidateCount"`
	RoutingCandidates  []ModelRouteCandidateDTO `json:"routingCandidates,omitempty"`
}

type ModelRouteCandidateDTO struct {
	ID             string `json:"id"`
	SourceID       string `json:"sourceId"`
	SourceName     string `json:"sourceName"`
	SourceStatus   string `json:"sourceStatus"`
	SourcePriority int    `json:"sourcePriority"`
	SourceKeyID    string `json:"sourceKeyId,omitempty"`
	SourceKeyAlias string `json:"sourceKeyAlias,omitempty"`
	RoutingWeight  int    `json:"routingWeight"`
	RoutingEnabled bool   `json:"routingEnabled"`
	ModelEnabled   bool   `json:"modelEnabled"`
	CoolingDown    bool   `json:"coolingDown"`
	CooldownUntil  string `json:"cooldownUntil,omitempty"`
	SchedulerState string `json:"schedulerState,omitempty"`
}

type APIKeyDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Key            string   `json:"key"`
	Masked         string   `json:"masked"`
	CreatedAt      string   `json:"createdAt"`
	LastUsedAt     *string  `json:"lastUsedAt,omitempty"`
	Status         string   `json:"status"`
	Limit          *float64 `json:"limit,omitempty"`
	Spent          float64  `json:"spent"`
	ModelGroupID   string   `json:"modelGroupId,omitempty"`
	ModelGroupName string   `json:"modelGroupName,omitempty"`
}

type ModelGroupDTO struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	IsDefault   bool                  `json:"isDefault"`
	KeyCount    int64                 `json:"keyCount,omitempty"`
	ModelCount  int64                 `json:"modelCount,omitempty"`
	Bindings    []modelBindingRequest `json:"bindings,omitempty"`
	CreatedAt   string                `json:"createdAt"`
}

func userDTO(user User, used int64) UserDTO {
	return UserDTO{
		ID:            id("u", user.ID),
		Email:         user.Email,
		Name:          user.Name,
		Role:          user.Role,
		Status:        user.Status,
		InviteCode:    user.InviteCode,
		RegisteredAt:  user.CreatedAt.UTC().Format(time.RFC3339),
		MonthlyQuota:  user.MonthlyQuota,
		WeeklyQuota:   user.WeeklyQuota,
		UsedThisMonth: used,
		Balance:       user.Balance,
		AvatarText:    avatarText(user.Email),
	}
}

func authUserDTO(user User) AuthUserDTO {
	return AuthUserDTO{
		ID:         id("auth", user.ID),
		Email:      user.Email,
		Name:       user.Name,
		Role:       user.Role,
		AvatarText: avatarText(user.Email),
	}
}

func inviteDTO(invite InviteCode, now time.Time) InviteDTO {
	expiresAt := ""
	if invite.ExpiresAt != nil {
		expiresAt = invite.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return InviteDTO{
		ID:        id("i", invite.ID),
		Code:      invite.Code,
		CreatedAt: invite.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: expiresAt,
		UsedCount: invite.UsedCount,
		Limit:     invite.MaxUse,
		Remark:    invite.Remark,
		Status:    invitePublicStatus(invite, now),
	}
}

func sourceDTO(source UpstreamSource, includeSecret bool) SourceDTO {
	out := SourceDTO{
		ID:               id("s", source.ID),
		Name:             source.Name,
		Type:             source.Type,
		APIBase:          source.BaseURL,
		OpenAIBaseURL:    source.OpenAIBaseURL,
		AnthropicBaseURL: source.AnthropicBaseURL,
		MaskedKey:        maskSecret(source.APIKey),
		HasManagementKey: strings.TrimSpace(source.ManagementKey) != "",
		AccountCount:     source.AccountCount,
		Priority:         source.Priority,
		Status:           source.Status,
		Load:             source.Load,
		LatencyMS:        source.LatencyMS,
		FailureCount:     source.FailureCount,
		SuccessCount:     source.SuccessCount,
		CoolingDown:      source.CooldownUntil != nil && source.CooldownUntil.After(time.Now()),
		CreatedAt:        source.CreatedAt.UTC().Format(time.RFC3339),
	}
	if source.LastFailureAt != nil {
		out.LastFailureAt = source.LastFailureAt.UTC().Format(time.RFC3339)
	}
	if source.LastSuccessAt != nil {
		out.LastSuccessAt = source.LastSuccessAt.UTC().Format(time.RFC3339)
	}
	if source.CooldownUntil != nil {
		out.CooldownUntil = source.CooldownUntil.UTC().Format(time.RFC3339)
	}
	if includeSecret {
		out.APIKey = source.APIKey
		out.ManagementKey = source.ManagementKey
	}
	return out
}

func sourceAccountDTO(account SourceAccount) SourceAccountDTO {
	out := SourceAccountDTO{
		ID:               id("a", account.ID),
		SourceID:         id("s", account.SourceID),
		Identifier:       account.Identifier,
		Provider:         cliProxyProviderLabel(account.Provider),
		AuthIndex:        account.AuthIndex,
		PlanType:         account.PlanType,
		OpenAIPlanType:   account.OpenAIPlanType,
		SubscriptionPlan: account.SubscriptionPlan,
		HasSubscription:  account.HasSubscription,
		Status:           account.Status,
		Balance:          account.Balance,
		BalanceLimit:     account.BalanceLimit,
		Used5h:           account.Used5h,
		Limit5h:          account.Limit5h,
		Used7d:           account.Used7d,
		Limit7d:          account.Limit7d,
		SuccessCount:     account.SuccessCount,
		FailedCount:      account.FailedCount,
		RecentRequests:   account.RecentRequests,
		LastRefreshed:    account.LastRefreshed.UTC().Format(time.RFC3339),
	}
	if account.NextRefresh5h != nil {
		out.NextRefresh5h = account.NextRefresh5h.UTC().Format(time.RFC3339)
	}
	if account.NextRefresh7d != nil {
		out.NextRefresh7d = account.NextRefresh7d.UTC().Format(time.RFC3339)
	}
	if account.SubscriptionExpiresAt != nil {
		out.SubscriptionExpiresAt = account.SubscriptionExpiresAt.UTC().Format(time.RFC3339)
	}
	if account.SubscriptionRenewsAt != nil {
		out.SubscriptionRenewsAt = account.SubscriptionRenewsAt.UTC().Format(time.RFC3339)
	}
	return out
}

func sourceKeyDTO(key SourceKey, includeSecret bool) SourceKeyDTO {
	var last *string
	if key.LastUsedAt != nil {
		v := key.LastUsedAt.UTC().Format(time.RFC3339)
		last = &v
	}
	out := SourceKeyDTO{
		ID:         id("sk", key.ID),
		SourceID:   id("s", key.SourceID),
		Alias:      key.Alias,
		Masked:     maskSecret(key.APIKey),
		Status:     key.Status,
		LastUsedAt: last,
		CreatedAt:  key.CreatedAt.UTC().Format(time.RFC3339),
	}
	if includeSecret {
		out.Key = key.APIKey
	}
	return out
}

func modelDTO(model ModelConfig, sourceName string, sourceKeyAlias string) ModelDTO {
	inputPrice := model.InputPrice
	if inputPrice == 0 && model.BillingInput > 0 {
		inputPrice = model.BillingInput
	}
	outputPrice := model.OutputPrice
	if outputPrice == 0 && model.BillingOutput > 0 {
		outputPrice = model.BillingOutput
	}
	cacheWritePrice := model.CacheWritePrice
	if cacheWritePrice == 0 && model.BillingCacheWrite > 0 {
		cacheWritePrice = model.BillingCacheWrite
	}
	cacheReadPrice := model.CacheReadPrice
	if cacheReadPrice == 0 && model.BillingCacheRead > 0 {
		cacheReadPrice = model.BillingCacheRead
	}
	out := ModelDTO{
		ID:                 id("m", model.ID),
		Name:               model.Name,
		ModelGroupID:       optionalPublicID("mg", model.ModelGroupID),
		SourceID:           id("s", model.SourceID),
		SourceName:         sourceName,
		Provider:           model.Provider,
		Formats:            modelFormatList(model),
		InputPrice:         inputPrice,
		OutputPrice:        outputPrice,
		CacheWritePrice:    cacheWritePrice,
		CacheReadPrice:     cacheReadPrice,
		InputMultiple:      nonZeroFloat(model.InputMultiple, 1),
		OutputMultiple:     nonZeroFloat(model.OutputMultiple, 1),
		CacheWriteMultiple: nonZeroFloat(model.CacheWriteMultiple, 1),
		CacheReadMultiple:  nonZeroFloat(model.CacheReadMultiple, 1),
		BillingInput:       model.BillingInput,
		BillingOutput:      model.BillingOutput,
		BillingCacheWrite:  model.BillingCacheWrite,
		BillingCacheRead:   model.BillingCacheRead,
		Enabled:            model.Status == ModelStatusActive,
		RoutingWeight:      nonZeroInt(model.RoutingWeight, 1),
		RoutingEnabled:     model.RoutingEnabled,
		CandidateCount:     1,
	}
	if model.SourceKeyID != nil && *model.SourceKeyID > 0 {
		out.SourceKeyID = id("sk", *model.SourceKeyID)
		out.SourceKeyAlias = sourceKeyAlias
	}
	return out
}

func modelRouteCandidateDTO(model ModelConfig, source UpstreamSource, sourceKeyAlias string) ModelRouteCandidateDTO {
	return modelRouteCandidateDTOFromBinding(model, legacyBindingFromModel(model), source, sourceKeyAlias)
}

func modelRouteCandidateDTOFromBinding(model ModelConfig, binding ModelRouteBinding, source UpstreamSource, sourceKeyAlias string) ModelRouteCandidateDTO {
	candidateID := id("mb", binding.ID)
	if binding.ID == 0 {
		candidateID = id("m", model.ID)
	}
	now := time.Now()
	cooldownUntil := source.CooldownUntil
	if binding.CooldownUntil != nil {
		cooldownUntil = binding.CooldownUntil
	}
	out := ModelRouteCandidateDTO{
		ID:             candidateID,
		SourceID:       id("s", binding.SourceID),
		SourceName:     source.Name,
		SourceStatus:   source.Status,
		SourcePriority: source.Priority,
		RoutingWeight:  nonZeroInt(binding.RoutingWeight, 1),
		RoutingEnabled: binding.RoutingEnabled,
		ModelEnabled:   model.Status == ModelStatusActive && binding.Enabled,
		CoolingDown:    cooldownUntil != nil && cooldownUntil.After(now),
		SchedulerState: schedulerBindingState(binding),
	}
	if binding.SourceKeyID != nil && *binding.SourceKeyID > 0 {
		out.SourceKeyID = id("sk", *binding.SourceKeyID)
		out.SourceKeyAlias = sourceKeyAlias
	}
	if cooldownUntil != nil {
		out.CooldownUntil = cooldownUntil.UTC().Format(time.RFC3339)
	}
	return out
}

func apiKeyDTO(key APIKey, reveal bool) APIKeyDTO {
	return apiKeyDTOWithGroup(key, reveal, "")
}

func apiKeyDTOWithGroup(key APIKey, reveal bool, groupName string) APIKeyDTO {
	var last *string
	if key.LastUsedAt != nil {
		v := key.LastUsedAt.UTC().Format(time.RFC3339)
		last = &v
	}
	visible := key.Masked
	if reveal {
		visible = key.Secret
	}
	return APIKeyDTO{
		ID:             id("k", key.ID),
		Name:           key.Name,
		Key:            visible,
		Masked:         key.Masked,
		CreatedAt:      key.CreatedAt.UTC().Format(time.RFC3339),
		LastUsedAt:     last,
		Status:         key.Status,
		Limit:          key.LimitUSD,
		Spent:          key.SpentUSD,
		ModelGroupID:   optionalPublicID("mg", key.ModelGroupID),
		ModelGroupName: groupName,
	}
}

func modelGroupDTO(group ModelGroup, keyCount, modelCount int64) ModelGroupDTO {
	return ModelGroupDTO{
		ID:          id("mg", group.ID),
		Name:        group.Name,
		Description: group.Description,
		IsDefault:   group.IsDefault,
		KeyCount:    keyCount,
		ModelCount:  modelCount,
		Bindings:    decodeModelGroupBindings(group.BindingsJSON),
		CreatedAt:   group.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func id(prefix string, value uint) string {
	return fmt.Sprintf("%s_%03d", prefix, value)
}

func avatarText(email string) string {
	name := strings.Split(email, "@")[0]
	if len(name) >= 2 {
		return strings.ToUpper(name[:2])
	}
	if len(name) == 1 {
		return strings.ToUpper(name)
	}
	return "U"
}

func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:4] + strings.Repeat("*", 12) + secret[len(secret)-4:]
}

func hashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func maskKey(secret string) string {
	if len(secret) <= 12 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:7] + strings.Repeat("•", 24) + secret[len(secret)-4:]
}

func invitePublicStatus(invite InviteCode, now time.Time) string {
	if invite.Status == InviteStatusDisabled {
		return "expired"
	}
	if invite.ExpiresAt != nil && now.After(*invite.ExpiresAt) {
		return "expired"
	}
	if invite.MaxUse > 0 && invite.UsedCount >= invite.MaxUse {
		return "exhausted"
	}
	return "valid"
}
