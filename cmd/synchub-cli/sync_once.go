package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/bruceblink/SyncHub/pkg/client"
)

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
