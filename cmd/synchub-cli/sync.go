package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

func runSync(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printSyncUsage(stderr)
		return errors.New("sync command is required")
	}
	switch args[0] {
	case "once":
		return runSyncOnce(ctx, args[1:], stdout, stderr)
	case "status":
		return runSyncStatus(ctx, args[1:], stdout, stderr)
	case "push":
		return runSyncPush(ctx, args[1:], stdout, stderr)
	case "pull":
		return runSyncPull(ctx, args[1:], stdout, stderr)
	case "watch":
		return runSyncWatch(ctx, args[1:], stdout, stderr)
	case "trash":
		return runSyncTrash(args[1:], stdout, stderr)
	case "conflicts":
		return runSyncConflicts(ctx, args[1:], stdout, stderr)
	case "devices":
		return runSyncDevices(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printSyncUsage(stdout)
		return nil
	default:
		printSyncUsage(stderr)
		return fmt.Errorf("unknown sync command: %s", args[0])
	}
}
