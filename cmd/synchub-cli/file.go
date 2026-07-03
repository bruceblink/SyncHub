package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	case "download":
		return runFileDownload(ctx, args[1:], stdout, stderr)
	case "delete":
		return runFileDelete(ctx, args[1:], stdout, stderr)
	case "move":
		return runFileMove(ctx, args[1:], stdout, stderr)
	case "mkdir":
		return runFileMkdir(ctx, args[1:], stdout, stderr)
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
	remotePath := fs.String("remote-path", "", "remote parent directory path")
	cursor := fs.String("cursor", "", "page cursor returned by a previous file list")
	pageSize := fs.Int("page-size", 100, "maximum files to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pageSize <= 0 {
		return errors.New("page size must be positive")
	}
	if strings.TrimSpace(*parentID) != "" && strings.TrimSpace(*remotePath) != "" {
		return errors.New("parent id and remote path cannot both be set")
	}
	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	parent, err := session.resolveFileListParent(ctx, *parentID, *remotePath)
	if err != nil {
		return err
	}
	files, err := session.apiClient.ListFiles(ctx, session.accessToken, parent, *cursor, int32(*pageSize))
	if err != nil {
		return err
	}
	printFileList(stdout, files)
	return nil
}

func runFileDelete(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote file or directory path")
	fileID := fs.String("file-id", "", "remote file or directory id")
	if err := fs.Parse(args); err != nil {
		return err
	}

	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	node, err := session.resolveFileNode(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}
	if err := session.apiClient.DeleteFileWithDevice(ctx, session.accessToken, node.ID, session.workspace.DeviceID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "deleted: %s\n", node.Path)
	fmt.Fprintf(stdout, "id: %s\n", node.ID)
	return nil
}

func runFileMove(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file move", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "source remote file or directory path")
	fileID := fs.String("file-id", "", "source remote file or directory id")
	targetPath := fs.String("to", "", "target remote file or directory path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target, err := normalizeRemotePath(*targetPath)
	if err != nil {
		return err
	}

	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	node, err := session.resolveFileNode(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}
	moved, err := session.apiClient.MoveFileWithDevice(ctx, session.accessToken, node.ID, target, session.workspace.DeviceID)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "moved: %s -> %s\n", node.Path, moved.Path)
	fmt.Fprintf(stdout, "id: %s\n", moved.ID)
	fmt.Fprintf(stdout, "version: %d\n", moved.Version)
	return nil
}

func runFileMkdir(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file mkdir", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote directory path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	remote, err := normalizeRemotePath(*remotePath)
	if err != nil {
		return err
	}

	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	node, err := session.apiClient.CreateDirectoryWithDevice(ctx, session.accessToken, remote, session.workspace.DeviceID)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created directory: %s\n", node.Path)
	fmt.Fprintf(stdout, "id: %s\n", node.ID)
	fmt.Fprintf(stdout, "version: %d\n", node.Version)
	return nil
}

func runFileDownload(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("file download", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	remotePath := fs.String("remote-path", "", "remote file path")
	fileID := fs.String("file-id", "", "remote file id")
	outputPath := fs.String("output", "", "local output file path")
	byteRange := fs.String("range", "", "HTTP byte range, for example bytes=0-1023")
	ifNoneMatch := fs.String("if-none-match", "", "ETag used for conditional download")
	if err := fs.Parse(args); err != nil {
		return err
	}

	session, err := openFileCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	node, err := session.resolveDownloadFile(ctx, *fileID, *remotePath)
	if err != nil {
		return err
	}
	output, err := session.downloadOutputPath(node, *outputPath)
	if err != nil {
		return err
	}
	result, written, err := downloadFileToPath(ctx, session.apiClient, session.accessToken, node.ID, output, client.DownloadOptions{
		Range:       *byteRange,
		IfNoneMatch: *ifNoneMatch,
	})
	if err != nil {
		return err
	}
	if result.StatusCode == http.StatusNotModified {
		fmt.Fprintf(stdout, "not modified: %s\n", node.Path)
		if strings.TrimSpace(result.ETag) != "" {
			fmt.Fprintf(stdout, "etag: %s\n", result.ETag)
		}
		return nil
	}
	fmt.Fprintf(stdout, "downloaded: %s\n", node.Path)
	fmt.Fprintf(stdout, "output: %s\n", output)
	fmt.Fprintf(stdout, "bytes: %d\n", written)
	if strings.TrimSpace(result.ContentRange) != "" {
		fmt.Fprintf(stdout, "content range: %s\n", result.ContentRange)
	}
	if strings.TrimSpace(result.ETag) != "" {
		fmt.Fprintf(stdout, "etag: %s\n", result.ETag)
	}
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

	restored, err := session.apiClient.RestoreFileVersionWithDevice(ctx, session.accessToken, id, parsedVersion, session.workspace.DeviceID)
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
	root        string
	workspace   workspaceConfig
}

func openFileCommandSession(ctx context.Context, rootPath, workspaceConfigPath, configPath string) (fileCommandSession, error) {
	root, workspace, _, err := loadWorkspace(rootPath, workspaceConfigPath)
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
		root:        root,
		workspace:   workspace,
	}, nil
}

