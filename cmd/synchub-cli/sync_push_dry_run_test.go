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
)

func TestRunSyncPushDryRunPreviewsLocalPlan(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "update.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	moveContent := []byte("same content")
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), moveContent, 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry run must not call server: %s %s", r.Method, r.URL.String())
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
	deletedVersion := int64(3)
	updateVersion := int64(4)
	moveVersion := int64(5)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/delete.txt", RelativePath: "delete.txt", Size: int64(len("deleted")), SHA256: testSHA([]byte("deleted")), RemoteVersion: &deletedVersion},
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len(moveContent)), SHA256: testSHA(moveContent), RemoteVersion: &moveVersion},
			{Path: "/workspace/update.txt", RelativePath: "update.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old")), RemoteVersion: &updateVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest before dry run: %v", err)
	}

	var stdout bytes.Buffer
	err = run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", filepath.Join(root, ".synchub", "missing-login.json"),
		"--dry-run",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push dry run: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"dry run: true",
		"changes: 4",
		"move /workspace/old.txt -> /workspace/renamed.txt base_version=5",
		"delete /workspace/delete.txt base_version=3",
		"create /workspace/new.txt size=8 base_version=-",
		"update /workspace/update.txt size=7 base_version=4",
		"uploaded: 2 files",
		"deleted: 1 files",
		"moved: 1 files",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after dry run: %v", err)
	}
	if !bytes.Equal(afterManifest, beforeManifest) {
		t.Fatalf("dry run changed manifest")
	}
}

func TestRunSyncPushDryRunCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "update.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	moveContent := []byte("same content")
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), moveContent, 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry run must not call server: %s %s", r.Method, r.URL.String())
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
	deletedVersion := int64(3)
	updateVersion := int64(4)
	moveVersion := int64(5)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/delete.txt", RelativePath: "delete.txt", Size: int64(len("deleted")), SHA256: testSHA([]byte("deleted")), RemoteVersion: &deletedVersion},
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len(moveContent)), SHA256: testSHA(moveContent), RemoteVersion: &moveVersion},
			{Path: "/workspace/update.txt", RelativePath: "update.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old")), RemoteVersion: &updateVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest before dry run: %v", err)
	}

	var stdout bytes.Buffer
	err = run(context.Background(), []string{
		"sync",
		"push",
		"--path", root,
		"--config", filepath.Join(root, ".synchub", "missing-login.json"),
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push dry run json: %v", err)
	}
	if strings.Contains(stdout.String(), "dry run: true") {
		t.Fatalf("json output includes text dry-run output: %s", stdout.String())
	}

	var snapshot syncPushSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode push dry-run json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if !snapshot.DryRun {
		t.Fatalf("dry_run = false")
	}
	if snapshot.Summary.Changes != 4 || snapshot.Summary.Uploaded != 2 || snapshot.Summary.Deleted != 1 || snapshot.Summary.Moved != 1 || snapshot.Summary.ConflictsKept != 0 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
	if len(snapshot.Uploads) != 2 || snapshot.Uploads[0].Action != "create" || snapshot.Uploads[0].Path != "/workspace/new.txt" || snapshot.Uploads[1].Action != "update" || snapshot.Uploads[1].BaseVersion == nil || *snapshot.Uploads[1].BaseVersion != updateVersion {
		t.Fatalf("uploads = %#v", snapshot.Uploads)
	}
	if len(snapshot.Deletes) != 1 || snapshot.Deletes[0].Path != "/workspace/delete.txt" || snapshot.Deletes[0].BaseVersion == nil || *snapshot.Deletes[0].BaseVersion != deletedVersion {
		t.Fatalf("deletes = %#v", snapshot.Deletes)
	}
	if len(snapshot.Moves) != 1 || snapshot.Moves[0].From != "/workspace/old.txt" || snapshot.Moves[0].To != "/workspace/renamed.txt" || snapshot.Moves[0].BaseVersion == nil || *snapshot.Moves[0].BaseVersion != moveVersion {
		t.Fatalf("moves = %#v", snapshot.Moves)
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after dry run: %v", err)
	}
	if !bytes.Equal(afterManifest, beforeManifest) {
		t.Fatalf("dry run changed manifest")
	}
}
