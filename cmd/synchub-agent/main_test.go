package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/version"
)

func TestRunOnceInvokesSyncOnce(t *testing.T) {
	root := t.TempDir()
	var got agentOptions
	runner := func(ctx context.Context, opts agentOptions, stdout, stderr io.Writer) error {
		_ = ctx
		_ = stderr
		got = opts
		_, _ = stdout.Write([]byte("synced\n"))
		return nil
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"--path", root,
		"--config", "login.json",
		"--workspace-config", "workspace.json",
		"--manifest", "manifest.json",
		"--cli", "synchub-cli-test",
		"--interval", "5s",
		"--device-name", "laptop",
		"--platform", "windows",
		"--limit", "20",
		"--max-failures", "3",
		"--once",
		"--dry-run",
	}, &stdout, &bytes.Buffer{}, runner)
	if err != nil {
		t.Fatalf("run agent once: %v", err)
	}
	if !strings.Contains(stdout.String(), "synced\nsync completed:") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !got.Once || !got.DryRun || got.RootPath != root || got.ConfigPath != "login.json" || got.WorkspaceConfigPath != "workspace.json" || got.ManifestPath != "manifest.json" || got.CLIPath != "synchub-cli-test" || got.DeviceName != "laptop" || got.DevicePlatform != "windows" {
		t.Fatalf("options = %#v", got)
	}
	if got.Interval != 5*time.Second {
		t.Fatalf("interval = %s, want 5s", got.Interval)
	}
	if got.WatchInterval != defaultWatchInterval {
		t.Fatalf("watch interval = %s, want default %s", got.WatchInterval, defaultWatchInterval)
	}
	if got.Limit != 20 {
		t.Fatalf("limit = %d, want 20", got.Limit)
	}
	if got.MaxFailures != 3 {
		t.Fatalf("max failures = %d, want 3", got.MaxFailures)
	}
}

func TestRunOnceWritesAgentState(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	err := run(context.Background(), []string{
		"--path", root,
		"--once",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		return nil
	})
	if err != nil {
		t.Fatalf("run agent once: %v", err)
	}

	state := readAgentState(t, root)
	if state.Root != root || state.Status != "ok" || state.CyclesRun != 1 || state.ConsecutiveFailures != 0 || state.LastSuccessAt == nil || state.LastError != "" {
		t.Fatalf("agent state = %#v", state)
	}
	if got := state.LastSuccessAt.UTC().Format(time.RFC3339); got != "2026-07-04T01:02:03Z" {
		t.Fatalf("last success = %s", got)
	}
}

func TestRunOnceWritesFailureAgentState(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	wantErr := errors.New("sync failed")
	err := run(context.Background(), []string{
		"--path", root,
		"--once",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}

	state := readAgentState(t, root)
	if state.Root != root || state.Status != "error" || state.CyclesRun != 1 || state.ConsecutiveFailures != 1 || state.LastFailureAt == nil || state.LastError != "sync failed" {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunStatusShowsAgentState(t *testing.T) {
	root := t.TempDir()
	if err := writeAgentState(agentState{
		Version:             1,
		Root:                root,
		Status:              "error",
		CyclesRun:           3,
		ConsecutiveFailures: 2,
		LastSuccessAt:       testAgentTimePtr(time.Date(2026, 7, 4, 1, 1, 1, 0, time.UTC)),
		LastFailureAt:       testAgentTimePtr(time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC)),
		LastError:           "sync failed",
		UpdatedAt:           time.Date(2026, 7, 4, 1, 2, 4, 0, time.UTC),
	}); err != nil {
		t.Fatalf("write agent state: %v", err)
	}

	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--path", root, "--status"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run agent status: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for status")
	}
	out := stdout.String()
	for _, want := range []string{
		"agent: error",
		"workspace: " + root,
		"cycles: 3",
		"consecutive failures: 2",
		"last success: 2026-07-04T01:01:01Z",
		"last failure: 2026-07-04T01:02:03Z",
		"last error: sync failed",
		"updated: 2026-07-04T01:02:04Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunStatusShowsNotRunWithoutState(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--path", root, "--status"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run agent status: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for status")
	}
	want := "agent: not run\nworkspace: " + root + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunStatusCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	if err := writeAgentState(agentState{
		Version:             1,
		Root:                root,
		Status:              "error",
		CyclesRun:           3,
		ConsecutiveFailures: 2,
		LastSuccessAt:       testAgentTimePtr(time.Date(2026, 7, 4, 1, 1, 1, 0, time.UTC)),
		LastFailureAt:       testAgentTimePtr(time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC)),
		LastError:           "sync failed",
		UpdatedAt:           time.Date(2026, 7, 4, 1, 2, 4, 0, time.UTC),
	}); err != nil {
		t.Fatalf("write agent state: %v", err)
	}

	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--path", root, "--status", "--json"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run agent status json: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for status")
	}
	if strings.Contains(stdout.String(), "agent:") {
		t.Fatalf("json output includes text status: %s", stdout.String())
	}
	var snapshot agentStatusSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, stdout.String())
	}
	if !snapshot.HasRun || snapshot.Workspace.Root != root || snapshot.State == nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.State.Status != "error" || snapshot.State.CyclesRun != 3 || snapshot.State.ConsecutiveFailures != 2 || snapshot.State.LastError != "sync failed" {
		t.Fatalf("state = %#v", snapshot.State)
	}
}

func TestRunStatusNotRunCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"--path", root, "--status", "--json"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		t.Fatal("runner should not be called for status")
		return nil
	})
	if err != nil {
		t.Fatalf("run agent status json: %v", err)
	}
	var snapshot agentStatusSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, stdout.String())
	}
	if snapshot.HasRun || snapshot.State != nil || snapshot.Workspace.Root != root {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunPauseWritesControlAndPausedState(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--path", root, "--pause"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("pause agent: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for pause")
	}
	want := "agent paused: " + root + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	control := readAgentControl(t, root)
	if !control.Paused || control.UpdatedAt.UTC().Format(time.RFC3339) != "2026-07-04T01:02:03Z" {
		t.Fatalf("control = %#v", control)
	}
	state := readAgentState(t, root)
	if state.Status != "paused" || state.CyclesRun != 0 || state.ConsecutiveFailures != 0 {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunResumeClearsControlAndWritesIdleState(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 4, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	lastSuccess := time.Date(2026, 7, 4, 1, 1, 1, 0, time.UTC)
	if err := writeAgentControl(root, agentControl{Version: 1, Paused: true, UpdatedAt: lastSuccess}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := writeAgentState(agentState{
		Version:       1,
		Root:          root,
		Status:        "paused",
		CyclesRun:     4,
		LastSuccessAt: &lastSuccess,
		UpdatedAt:     lastSuccess,
	}); err != nil {
		t.Fatalf("write state: %v", err)
	}

	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--path", root, "--resume"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("resume agent: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for resume")
	}
	want := "agent resumed: " + root + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if _, err := os.Stat(filepath.Join(root, ".synchub", "agent-control.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control file still exists or stat failed: %v", err)
	}
	state := readAgentState(t, root)
	if state.Status != "idle" || state.CyclesRun != 4 || state.LastSuccessAt == nil || state.LastSuccessAt.UTC().Format(time.RFC3339) != "2026-07-04T01:01:01Z" {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunWatchTriggersSyncOnLocalChanges(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()
	originalWatchReady := agentWatchReady
	defer func() { agentWatchReady = originalWatchReady }()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".synchub"), 0o755); err != nil {
		t.Fatalf("mkdir .synchub: %v", err)
	}
	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeAgentWorkspaceConfig(workspacePath, agentWorkspaceConfig{Root: root, RemotePath: "/workspace"}); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	calls := 0
	watchReady := make(chan struct{})
	agentWatchReady = func() { close(watchReady) }
	var stdout bytes.Buffer
	go func() {
		<-watchReady
		_ = os.WriteFile(filepath.Join(root, "created.txt"), []byte("created"), 0o644)
	}()
	err := run(context.Background(), []string{
		"--path", root,
		"--workspace-config", workspacePath,
		"--watch",
		"--watch-interval", "1ms",
		"--interval", "1h",
		"--cycles", "2",
	}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("run watch agent: %v", err)
	}
	if calls != 2 {
		t.Fatalf("sync calls = %d, want 2", calls)
	}
	out := stdout.String()
	for _, want := range []string{
		"agent started: " + root,
		"watch interval: 1ms",
		"local changes detected: 1",
		"agent stopped: sync cycles reached 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunOnceReturnsSyncError(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("sync failed")
	err := run(context.Background(), []string{"--path", root, "--once"}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestRunSyncCycleShowsCompletionTime(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	var stdout bytes.Buffer
	err := runSyncCycle(context.Background(), agentOptions{}, &stdout, &bytes.Buffer{}, func(ctx context.Context, opts agentOptions, stdout, stderr io.Writer) error {
		_, _ = stdout.Write([]byte("synced\n"))
		return nil
	})
	if err != nil {
		t.Fatalf("sync cycle: %v", err)
	}

	want := "synced\nsync completed: 2026-07-04T01:02:03Z\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncCycleReportsErrorWithoutCompletion(t *testing.T) {
	wantErr := errors.New("sync failed")
	var stdout, stderr bytes.Buffer
	err := runSyncCycle(context.Background(), agentOptions{}, &stdout, &stderr, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}

	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	want := "sync failed: sync failed\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunLoopStopsAfterMaxFailures(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("sync failed")
	err := run(context.Background(), []string{
		"--path", root,
		"--interval", "1ms",
		"--max-failures", "2",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		return wantErr
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped sync failure", err)
	}
	if !strings.Contains(err.Error(), "sync failed 2 consecutive times") {
		t.Fatalf("error = %v, want max failures message", err)
	}
}

func TestRunLoopStopsAfterCycles(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	calls := 0
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"--path", root,
		"--interval", "1ms",
		"--cycles", "2",
	}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("run agent cycles: %v", err)
	}
	if calls != 2 {
		t.Fatalf("sync calls = %d, want 2", calls)
	}
	out := stdout.String()
	for _, want := range []string{
		"agent started: " + root,
		"sync interval: 1ms",
		"sync completed: 2026-07-04T01:02:03Z",
		"agent stopped: sync cycles reached 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	state := readAgentState(t, root)
	if state.Status != "ok" || state.CyclesRun != 2 || state.LastSuccessAt == nil {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunLoopSkipsSyncWhenPaused(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	if err := writeAgentControl(root, agentControl{Version: 1, Paused: true, UpdatedAt: agentNow().UTC()}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	called := false
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"--path", root,
		"--interval", "1ms",
		"--cycles", "1",
	}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run paused agent: %v", err)
	}
	if called {
		t.Fatal("runner should not be called while paused")
	}
	out := stdout.String()
	for _, want := range []string{
		"sync skipped: agent is paused",
		"agent stopped: sync cycles reached 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	state := readAgentState(t, root)
	if state.Status != "paused" || state.CyclesRun != 1 || state.LastSuccessAt != nil {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunOnceSkipsSyncWhenPaused(t *testing.T) {
	originalNow := agentNow
	agentNow = func() time.Time { return time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC) }
	defer func() { agentNow = originalNow }()

	root := t.TempDir()
	if err := writeAgentControl(root, agentControl{Version: 1, Paused: true, UpdatedAt: agentNow().UTC()}); err != nil {
		t.Fatalf("write control: %v", err)
	}
	called := false
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"--path", root,
		"--once",
	}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run paused once: %v", err)
	}
	if called {
		t.Fatal("runner should not be called while paused")
	}
	if stdout.String() != "sync skipped: agent is paused\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	state := readAgentState(t, root)
	if state.Status != "paused" || state.CyclesRun != 1 {
		t.Fatalf("agent state = %#v", state)
	}
}

func TestRunLoopReturnsFinalCycleError(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("sync failed")
	calls := 0
	err := run(context.Background(), []string{
		"--path", root,
		"--interval", "1ms",
		"--cycles", "2",
	}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		calls++
		if calls == 2 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want final sync error", err)
	}
	if calls != 2 {
		t.Fatalf("sync calls = %d, want 2", calls)
	}
}

func TestShouldStopAfterFailures(t *testing.T) {
	if shouldStopAfterFailures(agentOptions{MaxFailures: 0}, 100) {
		t.Fatal("max failures 0 should disable failure stop")
	}
	if shouldStopAfterFailures(agentOptions{MaxFailures: 3}, 2) {
		t.Fatal("should not stop before threshold")
	}
	if !shouldStopAfterFailures(agentOptions{MaxFailures: 3}, 3) {
		t.Fatal("should stop at threshold")
	}
}

func TestShouldStopAfterCycles(t *testing.T) {
	if shouldStopAfterCycles(agentOptions{Cycles: 0}, 100) {
		t.Fatal("cycles 0 should disable cycle stop")
	}
	if shouldStopAfterCycles(agentOptions{Cycles: 3}, 2) {
		t.Fatal("should not stop before cycle limit")
	}
	if !shouldStopAfterCycles(agentOptions{Cycles: 3}, 3) {
		t.Fatal("should stop at cycle limit")
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--help"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run help: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for help")
	}
	if !strings.Contains(stdout.String(), "synchub-agent --path . --once --dry-run") || !strings.Contains(stdout.String(), "synchub-agent --path . --cycles 3") {
		t.Fatalf("usage output = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "synchub-agent --path . --status --json") {
		t.Fatalf("usage output missing status json: %q", stdout.String())
	}
}

func TestRunVersionPrintsVersion(t *testing.T) {
	var stdout bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--version"}, &stdout, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if called {
		t.Fatal("runner should not be called for version")
	}
	want := version.Name + " " + version.Version + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestParseOptionsRejectsInvalidInterval(t *testing.T) {
	_, err := parseOptions([]string{"--interval", "0s"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid interval error")
	}
}

func TestParseOptionsRejectsInvalidLimit(t *testing.T) {
	_, err := parseOptions([]string{"--limit", "0"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestParseOptionsRejectsInvalidMaxFailures(t *testing.T) {
	_, err := parseOptions([]string{"--max-failures", "-1"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid max failures error")
	}
}

func TestParseOptionsRejectsInvalidCycles(t *testing.T) {
	_, err := parseOptions([]string{"--cycles", "-1"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid cycles error")
	}
}

func TestParseOptionsRejectsCyclesWithOnce(t *testing.T) {
	_, err := parseOptions([]string{"--once", "--cycles", "1"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cycles cannot be used with --once") {
		t.Fatalf("error = %v, want cycles cannot be used with --once", err)
	}
}

func TestParseOptionsRejectsCyclesWithStatus(t *testing.T) {
	_, err := parseOptions([]string{"--status", "--cycles", "1"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cycles cannot be used with --status") {
		t.Fatalf("error = %v, want cycles cannot be used with --status", err)
	}
}

func TestParseOptionsRejectsWatchWithOnce(t *testing.T) {
	_, err := parseOptions([]string{"--watch", "--once"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "watch cannot be used with --once") {
		t.Fatalf("error = %v, want watch cannot be used with --once", err)
	}
}

func TestParseOptionsRejectsStatusWithOnce(t *testing.T) {
	_, err := parseOptions([]string{"--status", "--once"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "status cannot be used with --once") {
		t.Fatalf("error = %v, want status cannot be used with --once", err)
	}
}

func TestParseOptionsRejectsStatusWithWatch(t *testing.T) {
	_, err := parseOptions([]string{"--status", "--watch"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "status cannot be used with --watch") {
		t.Fatalf("error = %v, want status cannot be used with --watch", err)
	}
}

func TestParseOptionsRejectsJSONWithoutStatus(t *testing.T) {
	_, err := parseOptions([]string{"--json"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "json output requires --status") {
		t.Fatalf("error = %v, want json output requires --status", err)
	}
}

func TestParseOptionsRejectsPauseResumeCombinations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "pause resume", args: []string{"--pause", "--resume"}, want: "pause cannot be used with --resume"},
		{name: "pause status", args: []string{"--pause", "--status"}, want: "pause cannot be used with --status"},
		{name: "resume status", args: []string{"--resume", "--status"}, want: "resume cannot be used with --status"},
		{name: "pause once", args: []string{"--pause", "--once"}, want: "pause and resume cannot be used with --once"},
		{name: "resume watch", args: []string{"--resume", "--watch"}, want: "pause and resume cannot be used with --watch"},
		{name: "pause cycles", args: []string{"--pause", "--cycles", "1"}, want: "pause and resume cannot be used with --cycles"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseOptionsRejectsDryRunWithoutOnce(t *testing.T) {
	_, err := parseOptions([]string{"--dry-run"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "dry run requires --once") {
		t.Fatalf("error = %v, want dry run requires --once", err)
	}
}

func TestParseOptionsRejectsInvalidWatchInterval(t *testing.T) {
	_, err := parseOptions([]string{"--watch", "--watch-interval", "0s"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "watch interval must be positive") {
		t.Fatalf("error = %v, want watch interval must be positive", err)
	}
}

func TestBuildSyncOnceArgs(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{
		RootPath:            "workspace",
		ConfigPath:          "login.json",
		WorkspaceConfigPath: "workspace.json",
		ManifestPath:        "manifest.json",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		Limit:               20,
		DryRun:              true,
	})
	want := []string{
		"sync", "once",
		"--path", "workspace",
		"--config", "login.json",
		"--workspace-config", "workspace.json",
		"--manifest", "manifest.json",
		"--device-name", "laptop",
		"--platform", "windows",
		"--limit", "20",
		"--dry-run",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func writeAgentWorkspaceConfig(path string, cfg agentWorkspaceConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func readAgentState(t *testing.T, root string) agentState {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".synchub", "agent-state.json"))
	if err != nil {
		t.Fatalf("read agent state: %v", err)
	}
	var state agentState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode agent state: %v", err)
	}
	return state
}

func readAgentControl(t *testing.T, root string) agentControl {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".synchub", "agent-control.json"))
	if err != nil {
		t.Fatalf("read agent control: %v", err)
	}
	var control agentControl
	if err := json.Unmarshal(raw, &control); err != nil {
		t.Fatalf("decode agent control: %v", err)
	}
	return control
}

func testAgentTimePtr(value time.Time) *time.Time {
	return &value
}

func TestBuildSyncOnceArgsOmitsEmptyOptionalPaths(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{RootPath: "workspace", ConfigPath: "login.json", Limit: 500})
	want := []string{"sync", "once", "--path", "workspace", "--config", "login.json", "--limit", "500"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
