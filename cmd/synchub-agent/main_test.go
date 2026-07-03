package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
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
		"--once",
	}, &stdout, &bytes.Buffer{}, runner)
	if err != nil {
		t.Fatalf("run agent once: %v", err)
	}
	if stdout.String() != "synced\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !got.Once || got.RootPath != "." || got.ConfigPath != "login.json" || got.WorkspaceConfigPath != "workspace.json" || got.ManifestPath != "manifest.json" || got.CLIPath != "synchub-cli-test" {
		t.Fatalf("options = %#v", got)
	}
	if got.Interval != 5*time.Second {
		t.Fatalf("interval = %s, want 5s", got.Interval)
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

func TestParseOptionsRejectsInvalidInterval(t *testing.T) {
	_, err := parseOptions([]string{"--interval", "0s"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid interval error")
	}
}

func TestBuildSyncOnceArgs(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{
		RootPath:            "workspace",
		ConfigPath:          "login.json",
		WorkspaceConfigPath: "workspace.json",
		ManifestPath:        "manifest.json",
	})
	want := []string{
		"sync", "once",
		"--path", "workspace",
		"--config", "login.json",
		"--workspace-config", "workspace.json",
		"--manifest", "manifest.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildSyncOnceArgsOmitsEmptyOptionalPaths(t *testing.T) {
	got := buildSyncOnceArgs(agentOptions{RootPath: "workspace", ConfigPath: "login.json"})
	want := []string{"sync", "once", "--path", "workspace", "--config", "login.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
