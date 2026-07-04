package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
)

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
	want := "created create.txt\ndeleted delete.txt\nupdated update.txt\nchanges: 3\ncreated: 1\nupdated: 1\ndeleted: 1\nmoved: 0\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncWatchOnceCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	})
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/delete.txt", RelativePath: "delete.txt", Size: int64(len("delete")), SHA256: testSHA([]byte("delete"))},
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len("move me")), SHA256: testSHA([]byte("move me"))},
			{Path: "/workspace/update.txt", RelativePath: "update.txt", Size: int64(len("old")), SHA256: testSHA([]byte("old"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "create.txt"), []byte("create"), 0o644); err != nil {
		t.Fatalf("write create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), []byte("move me"), 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync watch once json: %v", err)
	}
	if strings.Contains(stdout.String(), "changes:") {
		t.Fatalf("json output includes text watch output: %s", stdout.String())
	}

	var snapshot watchChangesSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode watch json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Count != 4 || snapshot.Counts.Total != 4 || snapshot.Counts.Created != 1 || snapshot.Counts.Updated != 1 || snapshot.Counts.Deleted != 1 || snapshot.Counts.Moved != 1 {
		t.Fatalf("counts = %#v count=%d", snapshot.Counts, snapshot.Count)
	}
	got := map[string]watchChangeSnapshotItem{}
	for _, item := range snapshot.Items {
		got[item.Type+":"+item.RelativePath] = item
	}
	if item, ok := got["created:create.txt"]; !ok || item.After == nil || item.Before != nil {
		t.Fatalf("created item = %#v ok=%v", item, ok)
	}
	if item, ok := got["deleted:delete.txt"]; !ok || item.Before == nil || item.After != nil {
		t.Fatalf("deleted item = %#v ok=%v", item, ok)
	}
	if item, ok := got["updated:update.txt"]; !ok || item.Before == nil || item.After == nil {
		t.Fatalf("updated item = %#v ok=%v", item, ok)
	}
	if item, ok := got["moved:renamed.txt"]; !ok || item.Before == nil || item.After == nil || item.Before.RelativePath != "old.txt" || item.After.RelativePath != "renamed.txt" {
		t.Fatalf("moved item = %#v ok=%v", item, ok)
	}
}

func TestRunSyncWatchOnceShowsLocalMove(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len("move me")), SHA256: testSHA([]byte("move me"))},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), []byte("move me"), 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
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
	want := "moved old.txt -> renamed.txt\nchanges: 1\ncreated: 0\nupdated: 0\ndeleted: 0\nmoved: 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
