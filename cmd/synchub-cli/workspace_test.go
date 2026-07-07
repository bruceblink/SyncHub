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
	if !strings.Contains(stdout.String(), "daemon: registered for startup discovery") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "startup command: synchub-cli sync daemon") {
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

	registry, _, err := readWorkspaceRegistry(loginConfigPath)
	if err != nil {
		t.Fatalf("read workspace registry: %v", err)
	}
	if len(registry.Workspaces) != 1 {
		t.Fatalf("registry workspaces = %#v", registry.Workspaces)
	}
	if registry.Workspaces[0].Root != workspaceRoot || registry.Workspaces[0].WorkspaceConfigPath != filepath.Join(workspaceRoot, ".synchub", "workspace.json") || registry.Workspaces[0].ConfigPath != loginConfigPath {
		t.Fatalf("registry entry = %#v", registry.Workspaces[0])
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

func TestRunWorkspaceInitSupportsMultiplePaths(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	firstRoot := filepath.Join(tempDir, "alpha")
	secondRoot := filepath.Join(tempDir, "bravo")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
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
		"--path", firstRoot,
		"--path", secondRoot,
		"--remote-root", "/devices",
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace init multiple: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "workspaces initialized: 2") || !strings.Contains(out, "workspace initialized: "+firstRoot) || !strings.Contains(out, "workspace initialized: "+secondRoot) {
		t.Fatalf("stdout = %q", out)
	}
	firstConfig, err := readWorkspaceConfig(filepath.Join(firstRoot, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read first workspace config: %v", err)
	}
	secondConfig, err := readWorkspaceConfig(filepath.Join(secondRoot, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read second workspace config: %v", err)
	}
	if firstConfig.RemotePath != "/devices/alpha" || secondConfig.RemotePath != "/devices/bravo" {
		t.Fatalf("remote paths = %q %q", firstConfig.RemotePath, secondConfig.RemotePath)
	}
	registry, _, err := readWorkspaceRegistry(loginConfigPath)
	if err != nil {
		t.Fatalf("read workspace registry: %v", err)
	}
	if len(registry.Workspaces) != 2 {
		t.Fatalf("registry workspaces = %#v", registry.Workspaces)
	}
}

func TestRunWorkspaceInitSupportsMultiplePathsAsArgsJSON(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	firstRoot := filepath.Join(tempDir, "alpha")
	secondRoot := filepath.Join(tempDir, "bravo")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
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
		"--remote-root", "/devices",
		"--config", loginConfigPath,
		"--json",
		firstRoot,
		secondRoot,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace init multiple json: %v", err)
	}
	if strings.Contains(stdout.String(), "workspace initialized") {
		t.Fatalf("json output includes text init output: %s", stdout.String())
	}
	var snapshot workspaceInitBatchSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode workspace init batch json: %v\n%s", err, stdout.String())
	}
	if len(snapshot.Workspaces) != 2 {
		t.Fatalf("workspaces = %#v", snapshot.Workspaces)
	}
	if snapshot.Workspaces[0].Workspace.RemotePath != "/devices/alpha" || snapshot.Workspaces[1].Workspace.RemotePath != "/devices/bravo" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunWorkspaceListShowsRegisteredWorkspaces(t *testing.T) {
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
	if err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", workspaceRoot,
		"--remote-path", "/workspace",
		"--config", loginConfigPath,
	}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("workspace init: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"list",
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"registry: " + filepath.Join(tempDir, "workspaces.json"),
		"workspaces: 1",
		"workspace: " + workspaceRoot,
		"remote path: /workspace",
		"user: user@example.com",
		"status: ok",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace list output missing %q:\n%s", want, out)
		}
	}
}

