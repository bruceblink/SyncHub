package syncdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	NoWatch             bool
	Foreground          bool
	Status              bool
	JSON                bool
	Pause               bool
	Resume              bool
	ResetState          bool
}

type syncOnceRunner func(context.Context, agentOptions, io.Writer, io.Writer) error

type SyncOnceArgsRunner func(context.Context, []string, io.Writer, io.Writer) error

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

type agentControl struct {
	Version   int       `json:"version"`
	Paused    bool      `json:"paused"`
	UpdatedAt time.Time `json:"updated_at"`
}

type agentStatusSnapshot struct {
	Workspace agentStatusWorkspace `json:"workspace"`
	State     *agentState          `json:"state,omitempty"`
	Control   *agentControl        `json:"control,omitempty"`
	HasRun    bool                 `json:"has_run"`
	Paused    bool                 `json:"paused"`
}

type agentStatusWorkspace struct {
	Root string `json:"root"`
}

type agentControlSnapshot struct {
	Action    string               `json:"action"`
	Workspace agentStatusWorkspace `json:"workspace"`
	State     agentState           `json:"state"`
	Control   *agentControl        `json:"control,omitempty"`
}

type agentResetSnapshot struct {
	Action    string               `json:"action"`
	Workspace agentStatusWorkspace `json:"workspace"`
	Removed   []string             `json:"removed"`
}

type agentCycleSnapshot struct {
	Action    string               `json:"action"`
	Workspace agentStatusWorkspace `json:"workspace"`
	Skipped   bool                 `json:"skipped"`
	Reason    string               `json:"reason,omitempty"`
	State     agentState           `json:"state"`
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return run(ctx, args, stdout, stderr, runSyncOnceCommand)
}

func RunWithSyncOnce(ctx context.Context, args []string, stdout, stderr io.Writer, runner SyncOnceArgsRunner) error {
	if runner == nil {
		return errors.New("sync once runner is required")
	}
	return run(ctx, args, stdout, stderr, func(ctx context.Context, opts agentOptions, stdout, stderr io.Writer) error {
		return runner(ctx, buildSyncOnceArgs(opts), stdout, stderr)
	})
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncOnceRunner) error {
	opts, err := parseOptions(args, stdout, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if opts.Pause {
		return pauseAgent(opts, stdout, opts.JSON)
	}
	if opts.Resume {
		return resumeAgent(opts, stdout, opts.JSON)
	}
	if opts.ResetState {
		return resetAgentState(opts, stdout, opts.JSON)
	}
	if opts.Status {
		return runStatus(opts, stdout, opts.JSON)
	}
	if runner == nil {
		return errors.New("sync runner is required")
	}
	if opts.Once {
		return runOnce(ctx, opts, stdout, stderr, runner)
	}

	fmt.Fprintf(stdout, "daemon started: %s\n", opts.RootPath)
	fmt.Fprintf(stdout, "sync interval: %s\n", opts.Interval)
	if opts.Watch {
		return runWatchLoop(ctx, opts, stdout, stderr, runner)
	}
	return runPeriodicLoop(ctx, opts, stdout, stderr, runner)
}

func runOnce(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	state := syncLoopState{}
	_, err := state.runCycle(ctx, opts, stdout, stderr, runner)
	if err != nil {
		return err
	}
	return state.lastErr
}

func runStatus(opts agentOptions, stdout io.Writer, jsonOutput bool) error {
	root, err := resolveAgentRoot(opts.RootPath)
	if err != nil {
		return err
	}
	control, hasControl, err := loadAgentControl(root)
	if err != nil {
		return err
	}
	var controlPtr *agentControl
	if hasControl {
		controlPtr = &control
	}
	paused := hasControl && control.Paused

	state, err := loadAgentState(opts.RootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if jsonOutput {
				return writeAgentStatusJSON(stdout, agentStatusSnapshot{
					Workspace: agentStatusWorkspace{Root: root},
					Control:   controlPtr,
					HasRun:    false,
					Paused:    paused,
				})
			}
			fmt.Fprintf(stdout, "daemon: not run\n")
			fmt.Fprintf(stdout, "workspace: %s\n", root)
			fmt.Fprintf(stdout, "paused: %t\n", paused)
			return nil
		}
		return err
	}
	if jsonOutput {
		return writeAgentStatusJSON(stdout, agentStatusSnapshot{
			Workspace: agentStatusWorkspace{Root: state.Root},
			State:     &state,
			Control:   controlPtr,
			HasRun:    true,
			Paused:    paused,
		})
	}
	fmt.Fprintf(stdout, "daemon: %s\n", state.Status)
	fmt.Fprintf(stdout, "workspace: %s\n", state.Root)
	fmt.Fprintf(stdout, "paused: %t\n", paused)
	fmt.Fprintf(stdout, "cycles: %d\n", state.CyclesRun)
	fmt.Fprintf(stdout, "consecutive failures: %d\n", state.ConsecutiveFailures)
	fmt.Fprintf(stdout, "last success: %s\n", formatAgentTime(state.LastSuccessAt))
	fmt.Fprintf(stdout, "last failure: %s\n", formatAgentTime(state.LastFailureAt))
	if strings.TrimSpace(state.LastError) != "" {
		fmt.Fprintf(stdout, "last error: %s\n", state.LastError)
	}
	fmt.Fprintf(stdout, "updated: %s\n", state.UpdatedAt.UTC().Format(time.RFC3339))
	return nil
}

