package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	cfg              Config
	db               *gorm.DB
	router           *gin.Engine
	schedulerMu      sync.Mutex
	schedulerCurrent map[string]int
}

func New(cfg Config) (*App, error) {
	db, err := openDatabase(cfg)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}

	app := &App{cfg: cfg, db: db, schedulerCurrent: map[string]int{}}
	if err := app.bootstrap(); err != nil {
		return nil, err
	}
	app.router = app.buildRouter()
	return app, nil
}

func (a *App) Run() error {
	return a.router.Run(a.cfg.Addr)
}

func (a *App) Handler() http.Handler {
	return a.router
}

func openDatabase(cfg Config) (*gorm.DB, error) {
	switch cfg.DatabaseDriver {
	case "sqlite", "sqlite3", "":
		return gorm.Open(sqlite.Open(cfg.DatabaseDSN), &gorm.Config{})
	case "postgres", "postgresql":
		return gorm.Open(postgres.Open(cfg.DatabaseDSN), &gorm.Config{})
	default:
		return nil, errors.New("unsupported database driver: " + cfg.DatabaseDriver)
	}
}

func migrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&InviteCode{},
		&EmailVerificationCode{},
		&ModelGroup{},
		&UpstreamSource{},
		&SourceAccount{},
		&SourceAccountUsageLog{},
		&SourceKey{},
		&ModelConfig{},
		&ModelRouteBinding{},
		&APIKey{},
		&UsageLog{},
		&RequestAttempt{},
		&PlatformSettings{},
	)
	if err != nil {
		return err
	}
	if err := ensureDefaultModelGroup(db); err != nil {
		return err
	}
	return migrateModelRouteBindings(db)
}

