package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
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

func TestDefaultServerURLMatchesAPIDefault(t *testing.T) {
	if defaultServerURL != "http://localhost:8765" {
		t.Fatalf("default server url = %q, want http://localhost:8765", defaultServerURL)
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

func TestRunWorkspaceInitWritesConfig(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
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
	if cfg.ServerURL != "http://localhost:8765" || cfg.UserID != "u1" || cfg.UserEmail != "user@example.com" {
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

func TestRunManifestScanWritesManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("bravo"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest scan: %v", err)
	}
	if !strings.Contains(stdout.String(), "manifest scanned: 2 files") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(root, ".synchub", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Items []struct {
			Path         string `json:"path"`
			RelativePath string `json:"relative_path"`
			SHA256       string `json:"sha256"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(manifest.Items) != 2 {
		t.Fatalf("items = %d, want 2: %#v", len(manifest.Items), manifest.Items)
	}
	if manifest.Items[0].Path != "/workspace/a.txt" || manifest.Items[0].RelativePath != "a.txt" {
		t.Fatalf("first item = %#v", manifest.Items[0])
	}
	if manifest.Items[1].Path != "/workspace/nested/b.txt" || manifest.Items[1].RelativePath != "nested/b.txt" {
		t.Fatalf("second item = %#v", manifest.Items[1])
	}
}

func TestRunManifestScanRequiresWorkspace(t *testing.T) {
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", t.TempDir(),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "workspace is not initialized") {
		t.Fatalf("error = %v, want workspace not initialized", err)
	}
}

func TestRunSyncStatusShowsManifestSummary(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: 5, SHA256: "sha"},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"sync", "status", "--path", root}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"workspace: " + root,
		"remote path: /workspace",
		"user: user@example.com",
		"files: 1",
		"last scan: 2026-06-30T01:02:03Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusShowsMissingManifest(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"sync", "status", "--path", root}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status: %v", err)
	}
	if !strings.Contains(stdout.String(), "manifest: missing") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSyncWatchOnceShowsLocalChanges(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/delete.txt", RelativePath: "delete.txt", Size: int64(len("delete")), SHA256: testSHA([]byte("delete"))},
			{Path: "/workspace/update.txt", RelativePath: "update.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "create.txt"), []byte("create"), 0o644); err != nil {
		t.Fatalf("write create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "update.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write update file: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"watch",
		"--path", root,
		"--once",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync watch once: %v", err)
	}
	want := "created create.txt\ndeleted delete.txt\nupdated update.txt\nchanges: 3\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncConflictsShowsPendingConflicts(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/conflicts" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("resolution"); got != "pending" {
			t.Fatalf("resolution = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"conf_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"pending","created_at":"2026-06-30T00:00:00Z"}]}}`))
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"conflicts",
		"--path", root,
		"--config", loginConfigPath,
		"--limit", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync conflicts: %v", err)
	}
	want := "conflicts: 1\npending /workspace/a.txt local=1 remote=2 id=conf_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncPushUploadsManifestFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(root, "nested", "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var dirs []string
	var chunks []string
	committed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/refresh" {
			var req struct {
				RefreshToken string `json:"refresh_token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode refresh request: %v", err)
			}
			if req.RefreshToken != "refresh" {
				t.Fatalf("refresh token = %q", req.RefreshToken)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"access_token":"access-new","refresh_token":"refresh-new","expires_in":900}}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-new" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			dirs = append(dirs, req.Path)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"dir","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			if got := r.Header.Get("Idempotency-Key"); got == "" {
				t.Fatal("missing idempotency key")
			}
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/nested/a.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/nested/a.txt","chunk_size":3,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/uploads/upl_1/chunks/"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if got := r.Header.Get("X-Chunk-Sha256"); got != testSHA(body) {
				t.Fatalf("chunk sha = %q, want %q", got, testSHA(body))
			}
			chunks = append(chunks, string(body))
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":3,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			committed = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/nested/a.txt", RelativePath: "nested/a.txt", Size: int64(len(content)), SHA256: testSHA(content)},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(dirs) != 2 || dirs[0] != "/workspace" || dirs[1] != "/workspace/nested" {
		t.Fatalf("dirs = %#v", dirs)
	}
	if len(chunks) != 2 || chunks[0] != "hel" || chunks[1] != "lo" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if !committed {
		t.Fatal("upload was not committed")
	}
	refreshedConfig, err := readConfig(loginConfigPath)
	if err != nil {
		t.Fatalf("read refreshed config: %v", err)
	}
	if refreshedConfig.Tokens.AccessToken != "access-new" || refreshedConfig.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("refreshed tokens = %#v", refreshedConfig.Tokens)
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 1 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushDeletesRemovedManifestFiles(t *testing.T) {
	root := t.TempDir()
	remoteVersion := int64(3)
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/remove.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"remove.txt","path":"/workspace/remove.txt","node_type":"file","version":3}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/files/file_1":
			deleted = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/remove.txt", RelativePath: "remove.txt", Size: int64(len("remove me")), SHA256: testSHA([]byte("remove me")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	want := "uploaded: 0 files\ndeleted: 1 files\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if !deleted {
		t.Fatal("file was not deleted")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 0 {
		t.Fatalf("manifest items = %#v, want empty", updatedManifest.Items)
	}
}

func TestRunSyncPushDiscoversNewLocalFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("new file")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), content, 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	committed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			if req.Path != "/workspace" {
				t.Fatalf("directory path = %q", req.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/new.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			if req.BaseVersion != nil {
				t.Fatalf("base version = %#v, want nil", req.BaseVersion)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/new.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_1/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":8,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			committed = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !committed {
		t.Fatal("upload was not committed")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].Path != "/workspace/new.txt" || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 1 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncOncePushesAndPulls(t *testing.T) {
	root := t.TempDir()
	content := []byte("sync once")
	if err := os.WriteFile(filepath.Join(root, "once.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	registeredDevice := false
	listedChanges := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			if req.Path != "/workspace" {
				t.Fatalf("directory path = %q", req.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/once.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_once","path":"/workspace/once.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_once/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":9,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_once/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			registeredDevice = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"test-device","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			listedChanges = true
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[],"next_cursor":0}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"once",
		"--path", root,
		"--config", loginConfigPath,
		"--device-name", "test-device",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once: %v", err)
	}
	for _, want := range []string{"uploaded: 1 files", "pulled: 0 files", "cursor: 0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %s", want, stdout.String())
		}
	}
	if !registeredDevice || !listedChanges {
		t.Fatalf("sync once did not pull: registered=%v listed=%v", registeredDevice, listedChanges)
	}
	if _, err := readManifest(filepath.Join(root, ".synchub", "manifest.json")); err != nil {
		t.Fatalf("read manifest after sync once: %v", err)
	}
}

func TestRunSyncPushSkipsUnchangedManifestFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("unchanged")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	remoteVersion := int64(5)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len(content)), SHA256: testSHA(content), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if stdout.String() != "uploaded: 0 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != remoteVersion {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushMovesRenamedManifestFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("same content")
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), content, 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	remoteVersion := int64(3)
	moved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			if req.Path != "/workspace" {
				t.Fatalf("directory path = %q", req.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/old.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"old.txt","path":"/workspace/old.txt","node_type":"file","version":3}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/files/file_1":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode move request: %v", err)
			}
			if req.Path != "/workspace/renamed.txt" {
				t.Fatalf("move path = %q", req.Path)
			}
			moved = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"renamed.txt","path":"/workspace/renamed.txt","node_type":"file","version":4}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len(content)), SHA256: testSHA(content), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	want := "uploaded: 0 files\nmoved: 1 files\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if !moved {
		t.Fatal("file was not moved")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].Path != "/workspace/renamed.txt" || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 4 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushSendsManifestRemoteVersion(t *testing.T) {
	root := t.TempDir()
	content := []byte("local change")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	remoteVersion := int64(7)
	baseVersionSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/a.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			if req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
				t.Fatalf("base version = %#v, want %d", req.BaseVersion, remoteVersion)
			}
			baseVersionSeen = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_1/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":12,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":8,"change_id":9}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("old content")), SHA256: testSHA([]byte("old content")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !baseVersionSeen {
		t.Fatal("base version was not sent")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 8 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushKeepsConflictCopy(t *testing.T) {
	oldNow := syncPushNow
	syncPushNow = func() time.Time {
		return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC)
	}
	defer func() {
		syncPushNow = oldNow
	}()

	root := t.TempDir()
	content := []byte("local conflict")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	remoteVersion := int64(7)
	conflictPath := "/workspace/a.conflict-dev_1-20260630T010203.000000000Z.txt"
	var initPaths []string
	conflictCommitted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			if req.Path != "/workspace" {
				t.Fatalf("directory path = %q", req.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected upload content metadata: %#v", req)
			}
			initPaths = append(initPaths, req.Path)
			switch req.Path {
			case "/workspace/a.txt":
				if req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
					t.Fatalf("base version = %#v, want %d", req.BaseVersion, remoteVersion)
				}
				_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_original","path":"/workspace/a.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
			case conflictPath:
				if req.BaseVersion != nil {
					t.Fatalf("conflict copy base version = %#v, want nil", req.BaseVersion)
				}
				_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_conflict","path":"/workspace/a.conflict-dev_1-20260630T010203.000000000Z.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
			default:
				t.Fatalf("upload path = %q", req.Path)
			}
		case r.Method == http.MethodPut && (r.URL.Path == "/api/v1/uploads/upl_original/chunks/0" || r.URL.Path == "/api/v1/uploads/upl_conflict/chunks/0"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":14,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_original/commit":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"FILE_CONFLICT","message":"base version conflict"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_conflict/commit":
			conflictCommitted = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_conflict","version":1,"change_id":10}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("old content")), SHA256: testSHA([]byte("old content")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	want := "uploaded: 1 files\nconflicts kept: 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if len(initPaths) != 2 || initPaths[0] != "/workspace/a.txt" || initPaths[1] != conflictPath {
		t.Fatalf("init paths = %#v", initPaths)
	}
	if !conflictCommitted {
		t.Fatal("conflict copy was not committed")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != remoteVersion {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPullDownloadsChangesAndStoresCursor(t *testing.T) {
	root := t.TempDir()
	content := []byte("pulled file")
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			var req struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode device request: %v", err)
			}
			if req.Name == "" || req.Platform == "" {
				t.Fatalf("device request missing fields: %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			if got := r.URL.Query().Get("after_change_id"); got != "0" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":1,"file_id":"dir_1","event_type":"create","path":"/workspace/nested","created_at":"2026-06-30T00:01:00Z"},{"id":2,"file_id":"file_1","event_type":"create","version":1,"path":"/workspace/nested/a.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":2}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			var req struct {
				DeviceID            string `json:"device_id"`
				LastAppliedChangeID int64  `json:"last_applied_change_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ack request: %v", err)
			}
			if req.DeviceID != "dev_1" || req.LastAppliedChangeID != 2 {
				t.Fatalf("ack request = %#v", req)
			}
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull: %v", err)
	}
	if !strings.Contains(stdout.String(), "pulled: 1 files") || !strings.Contains(stdout.String(), "cursor: 2") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(root, "nested", "a.txt"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("pulled file = %q", string(raw))
	}
	if !acked {
		t.Fatal("changes were not acked")
	}
	var workspace workspaceConfig
	workspaceRaw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.DeviceID != "dev_1" || workspace.LastAppliedChangeID != 2 {
		t.Fatalf("workspace sync state = %#v", workspace)
	}
	m, err := readManifest(filepath.Join(root, ".synchub", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].Path != "/workspace/nested/a.txt" || m.Items[0].RemoteVersion == nil || *m.Items[0].RemoteVersion != 1 {
		t.Fatalf("manifest items = %#v", m.Items)
	}
}

func TestRunSyncPullKeepsLocalConflictBeforeOverwrite(t *testing.T) {
	root := t.TempDir()
	syncPushNow = func() time.Time { return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC) }
	defer func() { syncPushNow = time.Now }()

	localPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(localPath, []byte("local edit"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	remoteContent := []byte("remote update")
	remoteVersion := int64(1)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("old content")), SHA256: testSHA([]byte("old content")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":2,"file_id":"file_1","event_type":"update","version":2,"path":"/workspace/a.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":2}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(remoteContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "dev 1",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 1,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull conflict: %v", err)
	}
	if !strings.Contains(stdout.String(), "pulled: 1 files") || !strings.Contains(stdout.String(), "conflicts kept: 1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !acked {
		t.Fatal("update change was not acked")
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read remote file: %v", err)
	}
	if !bytes.Equal(raw, remoteContent) {
		t.Fatalf("remote file = %q", string(raw))
	}
	conflictPath := filepath.Join(root, "a.conflict-dev_1-20260630T010203.000000000Z.txt")
	conflict, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read conflict file: %v", err)
	}
	if string(conflict) != "local edit" {
		t.Fatalf("conflict file = %q", string(conflict))
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 2 {
		t.Fatalf("manifest items = %#v, want remote and conflict copy", m.Items)
	}
}

