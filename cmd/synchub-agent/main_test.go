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
		"--once",
	}, &stdout, &bytes.Buffer{}, runner)
	if err != nil {
		t.Fatalf("run agent once: %v", err)
	}
	if stdout.String() != "synced\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !got.Once || got.RootPath != "." || got.ConfigPath != "login.json" || got.WorkspaceConfigPath != "workspace.json" || got.ManifestPath != "manifest.json" || got.CLIPath != "synchub-cli-test" || got.DeviceName != "laptop" || got.DevicePlatform != "windows" {
		t.Fatalf("options = %#v", got)
	}
	if got.Interval != 5*time.Second {
		t.Fatalf("interval = %s, want 5s", got.Interval)
	}
	if got.Limit != 20 {
		t.Fatalf("limit = %d, want 20", got.Limit)
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
	if !strings.Contains(stdout.String(), "synchub-agent --path . --once") {
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

func TestBuildSyncOnceArgs(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{
		RootPath:            "workspace",
		ConfigPath:          "login.json",
		WorkspaceConfigPath: "workspace.json",
		ManifestPath:        "manifest.json",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		Limit:               20,
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