func (a *App) buildRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.RemoveExtraSlash = true
	r.Use(gin.Recovery())
	r.Use(a.cors())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		auth.POST("/login", a.login)
		auth.POST("/register/email-code", a.sendRegisterEmailCode)
		auth.POST("/register", a.register)
		auth.POST("/refresh", a.refreshToken)
		auth.GET("/settings", a.publicSettings)
		auth.GET("/me", a.requireAuth(), a.me)

		admin := api.Group("/admin")
		admin.Use(a.requireAuth(RoleAdmin))
		{
			admin.GET("/dashboard", a.adminDashboard)

			admin.GET("/users", a.adminUsers)
			admin.POST("/users", a.adminCreateUser)
			admin.PUT("/users/:id", a.adminUpdateUser)
			admin.PUT("/users/:id/quota", a.adminUpdateUserQuota)
			admin.DELETE("/users/:id", a.adminDeleteUser)

			admin.GET("/sources", a.adminSources)
			admin.POST("/sources", a.adminCreateSource)
			admin.PUT("/sources/:id", a.adminUpdateSource)
			admin.DELETE("/sources/:id", a.adminDeleteSource)
			admin.POST("/sources/:id/check", a.adminCheckSource)
			admin.POST("/sources/:id/recover", a.adminRecoverSource)
			admin.GET("/sources/:id/accounts", a.adminSourceAccounts)
			admin.POST("/sources/:id/accounts", a.adminCreateSourceAccount)
			admin.POST("/sources/:id/accounts/sync", a.adminSyncSourceAccounts)
			admin.POST("/sources/:id/accounts/oauth", a.adminCreateOAuthSession)
			admin.POST("/sources/:id/accounts/oauth/callback", a.adminSubmitOAuthCallback)
			admin.POST("/sources/:id/accounts/token", a.adminSubmitSourceAccountToken)
			admin.GET("/sources/:id/keys", a.adminSourceKeys)
			admin.POST("/sources/:id/keys", a.adminCreateSourceKey)
			admin.PUT("/source-accounts/:id", a.adminUpdateSourceAccount)
			admin.DELETE("/source-accounts/:id", a.adminDeleteSourceAccount)
			admin.POST("/source-accounts/:id/refresh", a.adminRefreshSourceAccount)
			admin.GET("/source-accounts/:id/token-usage", a.adminSourceAccountTokenUsage)
			admin.PUT("/source-keys/:id", a.adminUpdateSourceKey)
			admin.DELETE("/source-keys/:id", a.adminDeleteSourceKey)

			admin.GET("/models", a.adminModels)
			admin.GET("/model-groups", a.adminModelGroups)
			admin.POST("/model-groups", a.adminCreateModelGroup)
			admin.PUT("/model-groups/:id", a.adminUpdateModelGroup)
			admin.DELETE("/model-groups/:id", a.adminDeleteModelGroup)
			admin.POST("/models", a.adminCreateModel)
			admin.PUT("/models/:id", a.adminUpdateModel)
			admin.DELETE("/models/:id", a.adminDeleteModel)
			admin.POST("/models/batch", a.adminBatchModels)
			admin.POST("/models/sync-pricing", a.adminSyncPricing)
			admin.GET("/pricing/status", a.adminPricingStatus)

			admin.GET("/invite-codes", a.adminInvites)
			admin.POST("/invite-codes", a.adminCreateInvite)
			admin.PUT("/invite-codes/:id", a.adminUpdateInvite)
			admin.DELETE("/invite-codes/:id", a.adminDeleteInvite)

			admin.GET("/logs", a.adminLogs)
			admin.GET("/logs/:id", a.adminLogDetail)
			admin.GET("/logs/:id/attempts", a.adminLogAttempts)
			admin.DELETE("/logs", a.adminClearLogs)
			admin.GET("/usage/stats", a.adminUsageStats)
			admin.POST("/usage/reset", a.adminResetUsage)
			admin.GET("/settings", a.adminSettings)
			admin.PUT("/settings", a.adminUpdateSettings)
		}

		user := api.Group("/user")
		user.Use(a.requireAuth(RoleAdmin, RoleUser))
		{
			user.GET("/dashboard", a.userDashboard)
			user.GET("/usage", a.userUsage)
			user.GET("/logs", a.userLogs)
			user.GET("/logs/:id/attempts", a.userLogAttempts)
			user.GET("/models", a.userModels)
			user.GET("/model-groups", a.userModelGroups)
			user.POST("/models/:id/test", a.userTestModel)
			user.POST("/models/:id/invoke-test", a.userInvokeTestModel)

			user.GET("/api-keys", a.userAPIKeys)
			user.POST("/api-keys", a.userCreateAPIKey)
			user.PUT("/api-keys/:id", a.userUpdateAPIKey)
			user.DELETE("/api-keys/:id", a.userDeleteAPIKey)
			user.POST("/api-keys/:id/reveal", a.userRevealAPIKey)
		}
	}

	v1 := r.Group("/v1")
	v1.Use(a.requireAPIKey())
	{
		v1.GET("/models", a.openAIModels)
		v1.GET("/models/:model", a.openAIModel)
		v1.POST("/messages", a.proxyAnthropicMessages)
		v1.POST("/messages/count_tokens", a.proxyAnthropicCountTokens)
		v1.POST("/chat/completions", a.proxyChatCompletions)
		v1.POST("/completions", a.proxyCompletions)
		v1.POST("/responses", a.proxyResponses)
	}

	v1beta := r.Group("/v1beta")
	v1beta.Use(a.requireAPIKey())
	{
		v1beta.GET("/models", a.geminiModels)
		v1beta.POST("/models/*path", a.proxyGeminiGenerate)
	}

	a.serveFrontend(r)
	return r
}

func (a *App) serveFrontend(r *gin.Engine) {
	dist := strings.TrimSpace(a.cfg.FrontendDist)
	if dist == "" {
		return
	}
	if !filepath.IsAbs(dist) {
		dist = filepath.Clean(dist)
	}
	index := filepath.Join(dist, "index.html")
	if _, err := os.Stat(index); err != nil {
		return
	}

	r.Static("/assets", filepath.Join(dist, "assets"))
	r.StaticFile("/favicon.svg", filepath.Join(dist, "favicon.svg"))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			errorJSON(c, http.StatusNotFound, "not found")
			return
		}
		c.File(index)
	})
}

func (a *App) cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-API-Key,X-Goog-API-Key,Anthropic-Version,Anthropic-Beta")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		start := time.Now()
		c.Next()
		_ = start
	}
}
