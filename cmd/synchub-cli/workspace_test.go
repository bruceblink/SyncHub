package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunWorkspaceInitWritesConfig(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
		User:      clientUser("u1", "user@example.com"),
		Tokens: client.TokenPair{
			AccessToken:  "access",
			RefreshToken: "refresh",
			ExpiresIn:    900,
		},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", workspaceRoot,
		"--remote-path", "projects/demo",
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace init: %v", err)
	}
	if !strings.Contains(stdout.String(), "workspace initialized") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "next: synchub-cli sync daemon --path "+workspaceRoot) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(workspaceRoot, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	var cfg workspaceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if cfg.Version != 1 || cfg.Root != workspaceRoot || cfg.RemotePath != "/projects/demo" {
		t.Fatalf("unexpected workspace config: %#v", cfg)
	}
	if cfg.ServerURL != "http://localhost:8765" || cfg.UserID != "u1" || cfg.UserEmail != "user@example.com" {
		t.Fatalf("workspace config missing login context: %#v", cfg)
	}
	if cfg.CreatedAt.IsZero() || cfg.UpdatedAt.IsZero() {
		t.Fatalf("workspace config missing timestamps: %#v", cfg)
	}
}

func TestRunWorkspaceInitCanOutputJSON(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
		User:      clientUser("u1", "user@example.com"),
		Tokens: client.TokenPair{
			AccessToken:  "access",
			RefreshToken: "refresh",
			ExpiresIn:    900,
		},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", workspaceRoot,
		"--remote-path", "projects/demo",
		"--config", loginConfigPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace init json: %v", err)
	}
	if strings.Contains(stdout.String(), "workspace initialized") {
		t.Fatalf("json output includes text init output: %s", stdout.String())
	}

	var snapshot workspaceInitSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode workspace init json: %v\n%s", err, stdout.String())
	}
	wantConfig := filepath.Join(workspaceRoot, ".synchub", "workspace.json")
	if snapshot.Config != wantConfig {
		t.Fatalf("config = %q, want %q", snapshot.Config, wantConfig)
	}
	if snapshot.Workspace.Version != 1 || snapshot.Workspace.Root != workspaceRoot || snapshot.Workspace.RemotePath != "/projects/demo" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Workspace.ServerURL != "http://localhost:8765" || snapshot.Workspace.UserID != "u1" || snapshot.Workspace.UserEmail != "user@example.com" {
		t.Fatalf("workspace missing login context: %#v", snapshot.Workspace)
	}
	if snapshot.Workspace.CreatedAt.IsZero() || snapshot.Workspace.UpdatedAt.IsZero() {
		t.Fatalf("workspace missing timestamps: %#v", snapshot.Workspace)
	}

	written, err := readWorkspaceConfig(wantConfig)
	if err != nil {
		t.Fatalf("read written workspace config: %v", err)
	}
	if written.Root != snapshot.Workspace.Root || written.RemotePath != snapshot.Workspace.RemotePath || written.UserEmail != snapshot.Workspace.UserEmail {
		t.Fatalf("written config = %#v snapshot=%#v", written, snapshot.Workspace)
	}
}

func TestRunWorkspaceInitRequiresLogin(t *testing.T) {
	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", t.TempDir(),
		"--config", filepath.Join(t.TempDir(), "missing.json"),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("error = %v, want not logged in", err)
	}
}

func TestNormalizeRemotePathRejectsTraversal(t *testing.T) {
	for _, input := range []string{
		"../secret.txt",
		"/workspace/../secret.txt",
		`workspace\..\secret.txt`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := normalizeRemotePath(input)
			if err == nil || !strings.Contains(err.Error(), "remote path traversal is not allowed") {
				t.Fatalf("normalizeRemotePath(%q) error = %v, want traversal error", input, err)
			}
		})
	}
}

func TestNormalizeRemotePathCleansSafePath(t *testing.T) {
	got, err := normalizeRemotePath(`workspace//docs\guide.md`)
	if err != nil {
		t.Fatalf("normalize remote path: %v", err)
	}
	if got != "/workspace/docs/guide.md" {
		t.Fatalf("path = %q, want /workspace/docs/guide.md", got)
	}
}

func TestRunWorkspaceHelpIncludesInitJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"workspace", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli workspace init --path . --remote-path /workspace --json") {
		t.Fatalf("workspace help missing init json command: %s", stdout.String())
	}
}
