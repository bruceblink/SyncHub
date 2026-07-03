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

func runFile(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printFileUsage(stderr)
		return errors.New("file command is required")
	}
	switch args[0] {
	case "versions":
		return runFileVersions(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printFileUsage(stdout)
		return nil
	default:
		printFileUsage(stderr)
		return fmt.Errorf("unknown file command: %s", args[0])
	}
}

func runFileVersions(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file versions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote file path")
	fileID := fs.String("file-id", "", "remote file id")
	limit := fs.Int("limit", 100, "maximum versions to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}
	id := strings.TrimSpace(*fileID)
	remote := strings.TrimSpace(*remotePath)
	if id == "" && remote == "" {
		return errors.New("remote path or file id is required")
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

	apiClient := client.New(serverURL)
	if id == "" {
		normalized, err := normalizeRemotePath(remote)
		if err != nil {
			return err
		}
		node, err := apiClient.GetFileByPath(ctx, loginConfig.Tokens.AccessToken, normalized)
		if err != nil {
			return err
		}
		id = node.ID
		remote = node.Path
	}

	versions, err := apiClient.ListFileVersions(ctx, loginConfig.Tokens.AccessToken, id, int32(*limit))
	if err != nil {
		return err
	}
	printFileVersions(stdout, remote, id, versions.Items)
	return nil
}

func printFileVersions(stdout io.Writer, remotePath, fileID string, versions []client.FileVersion) {
	if strings.TrimSpace(remotePath) != "" {
		fmt.Fprintf(stdout, "file: %s\n", remotePath)
	}
	fmt.Fprintf(stdout, "file id: %s\n", fileID)
	fmt.Fprintf(stdout, "versions: %d\n", len(versions))
	for _, version := range versions {
		fmt.Fprintf(stdout, "v%d size=%d sha256=%s pinned=%s created=%s id=%s\n",
			version.Version,
			version.Size,
			version.SHA256,
			formatOptionalTime(version.PinnedAt),
			version.CreatedAt.UTC().Format(time.RFC3339),
			version.ID,
		)
	}
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
