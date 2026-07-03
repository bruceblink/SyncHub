package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncTrashListsLocalTrashEntries(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".synchub", "trash", "20260701T010000.000000000Z"), 0o755); err != nil {
		t.Fatalf("mkdir old trash batch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".synchub", "trash", "20260701T010000.000000000Z", "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old trash file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z", "docs", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir trash dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z", "docs", "nested", "a.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write nested trash file: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash: %v", err)
	}
	want := "trash entries: 2\n20260702T010000.000000000Z docs/\n20260701T010000.000000000Z old.txt size=3\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncTrashShowsEmptyTrash(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash empty: %v", err)
	}
	if stdout.String() != "trash entries: 0\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSyncTrashHonorsLimit(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	for _, batch := range []string{"20260701T010000.000000000Z", "20260702T010000.000000000Z"} {
		dir := filepath.Join(root, ".synchub", "trash", batch)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir trash batch: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, batch+".txt"), []byte(batch), 0o644); err != nil {
			t.Fatalf("write trash file: %v", err)
		}
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"--path", root,
		"--limit", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash limit: %v", err)
	}
	want := "trash entries: 1\n20260702T010000.000000000Z 20260702T010000.000000000Z.txt size=26\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncTrashRestoreRestoresEntry(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	trashFile := filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z", "docs", "readme.txt")
	if err := os.MkdirAll(filepath.Dir(trashFile), 0o755); err != nil {
		t.Fatalf("mkdir trash file: %v", err)
	}
	if err := os.WriteFile(trashFile, []byte("restore me"), 0o644); err != nil {
		t.Fatalf("write trash file: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"restore",
		"--path", root,
		"--batch", "20260702T010000.000000000Z",
		"--entry", "docs/readme.txt",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash restore: %v", err)
	}
	restoredPath := filepath.Join(root, "docs", "readme.txt")
	want := "restored: " + restoredPath + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	raw, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(raw) != "restore me" {
		t.Fatalf("restored file = %q", string(raw))
	}
	if _, err := os.Stat(trashFile); !os.IsNotExist(err) {
		t.Fatalf("trash file still exists or stat failed: %v", err)
	}
}

func TestRunSyncTrashRestoreDoesNotOverwriteExistingTarget(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	trashFile := filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z", "readme.txt")
	if err := os.MkdirAll(filepath.Dir(trashFile), 0o755); err != nil {
		t.Fatalf("mkdir trash file: %v", err)
	}
	if err := os.WriteFile(trashFile, []byte("trash"), 0o644); err != nil {
		t.Fatalf("write trash file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	err := run(context.Background(), []string{
		"sync",
		"trash",
		"restore",
		"--path", root,
		"--batch", "20260702T010000.000000000Z",
		"--entry", "readme.txt",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "restore target already exists") {
		t.Fatalf("error = %v, want restore target already exists", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, "readme.txt"))
	if readErr != nil {
		t.Fatalf("read existing file: %v", readErr)
	}
	if string(raw) != "existing" {
		t.Fatalf("existing file = %q", string(raw))
	}
}
