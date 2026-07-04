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

func TestRunSyncStatusShowsManifestSummary(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           "http://localhost:8765",
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 7,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	})
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("alpha")), SHA256: testSHA([]byte("alpha"))},
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
		"device: dev_1",
		"device name: laptop",
		"device platform: windows",
		"last applied change: 7",
		"files: 1",
		"remote tracked: 0",
		"local only: 1",
		"remote version range: -",
		"last scan: 2026-06-30T01:02:03Z",
		"pending changes: 0",
		"trash entries: 0",
		"agent: not run",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusShowsLocalTrashSummary(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	oldBatch := filepath.Join(root, ".synchub", "trash", "20260701T010000.000000000Z")
	if err := os.MkdirAll(oldBatch, 0o755); err != nil {
		t.Fatalf("mkdir old trash batch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldBatch, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old trash file: %v", err)
	}
	newBatch := filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z")
	if err := os.MkdirAll(newBatch, 0o755); err != nil {
		t.Fatalf("mkdir new trash batch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newBatch, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new trash file: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"sync", "status", "--path", root}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"trash entries: 2",
		"latest trash: 20260702T010000.000000000Z new.txt",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusShowsAgentState(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "agent-state.json"), syncAgentState{
		Version:             1,
		Root:                root,
		Status:              "error",
		CyclesRun:           3,
		ConsecutiveFailures: 2,
		LastFailureAt:       testTimePtr(time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC)),
		LastError:           "sync failed",
		UpdatedAt:           time.Date(2026, 7, 4, 1, 2, 4, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write agent state: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"sync", "status", "--path", root}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"agent: error",
		"agent cycles: 3",
		"agent consecutive failures: 2",
		"agent last success: -",
		"agent last failure: 2026-07-04T01:02:03Z",
		"agent last error: sync failed",
		"agent updated: 2026-07-04T01:02:04Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusShowsPendingLocalChanges(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "modified.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "unchanged.txt"), []byte("same"), 0o644); err != nil {
		t.Fatalf("write unchanged file: %v", err)
	}
	remoteVersion := int64(2)
	newerRemoteVersion := int64(5)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/deleted.txt", RelativePath: "deleted.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old")), RemoteVersion: &remoteVersion},
			{Path: "/workspace/unchanged.txt", RelativePath: "unchanged.txt", Size: int64(len("same")), SHA256: testSHA([]byte("same")), RemoteVersion: &newerRemoteVersion},
			{Path: "/workspace/modified.txt", RelativePath: "modified.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old"))},
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
		"remote tracked: 2",
		"local only: 1",
		"remote version range: 2-5",
		"pending changes: 3",
		"created: 1",
		"updated: 1",
		"deleted: 1",
		"moved: 0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusShowsPendingLocalMove(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), []byte("move me"), 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len("move me")), SHA256: testSHA([]byte("move me"))},
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
		"pending changes: 1",
		"created: 0",
		"updated: 0",
		"deleted: 0",
		"moved: 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusCanShowRemoteConflicts(t *testing.T) {
	root := t.TempDir()
	conflictsRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/conflicts" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("resolution"); got != "pending" {
			t.Fatalf("resolution = %q, want pending", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Fatalf("limit = %q, want 10", got)
		}
		conflictsRequested = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"conf_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"pending","created_at":"2026-06-30T00:00:00Z"}]}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	})
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("alpha")), SHA256: testSHA([]byte("alpha"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"status",
		"--path", root,
		"--config", loginConfigPath,
		"--show-conflicts",
		"--conflict-limit", "10",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status conflicts: %v", err)
	}
	if !conflictsRequested {
		t.Fatal("conflicts endpoint was not called")
	}
	out := stdout.String()
	for _, want := range []string{
		"pending changes: 0",
		"remote conflicts: 1",
		"pending /workspace/a.txt local=1 remote=2 id=conf_1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncStatusCanShowRemoteChanges(t *testing.T) {
	root := t.TempDir()
	changesRequested := false
	sourceDeviceID := "dev_1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/changes" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("device_id"); got != "dev_1" {
			t.Fatalf("device_id = %q", got)
		}
		if got := r.URL.Query().Get("after_change_id"); got != "4" {
			t.Fatalf("after_change_id = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Fatalf("limit = %q", got)
		}
		changesRequested = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":5,"file_id":"file_own","event_type":"update","version":2,"path":"/workspace/own.txt","source_device_id":"dev_1","created_at":"2026-06-30T00:01:00Z"},{"id":6,"file_id":"file_1","event_type":"update","version":3,"path":"/workspace/a.txt","created_at":"2026-06-30T00:02:00Z"},{"id":7,"file_id":"file_2","event_type":"move","version":4,"path":"/workspace/b.txt","old_path":"/workspace/old.txt","source_device_id":"dev_2","created_at":"2026-06-30T00:03:00Z"}],"next_cursor":7}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            sourceDeviceID,
		LastAppliedChangeID: 4,
	})
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"status",
		"--path", root,
		"--config", loginConfigPath,
		"--show-remote",
		"--remote-limit", "10",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status remote changes: %v", err)
	}
	if !changesRequested {
		t.Fatal("changes endpoint was not called")
	}
	out := stdout.String()
	for _, want := range []string{
		"pending changes: 0",
		"remote changes: 2",
		"update /workspace/a.txt version=3 id=6",
		"move /workspace/old.txt -> /workspace/b.txt version=4 id=7",
		"remote next cursor: 7",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "/workspace/own.txt") {
		t.Fatalf("stdout includes own change: %s", out)
	}
}

func TestRunSyncStatusSkipsRemoteChangesWithoutDevice(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("status without device must not call server: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	})
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"status",
		"--path", root,
		"--config", filepath.Join(root, ".synchub", "missing-login.json"),
		"--show-remote",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync status remote without device: %v", err)
	}
	if !strings.Contains(stdout.String(), "remote changes: skipped (workspace device is not registered)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSyncHelpIncludesOperationalCommands(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"sync", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"synchub-cli sync once --path . --dry-run",
		"synchub-cli sync status --path . --show-remote",
		"synchub-cli sync push --path . --dry-run",
		"synchub-cli sync pull --path . --dry-run",
		"synchub-cli sync trash --path .",
		"synchub-cli sync trash restore --path . --batch 20260702T010000.000000000Z --entry docs/",
		"synchub-cli sync devices --path .",
		"synchub-cli sync conflicts --path .",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sync help missing %q: %s", want, out)
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

func testTimePtr(t time.Time) *time.Time {
	return &t
}
