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

func TestSyncPullIdentifiesOwnChangeEvent(t *testing.T) {
	source := "dev_1"
	if !isOwnChangeEvent(workspaceConfig{DeviceID: "dev_1"}, client.ChangeEvent{SourceDeviceID: &source}) {
		t.Fatal("expected event from the same device to be identified as own change")
	}
	if isOwnChangeEvent(workspaceConfig{DeviceID: "dev_2"}, client.ChangeEvent{SourceDeviceID: &source}) {
		t.Fatal("event from another device should not be identified as own change")
	}
	if isOwnChangeEvent(workspaceConfig{DeviceID: "dev_1"}, client.ChangeEvent{}) {
		t.Fatal("event without source device should not be identified as own change")
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

func TestRunSyncPullCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	content := []byte("pulled file")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":4,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			if got := r.URL.Query().Get("after_change_id"); got != "4" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":5,"file_id":"file_own","event_type":"update","version":2,"path":"/workspace/own.txt","source_device_id":"dev_1","created_at":"2026-06-30T00:01:00Z"},{"id":6,"file_id":"dir_1","event_type":"create","path":"/workspace/nested","created_at":"2026-06-30T00:02:00Z"},{"id":7,"file_id":"file_1","event_type":"create","version":3,"path":"/workspace/nested/a.txt","created_at":"2026-06-30T00:03:00Z"}],"next_cursor":7}}`))
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
			if req.DeviceID != "dev_1" || req.LastAppliedChangeID != 7 {
				t.Fatalf("ack request = %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":7,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:04:00Z"}}`))
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
		LastAppliedChangeID: 4,
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync pull json: %v", err)
	}
	if strings.Contains(stdout.String(), "pulled:") {
		t.Fatalf("json output includes text pull output: %s", stdout.String())
	}

	var snapshot syncPullSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode pull json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.DryRun || snapshot.CurrentCursor != 4 || snapshot.NextCursor != 7 || snapshot.Cursor != 7 {
		t.Fatalf("cursors = dry_run:%v current:%d next:%d cursor:%d", snapshot.DryRun, snapshot.CurrentCursor, snapshot.NextCursor, snapshot.Cursor)
	}
	if snapshot.Summary.Changes != 2 || snapshot.Summary.Files != 1 || snapshot.Summary.Directories != 1 || snapshot.Summary.Deleted != 0 || snapshot.Summary.Moved != 0 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
	if len(snapshot.Changes) != 2 || snapshot.Changes[0].Path != "/workspace/nested" || snapshot.Changes[1].Path != "/workspace/nested/a.txt" {
		t.Fatalf("changes = %#v", snapshot.Changes)
	}
	if len(snapshot.ConflictPaths) != 0 || len(snapshot.TrashPaths) != 0 {
		t.Fatalf("paths = conflicts:%#v trash:%#v", snapshot.ConflictPaths, snapshot.TrashPaths)
	}
	raw, err := os.ReadFile(filepath.Join(root, "nested", "a.txt"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("pulled file = %q", string(raw))
	}
}