func writeAgentStatusJSON(stdout io.Writer, snapshot agentStatusSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func pauseAgent(opts agentOptions, stdout io.Writer, jsonOutput bool) error {
	root, err := resolveAgentRoot(opts.RootPath)
	if err != nil {
		return err
	}
	control := agentControl{Version: 1, Paused: true, UpdatedAt: agentNow().UTC()}
	if err := writeAgentControl(root, control); err != nil {
		return err
	}
	previous, ok, err := loadOptionalAgentState(root)
	if err != nil {
		return err
	}
	var previousPtr *agentState
	if ok {
		previousPtr = &previous
	}
	state := agentPausedState(agentOptions{RootPath: root}, cyclesFromState(previousPtr), previousPtr)
	if err := writeAgentState(state); err != nil {
		return err
	}
	if jsonOutput {
		return writeAgentControlJSON(stdout, agentControlSnapshot{
			Action:    "paused",
			Workspace: agentStatusWorkspace{Root: root},
			State:     state,
			Control:   &control,
		})
	}
	fmt.Fprintf(stdout, "daemon paused: %s\n", root)
	return nil
}

func resumeAgent(opts agentOptions, stdout io.Writer, jsonOutput bool) error {
	root, err := resolveAgentRoot(opts.RootPath)
	if err != nil {
		return err
	}
	controlPath, err := agentControlPath(root)
	if err != nil {
		return err
	}
	if err := os.Remove(controlPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	previous, ok, err := loadOptionalAgentState(root)
	if err != nil {
		return err
	}
	var previousPtr *agentState
	if ok {
		previousPtr = &previous
	}
	state := agentIdleState(agentOptions{RootPath: root}, previousPtr)
	if err := writeAgentState(state); err != nil {
		return err
	}
	if jsonOutput {
		return writeAgentControlJSON(stdout, agentControlSnapshot{
			Action:    "resumed",
			Workspace: agentStatusWorkspace{Root: root},
			State:     state,
		})
	}
	fmt.Fprintf(stdout, "daemon resumed: %s\n", root)
	return nil
}

func writeAgentControlJSON(stdout io.Writer, snapshot agentControlSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func resetAgentState(opts agentOptions, stdout io.Writer, jsonOutput bool) error {
	root, err := resolveAgentRoot(opts.RootPath)
	if err != nil {
		return err
	}
	removed := make([]string, 0, 2)
	for _, pathFn := range []func(string) (string, error){agentStatePath, agentControlPath} {
		path, err := pathFn(root)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		removed = append(removed, filepath.Base(path))
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(agentResetSnapshot{
			Action:    "reset_state",
			Workspace: agentStatusWorkspace{Root: root},
			Removed:   removed,
		})
	}
	fmt.Fprintf(stdout, "daemon state reset: %s\n", root)
	if len(removed) == 0 {
		fmt.Fprintln(stdout, "removed: none")
		return nil
	}
	fmt.Fprintf(stdout, "removed: %s\n", strings.Join(removed, ", "))
	return nil
}

type syncLoopState struct {
	consecutiveFailures int
	cyclesRun           int
	lastErr             error
	lastSkipped         bool
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
			if state.lastErr == nil && !state.lastSkipped {
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
			if state.lastErr == nil && !state.lastSkipped {
				if _, err := poller.Poll(ctx); err != nil {
					return err
				}
			}
		}
	}
}

func (s *syncLoopState) runCycle(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) (bool, error) {
	paused, err := agentIsPaused(opts.RootPath)
	if err != nil {
		return false, err
	}
	if paused {
		s.lastErr = nil
		s.lastSkipped = true
		s.consecutiveFailures = 0
		previous, ok, err := loadOptionalAgentState(opts.RootPath)
		if err != nil {
			return false, err
		}
		var previousPtr *agentState
		if ok {
			previousPtr = &previous
		}
		state := agentPausedState(opts, s.cyclesRun+1, previousPtr)
		if stateErr := writeAgentState(state); stateErr != nil {
			fmt.Fprintf(stderr, "write daemon state failed: %v\n", stateErr)
		}
		if opts.JSON {
			if err := writeAgentCycleJSON(stdout, agentCycleSnapshot{
				Action:    "skipped",
				Workspace: agentStatusWorkspace{Root: agentSnapshotRoot(opts.RootPath)},
				Skipped:   true,
				Reason:    "daemon is paused",
				State:     state,
			}); err != nil {
				return false, err
			}
			s.cyclesRun++
			return shouldStopAfterCycles(opts, s.cyclesRun), nil
		}
		fmt.Fprintln(stdout, "sync skipped: daemon is paused")
		s.cyclesRun++
		if shouldStopAfterCycles(opts, s.cyclesRun) {
			fmt.Fprintf(stdout, "daemon stopped: sync cycles reached %d\n", s.cyclesRun)
			return true, nil
		}
		return false, nil
	}
	if err := runSyncCycle(ctx, opts, stdout, stderr, runner); err != nil {
		s.lastErr = err
		s.lastSkipped = false
		s.consecutiveFailures++
		if stateErr := writeAgentState(agentFailureState(opts, s.consecutiveFailures, s.cyclesRun+1, err)); stateErr != nil {
			fmt.Fprintf(stderr, "write daemon state failed: %v\n", stateErr)
		}
		if shouldStopAfterFailures(opts, s.consecutiveFailures) {
			return true, maxFailuresError(s.consecutiveFailures, err)
		}
	} else {
		s.lastErr = nil
		s.lastSkipped = false
		s.consecutiveFailures = 0
		if err := writeAgentState(agentSuccessState(opts, s.cyclesRun+1)); err != nil {
			fmt.Fprintf(stderr, "write daemon state failed: %v\n", err)
		}
	}
	s.cyclesRun++
	if shouldStopAfterCycles(opts, s.cyclesRun) {
		fmt.Fprintf(stdout, "daemon stopped: sync cycles reached %d\n", s.cyclesRun)
		return true, s.lastErr
	}
	return false, nil
}

func writeAgentCycleJSON(stdout io.Writer, snapshot agentCycleSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func agentSnapshotRoot(root string) string {
	resolved, err := resolveAgentRoot(root)
	if err != nil {
		return root
	}
	return resolved
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

func agentPausedState(opts agentOptions, cyclesRun int, previous *agentState) agentState {
	now := agentNow().UTC()
	state := agentState{
		Version:             1,
		Root:                opts.RootPath,
		Status:              "paused",
		CyclesRun:           cyclesRun,
		ConsecutiveFailures: 0,
		UpdatedAt:           now,
	}
	if previous != nil {
		state.LastSuccessAt = previous.LastSuccessAt
		state.LastFailureAt = previous.LastFailureAt
		state.LastError = previous.LastError
	}
	return state
}

func agentIdleState(opts agentOptions, previous *agentState) agentState {
	now := agentNow().UTC()
	state := agentState{
		Version:             1,
		Root:                opts.RootPath,
		Status:              "idle",
		ConsecutiveFailures: 0,
		UpdatedAt:           now,
	}
	if previous != nil {
		state.CyclesRun = previous.CyclesRun
		state.LastSuccessAt = previous.LastSuccessAt
		state.LastFailureAt = previous.LastFailureAt
		state.LastError = previous.LastError
	}
	return state
}

func cyclesFromState(state *agentState) int {
	if state == nil {
		return 0
	}
	return state.CyclesRun
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

func loadAgentState(root string) (agentState, error) {
	statePath, err := agentStatePath(root)
	if err != nil {
		return agentState{}, err
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return agentState{}, err
	}
	var state agentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return agentState{}, err
	}
	if strings.TrimSpace(state.Root) == "" {
		state.Root = filepath.Dir(filepath.Dir(statePath))
	}
	return state, nil
}

func loadOptionalAgentState(root string) (agentState, bool, error) {
	state, err := loadAgentState(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentState{}, false, nil
		}
		return agentState{}, false, err
	}
	return state, true, nil
}

func agentStatePath(root string) (string, error) {
	root, err := resolveAgentRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".synchub", "daemon-state.json"), nil
}

func writeAgentControl(root string, control agentControl) error {
	controlPath, err := agentControlPath(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(controlPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(control, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(controlPath, raw, 0o600)
}

func loadAgentControl(root string) (agentControl, bool, error) {
	controlPath, err := agentControlPath(root)
	if err != nil {
		return agentControl{}, false, err
	}
	raw, err := os.ReadFile(controlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentControl{}, false, nil
		}
		return agentControl{}, false, err
	}
	var control agentControl
	if err := json.Unmarshal(raw, &control); err != nil {
		return agentControl{}, false, fmt.Errorf("read daemon control: %w", err)
	}
	return control, true, nil
}

func agentIsPaused(root string) (bool, error) {
	control, ok, err := loadAgentControl(root)
	if err != nil {
		return false, err
	}
	return ok && control.Paused, nil
}

func agentControlPath(root string) (string, error) {
	root, err := resolveAgentRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".synchub", "daemon-control.json"), nil
}

func runSyncCycle(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) error {
	if err := runner(ctx, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "sync failed: %v\n", err)
		return err
	}
	if opts.JSON {
		return nil
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
	fs := flag.NewFlagSet("sync daemon", flag.ContinueOnError)
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
	fs.BoolVar(&opts.Watch, "watch", false, "trigger sync when local workspace changes are detected; kept for compatibility because daemon loops watch by default")
	fs.BoolVar(&opts.NoWatch, "no-watch", false, "disable local workspace watching and sync only on the interval")
	fs.BoolVar(&opts.Foreground, "foreground", false, "run the daemon loop in the current terminal instead of starting a background process")
	fs.BoolVar(&opts.Status, "status", false, "print the last daemon state and exit")
	fs.BoolVar(&opts.JSON, "json", false, "print one-shot, status, pause, resume, or reset-state output as JSON")
	fs.BoolVar(&opts.Pause, "pause", false, "pause sync cycles for this workspace and exit")
	fs.BoolVar(&opts.Resume, "resume", false, "resume sync cycles for this workspace and exit")
	fs.BoolVar(&opts.ResetState, "reset-state", false, "delete daemon state and pause control files for this workspace and exit")
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
	if opts.Status && opts.Cycles > 0 {
		return agentOptions{}, errors.New("cycles cannot be used with --status")
	}
	if opts.Watch && opts.Once {
		return agentOptions{}, errors.New("watch cannot be used with --once")
	}
	if opts.Watch && opts.NoWatch {
		return agentOptions{}, errors.New("watch cannot be used with --no-watch")
	}
	if opts.Status && opts.Once {
		return agentOptions{}, errors.New("status cannot be used with --once")
	}
	if opts.Status && opts.Watch {
		return agentOptions{}, errors.New("status cannot be used with --watch")
	}
	if opts.JSON && !opts.Once && !opts.Status && !opts.Pause && !opts.Resume && !opts.ResetState {
		return agentOptions{}, errors.New("json output requires --once, --status, --pause, --resume, or --reset-state")
	}
	if opts.Pause && opts.Resume {
		return agentOptions{}, errors.New("pause cannot be used with --resume")
	}
	if opts.ResetState && (opts.Pause || opts.Resume || opts.Status) {
		return agentOptions{}, errors.New("reset state cannot be used with --pause, --resume, or --status")
	}
	if opts.Pause && opts.Status {
		return agentOptions{}, errors.New("pause cannot be used with --status")
	}
	if opts.Resume && opts.Status {
		return agentOptions{}, errors.New("resume cannot be used with --status")
	}
	if (opts.Pause || opts.Resume || opts.ResetState) && opts.Once {
		return agentOptions{}, errors.New("pause, resume, and reset state cannot be used with --once")
	}
	if (opts.Pause || opts.Resume || opts.ResetState) && opts.Watch {
		return agentOptions{}, errors.New("pause, resume, and reset state cannot be used with --watch")
	}
	if (opts.Pause || opts.Resume || opts.ResetState) && opts.Cycles > 0 {
		return agentOptions{}, errors.New("pause, resume, and reset state cannot be used with --cycles")
	}
	if opts.DryRun && !opts.Once {
		return agentOptions{}, errors.New("dry run requires --once")
	}
	if strings.TrimSpace(opts.RootPath) == "" {
		return agentOptions{}, errors.New("workspace path is required")
	}
	if daemonLoopMode(opts) && !opts.Watch && !opts.NoWatch {
		opts.Watch = true
	}
	if opts.Watch && opts.WatchInterval <= 0 {
		return agentOptions{}, errors.New("watch interval must be positive")
	}
	return opts, nil
}

func daemonLoopMode(opts agentOptions) bool {
	return !opts.Once && !opts.Status && !opts.Pause && !opts.Resume && !opts.ResetState
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli sync daemon --version")
	fmt.Fprintln(w, "  synchub-cli sync daemon")
	fmt.Fprintln(w, "  synchub-cli sync daemon --foreground")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path .")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --once")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --once --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --once --dry-run")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --once --dry-run --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --status")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --status --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --pause")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --pause --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --resume")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --resume --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --reset-state")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --reset-state --json")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --watch-interval 1s")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --no-watch --interval 30s --device-name laptop --platform windows --limit 500")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --cycles 3")
	fmt.Fprintln(w, "  synchub-cli sync daemon --path . --max-failures 5")
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", version.Name, version.Version)
}

func formatAgentTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
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
	if opts.JSON {
		args = append(args, "--json")
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
