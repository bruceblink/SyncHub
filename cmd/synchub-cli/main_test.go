package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunLoginWritesConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access","refresh_token":"refresh","expires_in":900}}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"login",
		"--server", server.URL,
		"--email", "user@example.com",
		"--password", "password123",
		"--config", configPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run login: %v", err)
	}
	if !strings.Contains(stdout.String(), "logged in as user@example.com") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg cliConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.ServerURL != server.URL || cfg.User.Email != "user@example.com" || cfg.Tokens.AccessToken != "access" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.AccessTokenExpiresAt.IsZero() || cfg.UpdatedAt.IsZero() {
		t.Fatalf("config missing timestamps: %#v", cfg)
	}
}

func TestRunLoginRequiresCredentials(t *testing.T) {
	err := run(context.Background(), []string{"login", "--email", "user@example.com"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("error = %v, want password required", err)
	}
}

func TestRunWorkspaceInitWritesConfig(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8080",
		User:      clientUser("u1", "user@example.com"),
		Tokens: client.TokenPair{
			AccessToken:  "access",
			RefreshToken: "refresh",
			ExpiresIn:    900,
		},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", workspaceRoot,
		"--remote-path", "projects/demo",
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace init: %v", err)
	}
	if !strings.Contains(stdout.String(), "workspace initialized") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(workspaceRoot, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	var cfg workspaceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if cfg.Version != 1 || cfg.Root != workspaceRoot || cfg.RemotePath != "/projects/demo" {
		t.Fatalf("unexpected workspace config: %#v", cfg)
	}
	if cfg.ServerURL != "http://localhost:8080" || cfg.UserID != "u1" || cfg.UserEmail != "user@example.com" {
		t.Fatalf("workspace config missing login context: %#v", cfg)
	}
	if cfg.CreatedAt.IsZero() || cfg.UpdatedAt.IsZero() {
		t.Fatalf("workspace config missing timestamps: %#v", cfg)
	}
}

func TestRunWorkspaceInitRequiresLogin(t *testing.T) {
	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", t.TempDir(),
		"--config", filepath.Join(t.TempDir(), "missing.json"),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("error = %v, want not logged in", err)
	}
}

func clientUser(id, email string) client.User {
	return client.User{ID: id, Email: email, Status: "active"}
}
