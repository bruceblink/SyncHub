package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestRunSyncPullDryRunPreviewsChangesWithoutApplying(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(localPath, []byte("local"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("local")), SHA256: testSHA([]byte("local"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	sourceDeviceID := "dev_1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":10,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("after_change_id"); got != "10" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"message":"ok","data":{"items":[{"id":11,"file_id":"file_own","event_type":"update","version":2,"path":"/workspace/own.txt","source_device_id":%q,"created_at":"2026-06-30T00:02:00Z"},{"id":12,"file_id":"file_1","event_type":"update","version":3,"path":"/workspace/a.txt","created_at":"2026-06-30T00:03:00Z"},{"id":13,"file_id":"file_2","event_type":"move","version":4,"path":"/workspace/renamed.txt","old_path":"/workspace/old.txt","created_at":"2026-06-30T00:04:00Z"}],"next_cursor":13}}`, sourceDeviceID)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/files/"):
			t.Fatalf("dry run must not download file content: %s", r.URL.Path)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			t.Fatal("dry run must not ack changes")
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
		LastAppliedChangeID: 10,
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
		"--dry-run",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull dry run: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"dry run: true",
		"changes: 2",
		"update /workspace/a.txt version=3 id=12",
		"move /workspace/old.txt -> /workspace/renamed.txt version=4 id=13",
		"current cursor: 10",
		"next cursor: 13",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "/workspace/own.txt") {
		t.Fatalf("stdout includes own change: %s", out)
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(raw) != "local" {
		t.Fatalf("local file changed: %q", string(raw))
	}
	workspaceRaw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	var workspace workspaceConfig
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.LastAppliedChangeID != 10 {
		t.Fatalf("last applied change id = %d, want 10", workspace.LastAppliedChangeID)
	}
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 1 || m.Items[0].Path != "/workspace/a.txt" || m.Items[0].SHA256 != testSHA([]byte("local")) {
		t.Fatalf("manifest changed: %#v", m.Items)
	}
}

func TestRunSyncPullDryRunCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(localPath, []byte("local"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("local")), SHA256: testSHA([]byte("local"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("after_change_id"); got != "10" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":11,"file_id":"file_own","event_type":"update","version":2,"path":"/workspace/own.txt","source_device_id":"dev_1","created_at":"2026-06-30T00:02:00Z"},{"id":12,"file_id":"file_1","event_type":"update","version":3,"path":"/workspace/a.txt","created_at":"2026-06-30T00:03:00Z"}],"next_cursor":12}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/files/"):
			t.Fatalf("dry run must not download file content: %s", r.URL.Path)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			t.Fatal("dry run must not ack changes")
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 10,
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
		"pull",
		"--path", root,
		"--config", loginConfigPath,
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull dry-run json: %v", err)
	}
	if strings.Contains(stdout.String(), "dry run: true") {
		t.Fatalf("json output includes text dry-run output: %s", stdout.String())
	}

	var snapshot syncPullSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode pull dry-run json: %v\n%s", err, stdout.String())
	}
	if !snapshot.DryRun || snapshot.CurrentCursor != 10 || snapshot.NextCursor != 12 || snapshot.Cursor != 10 {
		t.Fatalf("cursors = dry_run:%v current:%d next:%d cursor:%d", snapshot.DryRun, snapshot.CurrentCursor, snapshot.NextCursor, snapshot.Cursor)
	}
	if snapshot.Summary.Changes != 1 || snapshot.Summary.Files != 0 || snapshot.Summary.Directories != 0 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].ID != 12 || snapshot.Changes[0].Path != "/workspace/a.txt" {
		t.Fatalf("changes = %#v", snapshot.Changes)
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(raw) != "local" {
		t.Fatalf("local file changed: %q", string(raw))
	}
	var workspace workspaceConfig
	workspaceRaw, err := os.ReadFile(filepath.Join(root, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.LastAppliedChangeID != 10 {
		t.Fatalf("last applied change id = %d, want 10", workspace.LastAppliedChangeID)
	}
}
