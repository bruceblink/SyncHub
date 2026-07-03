package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruceblink/SyncHub/internal/manifest"
)

func TestDiffDetectsCreateUpdateDelete(t *testing.T) {
	previous := Snapshot{
		"delete.txt": entry("/workspace/delete.txt", "delete.txt", "delete"),
		"same.txt":   entry("/workspace/same.txt", "same.txt", "same"),
		"update.txt": entry("/workspace/update.txt", "update.txt", "old"),
	}
	current := Snapshot{
		"create.txt": entry("/workspace/create.txt", "create.txt", "create"),
		"same.txt":   entry("/workspace/same.txt", "same.txt", "same"),
		"update.txt": entry("/workspace/update.txt", "update.txt", "new"),
	}

	changes := Diff(previous, current)
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3: %#v", len(changes), changes)
	}
	assertChange(t, changes[0], ChangeCreated, "create.txt")
	assertChange(t, changes[1], ChangeDeleted, "delete.txt")
	assertChange(t, changes[2], ChangeUpdated, "update.txt")
	if changes[0].Before != nil || changes[0].After == nil {
		t.Fatalf("create before/after = %#v/%#v", changes[0].Before, changes[0].After)
	}
	if changes[1].Before == nil || changes[1].After != nil {
		t.Fatalf("delete before/after = %#v/%#v", changes[1].Before, changes[1].After)
	}
	if changes[2].Before == nil || changes[2].After == nil {
		t.Fatalf("update before/after = %#v/%#v", changes[2].Before, changes[2].After)
	}
}

func TestDiffDetectsUniqueMove(t *testing.T) {
	previous := Snapshot{
		"old.txt":  entry("/workspace/old.txt", "old.txt", "move me"),
		"same.txt": entry("/workspace/same.txt", "same.txt", "same"),
	}
	current := Snapshot{
		"renamed.txt": entry("/workspace/renamed.txt", "renamed.txt", "move me"),
		"same.txt":    entry("/workspace/same.txt", "same.txt", "same"),
	}

	changes := Diff(previous, current)
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1: %#v", len(changes), changes)
	}
	assertChange(t, changes[0], ChangeMoved, "renamed.txt")
	if changes[0].Before == nil || changes[0].Before.RelativePath != "old.txt" {
		t.Fatalf("move before = %#v, want old.txt", changes[0].Before)
	}
	if changes[0].After == nil || changes[0].After.RelativePath != "renamed.txt" {
		t.Fatalf("move after = %#v, want renamed.txt", changes[0].After)
	}
}

func TestDiffDoesNotGuessAmbiguousMoves(t *testing.T) {
	previous := Snapshot{
		"old.txt": entry("/workspace/old.txt", "old.txt", "same"),
	}
	current := Snapshot{
		"new-a.txt": entry("/workspace/new-a.txt", "new-a.txt", "same"),
		"new-b.txt": entry("/workspace/new-b.txt", "new-b.txt", "same"),
	}

	changes := Diff(previous, current)
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3: %#v", len(changes), changes)
	}
	assertChange(t, changes[0], ChangeCreated, "new-a.txt")
	assertChange(t, changes[1], ChangeCreated, "new-b.txt")
	assertChange(t, changes[2], ChangeDeleted, "old.txt")
}

func TestPollerDetectsWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "delete.txt"), "delete")
	writeFile(t, filepath.Join(root, "update.txt"), "old")
	writeFile(t, filepath.Join(root, ".synchub", "ignored.txt"), "ignored")

	poller, err := NewPoller(context.Background(), root, "/workspace")
	if err != nil {
		t.Fatalf("new poller: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	writeFile(t, filepath.Join(root, "update.txt"), "new")
	writeFile(t, filepath.Join(root, "create.txt"), "create")
	writeFile(t, filepath.Join(root, ".synchub", "new-ignored.txt"), "ignored")

	changes, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3: %#v", len(changes), changes)
	}
	assertChange(t, changes[0], ChangeCreated, "create.txt")
	assertChange(t, changes[1], ChangeDeleted, "delete.txt")
	assertChange(t, changes[2], ChangeUpdated, "update.txt")

	changes, err = poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("second poll changes = %#v, want none", changes)
	}
}

func assertChange(t *testing.T, change Change, changeType, relativePath string) {
	t.Helper()
	if change.Type != changeType || change.RelativePath != relativePath {
		t.Fatalf("change = %#v, want %s %s", change, changeType, relativePath)
	}
}

func entry(path, relativePath, content string) manifest.Entry {
	return manifest.Entry{
		Path:         path,
		RelativePath: relativePath,
		Size:         int64(len(content)),
		SHA256:       sha(content),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
