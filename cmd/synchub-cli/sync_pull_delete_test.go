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

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunSyncPullAppliesDeleteEvents(t *testing.T) {
	oldNow := syncPushNow
	syncPushNow = func() time.Time { return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC) }
	defer func() { syncPushNow = oldNow }()

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
	trashPath := filepath.Join(root, ".synchub", "trash", "20260630T010203.000000000Z", "obsolete.txt")
	if !strings.Contains(stdout.String(), "trashed: 1") || !strings.Contains(stdout.String(), "trash: "+trashPath) {
		t.Fatalf("stdout = %q, want trash path %s", stdout.String(), trashPath)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("obsolete file still exists or stat failed: %v", err)
	}
	trashed, err := os.ReadFile(trashPath)
	if err != nil {
		t.Fatalf("read trash file: %v", err)
	}
	if string(trashed) != "remove me" {
		t.Fatalf("trash file = %q", string(trashed))
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

func TestRunSyncPullDeleteDirectoryKeepsLocalDescendantConflict(t *testing.T) {
	root := t.TempDir()
	syncPushNow = func() time.Time { return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC) }
	defer func() { syncPushNow = time.Now }()

	targetPath := filepath.Join(root, "obsolete", "nested", "a.txt")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("local edit"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	remoteVersion := int64(2)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/obsolete/nested/a.txt", RelativePath: "obsolete/nested/a.txt", Size: int64(len("server copy")), SHA256: testSHA([]byte("server copy")), RemoteVersion: &remoteVersion},
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
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":3,"file_id":"dir_1","event_type":"delete","path":"/workspace/obsolete","old_path":"/workspace/obsolete","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":3}}`))
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
		t.Fatalf("sync pull delete directory conflict: %v", err)
	}
	if !strings.Contains(stdout.String(), "deleted: 1") || !strings.Contains(stdout.String(), "conflicts kept: 1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "obsolete")); !os.IsNotExist(err) {
		t.Fatalf("obsolete directory still exists or stat failed: %v", err)
	}
	conflictPath := filepath.Join(root, "obsolete.conflict-dev_1-20260630T010203.000000000Z", "nested", "a.txt")
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
	if len(m.Items) != 1 || m.Items[0].RelativePath != "obsolete.conflict-dev_1-20260630T010203.000000000Z/nested/a.txt" {
		t.Fatalf("manifest items = %#v, want conflict directory copy only", m.Items)
	}
}

func TestDirectoryHasLocalChangesDetectsDeletedTrackedDescendant(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "obsolete")
	remainingPath := filepath.Join(localDir, "nested", "b.txt")
	if err := os.MkdirAll(filepath.Dir(remainingPath), 0o755); err != nil {
		t.Fatalf("mkdir local dir: %v", err)
	}
	if err := os.WriteFile(remainingPath, []byte("unchanged"), 0o644); err != nil {
		t.Fatalf("write remaining file: %v", err)
	}

	previousEntries := map[string]manifest.Entry{
		"/workspace/obsolete/nested/a.txt": {
			Path:         "/workspace/obsolete/nested/a.txt",
			RelativePath: "obsolete/nested/a.txt",
			Size:         int64(len("removed")),
			SHA256:       testSHA([]byte("removed")),
		},
		"/workspace/obsolete/nested/b.txt": {
			Path:         "/workspace/obsolete/nested/b.txt",
			RelativePath: "obsolete/nested/b.txt",
			Size:         int64(len("unchanged")),
			SHA256:       testSHA([]byte("unchanged")),
		},
	}

	changed, err := directoryHasLocalChanges(root, localDir, "/workspace/obsolete", previousEntries, nil)
	if err != nil {
		t.Fatalf("directory local changes: %v", err)
	}
	if !changed {
		t.Fatal("directory missing a tracked descendant was not reported as changed")
	}
}

func TestDirectoryHasLocalChangesIgnoresIgnoredDescendants(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "obsolete")
	ignoredPath := filepath.Join(localDir, "build", "cache.bin")
	unchangedPath := filepath.Join(localDir, "nested", "a.txt")
	if err := os.MkdirAll(filepath.Dir(ignoredPath), 0o755); err != nil {
		t.Fatalf("mkdir ignored path: %v", err)
	}
	if err := os.WriteFile(ignoredPath, []byte("generated"), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(unchangedPath), 0o755); err != nil {
		t.Fatalf("mkdir unchanged path: %v", err)
	}
	if err := os.WriteFile(unchangedPath, []byte("unchanged"), 0o644); err != nil {
		t.Fatalf("write unchanged file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".synchubignore"), []byte("obsolete/build/\n"), 0o644); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}
	ignoreRules, err := manifest.LoadIgnoreRules(root)
	if err != nil {
		t.Fatalf("load ignore rules: %v", err)
	}
	previousEntries := map[string]manifest.Entry{
		"/workspace/obsolete/nested/a.txt": {
			Path:         "/workspace/obsolete/nested/a.txt",
			RelativePath: "obsolete/nested/a.txt",
			Size:         int64(len("unchanged")),
			SHA256:       testSHA([]byte("unchanged")),
		},
	}

	changed, err := directoryHasLocalChanges(root, localDir, "/workspace/obsolete", previousEntries, ignoreRules)
	if err != nil {
		t.Fatalf("directory local changes: %v", err)
	}
	if changed {
		t.Fatal("ignored descendant should not be reported as a local directory change")
	}
}

func TestMoveDeletedLocalPathToTrashRejectsProtectedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".synchub"), 0o755); err != nil {
		t.Fatalf("mkdir .synchub: %v", err)
	}

	for _, path := range []string{root, filepath.Join(root, ".synchub")} {
		t.Run(path, func(t *testing.T) {
			_, err := moveDeletedLocalPathToTrash(root, path, time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC))
			if err == nil || !strings.Contains(err.Error(), "refusing to trash protected workspace path") {
				t.Fatalf("error = %v, want protected path error", err)
			}
		})
	}
}
