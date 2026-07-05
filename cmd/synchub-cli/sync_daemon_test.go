package main

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

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
