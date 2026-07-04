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
	"time"

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

func TestRunLoginCanOutputJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":900}}}`))
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run login json: %v", err)
	}
	if strings.Contains(stdout.String(), "logged in as") {
		t.Fatalf("json output includes text login output: %s", stdout.String())
	}
	assertAuthJSONDoesNotExposeTokens(t, stdout.String())

	var snapshot authCommandSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode login json: %v\n%s", err, stdout.String())
	}
	if snapshot.Action != "login" || snapshot.Server != server.URL || snapshot.Config != configPath {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.User.ID != "u1" || snapshot.User.Email != "user@example.com" || snapshot.User.Status != "active" {
		t.Fatalf("snapshot user = %#v", snapshot.User)
	}
	if snapshot.AccessTokenExpiresAt == nil || !snapshot.AccessTokenExpiresAt.After(time.Now()) {
		t.Fatalf("access token expires at = %v", snapshot.AccessTokenExpiresAt)
	}

	cfg, err := readConfig(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.Tokens.AccessToken != "access-secret" || cfg.Tokens.RefreshToken != "refresh-secret" {
		t.Fatalf("tokens were not persisted: %#v", cfg.Tokens)
	}
}

func TestRunRegisterWritesConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/register" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access","refresh_token":"refresh","expires_in":900}}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"register",
		"--server", server.URL,
		"--email", "user@example.com",
		"--password", "password123",
		"--config", configPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run register: %v", err)
	}
	if !strings.Contains(stdout.String(), "registered as user@example.com") {
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
}

func TestRunRegisterCanOutputJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/register" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":900}}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"register",
		"--server", server.URL,
		"--email", "user@example.com",
		"--password", "password123",
		"--config", configPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run register json: %v", err)
	}
	if strings.Contains(stdout.String(), "registered as") {
		t.Fatalf("json output includes text register output: %s", stdout.String())
	}
	assertAuthJSONDoesNotExposeTokens(t, stdout.String())

	var snapshot authCommandSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode register json: %v\n%s", err, stdout.String())
	}
	if snapshot.Action != "register" || snapshot.Server != server.URL || snapshot.Config != configPath {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.User.ID != "u1" || snapshot.User.Email != "user@example.com" || snapshot.User.Status != "active" {
		t.Fatalf("snapshot user = %#v", snapshot.User)
	}
	if snapshot.AccessTokenExpiresAt == nil || !snapshot.AccessTokenExpiresAt.After(time.Now()) {
		t.Fatalf("access token expires at = %v", snapshot.AccessTokenExpiresAt)
	}

	cfg, err := readConfig(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.Tokens.AccessToken != "access-secret" || cfg.Tokens.RefreshToken != "refresh-secret" {
		t.Fatalf("tokens were not persisted: %#v", cfg.Tokens)
	}
}

func TestRunLogoutRevokesRefreshTokenAndRemovesConfig(t *testing.T) {
	var sawLogout bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/logout" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode logout request: %v", err)
		}
		if req.RefreshToken != "refresh" {
			t.Fatalf("refresh token = %q", req.RefreshToken)
		}
		sawLogout = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(configPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"logout",
		"--config", configPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !sawLogout {
		t.Fatal("logout endpoint was not called")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config still exists or stat failed: %v", err)
	}
	want := "logged out\nconfig removed: " + configPath + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunLogoutCanOutputJSON(t *testing.T) {
	var sawLogout bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/logout" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode logout request: %v", err)
		}
		if req.RefreshToken != "refresh-secret" {
			t.Fatalf("refresh token = %q", req.RefreshToken)
		}
		sawLogout = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(configPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"logout",
		"--config", configPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("logout json: %v", err)
	}
	if !sawLogout {
		t.Fatal("logout endpoint was not called")
	}
	if strings.Contains(stdout.String(), "logged out") {
		t.Fatalf("json output includes text logout output: %s", stdout.String())
	}
	assertAuthJSONDoesNotExposeTokens(t, stdout.String())

	var snapshot authCommandSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode logout json: %v\n%s", err, stdout.String())
	}
	if snapshot.Action != "logout" || snapshot.Server != server.URL || snapshot.Config != configPath {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.User.ID != "u1" || snapshot.User.Email != "user@example.com" {
		t.Fatalf("snapshot user = %#v", snapshot.User)
	}
	if snapshot.AccessTokenExpiresAt != nil {
		t.Fatalf("logout JSON includes access token expiry: %v", snapshot.AccessTokenExpiresAt)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config still exists or stat failed: %v", err)
	}
}

func TestDefaultServerURLMatchesAPIDefault(t *testing.T) {
	if defaultServerURL != "http://localhost:8765" {
		t.Fatalf("default server url = %q, want http://localhost:8765", defaultServerURL)
	}
}

func TestRunHelpIncludesAuthJSONCommands(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"synchub-cli register --server http://localhost:8765 --email user@example.com --password password --json",
		"synchub-cli login --server http://localhost:8765 --email user@example.com --password password --json",
		"synchub-cli logout --json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q: %s", want, out)
		}
	}
}

func assertAuthJSONDoesNotExposeTokens(t *testing.T, out string) {
	t.Helper()
	for _, token := range []string{"access-secret", "refresh-secret"} {
		if strings.Contains(out, token) {
			t.Fatalf("auth JSON exposed token %q: %s", token, out)
		}
	}
}

func TestRunLoginRequiresCredentials(t *testing.T) {
	err := run(context.Background(), []string{"login", "--email", "user@example.com"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("error = %v, want password required", err)
	}
}

func TestReadConfigWithRefreshRefreshesExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/refresh" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		if req.RefreshToken != "refresh-old" {
			t.Fatalf("refresh token = %q", req.RefreshToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"access_token":"access-new","refresh_token":"refresh-new","expires_in":900}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(configPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access-old", RefreshToken: "refresh-old", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(-time.Minute),
		UpdatedAt:            time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := readConfigWithRefresh(context.Background(), configPath)
	if err != nil {
		t.Fatalf("read config with refresh: %v", err)
	}
	if cfg.Tokens.AccessToken != "access-new" || cfg.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("tokens = %#v", cfg.Tokens)
	}
	if !cfg.AccessTokenExpiresAt.After(time.Now()) {
		t.Fatalf("access token expires at = %s, want future", cfg.AccessTokenExpiresAt)
	}

	persisted, err := readConfig(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if persisted.Tokens.AccessToken != "access-new" || persisted.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("persisted tokens = %#v", persisted.Tokens)
	}
}
