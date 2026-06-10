package app

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

func (a *App) bootstrap() error {
	if err := a.ensureSettings(); err != nil {
		return err
	}
	if err := ensureDefaultModelGroup(a.db); err != nil {
		return err
	}
	if err := a.ensureAdmin(); err != nil {
		return err
	}
	if err := a.ensureBuiltInCLIProxySource(); err != nil {
		return err
	}
	if a.cfg.SeedData {
		if err := a.seedReferenceData(); err != nil {
			return err
		}
	}
	if err := migrateModelRouteBindings(a.db); err != nil {
		return err
	}
	if err := a.normalizeSourceBaseURLs(); err != nil {
		return err
	}
	if err := a.resetSourceHealthOnStartup(); err != nil {
		return err
	}
	return a.normalizeSourceAccountCounts()
}

func (a *App) ensureSettings() error {
	var count int64
	if err := a.db.Model(&PlatformSettings{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return a.db.Create(&PlatformSettings{
		PlatformName:       "Relay API",
		SupportEmail:       "support@relay.io",
		OpenRegistration:   true,
		RequireInviteCode:  true,
		DefaultUserBalance: 100,
		MaxRetries:         3,
		DefaultTimeout:     120,
		StreamingEnabled:   true,
	}).Error
}

func ensureDefaultModelGroup(db *gorm.DB) error {
	var group ModelGroup
	err := db.Where("is_default = ?", true).Order("id asc").First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		group = ModelGroup{
			Name:        DefaultModelGroupName,
			Description: "Legacy models default group",
			IsDefault:   true,
		}
		if err := db.Create(&group).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if group.Name == "" {
		if err := db.Model(&group).Update("name", DefaultModelGroupName).Error; err != nil {
			return err
		}
	}
	if group.ID == 0 {
		return nil
	}
	if err := db.Model(&ModelConfig{}).Where("model_group_id = 0 OR model_group_id IS NULL").Update("model_group_id", group.ID).Error; err != nil {
		return err
	}
	if err := db.Model(&APIKey{}).Where("model_group_id = 0 OR model_group_id IS NULL").Update("model_group_id", group.ID).Error; err != nil {
		return err
	}
	return nil
}

func (a *App) ensureAdmin() error {
	var count int64
	if err := a.db.Model(&User{}).Where("role = ?", RoleAdmin).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(a.cfg.AdminPassword)
	if err != nil {
		return err
	}
	return a.db.Create(&User{
		Email:        a.cfg.AdminEmail,
		Name:         "Admin Master",
		PasswordHash: hash,
		Role:         RoleAdmin,
		Status:       UserStatusNormal,
		MonthlyQuota: 5_000_000,
		WeeklyQuota:  1_500_000,
		Balance:      3760,
	}).Error
}

func (a *App) ensureBuiltInCLIProxySource() error {
	baseURL := normalizeSourceBaseURL(a.cfg.CLIProxyAPIBaseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8317"
	}
	var source UpstreamSource
	err := a.db.Where("type = ?", SourceTypeCLIProxyAPI).Order("id asc").First(&source).Error
	if err == nil {
		updates := map[string]any{
			"base_url":           baseURL,
			"open_ai_base_url":   "",
			"anthropic_base_url": "",
			"api_key":            a.cfg.CLIProxyAPIAPIKey,
			"management_key":     a.cfg.CLIProxyAPIManagementKey,
		}
		if source.Name == "" || source.Name == "CLIProxyAPI_Primary" {
			updates["name"] = "CLIProxyAPI"
		}
		return a.db.Model(&UpstreamSource{}).Where("id = ?", source.ID).Updates(updates).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return a.db.Create(&UpstreamSource{
		Name:          "CLIProxyAPI",
		Type:          SourceTypeCLIProxyAPI,
		BaseURL:       baseURL,
		APIKey:        a.cfg.CLIProxyAPIAPIKey,
		ManagementKey: a.cfg.CLIProxyAPIManagementKey,
		Priority:      10,
		Status:        SourceStatusOffline,
		Load:          0,
		LatencyMS:     0,
	}).Error
}

func (a *App) seedReferenceData() error {
	if err := a.seedSources(); err != nil {
		return err
	}
	if err := a.seedModels(); err != nil {
		return err
	}
	if err := a.seedInvites(); err != nil {
		return err
	}
	return nil
}

func (a *App) seedSources() error {
	var count int64
	if err := a.db.Model(&UpstreamSource{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return nil
}

func (a *App) normalizeSourceBaseURLs() error {
	var sources []UpstreamSource
	if err := a.db.Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		normalized := normalizeSourceBaseURL(source.BaseURL)
		if normalized == source.BaseURL {
			continue
		}
		if err := a.db.Model(&UpstreamSource{}).Where("id = ?", source.ID).Update("base_url", normalized).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *App) resetSourceHealthOnStartup() error {
	return a.db.Model(&UpstreamSource{}).
		Where("status = ?", SourceStatusOnline).
		Updates(map[string]any{"status": SourceStatusOffline, "latency_ms": 0, "load": 0}).Error
}

func (a *App) normalizeSourceAccountCounts() error {
	return a.db.Model(&UpstreamSource{}).
		Where("type <> ?", "CLIProxyAPI").
		Update("account_count", 0).Error
}

func (a *App) seedModels() error {
	var count int64
	if err := a.db.Model(&ModelConfig{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var source UpstreamSource
	if err := a.db.Order("priority asc").First(&source).Error; err != nil {
		return err
	}
	group, err := defaultPlatformModelGroup(a.db)
	if err != nil {
		return err
	}
	models := []ModelConfig{
		{ModelGroupID: group.ID, SourceID: source.ID, Name: "gpt-4o", DisplayName: "GPT-4o", Provider: "OpenAI", Formats: ModelFormatOpenAI, BillingInput: 1, BillingOutput: 1, Status: ModelStatusActive, LatencyMS: 0},
		{ModelGroupID: group.ID, SourceID: source.ID, Name: "gpt-4o-mini", DisplayName: "GPT-4o Mini", Provider: "OpenAI", Formats: ModelFormatOpenAI, BillingInput: 0.2, BillingOutput: 0.2, Status: ModelStatusActive, LatencyMS: 0},
		{ModelGroupID: group.ID, SourceID: source.ID, Name: "claude-3-5-sonnet", DisplayName: "Claude 3.5 Sonnet", Provider: "Anthropic", Formats: ModelFormatAnthropic, BillingInput: 1.5, BillingOutput: 1.5, Status: ModelStatusActive, LatencyMS: 0},
		{ModelGroupID: group.ID, SourceID: source.ID, Name: "gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash", Provider: "Google", Formats: ModelFormatOpenAI, BillingInput: 0.5, BillingOutput: 0.5, Status: ModelStatusActive, LatencyMS: 0},
	}
	return a.db.Create(&models).Error
}

func (a *App) seedInvites() error {
	var count int64
	if err := a.db.Model(&InviteCode{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var admin User
	_ = a.db.Where("role = ?", RoleAdmin).First(&admin).Error
	expires := time.Now().AddDate(0, 3, 0)
	return a.db.Create(&InviteCode{
		Code:      "TEAM-DEV-2026",
		MaxUse:    50,
		UsedCount: 0,
		CreatedBy: admin.ID,
		ExpiresAt: &expires,
		Remark:    "Default team invitation",
		Status:    InviteStatusActive,
	}).Error
}
