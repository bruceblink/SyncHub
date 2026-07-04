package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
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
	dryRun := fs.Bool("dry-run", false, "preview one sync cycle without uploading, downloading, or updating local state")
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
	if *dryRun {
		pushArgs := append([]string{}, commonArgs...)
		pushArgs = append(pushArgs, "--dry-run")
		if err := runSyncPush(ctx, pushArgs, stdout, stderr); err != nil {
			return err
		}
		_, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
		if err != nil {
			return err
		}
		if strings.TrimSpace(workspace.DeviceID) == "" {
			fmt.Fprintln(stdout, "pull dry run skipped: workspace device is not registered")
			return nil
		}
		pullArgs := append([]string{}, commonArgs...)
		pullArgs = append(pullArgs, "--limit", fmt.Sprintf("%d", *limit), "--dry-run")
		return runSyncPullWithDeviceEnsure(ctx, pullArgs, stdout, stderr, false)
	}
	if err := ensureSyncOnceDevice(ctx, *rootPath, *workspaceConfigPath, *configPath, *deviceName, *devicePlatform); err != nil {
		return err
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
	return runSyncPullWithDeviceEnsure(ctx, pullArgs, stdout, stderr, false)
}

func ensureSyncOnceDevice(ctx context.Context, rootPath, workspaceConfigPath, configPath, deviceName, devicePlatform string) error {
	root, workspace, workspacePath, err := loadWorkspace(rootPath, workspaceConfigPath)
	if err != nil {
		return err
	}
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	apiClient := client.New(serverURL)
	changed, err := ensureWorkspaceDevice(ctx, apiClient, loginConfig.Tokens.AccessToken, root, &workspace, deviceName, devicePlatform)
	if err != nil {
		return err
	}
	if changed {
		return writeWorkspaceConfig(workspacePath, workspace)
	}
	return nil
}

func runSyncDevices(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync devices", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	limit := fs.Int("limit", 100, "maximum devices to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	_, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	loginConfig, err := readConfigWithRefresh(ctx, *configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	devices, err := client.New(serverURL).ListDevices(ctx, loginConfig.Tokens.AccessToken, int32(*limit))
	if err != nil {
		return err
	}
	printSyncDevices(stdout, workspace, devices.Items)
	return nil
}

func printSyncDevices(stdout io.Writer, workspace workspaceConfig, devices []client.Device) {
	fmt.Fprintf(stdout, "devices: %d\n", len(devices))
	for _, device := range devices {
		marker := "-"
		if strings.TrimSpace(workspace.DeviceID) != "" && device.ID == workspace.DeviceID {
			marker = "*"
		}
		fmt.Fprintf(stdout, "%s %s name=%s platform=%s cursor=%d last_seen=%s updated=%s\n",
			marker,
			device.ID,
			device.Name,
			device.Platform,
			device.LastAppliedChangeID,
			formatOptionalTime(device.LastSeenAt),
			device.UpdatedAt.UTC().Format(time.RFC3339),
		)
	}
}
