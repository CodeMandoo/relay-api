package app

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                     string
	DatabaseDriver           string
	DatabaseDSN              string
	JWTSecret                string
	AccessTTL                time.Duration
	RefreshTTL               time.Duration
	AdminEmail               string
	AdminPassword            string
	CLIProxyAPIBaseURL       string
	CLIProxyAPIAPIKey        string
	CLIProxyAPIManagementKey string
	FrontendDist             string
	SeedData                 bool
	SMTPHost                 string
	SMTPPort                 string
	SMTPUsername             string
	SMTPPassword             string
	SMTPFrom                 string
	EmailCodeTTL             time.Duration
	EmailCodeCooldown        time.Duration
	EmailCodeDevMode         bool
	RequireEmailVerification bool
}

func LoadConfigFromEnv() Config {
	loadBackendEnv()

	smtpUsername := env("RELAY_SMTP_USERNAME", "")
	return Config{
		Addr:                     env("RELAY_ADDR", ":8080"),
		DatabaseDriver:           strings.ToLower(env("RELAY_DATABASE_DRIVER", "sqlite")),
		DatabaseDSN:              env("RELAY_DATABASE_DSN", "relay.db"),
		JWTSecret:                env("RELAY_JWT_SECRET", "dev-only-change-me"),
		AccessTTL:                durationEnv("RELAY_ACCESS_TTL", 2*time.Hour),
		RefreshTTL:               durationEnv("RELAY_REFRESH_TTL", 14*24*time.Hour),
		AdminEmail:               env("RELAY_ADMIN_EMAIL", "admin@relay.io"),
		AdminPassword:            env("RELAY_ADMIN_PASSWORD", "admin123456"),
		CLIProxyAPIBaseURL:       env("RELAY_CLIPROXYAPI_BASE_URL", "http://127.0.0.1:8317"),
		CLIProxyAPIAPIKey:        env("RELAY_CLIPROXYAPI_API_KEY", ""),
		CLIProxyAPIManagementKey: env("RELAY_CLIPROXYAPI_MANAGEMENT_KEY", ""),
		FrontendDist:             env("RELAY_FRONTEND_DIST", "../apps/web/dist"),
		SeedData:                 boolEnv("RELAY_SEED_DATA", true),
		SMTPHost:                 env("RELAY_SMTP_HOST", ""),
		SMTPPort:                 env("RELAY_SMTP_PORT", "587"),
		SMTPUsername:             smtpUsername,
		SMTPPassword:             env("RELAY_SMTP_PASSWORD", ""),
		SMTPFrom:                 env("RELAY_SMTP_FROM", smtpUsername),
		EmailCodeTTL:             durationEnv("RELAY_EMAIL_CODE_TTL", 10*time.Minute),
		EmailCodeCooldown:        durationEnv("RELAY_EMAIL_CODE_COOLDOWN", time.Minute),
		EmailCodeDevMode:         boolEnv("RELAY_EMAIL_CODE_DEV", false),
		RequireEmailVerification: boolEnv("RELAY_REQUIRE_EMAIL_VERIFICATION", false),
	}
}

func loadBackendEnv() {
	for _, path := range backendEnvCandidates() {
		if err := loadEnvFile(path); err == nil {
			return
		}
	}
}

func backendEnvCandidates() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return []string{".env"}
	}

	var candidates []string
	add := func(path string) {
		for _, existing := range candidates {
			if existing == path {
				return
			}
		}
		candidates = append(candidates, path)
	}

	add(filepath.Join(cwd, ".env"))
	add(filepath.Join(cwd, "server", ".env"))

	for dir := cwd; ; dir = filepath.Dir(dir) {
		if isServerModuleDir(dir) {
			add(filepath.Join(dir, ".env"))
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return candidates
}

func isServerModuleDir(dir string) bool {
	contents, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(contents), "module relay-api/server")
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, unquoteEnvValue(strings.TrimSpace(value)))
	}

	return scanner.Err()
}

func unquoteEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
		return value[1 : len(value)-1]
	}
	return value
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err == nil {
		return parsed
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