func TestRunWorkspaceListReportsStaleEntriesJSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	registryPath := filepath.Join(tempDir, "workspaces.json")
	registry := workspaceRegistry{
		Version: 1,
		Workspaces: []workspaceRegistryEntry{{
			Root:                filepath.Join(tempDir, "missing"),
			WorkspaceConfigPath: filepath.Join(tempDir, "missing", ".synchub", "workspace.json"),
			ConfigPath:          configPath,
			RemotePath:          "/missing",
			ServerURL:           "http://localhost:8765",
			UserEmail:           "user@example.com",
		}},
	}
	if err := writeJSONFile(registryPath, registry, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"list",
		"--config", configPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace list json: %v", err)
	}
	if strings.Contains(stdout.String(), "status:") {
		t.Fatalf("json output includes text list output: %s", stdout.String())
	}
	var snapshot workspaceListSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode workspace list json: %v\n%s", err, stdout.String())
	}
	if snapshot.Registry != registryPath {
		t.Fatalf("registry = %q, want %q", snapshot.Registry, registryPath)
	}
	if len(snapshot.Workspaces) != 1 {
		t.Fatalf("workspaces = %#v", snapshot.Workspaces)
	}
	workspace := snapshot.Workspaces[0]
	if workspace.Available || workspace.Reason == "" {
		t.Fatalf("workspace = %#v, want unavailable with reason", workspace)
	}
	if workspace.Root != filepath.Join(tempDir, "missing") || workspace.RemotePath != "/missing" || workspace.UserEmail != "user@example.com" {
		t.Fatalf("workspace = %#v", workspace)
	}
}

func TestRunWorkspacePruneRemovesStaleEntries(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	keepRoot := filepath.Join(tempDir, "keep")
	if err := os.MkdirAll(keepRoot, 0o755); err != nil {
		t.Fatalf("create keep root: %v", err)
	}
	keepConfig := workspaceConfig{
		Version:    1,
		Root:       keepRoot,
		RemotePath: "/keep",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}
	if err := writeJSONFile(defaultWorkspaceConfigPath(keepRoot), keepConfig, 0o600); err != nil {
		t.Fatalf("write keep workspace config: %v", err)
	}
	registryPath := filepath.Join(tempDir, "workspaces.json")
	missingRoot := filepath.Join(tempDir, "missing")
	registry := workspaceRegistry{
		Version: 1,
		Workspaces: []workspaceRegistryEntry{
			{
				Root:                keepRoot,
				WorkspaceConfigPath: defaultWorkspaceConfigPath(keepRoot),
				ConfigPath:          configPath,
				RemotePath:          "/keep",
				ServerURL:           "http://localhost:8765",
				UserEmail:           "user@example.com",
			},
			{
				Root:                missingRoot,
				WorkspaceConfigPath: defaultWorkspaceConfigPath(missingRoot),
				ConfigPath:          configPath,
				RemotePath:          "/missing",
				ServerURL:           "http://localhost:8765",
				UserEmail:           "user@example.com",
			},
		},
	}
	if err := writeJSONFile(registryPath, registry, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"prune",
		"--config", configPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace prune: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"removed: 1",
		"kept: 1",
		"workspace: " + missingRoot,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace prune output missing %q:\n%s", want, out)
		}
	}
	pruned, err := readWorkspaceRegistryFile(registryPath)
	if err != nil {
		t.Fatalf("read pruned registry: %v", err)
	}
	if len(pruned.Workspaces) != 1 || pruned.Workspaces[0].Root != keepRoot {
		t.Fatalf("pruned registry = %#v", pruned.Workspaces)
	}
	if pruned.UpdatedAt.IsZero() {
		t.Fatalf("pruned registry missing updated timestamp: %#v", pruned)
	}
}

func TestRunWorkspacePruneDryRunDoesNotWriteRegistryJSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	registryPath := filepath.Join(tempDir, "workspaces.json")
	registry := workspaceRegistry{
		Version: 1,
		Workspaces: []workspaceRegistryEntry{{
			Root:                filepath.Join(tempDir, "missing"),
			WorkspaceConfigPath: filepath.Join(tempDir, "missing", ".synchub", "workspace.json"),
			ConfigPath:          configPath,
			RemotePath:          "/missing",
			ServerURL:           "http://localhost:8765",
			UserEmail:           "user@example.com",
		}},
	}
	if err := writeJSONFile(registryPath, registry, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"prune",
		"--config", configPath,
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace prune dry run json: %v", err)
	}
	var snapshot workspacePruneSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode workspace prune json: %v\n%s", err, stdout.String())
	}
	if !snapshot.DryRun || len(snapshot.Removed) != 1 || len(snapshot.Kept) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	remaining, err := readWorkspaceRegistryFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(remaining.Workspaces) != 1 {
		t.Fatalf("dry run changed registry: %#v", remaining.Workspaces)
	}
}

