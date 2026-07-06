package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunSyncDaemonStartsBackgroundByDefault(t *testing.T) {
	original := startSyncDaemonBackground
	defer func() { startSyncDaemonBackground = original }()

	var got []string
	startSyncDaemonBackground = func(args []string, stdout, stderr io.Writer) error {
		_ = stderr
		got = append([]string{}, args...)
		_, _ = stdout.Write([]byte("daemon started in background\n"))
		return nil
	}

	var stdout bytes.Buffer
	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"--config", "login.json",
		"--interval", "30s",
	}, &stdout, &bytes.Buffer{}, func(context.Context, []string, io.Writer, io.Writer) error {
		t.Fatal("runner should not be called when daemon starts in background")
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon background: %v", err)
	}
	want := []string{"--config", "login.json", "--interval", "30s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("background args = %#v, want %#v", got, want)
	}
	if !strings.Contains(stdout.String(), "daemon started in background") {
		t.Fatalf("stdout = %q, want background start", stdout.String())
	}
}

func TestRunSyncDaemonForegroundRunsLoop(t *testing.T) {
	root := t.TempDir()
	var got []string

	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"--foreground",
		"--path", root,
		"--no-watch",
		"--cycles", "1",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_ = ctx
		_ = stdout
		_ = stderr
		got = append([]string{}, args...)
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon foreground: %v", err)
	}
	if testDaemonFlagValue(got, "path") != root {
		t.Fatalf("foreground sync args = %#v, want path %q", got, root)
	}
}

func TestRunSyncDaemonSingleDashHelpDoesNotStartBackground(t *testing.T) {
	original := startSyncDaemonBackground
	defer func() { startSyncDaemonBackground = original }()

	startSyncDaemonBackground = func([]string, io.Writer, io.Writer) error {
		t.Fatal("background daemon should not start for -h")
		return nil
	}

	var stdout bytes.Buffer
	err := runSyncDaemonWithSyncOnce(context.Background(), []string{"-h"}, &stdout, &bytes.Buffer{}, func(context.Context, []string, io.Writer, io.Writer) error {
		t.Fatal("runner should not be called for help")
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli sync daemon") {
		t.Fatalf("stdout = %q, want daemon usage", stdout.String())
	}
}

func TestRunSyncDaemonSingleDashForegroundRunsLoop(t *testing.T) {
	root := t.TempDir()
	var got []string

	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"-foreground",
		"-path", root,
		"-no-watch",
		"-cycles", "1",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_ = ctx
		_ = stdout
		_ = stderr
		got = append([]string{}, args...)
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon foreground: %v", err)
	}
	if testDaemonFlagValue(got, "path") != root {
		t.Fatalf("foreground sync args = %#v, want path %q", got, root)
	}
}

func TestRunSyncDaemonOnceInvokesSyncOnce(t *testing.T) {
	root := t.TempDir()
	var got []string
	var stdout bytes.Buffer

	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"--path", root,
		"--config", "login.json",
		"--workspace-config", "workspace.json",
		"--manifest", "manifest.json",
		"--once",
		"--dry-run",
		"--device-name", "laptop",
		"--platform", "windows",
		"--limit", "25",
	}, &stdout, &bytes.Buffer{}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_ = ctx
		_ = stderr
		got = append([]string{}, args...)
		_, _ = stdout.Write([]byte("synced\n"))
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon once: %v", err)
	}

	want := []string{
		"sync", "once",
		"--path", root,
		"--config", "login.json",
		"--workspace-config", "workspace.json",
		"--manifest", "manifest.json",
		"--device-name", "laptop",
		"--platform", "windows",
		"--limit", "25",
		"--dry-run",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	if !strings.Contains(stdout.String(), "sync completed:") {
		t.Fatalf("stdout = %q, want sync completed", stdout.String())
	}
}

func TestRunSyncDaemonHelpUsesCLICommand(t *testing.T) {
	var stdout bytes.Buffer
	err := runSyncDaemonWithSyncOnce(context.Background(), []string{"--help"}, &stdout, &bytes.Buffer{}, func(context.Context, []string, io.Writer, io.Writer) error {
		t.Fatal("runner should not be called for help")
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli sync daemon --path . --once") {
		t.Fatalf("stdout = %q, want daemon usage", stdout.String())
	}
}

func TestRunSyncDaemonWithoutPathUsesRegisteredWorkspaces(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	workspaceRoots := []string{
		filepath.Join(tempDir, "workspace-a"),
		filepath.Join(tempDir, "workspace-b"),
	}
	for _, root := range workspaceRoots {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
		cfg := workspaceConfig{
			Version:    1,
			Root:       root,
			RemotePath: "/workspace",
			ServerURL:  "http://localhost:8765",
			UserID:     "u1",
			UserEmail:  "user@example.com",
		}
		workspaceConfigPath := defaultWorkspaceConfigPath(root)
		if err := writeJSONFile(workspaceConfigPath, cfg, 0o600); err != nil {
			t.Fatalf("write workspace config: %v", err)
		}
		if err := registerWorkspace(configPath, workspaceConfigPath, cfg); err != nil {
			t.Fatalf("register workspace: %v", err)
		}
	}

	var got [][]string
	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"--config", configPath,
		"--once",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_ = ctx
		_ = stdout
		_ = stderr
		got = append(got, append([]string{}, args...))
		return nil
	})
	if err != nil {
		t.Fatalf("sync daemon registered workspaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sync calls = %d, want 2: %#v", len(got), got)
	}
	for i, root := range workspaceRoots {
		if value := testDaemonFlagValue(got[i], "path"); value != root {
			t.Fatalf("call %d path = %q, want %q; args=%#v", i, value, root, got[i])
		}
		if value := testDaemonFlagValue(got[i], "config"); value != configPath {
			t.Fatalf("call %d config = %q, want %q; args=%#v", i, value, configPath, got[i])
		}
		if value := testDaemonFlagValue(got[i], "workspace-config"); value != defaultWorkspaceConfigPath(root) {
			t.Fatalf("call %d workspace config = %q, want %q; args=%#v", i, value, defaultWorkspaceConfigPath(root), got[i])
		}
	}
}

func TestRunSyncDaemonWithoutPathRequiresRegisteredWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	err := runSyncDaemonWithSyncOnce(context.Background(), []string{
		"--config", configPath,
		"--once",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, []string, io.Writer, io.Writer) error {
		t.Fatal("runner should not be called without registered workspaces")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "no initialized workspaces registered") {
		t.Fatalf("error = %v, want no registered workspaces", err)
	}
}

func testDaemonFlagValue(args []string, name string) string {
	prefix := "--" + name + "="
	flagName := "--" + name
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if arg == flagName && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
