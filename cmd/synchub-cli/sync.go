package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runSync(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printSyncUsage(stderr)
		return errors.New("sync command is required")
	}
	switch args[0] {
	case "status":
		return runSyncStatus(args[1:], stdout, stderr)
	case "push":
		return runSyncPush(ctx, args[1:], stdout, stderr)
	case "pull":
		return runSyncPull(ctx, args[1:], stdout, stderr)
	case "watch":
		return runSyncWatch(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printSyncUsage(stdout)
		return nil
	default:
		printSyncUsage(stderr)
		return fmt.Errorf("unknown sync command: %s", args[0])
	}
}

func runSyncStatus(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	configPath := *workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
	if err != nil {
		return err
	}
	if workspace.Root != "" {
		root = workspace.Root
	}
	localManifestPath := *manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}

	fmt.Fprintf(stdout, "workspace: %s\n", root)
	fmt.Fprintf(stdout, "remote path: %s\n", workspace.RemotePath)
	fmt.Fprintf(stdout, "user: %s\n", workspace.UserEmail)
	if workspace.DeviceID != "" {
		fmt.Fprintf(stdout, "device: %s\n", workspace.DeviceID)
		fmt.Fprintf(stdout, "last applied change: %d\n", workspace.LastAppliedChangeID)
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "manifest: missing")
			fmt.Fprintln(stdout, "next: run synchub-cli manifest scan --path .")
			return nil
		}
		return err
	}
	fmt.Fprintf(stdout, "manifest: %s\n", localManifestPath)
	fmt.Fprintf(stdout, "files: %d\n", len(m.Items))
	fmt.Fprintf(stdout, "last scan: %s\n", m.GeneratedAt.Format(time.RFC3339))
	return nil
}
