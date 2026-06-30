package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func runSyncConflicts(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync conflicts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	resolution := fs.String("resolution", "pending", "conflict resolution filter")
	limit := fs.Int("limit", 100, "maximum conflicts to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}

	_, workspace, _, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	loginConfig, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}

	apiClient := client.New(serverURL)
	conflicts, err := apiClient.ListSyncConflicts(ctx, loginConfig.Tokens.AccessToken, *resolution, int32(*limit))
	if err != nil {
		return err
	}
	printSyncConflicts(stdout, conflicts.Items)
	return nil
}

func printSyncConflicts(stdout io.Writer, conflicts []client.SyncConflict) {
	fmt.Fprintf(stdout, "conflicts: %d\n", len(conflicts))
	for _, conflict := range conflicts {
		fmt.Fprintf(stdout, "%s %s local=%s remote=%s id=%s\n",
			conflict.Resolution,
			conflict.Path,
			versionString(conflict.LocalVersion),
			versionString(conflict.RemoteVersion),
			conflict.ID,
		)
	}
}

func versionString(version *int64) string {
	if version == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *version)
}
