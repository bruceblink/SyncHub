package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bruceblink/SyncHub/internal/version"
	"github.com/bruceblink/SyncHub/internal/watch"
)

const defaultAgentInterval = 30 * time.Second
const defaultWatchInterval = time.Second

var agentNow = time.Now
var agentWatchReady = func() {}

type agentOptions struct {
	RootPath            string
	ConfigPath          string
	WorkspaceConfigPath string
	ManifestPath        string
	CLIPath             string
	Interval            time.Duration
	WatchInterval       time.Duration
	DeviceName          string
	DevicePlatform      string
	Limit               int
	MaxFailures         int
	Cycles              int
	Once                bool
	DryRun              bool
	Watch               bool
}

type syncOnceRunner func(context.Context, agentOptions, io.Writer, io.Writer) error

type agentWorkspaceConfig struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
}

type agentState struct {
	Version             int        `json:"version"`
	Root                string     `json:"root"`
	Status              string     `json:"status"`
	CyclesRun           int        `json:"cycles_run"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, runSyncOnceCommand); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncOnceRunner) error {
	opts, err := parseOptions(args, stdout, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if runner == nil {
		return errors.New("sync runner is required")
	}
	if opts.Once {
		return runOnce(ctx, opts, stdout, stderr, runner)
	}

	fmt.Fprintf(stdout, "agent started: %s\n", opts.RootPath)
	fmt.Fprintf(stdout, "sync interval: %s\n", opts.Interval)
	if opts.Watch {
		return runWatchLoop(ctx, opts, stdout, stderr, runner)
	}
	return runPeriodicLoop(ctx, opts, stdout, stderr, runner)
}

func runOnce(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	err := runner(ctx, opts, stdout, stderr)
	if err != nil {
		if stateErr := writeAgentState(agentFailureState(opts, 1, 1, err)); stateErr != nil {
			fmt.Fprintf(stderr, "write agent state failed: %v\n", stateErr)
		}
		return err
	}
	if stateErr := writeAgentState(agentSuccessState(opts, 1)); stateErr != nil {
		fmt.Fprintf(stderr, "write agent state failed: %v\n", stateErr)
	}
	return nil
}

type syncLoopState struct {
	consecutiveFailures int
	cyclesRun           int
	lastErr             error
}

func runPeriodicLoop(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	state := syncLoopState{}
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		if stop, err := state.runCycle(ctx, opts, stdout, stderr, runner); stop {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runWatchLoop(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	root, remotePath, err := loadAgentWatchTarget(opts.RootPath, opts.WorkspaceConfigPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "watch interval: %s\n", opts.WatchInterval)

	state := syncLoopState{}
	if stop, err := state.runCycle(ctx, opts, stdout, stderr, runner); stop {
		return err
	}
	poller, err := watch.NewPoller(ctx, root, remotePath)
	if err != nil {
		return err
	}
	agentWatchReady()

	syncTicker := time.NewTicker(opts.Interval)
	defer syncTicker.Stop()
	watchTicker := time.NewTicker(opts.WatchInterval)
	defer watchTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-syncTicker.C:
			if stop, err := state.runCycle(ctx, opts, stdout, stderr, runner); stop {
				return err
			}
			if state.lastErr == nil {
				if _, err := poller.Poll(ctx); err != nil {
					return err
				}
			}
		case <-watchTicker.C:
			changes, err := poller.Poll(ctx)
			if err != nil {
				return err
			}
			if len(changes) == 0 {
				continue
			}
			fmt.Fprintf(stdout, "local changes detected: %d\n", len(changes))
			if stop, err := state.runCycle(ctx, opts, stdout, stderr, runner); stop {
				return err
			}
			if state.lastErr == nil {
				if _, err := poller.Poll(ctx); err != nil {
					return err
				}
			}
		}
	}
}

func (s *syncLoopState) runCycle(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) (bool, error) {
	if err := runSyncCycle(ctx, opts, stdout, stderr, runner); err != nil {
		s.lastErr = err
		s.consecutiveFailures++
		if stateErr := writeAgentState(agentFailureState(opts, s.consecutiveFailures, s.cyclesRun+1, err)); stateErr != nil {
			fmt.Fprintf(stderr, "write agent state failed: %v\n", stateErr)
		}
		if shouldStopAfterFailures(opts, s.consecutiveFailures) {
			return true, maxFailuresError(s.consecutiveFailures, err)
		}
	} else {
		s.lastErr = nil
		s.consecutiveFailures = 0
		if err := writeAgentState(agentSuccessState(opts, s.cyclesRun+1)); err != nil {
			fmt.Fprintf(stderr, "write agent state failed: %v\n", err)
		}
	}
	s.cyclesRun++
	if shouldStopAfterCycles(opts, s.cyclesRun) {
		fmt.Fprintf(stdout, "agent stopped: sync cycles reached %d\n", s.cyclesRun)
		return true, s.lastErr
	}
	return false, nil
}

func agentSuccessState(opts agentOptions, cyclesRun int) agentState {
	now := agentNow().UTC()
	return agentState{
		Version:             1,
		Root:                opts.RootPath,
		Status:              "ok",
		CyclesRun:           cyclesRun,
		ConsecutiveFailures: 0,
		LastSuccessAt:       &now,
		UpdatedAt:           now,
	}
}

func agentFailureState(opts agentOptions, consecutiveFailures, cyclesRun int, err error) agentState {
	now := agentNow().UTC()
	return agentState{
		Version:             1,
		Root:                opts.RootPath,
		Status:              "error",
		CyclesRun:           cyclesRun,
		ConsecutiveFailures: consecutiveFailures,
		LastFailureAt:       &now,
		LastError:           err.Error(),
		UpdatedAt:           now,
	}
}

func writeAgentState(state agentState) error {
	statePath, err := agentStatePath(state.Root)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(statePath))
	state.Root = root
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(statePath, raw, 0o600)
}

func agentStatePath(root string) (string, error) {
	root, err := resolveAgentRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".synchub", "agent-state.json"), nil
}

func runSyncCycle(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	if err := runner(ctx, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "sync failed: %v\n", err)
		return err
	}
	fmt.Fprintf(stdout, "sync completed: %s\n", agentNow().UTC().Format(time.RFC3339))
	return nil
}

func shouldStopAfterFailures(opts agentOptions, consecutiveFailures int) bool {
	return opts.MaxFailures > 0 && consecutiveFailures >= opts.MaxFailures
}

func shouldStopAfterCycles(opts agentOptions, cyclesRun int) bool {
	return opts.Cycles > 0 && cyclesRun >= opts.Cycles
}

func maxFailuresError(consecutiveFailures int, err error) error {
	return fmt.Errorf("sync failed %d consecutive times; max failures reached: %w", consecutiveFailures, err)
}

func parseOptions(args []string, stdout, stderr io.Writer) (agentOptions, error) {
	fs := flag.NewFlagSet("synchub-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}
	opts := agentOptions{}
	fs.StringVar(&opts.RootPath, "path", ".", "local workspace root")
	fs.StringVar(&opts.ConfigPath, "config", defaultConfigPath(), "login config file path")
	fs.StringVar(&opts.WorkspaceConfigPath, "workspace-config", "", "workspace config file path")
	fs.StringVar(&opts.ManifestPath, "manifest", "", "manifest file path")
	fs.StringVar(&opts.CLIPath, "cli", "", "synchub-cli executable path")
	fs.DurationVar(&opts.Interval, "interval", defaultAgentInterval, "sync interval")
	fs.DurationVar(&opts.WatchInterval, "watch-interval", defaultWatchInterval, "local workspace polling interval when --watch is enabled")
	fs.StringVar(&opts.DeviceName, "device-name", "", "device name")
	fs.StringVar(&opts.DevicePlatform, "platform", "", "device platform")
	fs.IntVar(&opts.Limit, "limit", 500, "maximum changes to pull per sync cycle")
	fs.IntVar(&opts.MaxFailures, "max-failures", 0, "maximum consecutive sync failures before exit; 0 disables")
	fs.IntVar(&opts.Cycles, "cycles", 0, "number of sync cycles to run before exit; 0 runs until interrupted")
	fs.BoolVar(&opts.Once, "once", false, "run one sync cycle and exit")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "preview one sync cycle without uploading, downloading, or updating local state; requires --once")
	fs.BoolVar(&opts.Watch, "watch", false, "trigger sync when local workspace changes are detected")
	if len(args) > 0 {
		switch args[0] {
		case "--version":
			printVersion(stdout)
			return agentOptions{}, flag.ErrHelp
		case "help", "-h", "--help":
			printUsage(stdout)
			return agentOptions{}, flag.ErrHelp
		}
	}
	if err := fs.Parse(args); err != nil {
		return agentOptions{}, err
	}
	if opts.Interval <= 0 {
		return agentOptions{}, errors.New("sync interval must be positive")
	}
	if opts.Limit <= 0 {
		return agentOptions{}, errors.New("limit must be positive")
	}
	if opts.MaxFailures < 0 {
		return agentOptions{}, errors.New("max failures must be non-negative")
	}
	if opts.Cycles < 0 {
		return agentOptions{}, errors.New("cycles must be non-negative")
	}
	if opts.Once && opts.Cycles > 0 {
		return agentOptions{}, errors.New("cycles cannot be used with --once")
	}
	if opts.Watch && opts.Once {
		return agentOptions{}, errors.New("watch cannot be used with --once")
	}
	if opts.DryRun && !opts.Once {
		return agentOptions{}, errors.New("dry run requires --once")
	}
	if opts.Watch && opts.WatchInterval <= 0 {
		return agentOptions{}, errors.New("watch interval must be positive")
	}
	if strings.TrimSpace(opts.RootPath) == "" {
		return agentOptions{}, errors.New("workspace path is required")
	}
	return opts, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-agent --version")
	fmt.Fprintln(w, "  synchub-agent --path .")
	fmt.Fprintln(w, "  synchub-agent --path . --once")
	fmt.Fprintln(w, "  synchub-agent --path . --once --dry-run")
	fmt.Fprintln(w, "  synchub-agent --path . --interval 30s --device-name laptop --platform windows --limit 500")
	fmt.Fprintln(w, "  synchub-agent --path . --watch --watch-interval 1s")
	fmt.Fprintln(w, "  synchub-agent --path . --cycles 3")
	fmt.Fprintln(w, "  synchub-agent --path . --max-failures 5")
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", version.Name, version.Version)
}

func runSyncOnceCommand(ctx context.Context, opts agentOptions, stdout, stderr io.Writer) error {
	name, baseArgs := syncCommand(opts.CLIPath)
	args := append(baseArgs, buildSyncOnceArgs(opts)...)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func syncCommand(cliPath string) (string, []string) {
	if cliPath = strings.TrimSpace(cliPath); cliPath != "" {
		return cliPath, nil
	}
	if cliPath = strings.TrimSpace(os.Getenv("SYNCHUB_CLI")); cliPath != "" {
		return cliPath, nil
	}
	if path, err := exec.LookPath("synchub-cli"); err == nil {
		return path, nil
	}
	if _, err := os.Stat(filepath.Join("cmd", "synchub-cli")); err == nil {
		return "go", []string{"run", "./cmd/synchub-cli"}
	}
	return "synchub-cli", nil
}

func buildSyncOnceArgs(opts agentOptions) []string {
	args := []string{
		"sync",
		"once",
		"--path",
		opts.RootPath,
		"--config",
		opts.ConfigPath,
	}
	if strings.TrimSpace(opts.WorkspaceConfigPath) != "" {
		args = append(args, "--workspace-config", opts.WorkspaceConfigPath)
	}
	if strings.TrimSpace(opts.ManifestPath) != "" {
		args = append(args, "--manifest", opts.ManifestPath)
	}
	if strings.TrimSpace(opts.DeviceName) != "" {
		args = append(args, "--device-name", opts.DeviceName)
	}
	if strings.TrimSpace(opts.DevicePlatform) != "" {
		args = append(args, "--platform", opts.DevicePlatform)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", opts.Limit))
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	return args
}

func loadAgentWatchTarget(rootPath, workspaceConfigPath string) (string, string, error) {
	root, err := resolveAgentRoot(rootPath)
	if err != nil {
		return "", "", err
	}
	configPath := strings.TrimSpace(workspaceConfigPath)
	if configPath == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", errors.New("workspace is not initialized; run synchub-cli workspace init first")
		}
		return "", "", err
	}
	var cfg agentWorkspaceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(cfg.Root) != "" {
		root = filepath.Clean(cfg.Root)
	}
	if strings.TrimSpace(cfg.RemotePath) == "" {
		return "", "", errors.New("workspace config is incomplete; run synchub-cli workspace init again")
	}
	return root, cfg.RemotePath, nil
}

func resolveAgentRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("workspace path is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace path is not a directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

func defaultConfigPath() string {
	if v := os.Getenv("SYNCHUB_CONFIG"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".synchub", "config.json")
	}
	return filepath.Join(dir, "SyncHub", "config.json")
}
