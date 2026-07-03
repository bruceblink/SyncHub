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

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/internal/watch"
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

func runSyncStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	loginConfigPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	showConflicts := fs.Bool("show-conflicts", false, "include pending remote conflicts")
	conflictLimit := fs.Int("conflict-limit", 100, "maximum conflicts to fetch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *conflictLimit <= 0 {
		return errors.New("conflict limit must be positive")
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	workspacePath := *workspaceConfigPath
	if strings.TrimSpace(workspacePath) == "" {
		workspacePath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(workspacePath)
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
		if strings.TrimSpace(workspace.DeviceName) != "" {
			fmt.Fprintf(stdout, "device name: %s\n", workspace.DeviceName)
		}
		if strings.TrimSpace(workspace.DevicePlatform) != "" {
			fmt.Fprintf(stdout, "device platform: %s\n", workspace.DevicePlatform)
		}
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
	remoteTracked, localOnly := manifestRemoteVersionSummary(m.Items)
	fmt.Fprintf(stdout, "remote tracked: %d\n", remoteTracked)
	fmt.Fprintf(stdout, "local only: %d\n", localOnly)
	fmt.Fprintf(stdout, "last scan: %s\n", m.GeneratedAt.Format(time.RFC3339))
	changes, err := scanManifestChanges(ctx, root, workspace.RemotePath, localManifestPath)
	if err != nil {
		return err
	}
	printSyncStatusChanges(stdout, changes)
	if *showConflicts {
		if err := printSyncStatusConflicts(ctx, stdout, workspace, *loginConfigPath, *conflictLimit); err != nil {
			return err
		}
	}
	return nil
}

func printSyncStatusConflicts(ctx context.Context, stdout io.Writer, workspace workspaceConfig, configPath string, limit int) error {
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	conflicts, err := client.New(serverURL).ListSyncConflicts(ctx, loginConfig.Tokens.AccessToken, "pending", int32(limit))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "remote conflicts: %d\n", len(conflicts.Items))
	printSyncConflictItems(stdout, conflicts.Items)
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

func printSyncStatusChanges(stdout io.Writer, changes []watch.Change) {
	fmt.Fprintf(stdout, "pending changes: %d\n", len(changes))
	printChangeTypeCounts(stdout, changes)
}

func printChangeTypeCounts(stdout io.Writer, changes []watch.Change) {
	counts := map[string]int{}
	for _, change := range changes {
		counts[change.Type]++
	}
	fmt.Fprintf(stdout, "created: %d\n", counts[watch.ChangeCreated])
	fmt.Fprintf(stdout, "updated: %d\n", counts[watch.ChangeUpdated])
	fmt.Fprintf(stdout, "deleted: %d\n", counts[watch.ChangeDeleted])
	fmt.Fprintf(stdout, "moved: %d\n", counts[watch.ChangeMoved])
}

func manifestRemoteVersionSummary(items []manifest.Entry) (remoteTracked, localOnly int) {
	for _, item := range items {
		if item.RemoteVersion == nil {
			localOnly++
			continue
		}
		remoteTracked++
	}
	return remoteTracked, localOnly
}
