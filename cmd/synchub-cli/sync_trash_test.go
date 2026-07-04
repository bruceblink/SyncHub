package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRunSyncTrashEmptyCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"--path", root,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash empty json: %v", err)
	}

	var snapshot syncTrashSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode trash json: %v\n%s", err, stdout.String())
	}
	if snapshot.Items == nil || len(snapshot.Items) != 0 {
		t.Fatalf("items = %#v, want empty slice", snapshot.Items)
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

func TestRunSyncTrashCanOutputJSON(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(root, ".synchub", "trash", "20260701T010000.000000000Z"), 0o755); err != nil {
		t.Fatalf("mkdir old trash batch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".synchub", "trash", "20260701T010000.000000000Z", "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old trash file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".synchub", "trash", "20260702T010000.000000000Z", "docs"), 0o755); err != nil {
		t.Fatalf("mkdir trash dir: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"trash",
		"--path", root,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash json: %v", err)
	}
	if strings.Contains(stdout.String(), "trash entries:") {
		t.Fatalf("json output includes text trash output: %s", stdout.String())
	}

	var snapshot syncTrashSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode trash json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("items = %#v, want two", snapshot.Items)
	}
	if snapshot.Items[0].Batch != "20260702T010000.000000000Z" || snapshot.Items[0].Path != "docs/" || !snapshot.Items[0].IsDir {
		t.Fatalf("first item = %#v", snapshot.Items[0])
	}
	if snapshot.Items[1].Batch != "20260701T010000.000000000Z" || snapshot.Items[1].Path != "old.txt" || snapshot.Items[1].Size != 3 || snapshot.Items[1].IsDir {
		t.Fatalf("second item = %#v", snapshot.Items[1])
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

func TestRunSyncTrashRestoreCanOutputJSON(t *testing.T) {
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync trash restore json: %v", err)
	}
	if strings.Contains(stdout.String(), "restored:") {
		t.Fatalf("json output includes text restore output: %s", stdout.String())
	}

	var snapshot syncTrashRestoreSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode trash restore json: %v\n%s", err, stdout.String())
	}
	restoredPath := filepath.Join(root, "docs", "readme.txt")
	if snapshot.Workspace.Root != root || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Action != "restore" || snapshot.Batch != "20260702T010000.000000000Z" || snapshot.Entry != "docs/readme.txt" || snapshot.RestoredPath != restoredPath {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	raw, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(raw) != "restore me" {
		t.Fatalf("restored file = %q", string(raw))
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
