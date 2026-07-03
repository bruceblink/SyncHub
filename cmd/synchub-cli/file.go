package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
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
	case "list":
		return runFileList(ctx, args[1:], stdout, stderr)
	case "versions":
		return runFileVersions(ctx, args[1:], stdout, stderr)
	case "restore":
		return runFileRestore(ctx, args[1:], stdout, stderr)
	case "pin":
		return runFilePin(ctx, args[1:], stdout, stderr, true)
	case "unpin":
		return runFilePin(ctx, args[1:], stdout, stderr, false)
	case "help", "-h", "--help":
		printFileUsage(stdout)
		return nil
	default:
		printFileUsage(stderr)
		return fmt.Errorf("unknown file command: %s", args[0])
	}
}

func runFileList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	parentID := fs.String("parent-id", "", "remote parent directory id")
	pageSize := fs.Int("page-size", 100, "maximum files to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pageSize <= 0 {
		return errors.New("page size must be positive")
	}
	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	var parent *string
	if trimmed := strings.TrimSpace(*parentID); trimmed != "" {
		parent = &trimmed
	}
	files, err := session.apiClient.ListFiles(ctx, session.accessToken, parent, int32(*pageSize))
	if err != nil {
		return err
	}
	printFileList(stdout, files.Items)
	return nil
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
	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	id, remote, err := session.resolveFileID(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}

	versions, err := session.apiClient.ListFileVersions(ctx, session.accessToken, id, int32(*limit))
	if err != nil {
		return err
	}
	printFileVersions(stdout, remote, id, versions.Items)
	return nil
}

func runFileRestore(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote file path")
	fileID := fs.String("file-id", "", "remote file id")
	version := fs.String("version", "", "version number to restore")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsedVersion, err := parseFileVersionFlag(*version)
	if err != nil {
		return err
	}
	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	id, _, err := session.resolveFileID(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}

	restored, err := session.apiClient.RestoreFileVersion(ctx, session.accessToken, id, parsedVersion)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "restored: %s\n", restored.File.Path)
	fmt.Fprintf(stdout, "version: %d\n", restored.File.Version)
	fmt.Fprintf(stdout, "change id: %d\n", restored.ChangeID)
	return nil
}

func runFilePin(ctx context.Context, args []string, stdout, stderr io.Writer, pin bool) error {
	command := "file pin"
	if !pin {
		command = "file unpin"
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote file path")
	fileID := fs.String("file-id", "", "remote file id")
	version := fs.String("version", "", "version number to update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsedVersion, err := parseFileVersionFlag(*version)
	if err != nil {
		return err
	}
	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	id, remote, err := session.resolveFileID(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}

	var updated client.FileVersion
	if pin {
		updated, err = session.apiClient.PinFileVersion(ctx, session.accessToken, id, parsedVersion)
	} else {
		updated, err = session.apiClient.UnpinFileVersion(ctx, session.accessToken, id, parsedVersion)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(remote) != "" {
		fmt.Fprintf(stdout, "file: %s\n", remote)
	}
	action := "pinned"
	if !pin {
		action = "unpinned"
	}
	fmt.Fprintf(stdout, "%s: %s v%d\n", action, updated.FileID, updated.Version)
	fmt.Fprintf(stdout, "pinned at: %s\n", formatOptionalTime(updated.PinnedAt))
	return nil
}

type fileCommandSession struct {
	apiClient   *client.Client
	accessToken string
}

func openFileCommandSession(ctx context.Context, rootPath, workspaceConfigPath, configPath string) (fileCommandSession, error) {
	_, workspace, _, err := loadWorkspace(rootPath, workspaceConfigPath)
	if err != nil {
		return fileCommandSession{}, err
	}
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return fileCommandSession{}, err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	return fileCommandSession{
		apiClient:   client.New(serverURL),
		accessToken: loginConfig.Tokens.AccessToken,
	}, nil
}

func (s fileCommandSession) resolveFileID(ctx context.Context, fileID, remotePath string) (string, string, error) {
	id := strings.TrimSpace(fileID)
	remote := strings.TrimSpace(remotePath)
	if id == "" && remote == "" {
		return "", "", errors.New("remote path or file id is required")
	}
	if id != "" {
		return id, remote, nil
	}
	normalized, err := normalizeRemotePath(remote)
	if err != nil {
		return "", "", err
	}
	node, err := s.apiClient.GetFileByPath(ctx, s.accessToken, normalized)
	if err != nil {
		return "", "", err
	}
	return node.ID, node.Path, nil
}

func parseFileVersionFlag(raw string) (int64, error) {
	version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || version <= 0 {
		return 0, errors.New("version must be a positive integer")
	}
	return version, nil
}

func printFileList(stdout io.Writer, files []client.FileNode) {
	fmt.Fprintf(stdout, "files: %d\n", len(files))
	for _, file := range files {
		name := file.Name
		if file.NodeType == "directory" {
			name += "/"
		}
		fmt.Fprintf(stdout, "%s %s size=%d version=%d id=%s\n", file.NodeType, name, file.Size, file.Version, file.ID)
	}
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
