package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
