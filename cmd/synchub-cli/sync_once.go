package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func runSyncOnce(ctx context.Context, args []string, stdout, stderr io.Writer) (runErr error) {
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
	jsonOutput := fs.Bool("json", false, "print one sync cycle result as JSON")
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
		if *jsonOutput {
			pushArgs = append(pushArgs, "--json")
		}
		pushOut := stdout
		var pushBuffer bytes.Buffer
		if *jsonOutput {
			pushOut = &pushBuffer
		}
		if err := runSyncPush(ctx, pushArgs, pushOut, stderr); err != nil {
			return err
		}
		var pushSnapshot syncPushSnapshot
		if *jsonOutput {
			if err := json.Unmarshal(pushBuffer.Bytes(), &pushSnapshot); err != nil {
				return fmt.Errorf("decode sync push JSON: %w", err)
			}
		}
		_, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
		if err != nil {
			return err
		}
		if strings.TrimSpace(workspace.DeviceID) == "" {
			if *jsonOutput {
				return writeSyncOnceJSON(stdout, syncOnceSnapshot{
					Workspace: syncOnceWorkspaceFromConfig(workspace),
					DryRun:    true,
					Push:      &pushSnapshot,
					Pull: &syncOncePullResult{
						Skipped: true,
						Reason:  "workspace device is not registered",
					},
				})
			}
			fmt.Fprintln(stdout, "pull dry run skipped: workspace device is not registered")
			return nil
		}
		pullArgs := append([]string{}, commonArgs...)
		pullArgs = append(pullArgs, "--limit", fmt.Sprintf("%d", *limit), "--dry-run")
		if *jsonOutput {
			pullArgs = append(pullArgs, "--json")
			var pullBuffer bytes.Buffer
			if err := runSyncPullWithDeviceEnsure(ctx, pullArgs, &pullBuffer, stderr, false); err != nil {
				return err
			}
			var pullSnapshot syncPullSnapshot
			if err := json.Unmarshal(pullBuffer.Bytes(), &pullSnapshot); err != nil {
				return fmt.Errorf("decode sync pull JSON: %w", err)
			}
			return writeSyncOnceJSON(stdout, syncOnceSnapshot{
				Workspace: syncOnceWorkspaceFromConfig(workspace),
				DryRun:    true,
				Push:      &pushSnapshot,
				Pull:      &syncOncePullResult{Result: &pullSnapshot},
			})
		}
		return runSyncPullWithDeviceEnsure(ctx, pullArgs, stdout, stderr, false)
	}
	if err := ensureSyncOnceDevice(ctx, *rootPath, *workspaceConfigPath, *configPath, *deviceName, *devicePlatform); err != nil {
		return err
	}
	defer func() {
		if err := reportSyncOnceResult(ctx, *rootPath, *workspaceConfigPath, *configPath, runErr); err != nil {
			fmt.Fprintf(stderr, "report sync result failed: %v\n", err)
		}
	}()
	pushArgs := append([]string{}, commonArgs...)
	if *jsonOutput {
		pushArgs = append(pushArgs, "--json")
	}
	pushOut := stdout
	var pushBuffer bytes.Buffer
	if *jsonOutput {
		pushOut = &pushBuffer
	}
	if err := runSyncPush(ctx, pushArgs, pushOut, stderr); err != nil {
		return err
	}
	var pushSnapshot syncPushSnapshot
	if *jsonOutput {
		if err := json.Unmarshal(pushBuffer.Bytes(), &pushSnapshot); err != nil {
			return fmt.Errorf("decode sync push JSON: %w", err)
		}
	}

	pullArgs := append([]string{}, commonArgs...)
	if strings.TrimSpace(*deviceName) != "" {
		pullArgs = append(pullArgs, "--device-name", *deviceName)
	}
	if strings.TrimSpace(*devicePlatform) != "" {
		pullArgs = append(pullArgs, "--platform", *devicePlatform)
	}
	pullArgs = append(pullArgs, "--limit", fmt.Sprintf("%d", *limit))
	if *jsonOutput {
		pullArgs = append(pullArgs, "--json")
		var pullBuffer bytes.Buffer
		if err := runSyncPullWithDeviceEnsure(ctx, pullArgs, &pullBuffer, stderr, false); err != nil {
			return err
		}
		var pullSnapshot syncPullSnapshot
		if err := json.Unmarshal(pullBuffer.Bytes(), &pullSnapshot); err != nil {
			return fmt.Errorf("decode sync pull JSON: %w", err)
		}
		_, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
		if err != nil {
			return err
		}
		return writeSyncOnceJSON(stdout, syncOnceSnapshot{
			Workspace: syncOnceWorkspaceFromConfig(workspace),
			DryRun:    false,
			Push:      &pushSnapshot,
			Pull:      &syncOncePullResult{Result: &pullSnapshot},
		})
	}
	return runSyncPullWithDeviceEnsure(ctx, pullArgs, stdout, stderr, false)
}

func reportSyncOnceResult(ctx context.Context, rootPath, workspaceConfigPath, configPath string, syncErr error) error {
	_, workspace, _, err := loadWorkspace(rootPath, workspaceConfigPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(workspace.DeviceID) == "" {
		return nil
	}
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	status := "success"
	errorMessage := ""
	if syncErr != nil {
		status = "error"
		errorMessage = syncErr.Error()
	}
	_, err = client.New(serverURL).ReportDeviceSync(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, status, errorMessage)
	return err
}

type syncOnceSnapshot struct {
	Workspace syncOnceWorkspace   `json:"workspace"`
	DryRun    bool                `json:"dry_run"`
	Push      *syncPushSnapshot   `json:"push"`
	Pull      *syncOncePullResult `json:"pull"`
}

type syncOnceWorkspace struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
	UserEmail  string `json:"user_email"`
	DeviceID   string `json:"device_id,omitempty"`
}

type syncOncePullResult struct {
	Skipped bool              `json:"skipped"`
	Reason  string            `json:"reason,omitempty"`
	Result  *syncPullSnapshot `json:"result,omitempty"`
}

func syncOnceWorkspaceFromConfig(workspace workspaceConfig) syncOnceWorkspace {
	return syncOnceWorkspace{
		Root:       workspace.Root,
		RemotePath: workspace.RemotePath,
		UserEmail:  workspace.UserEmail,
		DeviceID:   workspace.DeviceID,
	}
}

func writeSyncOnceJSON(stdout io.Writer, snapshot syncOnceSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
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
