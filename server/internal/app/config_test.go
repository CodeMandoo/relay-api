package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromEnvLoadsServerDotEnv(t *testing.T) {
	serverDir := filepath.Join(t.TempDir(), "server")
	appDir := filepath.Join(serverDir, "internal", "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "go.mod"), []byte("module relay-api/server\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	envFile := []byte("RELAY_CLIPROXYAPI_BASE_URL=http://127.0.0.1:8317\nRELAY_CLIPROXYAPI_API_KEY=relay-key\nRELAY_CLIPROXYAPI_MANAGEMENT_KEY=management-key\n")
	if err := os.WriteFile(filepath.Join(serverDir, ".env"), envFile, 0o600); err != nil {
		t.Fatal(err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})
	if err := os.Chdir(appDir); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_CLIPROXYAPI_BASE_URL", "")
	t.Setenv("RELAY_CLIPROXYAPI_API_KEY", "")
	t.Setenv("RELAY_CLIPROXYAPI_MANAGEMENT_KEY", "")

	cfg := LoadConfigFromEnv()
	if cfg.CLIProxyAPIBaseURL != "http://127.0.0.1:8317" {
		t.Fatalf("CLIProxyAPIBaseURL = %q", cfg.CLIProxyAPIBaseURL)
	}
	if cfg.CLIProxyAPIAPIKey != "relay-key" {
		t.Fatalf("CLIProxyAPIAPIKey = %q", cfg.CLIProxyAPIAPIKey)
	}
	if cfg.CLIProxyAPIManagementKey != "management-key" {
		t.Fatalf("CLIProxyAPIManagementKey = %q", cfg.CLIProxyAPIManagementKey)
	}
}
