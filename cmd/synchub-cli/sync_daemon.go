package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bruceblink/SyncHub/internal/syncdaemon"
)

var startSyncDaemonBackground = startSyncDaemonBackgroundProcess

func runSyncDaemon(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runSyncDaemonWithSyncOnce(ctx, args, stdout, stderr, func(ctx context.Context, syncArgs []string, stdout, stderr io.Writer) error {
		if len(syncArgs) < 2 || syncArgs[0] != "sync" || syncArgs[1] != "once" {
			return fmt.Errorf("unexpected daemon sync command: %v", syncArgs)
		}
		return runSyncOnce(ctx, syncArgs[2:], stdout, stderr)
	})
}

func runSyncDaemonWithSyncOnce(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncdaemon.SyncOnceArgsRunner) error {
	if shouldStartSyncDaemonInBackground(args) {
		return startSyncDaemonBackground(args, stdout, stderr)
	}
	if shouldRunRegisteredWorkspaceDaemons(args) {
		return runRegisteredWorkspaceDaemons(ctx, args, stdout, stderr, runner)
	}
	return syncdaemon.RunWithSyncOnce(ctx, args, stdout, stderr, runner)
}

func shouldStartSyncDaemonInBackground(args []string) bool {
	if daemonHelpRequested(args) || daemonVersionRequested(args) {
		return false
	}
	if daemonFlagPresent(args, "foreground") ||
		daemonFlagPresent(args, "json") ||
		daemonFlagPresent(args, "dry-run") ||
		daemonFlagPresent(args, "cycles") ||
		daemonActionExits(args) {
		return false
	}
	return true
}

type daemonProcessInfo struct {
	PID       int       `json:"pid"`
	Args      []string  `json:"args"`
	LogPath   string    `json:"log_path"`
	StartedAt time.Time `json:"started_at"`
}

func startSyncDaemonBackgroundProcess(args []string, stdout, stderr io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath, err := daemonLogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	childArgs := append([]string{"sync", "daemon", "--foreground"}, args...)
	cmd := exec.Command(exe, childArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	configureBackgroundProcess(cmd)
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	if err := writeDaemonProcessInfo(daemonProcessInfo{
		PID:       pid,
		Args:      childArgs,
		LogPath:   logPath,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		fmt.Fprintf(stderr, "write daemon process state failed: %v\n", err)
	}
	if err := cmd.Process.Release(); err != nil {
		fmt.Fprintf(stderr, "release daemon process failed: %v\n", err)
	}
	fmt.Fprintln(stdout, "daemon started in background")
	fmt.Fprintf(stdout, "pid: %d\n", pid)
	fmt.Fprintf(stdout, "log: %s\n", logPath)
	fmt.Fprintln(stdout, "status: synchub-cli sync daemon --status")
	return nil
}

func daemonLogPath() (string, error) {
	dir, err := daemonStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.log"), nil
}

func daemonProcessInfoPath() (string, error) {
	dir, err := daemonStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon-process.json"), nil
}

func daemonStateDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return filepath.Join(".synchub"), nil
	}
	return filepath.Join(dir, "SyncHub"), nil
}

