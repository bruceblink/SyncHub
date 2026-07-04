package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunFileVersionsByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/versions":
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("limit = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"ver_2","file_id":"file_1","version":2,"size":6,"sha256":"sha2","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:02:00Z"},{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}]}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
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
		"file",
		"versions",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--limit", "2",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	want := "file: /workspace/a.txt\nfile id: file_1\nversions: 2\nv2 size=6 sha256=sha2 pinned=2026-06-30T00:03:00Z created=2026-06-30T00:02:00Z id=ver_2\nv1 size=5 sha256=sha1 pinned=- created=2026-06-30T00:01:00Z id=ver_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileVersionsCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/versions":
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("limit = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"ver_2","file_id":"file_1","version":2,"size":6,"sha256":"sha2","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:02:00Z"},{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}]}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	})
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
		"file",
		"versions",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--limit", "2",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file versions json: %v", err)
	}
	if strings.Contains(stdout.String(), "versions:") {
		t.Fatalf("json output includes text versions output: %s", stdout.String())
	}

	var snapshot fileVersionsSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode file versions json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.File.ID != "file_1" || snapshot.File.Path != "/workspace/a.txt" {
		t.Fatalf("file = %#v", snapshot.File)
	}
	if len(snapshot.Items) != 2 || snapshot.Items[0].ID != "ver_2" || snapshot.Items[0].PinnedAt == nil || snapshot.Items[1].PinnedAt != nil {
		t.Fatalf("items = %#v", snapshot.Items)
	}
}

func TestRunFileHelpIncludesVersionJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"file", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"synchub-cli file versions --path . --remote-path /workspace/readme.txt",
		"synchub-cli file versions --path . --remote-path /workspace/readme.txt --json",
		"synchub-cli file restore --path . --remote-path /workspace/readme.txt --version 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("file help missing %q: %s", want, out)
		}
	}
}

func TestRunFileVersionsByFileID(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1/versions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[]}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
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
		"file",
		"versions",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	want := "file id: file_1\nversions: 0\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileRestoreByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/file_1/versions/1/restore":
			var req struct {
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode restore request: %v", err)
			}
			if req.DeviceID != "dev_1" {
				t.Fatalf("restore device id = %q", req.DeviceID)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:04:00Z"},"change_id":9}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:        1,
		Root:           root,
		RemotePath:     "/workspace",
		ServerURL:      server.URL,
		UserID:         "u1",
		UserEmail:      "user@example.com",
		DeviceID:       "dev_1",
		DeviceName:     "laptop",
		DevicePlatform: "windows",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
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
		"file",
		"restore",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file restore: %v", err)
	}
	want := "restored: /workspace/a.txt\nversion: 3\nchange id: 9\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFilePinByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/file_1/versions/1/pin":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:01:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
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
		"file",
		"pin",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file pin: %v", err)
	}
	want := "file: /workspace/a.txt\npinned: file_1 v1\npinned at: 2026-06-30T00:03:00Z\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileUnpinByFileID(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/files/file_1/versions/1/pin" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
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
		"file",
		"unpin",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file unpin: %v", err)
	}
	want := "unpinned: file_1 v1\npinned at: -\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
