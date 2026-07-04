package main

import (
	"context"
	"encoding/json"
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
	jsonOutput := fs.Bool("json", false, "print devices as JSON")
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
	if *jsonOutput {
		return writeSyncDevicesJSON(stdout, workspace, devices.Items)
	}
	printSyncDevices(stdout, workspace, devices.Items)
	return nil
}

type syncDevicesSnapshot struct {
	Workspace syncDevicesWorkspace `json:"workspace"`
	Items     []syncDeviceItem     `json:"items"`
}

type syncDevicesWorkspace struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
	UserEmail  string `json:"user_email"`
	DeviceID   string `json:"device_id,omitempty"`
}

type syncDeviceItem struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Platform            string     `json:"platform"`
	Current             bool       `json:"current"`
	LastSeenAt          *time.Time `json:"last_seen_at"`
	LastAppliedChangeID int64      `json:"last_applied_change_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func writeSyncDevicesJSON(stdout io.Writer, workspace workspaceConfig, devices []client.Device) error {
	items := make([]syncDeviceItem, 0, len(devices))
	for _, device := range devices {
		items = append(items, syncDeviceItem{
			ID:                  device.ID,
			Name:                device.Name,
			Platform:            device.Platform,
			Current:             strings.TrimSpace(workspace.DeviceID) != "" && device.ID == workspace.DeviceID,
			LastSeenAt:          device.LastSeenAt,
			LastAppliedChangeID: device.LastAppliedChangeID,
			CreatedAt:           device.CreatedAt,
			UpdatedAt:           device.UpdatedAt,
		})
	}
	snapshot := syncDevicesSnapshot{
		Workspace: syncDevicesWorkspace{
			Root:       workspace.Root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		Items: items,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
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
