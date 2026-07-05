package main

import (
	"context"
	"fmt"
	"io"

	"github.com/bruceblink/SyncHub/internal/syncdaemon"
)

func runSyncDaemon(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runSyncDaemonWithSyncOnce(ctx, args, stdout, stderr, func(ctx context.Context, syncArgs []string, stdout, stderr io.Writer) error {
		if len(syncArgs) < 2 || syncArgs[0] != "sync" || syncArgs[1] != "once" {
			return fmt.Errorf("unexpected daemon sync command: %v", syncArgs)
		}
		return runSyncOnce(ctx, syncArgs[2:], stdout, stderr)
	})
}

func runSyncDaemonWithSyncOnce(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncdaemon.SyncOnceArgsRunner) error {
	return syncdaemon.RunWithSyncOnce(ctx, args, stdout, stderr, runner)
}