func TestRunSyncPullAppliesDeleteEvents(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "obsolete.txt")
	if err := os.WriteFile(targetPath, []byte("remove me"), 0o644); err != nil {
		t.Fatalf("write obsolete file: %v", err)
	}
	remoteVersion := int64(1)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/obsolete.txt", RelativePath: "obsolete.txt", Size: int64(len("remove me")), SHA256: testSHA([]byte("remove me")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("after_change_id"); got != "2" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":3,"file_id":"file_1","event_type":"delete","version":2,"path":"/workspace/obsolete.txt","old_path":"/workspace/obsolete.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":3}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			var req struct {
				DeviceID            string `json:"device_id"`
				LastAppliedChangeID int64  `json:"last_applied_change_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ack request: %v", err)
			}
			if req.DeviceID != "dev_1" || req.LastAppliedChangeID != 3 {
				t.Fatalf("ack request = %#v", req)
			}
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 2,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull delete: %v", err)
	}
	if !strings.Contains(stdout.String(), "deleted: 1") || !strings.Contains(stdout.String(), "cursor: 3") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("obsolete file still exists or stat failed: %v", err)
	}
	if !acked {
		t.Fatal("delete change was not acked")
	}
	var workspace workspaceConfig
	workspaceRaw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.LastAppliedChangeID != 3 {
		t.Fatalf("last applied change id = %d", workspace.LastAppliedChangeID)
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 0 {
		t.Fatalf("manifest items = %#v, want empty", m.Items)
	}
}

func TestRunSyncPullDeleteKeepsLocalConflict(t *testing.T) {
	root := t.TempDir()
	syncPushNow = func() time.Time { return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC) }
	defer func() { syncPushNow = time.Now }()

	targetPath := filepath.Join(root, "obsolete.txt")
	if err := os.WriteFile(targetPath, []byte("local edit"), 0o644); err != nil {
		t.Fatalf("write obsolete file: %v", err)
	}
	remoteVersion := int64(1)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/obsolete.txt", RelativePath: "obsolete.txt", Size: int64(len("old content")), SHA256: testSHA([]byte("old content")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":3,"file_id":"file_1","event_type":"delete","version":2,"path":"/workspace/obsolete.txt","old_path":"/workspace/obsolete.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":3}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 2,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull delete conflict: %v", err)
	}
	if !strings.Contains(stdout.String(), "deleted: 1") || !strings.Contains(stdout.String(), "conflicts kept: 1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("obsolete file still exists or stat failed: %v", err)
	}
	conflictPath := filepath.Join(root, "obsolete.conflict-dev_1-20260630T010203.000000000Z.txt")
	conflict, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read conflict file: %v", err)
	}
	if string(conflict) != "local edit" {
		t.Fatalf("conflict file = %q", string(conflict))
	}
	if !acked {
		t.Fatal("delete change was not acked")
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].RelativePath != "obsolete.conflict-dev_1-20260630T010203.000000000Z.txt" {
		t.Fatalf("manifest items = %#v, want conflict copy only", m.Items)
	}
}

func TestRunSyncPullAppliesMoveEvents(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.txt")
	newPath := filepath.Join(root, "renamed.txt")
	if err := os.WriteFile(oldPath, []byte("move me"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	remoteVersion := int64(2)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len("move me")), SHA256: testSHA([]byte("move me")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("after_change_id"); got != "3" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":4,"file_id":"file_1","event_type":"move","version":3,"path":"/workspace/renamed.txt","old_path":"/workspace/old.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":4}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			var req struct {
				DeviceID            string `json:"device_id"`
				LastAppliedChangeID int64  `json:"last_applied_change_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ack request: %v", err)
			}
			if req.DeviceID != "dev_1" || req.LastAppliedChangeID != 4 {
				t.Fatalf("ack request = %#v", req)
			}
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":4,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 3,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull move: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved: 1") || !strings.Contains(stdout.String(), "cursor: 4") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists or stat failed: %v", err)
	}
	raw, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read renamed file: %v", err)
	}
	if string(raw) != "move me" {
		t.Fatalf("renamed file = %q", string(raw))
	}
	if !acked {
		t.Fatal("move change was not acked")
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].Path != "/workspace/renamed.txt" || m.Items[0].RemoteVersion == nil || *m.Items[0].RemoteVersion != 3 {
		t.Fatalf("manifest items = %#v", m.Items)
	}
}

