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

	"github.com/bruceblink/SyncHub/pkg/client"
)

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

func TestRunSyncConflictsCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/conflicts" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("resolution"); got != "keep_both" {
			t.Fatalf("resolution = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"conf_1","file_id":"file_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"keep_both","created_at":"2026-06-30T00:00:00Z","resolved_at":"2026-06-30T00:01:00Z"}]}}`))
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
		"sync",
		"conflicts",
		"--path", root,
		"--config", loginConfigPath,
		"--resolution", "keep_both",
		"--limit", "20",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync conflicts json: %v", err)
	}
	if strings.Contains(stdout.String(), "conflicts:") {
		t.Fatalf("json output includes text conflicts output: %s", stdout.String())
	}

	var snapshot syncConflictsSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode conflicts json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Resolution != "keep_both" {
		t.Fatalf("resolution = %q", snapshot.Resolution)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "conf_1" || snapshot.Items[0].FileID == nil || *snapshot.Items[0].FileID != "file_1" || snapshot.Items[0].ResolvedAt == nil {
		t.Fatalf("items = %#v", snapshot.Items)
	}
}

func TestRunSyncConflictResolve(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sync/conflicts/conf_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Resolution string `json:"resolution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Resolution != "keep_both" {
			t.Fatalf("resolution = %q", req.Resolution)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"conf_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"keep_both","created_at":"2026-06-30T00:00:00Z","resolved_at":"2026-06-30T00:01:00Z"}}`))
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
		"sync",
		"conflicts",
		"resolve",
		"--path", root,
		"--config", loginConfigPath,
		"--id", "conf_1",
		"--resolution", "keep_both",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync conflicts resolve: %v", err)
	}
	want := "resolved: keep_both /workspace/a.txt id=conf_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncConflictResolveCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sync/conflicts/conf_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Resolution string `json:"resolution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Resolution != "keep_remote" {
			t.Fatalf("resolution = %q", req.Resolution)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"conf_1","file_id":"file_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"keep_remote","created_at":"2026-06-30T00:00:00Z","resolved_at":"2026-06-30T00:01:00Z"}}`))
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
		"sync",
		"conflicts",
		"resolve",
		"--path", root,
		"--config", loginConfigPath,
		"--id", "conf_1",
		"--resolution", "keep_remote",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync conflicts resolve json: %v", err)
	}
	if strings.Contains(stdout.String(), "resolved:") {
		t.Fatalf("json output includes text resolve output: %s", stdout.String())
	}

	var snapshot syncConflictResolveSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode resolve json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Action != "resolve" {
		t.Fatalf("action = %q", snapshot.Action)
	}
	if snapshot.Resolved.ID != "conf_1" || snapshot.Resolved.FileID == nil || *snapshot.Resolved.FileID != "file_1" || snapshot.Resolved.Resolution != "keep_remote" || snapshot.Resolved.ResolvedAt == nil {
		t.Fatalf("resolved = %#v", snapshot.Resolved)
	}
}
