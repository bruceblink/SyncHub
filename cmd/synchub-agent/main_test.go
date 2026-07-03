package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunOnceInvokesSyncOnce(t *testing.T) {
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
		"--path", ".",
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
	if stdout.String() != "synced\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !got.Once || !got.DryRun || got.RootPath != "." || got.ConfigPath != "login.json" || got.WorkspaceConfigPath != "workspace.json" || got.ManifestPath != "manifest.json" || got.CLIPath != "synchub-cli-test" || got.DeviceName != "laptop" || got.DevicePlatform != "windows" {
		t.Fatalf("options = %#v", got)
	}
	if got.Interval != 5*time.Second {
		t.Fatalf("interval = %s, want 5s", got.Interval)
	}
	if got.Limit != 20 {
		t.Fatalf("limit = %d, want 20", got.Limit)
	}
	if got.MaxFailures != 3 {
		t.Fatalf("max failures = %d, want 3", got.MaxFailures)
	}
}

func TestRunOnceReturnsSyncError(t *testing.T) {
	wantErr := errors.New("sync failed")
	err := run(context.Background(), []string{"--once"}, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, agentOptions, io.Writer, io.Writer) error {
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
	wantErr := errors.New("sync failed")
	err := run(context.Background(), []string{
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
	if !strings.Contains(stdout.String(), "synchub-agent --path . --once --dry-run") {
		t.Fatalf("usage output = %q", stdout.String())
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

func TestParseOptionsRejectsDryRunWithoutOnce(t *testing.T) {
	_, err := parseOptions([]string{"--dry-run"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "dry run requires --once") {
		t.Fatalf("error = %v, want dry run requires --once", err)
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

func TestBuildSyncOnceArgsOmitsEmptyOptionalPaths(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{RootPath: "workspace", ConfigPath: "login.json", Limit: 500})
	want := []string{"sync", "once", "--path", "workspace", "--config", "login.json", "--limit", "500"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
