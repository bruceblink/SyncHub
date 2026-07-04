package main

import (
	"bytes"
	"context"
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

func TestRunSyncPullOverwritesUnchangedLocalFile(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "a.txt")
	oldContent := []byte("old content")
	if err := os.WriteFile(localPath, oldContent, 0o644); err != nil {
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
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len(oldContent)), SHA256: testSHA(oldContent), RemoteVersion: &remoteVersion},
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
		t.Fatalf("sync pull update: %v", err)
	}
	if !strings.Contains(stdout.String(), "pulled: 1 files") || strings.Contains(stdout.String(), "conflicts kept") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !acked {
		t.Fatal("update change was not acked")
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	if !bytes.Equal(raw, remoteContent) {
		t.Fatalf("updated file = %q", string(raw))
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].RemoteVersion == nil || *m.Items[0].RemoteVersion != 2 {
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
	conflictPath := filepath.Join(root, "a.conflict-dev_1-20260630T010203.000000000Z.txt")
	if !strings.Contains(stdout.String(), "conflict: "+conflictPath) {
		t.Fatalf("stdout = %q, want conflict path %s", stdout.String(), conflictPath)
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
