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

func TestRunManifestScanWritesManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("bravo"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest scan: %v", err)
	}
	if !strings.Contains(stdout.String(), "manifest scanned: 2 files") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(root, ".synchub", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Items []struct {
			Path         string `json:"path"`
			RelativePath string `json:"relative_path"`
			SHA256       string `json:"sha256"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(manifest.Items) != 2 {
		t.Fatalf("items = %d, want 2: %#v", len(manifest.Items), manifest.Items)
	}
	if manifest.Items[0].Path != "/workspace/a.txt" || manifest.Items[0].RelativePath != "a.txt" {
		t.Fatalf("first item = %#v", manifest.Items[0])
	}
	if manifest.Items[1].Path != "/workspace/nested/b.txt" || manifest.Items[1].RelativePath != "nested/b.txt" {
		t.Fatalf("second item = %#v", manifest.Items[1])
	}
}

func TestRunManifestScanDryRunDoesNotWriteManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", root,
		"--dry-run",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest scan dry-run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "manifest scanned: 1 files") || !strings.Contains(out, "dry run: true") {
		t.Fatalf("stdout = %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".synchub", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest was written or stat failed: %v", err)
	}
}

func TestRunManifestScanDryRunCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", root,
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest scan dry-run json: %v", err)
	}
	if strings.Contains(stdout.String(), "manifest scanned:") {
		t.Fatalf("json output includes text scan output: %s", stdout.String())
	}

	var snapshot manifestScanSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode manifest scan json: %v\n%s", err, stdout.String())
	}
	if !snapshot.DryRun || snapshot.Output != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Manifest.Root != root || snapshot.Manifest.RemotePath != "/workspace" || len(snapshot.Manifest.Items) != 1 {
		t.Fatalf("manifest = %#v", snapshot.Manifest)
	}
	if snapshot.Manifest.Items[0].Path != "/workspace/a.txt" || snapshot.Manifest.Items[0].RelativePath != "a.txt" {
		t.Fatalf("item = %#v", snapshot.Manifest.Items[0])
	}
	if _, err := os.Stat(filepath.Join(root, ".synchub", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest was written or stat failed: %v", err)
	}
}

func TestRunManifestScanCanOutputJSONAfterWritingManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	outputPath := filepath.Join(root, ".synchub", "custom-manifest.json")

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", root,
		"--output", outputPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest scan json: %v", err)
	}
	if strings.Contains(stdout.String(), "manifest scanned:") {
		t.Fatalf("json output includes text scan output: %s", stdout.String())
	}

	var snapshot manifestScanSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode manifest scan json: %v\n%s", err, stdout.String())
	}
	if snapshot.DryRun || snapshot.Output != outputPath || len(snapshot.Manifest.Items) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	written, err := readManifest(outputPath)
	if err != nil {
		t.Fatalf("read written manifest: %v", err)
	}
	if written.Root != snapshot.Manifest.Root || len(written.Items) != len(snapshot.Manifest.Items) || written.Items[0].Path != snapshot.Manifest.Items[0].Path {
		t.Fatalf("written manifest = %#v snapshot=%#v", written, snapshot.Manifest)
	}
}

func TestRunManifestScanRequiresWorkspace(t *testing.T) {
	err := run(context.Background(), []string{
		"manifest",
		"scan",
		"--path", t.TempDir(),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "workspace is not initialized") {
		t.Fatalf("error = %v, want workspace not initialized", err)
	}
}

func TestRunManifestIgnoresListsWorkspaceRules(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".synchubignore"), []byte("# generated files\n*.tmp\nbuild/\nlogs/*.log\n"), 0o644); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"ignores",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest ignores: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"ignore file: " + filepath.Join(root, ".synchubignore"),
		"rules: 3",
		"*.tmp",
		"build/",
		"logs/*.log",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunManifestIgnoresShowsEmptyRules(t *testing.T) {
	root := t.TempDir()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"manifest",
		"ignores",
		"--path", root,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest ignores without file: %v", err)
	}
	if !strings.Contains(stdout.String(), "rules: 0") {
		t.Fatalf("stdout = %q, want rules: 0", stdout.String())
	}
}

func TestRunManifestHelpIncludesScanJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"manifest", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("manifest help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli manifest scan --path . --json") {
		t.Fatalf("manifest help missing scan json command: %s", stdout.String())
	}
}
