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