func writeDaemonProcessInfo(info daemonProcessInfo) error {
	path, err := daemonProcessInfoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func runRegisteredWorkspaceDaemons(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncdaemon.SyncOnceArgsRunner) error {
	configPath := daemonFlagValue(args, "config", defaultConfigPath())
	entries, err := runnableWorkspaceEntries(configPath, stderr)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("no initialized workspaces registered; run synchub-cli workspace init first")
	}
	if daemonFlagPresent(args, "json") && len(entries) > 1 {
		return errors.New("json output for multiple registered workspaces requires --path")
	}

	hasConfig := daemonFlagPresent(args, "config")
	hasWorkspaceConfig := daemonFlagPresent(args, "workspace-config")
	if daemonActionExits(args) {
		var errs []error
		for _, entry := range entries {
			if err := runSyncDaemonWithSyncOnce(ctx, daemonArgsForWorkspace(args, entry, hasConfig, hasWorkspaceConfig), stdout, stderr, runner); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", entry.Root, err))
			}
		}
		return errors.Join(errs...)
	}

	var wg sync.WaitGroup
	var writeMu sync.Mutex
	safeStdout := lockedWriter{mu: &writeMu, w: stdout}
	safeStderr := lockedWriter{mu: &writeMu, w: stderr}
	errs := make(chan error, len(entries))
	for _, entry := range entries {
		entry := entry
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runSyncDaemonWithSyncOnce(ctx, daemonArgsForWorkspace(args, entry, hasConfig, hasWorkspaceConfig), safeStdout, safeStderr, runner); err != nil {
				fmt.Fprintf(safeStderr, "daemon failed for %s: %v\n", entry.Root, err)
				errs <- fmt.Errorf("%s: %w", entry.Root, err)
			}
		}()
	}
	wg.Wait()
	close(errs)

	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func runnableWorkspaceEntries(configPath string, stderr io.Writer) ([]workspaceRegistryEntry, error) {
	registry, registryPath, err := readWorkspaceRegistry(configPath)
	if err != nil {
		return nil, err
	}
	if len(registry.Workspaces) == 0 {
		return fallbackCurrentWorkspaceEntry(configPath)
	}
	entries := make([]workspaceRegistryEntry, 0, len(registry.Workspaces))
	for _, entry := range registry.Workspaces {
		root, err := resolveWorkspaceRoot(entry.Root)
		if err != nil {
			fmt.Fprintf(stderr, "skip workspace %s from %s: %v\n", entry.Root, registryPath, err)
			continue
		}
		workspaceConfigPath := strings.TrimSpace(entry.WorkspaceConfigPath)
		if workspaceConfigPath == "" {
			workspaceConfigPath = defaultWorkspaceConfigPath(root)
		}
		workspace, err := readWorkspaceConfig(workspaceConfigPath)
		if err != nil {
			fmt.Fprintf(stderr, "skip workspace %s from %s: %v\n", root, registryPath, err)
			continue
		}
		if strings.TrimSpace(workspace.Root) != "" {
			root = workspace.Root
		}
		entry.Root = root
		entry.WorkspaceConfigPath = workspaceConfigPath
		if strings.TrimSpace(entry.ConfigPath) == "" {
			entry.ConfigPath = configPath
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return fallbackCurrentWorkspaceEntry(configPath)
	}
	return entries, nil
}

func fallbackCurrentWorkspaceEntry(configPath string) ([]workspaceRegistryEntry, error) {
	root, err := resolveWorkspaceRoot(".")
	if err != nil {
		return nil, nil
	}
	workspaceConfigPath := defaultWorkspaceConfigPath(root)
	workspace, err := readWorkspaceConfig(workspaceConfigPath)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(workspace.Root) != "" {
		root = workspace.Root
	}
	return []workspaceRegistryEntry{{
		Root:                root,
		WorkspaceConfigPath: workspaceConfigPath,
		ConfigPath:          configPath,
		RemotePath:          workspace.RemotePath,
		ServerURL:           workspace.ServerURL,
		UserID:              workspace.UserID,
		UserEmail:           workspace.UserEmail,
	}}, nil
}

func daemonArgsForWorkspace(args []string, entry workspaceRegistryEntry, hasConfig, hasWorkspaceConfig bool) []string {
	workspaceArgs := append([]string{}, args...)
	workspaceArgs = append(workspaceArgs, "--path", entry.Root)
	if !hasConfig && strings.TrimSpace(entry.ConfigPath) != "" {
		workspaceArgs = append(workspaceArgs, "--config", entry.ConfigPath)
	}
	if !hasWorkspaceConfig && strings.TrimSpace(entry.WorkspaceConfigPath) != "" {
		workspaceArgs = append(workspaceArgs, "--workspace-config", entry.WorkspaceConfigPath)
	}
	return workspaceArgs
}

func shouldRunRegisteredWorkspaceDaemons(args []string) bool {
	if daemonHelpRequested(args) || daemonVersionRequested(args) {
		return false
	}
	return !daemonFlagPresent(args, "path")
}

func daemonActionExits(args []string) bool {
	for _, name := range []string{"once", "status", "pause", "resume", "reset-state"} {
		if daemonFlagPresent(args, name) {
			return true
		}
	}
	return false
}

func daemonFlagPresent(args []string, name string) bool {
	prefix := "--" + name + "="
	shortPrefix := "-" + name + "="
	flagName := "--" + name
	shortFlagName := "-" + name
	for _, arg := range args {
		if arg == flagName || arg == shortFlagName || strings.HasPrefix(arg, prefix) || strings.HasPrefix(arg, shortPrefix) {
			return true
		}
	}
	return false
}

func daemonFlagValue(args []string, name, fallback string) string {
	prefix := "--" + name + "="
	shortPrefix := "-" + name + "="
	flagName := "--" + name
	shortFlagName := "-" + name
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if strings.HasPrefix(arg, shortPrefix) {
			return strings.TrimPrefix(arg, shortPrefix)
		}
		if (arg == flagName || arg == shortFlagName) && i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
}

func daemonHelpRequested(args []string) bool {
	for i, arg := range args {
		switch arg {
		case "-h", "--h", "-help", "--help":
			return true
		case "help":
			return i == 0
		}
	}
	return false
}

func daemonVersionRequested(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-version", "--version":
			return true
		}
	}
	return false
}
