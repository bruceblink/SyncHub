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
	case "once":
		return runSyncOnce(ctx, args[1:], stdout, stderr)
	case "status":
		return runSyncStatus(args[1:], stdout, stderr)
	case "push":
		return runSyncPush(ctx, args[1:], stdout, stderr)
	case "pull":
		return runSyncPull(ctx, args[1:], stdout, stderr)
	case "watch":
		return runSyncWatch(ctx, args[1:], stdout, stderr)
	case "conflicts":
		return runSyncConflicts(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printSyncUsage(stdout)
		return nil
	default:
		printSyncUsage(stderr)
		return fmt.Errorf("unknown sync command: %s", args[0])
	}
}

func runSyncOnce(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync once", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	deviceName := fs.String("device-name", "", "device name")
	devicePlatform := fs.String("platform", "", "device platform")
	limit := fs.Int("limit", 500, "maximum changes to pull")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	commonArgs := []string{"--path", *rootPath, "--config", *configPath}
	if strings.TrimSpace(*workspaceConfigPath) != "" {
		commonArgs = append(commonArgs, "--workspace-config", *workspaceConfigPath)
	}
	if strings.TrimSpace(*manifestPath) != "" {
		commonArgs = append(commonArgs, "--manifest", *manifestPath)
	}
	if err := runSyncPush(ctx, commonArgs, stdout, stderr); err != nil {
		return err
	}

	pullArgs := append([]string{}, commonArgs...)
	if strings.TrimSpace(*deviceName) != "" {
		pullArgs = append(pullArgs, "--device-name", *deviceName)
	}
	if strings.TrimSpace(*devicePlatform) != "" {
		pullArgs = append(pullArgs, "--platform", *devicePlatform)
	}
	pullArgs = append(pullArgs, "--limit", fmt.Sprintf("%d", *limit))
	return runSyncPull(ctx, pullArgs, stdout, stderr)
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
			fmt.Fprintln(stdout, "next: run synchub-cli sync once --path .")
			return nil
		}
		return err
	}
	fmt.Fprintf(stdout, "manifest: %s\n", localManifestPath)
	fmt.Fprintf(stdout, "files: %d\n", len(m.Items))
	fmt.Fprintf(stdout, "last scan: %s\n", m.GeneratedAt.Format(time.RFC3339))
	return nil
}