func (s fileCommandSession) resolveFileListParent(ctx context.Context, parentID, remotePath string) (*string, error) {
	if trimmed := strings.TrimSpace(parentID); trimmed != "" {
		return &trimmed, nil
	}
	remote := strings.TrimSpace(remotePath)
	if remote == "" {
		return nil, nil
	}
	normalized, err := normalizeRemotePath(remote)
	if err != nil {
		return nil, err
	}
	if normalized == "/" {
		return nil, nil
	}
	node, err := s.apiClient.GetFileByPath(ctx, s.accessToken, normalized)
	if err != nil {
		return nil, err
	}
	if node.NodeType != "directory" {
		return nil, fmt.Errorf("remote path is not a directory: %s", normalized)
	}
	return &node.ID, nil
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

func (s fileCommandSession) resolveDownloadFile(ctx context.Context, fileID, remotePath string) (client.FileNode, error) {
	node, err := s.resolveFileNode(ctx, fileID, remotePath)
	if err != nil {
		return client.FileNode{}, err
	}
	if node.NodeType != "file" {
		if strings.TrimSpace(node.Path) != "" {
			return client.FileNode{}, fmt.Errorf("remote path is not a file: %s", node.Path)
		}
		return client.FileNode{}, fmt.Errorf("file id is not a file: %s", node.ID)
	}
	return node, nil
}

func (s fileCommandSession) resolveFileNode(ctx context.Context, fileID, remotePath string) (client.FileNode, error) {
	id := strings.TrimSpace(fileID)
	remote := strings.TrimSpace(remotePath)
	if id == "" && remote == "" {
		return client.FileNode{}, errors.New("remote path or file id is required")
	}
	if id != "" && remote != "" {
		return client.FileNode{}, errors.New("remote path and file id cannot both be set")
	}
	var (
		node client.FileNode
		err  error
	)
	if id != "" {
		node, err = s.apiClient.GetFile(ctx, s.accessToken, id)
	} else {
		normalized, normalizeErr := normalizeRemotePath(remote)
		if normalizeErr != nil {
			return client.FileNode{}, normalizeErr
		}
		node, err = s.apiClient.GetFileByPath(ctx, s.accessToken, normalized)
	}
	if err != nil {
		return client.FileNode{}, err
	}
	return node, nil
}

func (s fileCommandSession) downloadOutputPath(node client.FileNode, outputPath string) (string, error) {
	output := strings.TrimSpace(outputPath)
	if output == "" {
		localPath, ok, err := localPathForRemote(s.root, s.workspace.RemotePath, node.Path)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errors.New("remote file is outside workspace remote path; pass --output to choose a destination")
		}
		if filepath.Clean(localPath) == filepath.Clean(s.root) {
			return "", errors.New("remote file maps to the workspace root; pass --output to choose a destination")
		}
		return localPath, nil
	}
	abs, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = filepath.Base(node.Path)
		}
		abs = filepath.Join(abs, name)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func downloadFileToPath(ctx context.Context, apiClient *client.Client, accessToken, fileID, outputPath string, opts client.DownloadOptions) (client.DownloadResult, int64, error) {
	result, err := apiClient.DownloadFile(ctx, accessToken, fileID, opts)
	if err != nil {
		return client.DownloadResult{}, 0, err
	}
	defer result.Body.Close()
	if result.StatusCode == http.StatusNotModified {
		return result, 0, nil
	}
	written, err := writeStreamAtomically(outputPath, result.Body)
	return result, written, err
}

func writeStreamAtomically(outputPath string, r io.Reader) (int64, error) {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
		return 0, fmt.Errorf("output path is a directory: %s", outputPath)
	} else if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".synchub-download-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	written, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	if err := replaceFile(tmpName, outputPath); err != nil {
		return written, err
	}
	removeTmp = false
	return written, nil
}

func replaceFile(tmpName, outputPath string) error {
	if info, err := os.Stat(outputPath); err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path is a directory: %s", outputPath)
		}
		if err := os.Remove(outputPath); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, outputPath)
}

func parseFileVersionFlag(raw string) (int64, error) {
	version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || version <= 0 {
		return 0, errors.New("version must be a positive integer")
	}
	return version, nil
}

func printFileList(stdout io.Writer, files client.FileList) {
	fmt.Fprintf(stdout, "files: %d\n", len(files.Items))
	for _, file := range files.Items {
		name := file.Name
		if file.NodeType == "directory" {
			name += "/"
		}
		fmt.Fprintf(stdout, "%s %s path=%s size=%d version=%d id=%s\n", file.NodeType, name, file.Path, file.Size, file.Version, file.ID)
	}
	if strings.TrimSpace(files.NextCursor) != "" {
		fmt.Fprintf(stdout, "next cursor: %s\n", files.NextCursor)
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