func TestRunWorkspaceRemoveByPathRemovesRegisteredEntry(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	firstRoot := filepath.Join(tempDir, "first")
	secondRoot := filepath.Join(tempDir, "second")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
		cfg := workspaceConfig{
			Version:    1,
			Root:       root,
			RemotePath: "/" + filepath.Base(root),
			ServerURL:  "http://localhost:8765",
			UserID:     "u1",
			UserEmail:  "user@example.com",
		}
		if err := writeJSONFile(defaultWorkspaceConfigPath(root), cfg, 0o600); err != nil {
			t.Fatalf("write workspace config: %v", err)
		}
		if err := registerWorkspace(configPath, defaultWorkspaceConfigPath(root), cfg); err != nil {
			t.Fatalf("register workspace: %v", err)
		}
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"remove",
		"--path", firstRoot,
		"--config", configPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace remove: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"removed: 1",
		"kept: 1",
		"workspace: " + firstRoot,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace remove output missing %q:\n%s", want, out)
		}
	}
	registry, _, err := readWorkspaceRegistry(configPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(registry.Workspaces) != 1 || registry.Workspaces[0].Root != secondRoot {
		t.Fatalf("registry = %#v", registry.Workspaces)
	}
	if registry.UpdatedAt.IsZero() {
		t.Fatalf("registry missing updated timestamp: %#v", registry)
	}
}

func TestRunWorkspaceRemoveByConfigDryRunJSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	workspaceConfigPath := filepath.Join(tempDir, "custom-workspace.json")
	cfg := workspaceConfig{
		Version:    1,
		Root:       workspaceRoot,
		RemotePath: "/workspace",
		ServerURL:  "http://localhost:8765",
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}
	if err := writeJSONFile(workspaceConfigPath, cfg, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	if err := registerWorkspace(configPath, workspaceConfigPath, cfg); err != nil {
		t.Fatalf("register workspace: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"workspace",
		"remove",
		"--workspace-config", workspaceConfigPath,
		"--config", configPath,
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("workspace remove dry run json: %v", err)
	}
	var snapshot workspaceRemoveSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode remove json: %v\n%s", err, stdout.String())
	}
	if !snapshot.DryRun || len(snapshot.Removed) != 1 || snapshot.Removed[0].WorkspaceConfigPath != workspaceConfigPath {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	registry, _, err := readWorkspaceRegistry(configPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(registry.Workspaces) != 1 {
		t.Fatalf("dry run changed registry: %#v", registry.Workspaces)
	}
}

func TestRunWorkspaceRemoveRequiresSelector(t *testing.T) {
	err := run(context.Background(), []string{
		"workspace",
		"remove",
		"--config", filepath.Join(t.TempDir(), "config.json"),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "workspace path or workspace config is required") {
		t.Fatalf("error = %v, want selector error", err)
	}
}

func TestRunWorkspaceInitRejectsRemotePathWithMultiplePaths(t *testing.T) {
	tempDir := t.TempDir()
	loginConfigPath := filepath.Join(tempDir, "config.json")
	firstRoot := filepath.Join(tempDir, "alpha")
	secondRoot := filepath.Join(tempDir, "bravo")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
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

	err := run(context.Background(), []string{
		"workspace",
		"init",
		"--path", firstRoot,
		"--path", secondRoot,
		"--remote-path", "/shared",
		"--config", loginConfigPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--remote-path can only be used with one workspace path") {
		t.Fatalf("error = %v, want remote-path multiple path error", err)
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
	if !strings.Contains(stdout.String(), "synchub-cli workspace init --remote-root /workspace C:\\work\\notes D:\\work\\code") {
		t.Fatalf("workspace help missing multiple init command: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "synchub-cli workspace list --json") {
		t.Fatalf("workspace help missing list json command: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "synchub-cli workspace prune --dry-run") {
		t.Fatalf("workspace help missing prune command: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "synchub-cli workspace remove --path . --json") {
		t.Fatalf("workspace help missing remove command: %s", stdout.String())
	}
}
