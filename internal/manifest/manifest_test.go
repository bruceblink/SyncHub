package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "alpha")
	writeFile(t, filepath.Join(root, "nested", "b.txt"), "bravo")
	writeFile(t, filepath.Join(root, ".synchub", "manifest.json"), "ignored")

	m, err := Scan(context.Background(), root, "/workspace")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if m.Version != 1 || m.Root != filepath.Clean(root) || m.RemotePath != "/workspace" {
		t.Fatalf("unexpected manifest header: %#v", m)
	}
	if len(m.Items) != 2 {
		t.Fatalf("items = %d, want 2: %#v", len(m.Items), m.Items)
	}
	if m.Items[0].RelativePath != "a.txt" || m.Items[0].Path != "/workspace/a.txt" {
		t.Fatalf("first item = %#v", m.Items[0])
	}
	if m.Items[0].Size != 5 || m.Items[0].SHA256 != sha("alpha") {
		t.Fatalf("first item content metadata = %#v", m.Items[0])
	}
	if m.Items[1].RelativePath != "nested/b.txt" || m.Items[1].Path != "/workspace/nested/b.txt" {
		t.Fatalf("second item = %#v", m.Items[1])
	}
}

func TestScanHonorsSynchubIgnore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".synchubignore"), "# local build artifacts\n*.tmp\nbuild/\nlogs/*.log\n")
	writeFile(t, filepath.Join(root, ".ignore"), "\ufeffcache/\n*.secret\n")
	writeFile(t, filepath.Join(root, "keep.txt"), "keep")
	writeFile(t, filepath.Join(root, "scratch.tmp"), "temporary")
	writeFile(t, filepath.Join(root, "build", "app.bin"), "binary")
	writeFile(t, filepath.Join(root, "cache", "state.db"), "cache")
	writeFile(t, filepath.Join(root, "logs", "debug.log"), "log")
	writeFile(t, filepath.Join(root, "logs", "keep.txt"), "keep log note")
	writeFile(t, filepath.Join(root, "nested", "scratch.tmp"), "nested temporary")
	writeFile(t, filepath.Join(root, "nested", "token.secret"), "secret")

	m, err := Scan(context.Background(), root, "/workspace")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := make([]string, 0, len(m.Items))
	for _, item := range m.Items {
		got = append(got, item.RelativePath)
	}
	want := []string{"keep.txt", "logs/keep.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

func TestIgnoreFilePathsIncludesSupportedWorkspaceIgnoreFiles(t *testing.T) {
	root := t.TempDir()
	paths := IgnoreFilePaths(root)
	want := []string{filepath.Join(root, ".synchubignore"), filepath.Join(root, ".ignore")}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestTransientFileReadErrorTreatsPermissionAsSkippable(t *testing.T) {
	if !isTransientFileReadError(&os.PathError{Op: "open", Path: "locked", Err: os.ErrPermission}) {
		t.Fatal("permission error should be skippable")
	}
}

func TestTransientFileReadErrorTreatsWindowsSharingViolationAsSkippable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific sharing violation")
	}
	if !isTransientFileReadError(&os.PathError{Op: "open", Path: "locked", Err: syscall.Errno(32)}) {
		t.Fatal("windows sharing violation should be skippable")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