func TestRunSyncPullMoveIsIdempotentAfterInterruptedAck(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.txt")
	newPath := filepath.Join(root, "renamed.txt")
	if err := os.WriteFile(newPath, []byte("move me"), 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file exists or stat failed: %v", err)
	}
	remoteVersion := int64(2)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len("move me")), SHA256: testSHA([]byte("move me")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":4,"file_id":"file_1","event_type":"move","version":3,"path":"/workspace/renamed.txt","old_path":"/workspace/old.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":4}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			acked = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":4,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 3,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"pull",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull move retry: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved: 1") || !strings.Contains(stdout.String(), "cursor: 4") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !acked {
		t.Fatal("move change was not acked")
	}
	raw, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read renamed file: %v", err)
	}
	if string(raw) != "move me" {
		t.Fatalf("renamed file = %q", string(raw))
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].Path != "/workspace/renamed.txt" || m.Items[0].RemoteVersion == nil || *m.Items[0].RemoteVersion != 3 {
		t.Fatalf("manifest items = %#v", m.Items)
	}
}

func clientUser(id, email string) client.User {
	return client.User{ID: id, Email: email, Status: "active"}
}

func writeTestWorkspaceConfig(t *testing.T, root string) {
	t.Helper()
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
}

func testSHA(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
