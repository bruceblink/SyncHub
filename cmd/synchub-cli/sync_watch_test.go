package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
