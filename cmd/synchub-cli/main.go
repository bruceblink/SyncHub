package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

const defaultServerURL = "http://localhost:8080"

type cliConfig struct {
	ServerURL            string           `json:"server_url"`
	User                 client.User      `json:"user"`
	Tokens               client.TokenPair `json:"tokens"`
	AccessTokenExpiresAt time.Time        `json:"access_token_expires_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

type workspaceConfig struct {
	Version             int       `json:"version"`
	Root                string    `json:"root"`
	RemotePath          string    `json:"remote_path"`
	ServerURL           string    `json:"server_url"`
	UserID              string    `json:"user_id"`
	UserEmail           string    `json:"user_email"`
	DeviceID            string    `json:"device_id,omitempty"`
	DeviceName          string    `json:"device_name,omitempty"`
	DevicePlatform      string    `json:"device_platform,omitempty"`
	LastAppliedChangeID int64     `json:"last_applied_change_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("command is required")
	}
	switch args[0] {
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr)
	case "workspace":
		return runWorkspace(args[1:], stdout, stderr)
	case "manifest":
		return runManifest(ctx, args[1:], stdout, stderr)
	case "sync":
		return runSync(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", envOrDefault("SYNCHUB_SERVER", defaultServerURL), "SyncHub API server URL")
	email := fs.String("email", os.Getenv("SYNCHUB_EMAIL"), "login email")
	password := fs.String("password", os.Getenv("SYNCHUB_PASSWORD"), "login password")
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" {
		return errors.New("email is required")
	}
	if *password == "" {
		return errors.New("password is required")
	}

	apiClient := client.New(*serverURL)
	data, err := apiClient.Login(ctx, *email, *password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	cfg := cliConfig{
		ServerURL:            apiClient.BaseURL,
		User:                 data.User,
		Tokens:               data.Tokens,
		AccessTokenExpiresAt: data.Tokens.AccessTokenExpiresAt(now),
		UpdatedAt:            now,
	}
	if err := writeConfig(*configPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "logged in as %s\n", data.User.Email)
	fmt.Fprintf(stdout, "config: %s\n", *configPath)
	return nil
}

func runWorkspace(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printWorkspaceUsage(stderr)
		return errors.New("workspace command is required")
	}
	switch args[0] {
	case "init":
		return runWorkspaceInit(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printWorkspaceUsage(stdout)
		return nil
	default:
		printWorkspaceUsage(stderr)
		return fmt.Errorf("unknown workspace command: %s", args[0])
	}
}

func runWorkspaceInit(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	remotePath := fs.String("remote-path", "", "remote workspace path")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	loginConfig, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	remote := *remotePath
	if strings.TrimSpace(remote) == "" {
		remote = defaultRemotePath(root)
	}
	normalizedRemote, err := normalizeRemotePath(remote)
	if err != nil {
		return err
	}
	outputPath := *workspaceConfigPath
	if strings.TrimSpace(outputPath) == "" {
		outputPath = filepath.Join(root, ".synchub", "workspace.json")
	}

	now := time.Now().UTC()
	cfg := workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: normalizedRemote,
		ServerURL:  loginConfig.ServerURL,
		UserID:     loginConfig.User.ID,
		UserEmail:  loginConfig.User.Email,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := writeJSONFile(outputPath, cfg, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "workspace initialized: %s\n", root)
	fmt.Fprintf(stdout, "remote path: %s\n", normalizedRemote)
	fmt.Fprintf(stdout, "config: %s\n", outputPath)
	return nil
}

func runSync(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printSyncUsage(stderr)
		return errors.New("sync command is required")
	}
	switch args[0] {
	case "status":
		return runSyncStatus(args[1:], stdout, stderr)
	case "push":
		return runSyncPush(ctx, args[1:], stdout, stderr)
	case "pull":
		return runSyncPull(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printSyncUsage(stdout)
		return nil
	default:
		printSyncUsage(stderr)
		return fmt.Errorf("unknown sync command: %s", args[0])
	}
}

func runSyncStatus(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	configPath := *workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
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
		fmt.Fprintf(stdout, "last applied change: %d\n", workspace.LastAppliedChangeID)
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "manifest: missing")
			fmt.Fprintln(stdout, "next: run synchub-cli manifest scan --path .")
			return nil
		}
		return err
	}
	fmt.Fprintf(stdout, "manifest: %s\n", localManifestPath)
	fmt.Fprintf(stdout, "files: %d\n", len(m.Items))
	fmt.Fprintf(stdout, "last scan: %s\n", m.GeneratedAt.Format(time.RFC3339))
	return nil
}

func runSyncPull(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	deviceName := fs.String("device-name", "", "device name")
	devicePlatform := fs.String("platform", runtime.GOOS, "device platform")
	limit := fs.Int("limit", 500, "maximum changes to pull")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	root, workspace, workspacePath, err := loadWorkspace(*rootPath, *workspaceConfigPath)
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
	changed, err := ensureWorkspaceDevice(ctx, apiClient, loginConfig.Tokens.AccessToken, root, &workspace, *deviceName, *devicePlatform)
	if err != nil {
		return err
	}
	if changed {
		if err := writeWorkspaceConfig(workspacePath, workspace); err != nil {
			return err
		}
	}

	changes, err := apiClient.ListChanges(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, workspace.LastAppliedChangeID, int32(*limit))
	if err != nil {
		return err
	}
	files, dirs := 0, 0
	for _, event := range changes.Items {
		result, err := applyChangeEvent(ctx, apiClient, loginConfig.Tokens.AccessToken, root, workspace.RemotePath, event)
		if err != nil {
			return err
		}
		files += result.files
		dirs += result.dirs
	}
	nextCursor := changes.NextCursor
	if nextCursor == 0 && len(changes.Items) > 0 {
		nextCursor = changes.Items[len(changes.Items)-1].ID
	}
	if nextCursor > workspace.LastAppliedChangeID {
		device, err := apiClient.AckChanges(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, nextCursor)
		if err != nil {
			return err
		}
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		if workspace.LastAppliedChangeID < nextCursor {
			workspace.LastAppliedChangeID = nextCursor
		}
		if err := writeWorkspaceConfig(workspacePath, workspace); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "pulled: %d files\n", files)
	fmt.Fprintf(stdout, "directories: %d\n", dirs)
	fmt.Fprintf(stdout, "cursor: %d\n", workspace.LastAppliedChangeID)
	return nil
}

func runSyncPush(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, workspace, localManifestPath, err := loadWorkspaceAndManifestPath(*rootPath, *workspaceConfigPath, *manifestPath)
	if err != nil {
		return err
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("manifest is missing; run synchub-cli manifest scan first")
		}
		return err
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		return fmt.Errorf("manifest root %s does not match workspace root %s", m.Root, root)
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
	createdDirs := map[string]struct{}{}
	uploaded := 0
	for _, item := range m.Items {
		if err := pushManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, root, item, createdDirs); err != nil {
			return err
		}
		uploaded++
	}
	fmt.Fprintf(stdout, "uploaded: %d files\n", uploaded)
	return nil
}

func runManifest(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printManifestUsage(stderr)
		return errors.New("manifest command is required")
	}
	switch args[0] {
	case "scan":
		return runManifestScan(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printManifestUsage(stdout)
		return nil
	default:
		printManifestUsage(stderr)
		return fmt.Errorf("unknown manifest command: %s", args[0])
	}
}

func runManifestScan(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("manifest scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	outputPath := fs.String("output", "", "manifest output file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := resolveWorkspaceRoot(*rootPath)
	if err != nil {
		return err
	}
	configPath := *workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
	if err != nil {
		return err
	}
	if workspace.Root != "" {
		root = workspace.Root
	}
	m, err := manifest.Scan(ctx, root, workspace.RemotePath)
	if err != nil {
		return err
	}
	out := *outputPath
	if strings.TrimSpace(out) == "" {
		out = filepath.Join(root, ".synchub", "manifest.json")
	}
	if err := writeJSONFile(out, m, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "manifest scanned: %d files\n", len(m.Items))
	fmt.Fprintf(stdout, "output: %s\n", out)
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli login --server http://localhost:8080 --email user@example.com --password password")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
}

func printWorkspaceUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
}

func printManifestUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
}

func printSyncUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli sync status --path .")
	fmt.Fprintln(w, "  synchub-cli sync push --path .")
	fmt.Fprintln(w, "  synchub-cli sync pull --path .")
}

func loadWorkspace(rootPath, workspaceConfigPath string) (string, workspaceConfig, string, error) {
	root, err := resolveWorkspaceRoot(rootPath)
	if err != nil {
		return "", workspaceConfig{}, "", err
	}
	configPath := workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
	if err != nil {
		return "", workspaceConfig{}, "", err
	}
	if workspace.Root != "" {
		root = workspace.Root
	}
	return root, workspace, configPath, nil
}

func loadWorkspaceAndManifestPath(rootPath, workspaceConfigPath, manifestPath string) (string, workspaceConfig, string, error) {
	root, workspace, _, err := loadWorkspace(rootPath, workspaceConfigPath)
	if err != nil {
		return "", workspaceConfig{}, "", err
	}
	localManifestPath := manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}
	return root, workspace, localManifestPath, nil
}

func ensureWorkspaceDevice(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace *workspaceConfig, deviceName, platform string) (bool, error) {
	if strings.TrimSpace(deviceName) == "" {
		deviceName = defaultDeviceName(root)
	}
	if strings.TrimSpace(platform) == "" {
		platform = runtime.GOOS
	}
	if strings.TrimSpace(workspace.DeviceID) == "" {
		device, err := apiClient.RegisterDevice(ctx, accessToken, deviceName, platform)
		if err != nil {
			return false, err
		}
		workspace.DeviceID = device.ID
		workspace.DeviceName = device.Name
		workspace.DevicePlatform = device.Platform
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		return true, nil
	}
	device, err := apiClient.HeartbeatDevice(ctx, accessToken, workspace.DeviceID)
	if err != nil {
		return false, err
	}
	changed := false
	if device.LastAppliedChangeID > workspace.LastAppliedChangeID {
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		changed = true
	}
	if workspace.DeviceName == "" && device.Name != "" {
		workspace.DeviceName = device.Name
		changed = true
	}
	if workspace.DevicePlatform == "" && device.Platform != "" {
		workspace.DevicePlatform = device.Platform
		changed = true
	}
	return changed, nil
}

type pullApplyResult struct {
	files int
	dirs  int
}

func applyChangeEvent(ctx context.Context, apiClient *client.Client, accessToken, root, remoteRoot string, event client.ChangeEvent) (pullApplyResult, error) {
	switch event.EventType {
	case "create", "update", "restore":
		localPath, ok, err := localPathForRemote(root, remoteRoot, event.Path)
		if err != nil || !ok {
			return pullApplyResult{}, err
		}
		if event.Version == nil {
			if err := os.MkdirAll(localPath, 0o755); err != nil {
				return pullApplyResult{}, err
			}
			return pullApplyResult{dirs: 1}, nil
		}
		if err := downloadChangeFile(ctx, apiClient, accessToken, event.FileID, localPath); err != nil {
			return pullApplyResult{}, err
		}
		return pullApplyResult{files: 1}, nil
	case "delete", "move":
		return pullApplyResult{}, fmt.Errorf("sync pull does not support %s events yet", event.EventType)
	default:
		return pullApplyResult{}, fmt.Errorf("unsupported change event type: %s", event.EventType)
	}
}

func downloadChangeFile(ctx context.Context, apiClient *client.Client, accessToken, fileID, localPath string) error {
	result, err := apiClient.DownloadFile(ctx, accessToken, fileID, client.DownloadOptions{})
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode == http.StatusNotModified {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".synchub-pull-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, result.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, localPath); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func localPathForRemote(root, remoteRoot, remotePath string) (string, bool, error) {
	remoteRoot, err := normalizeRemotePath(remoteRoot)
	if err != nil {
		return "", false, err
	}
	remotePath, err = normalizeRemotePath(remotePath)
	if err != nil {
		return "", false, err
	}
	var relative string
	switch {
	case remoteRoot == "/":
		relative = strings.TrimPrefix(remotePath, "/")
	case remotePath == remoteRoot:
		relative = ""
	case strings.HasPrefix(remotePath, remoteRoot+"/"):
		relative = strings.TrimPrefix(strings.TrimPrefix(remotePath, remoteRoot), "/")
	default:
		return "", false, nil
	}
	localPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := ensureLocalPathInsideRoot(root, localPath); err != nil {
		return "", false, err
	}
	return localPath, true, nil
}

func ensureLocalPathInsideRoot(root, localPath string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absLocal)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("remote path resolves outside workspace: %s", localPath)
	}
	return nil
}

func writeWorkspaceConfig(path string, cfg workspaceConfig) error {
	cfg.UpdatedAt = time.Now().UTC()
	return writeJSONFile(path, cfg, 0o600)
}

func defaultDeviceName(root string) string {
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return hostname
	}
	name := filepath.Base(filepath.Clean(root))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "synchub-cli"
	}
	return name
}

func pushManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, item manifest.Entry, createdDirs map[string]struct{}) error {
	localPath := filepath.Join(root, filepath.FromSlash(item.RelativePath))
	if err := ensureRemoteDirectories(ctx, apiClient, accessToken, item.Path, createdDirs); err != nil {
		return err
	}
	session, err := apiClient.InitUpload(ctx, accessToken, client.InitUploadRequest{
		Path:   item.Path,
		Size:   item.Size,
		SHA256: item.SHA256,
	}, uploadIdempotencyKey(item))
	if err != nil {
		return err
	}
	if err := uploadFileChunks(ctx, apiClient, accessToken, session.UploadID, localPath, session.ChunkSize); err != nil {
		return err
	}
	_, err = apiClient.CommitUpload(ctx, accessToken, session.UploadID)
	return err
}

func ensureRemoteDirectories(ctx context.Context, apiClient *client.Client, accessToken, filePath string, created map[string]struct{}) error {
	for _, dir := range remoteParentDirs(filePath) {
		if _, ok := created[dir]; ok {
			continue
		}
		if _, err := apiClient.CreateDirectory(ctx, accessToken, dir); err != nil && !isAPIErrorCode(err, "ALREADY_EXISTS") {
			return err
		}
		created[dir] = struct{}{}
	}
	return nil
}

func remoteParentDirs(filePath string) []string {
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(filePath, "\\", "/"), "/"))
	dir := pathpkg.Dir(cleaned)
	if dir == "." || dir == "/" {
		return nil
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	dirs := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		current = pathpkg.Join(current, part)
		dirs = append(dirs, "/"+strings.TrimPrefix(current, "/"))
	}
	return dirs
}

func uploadFileChunks(ctx context.Context, apiClient *client.Client, accessToken, uploadID, localPath string, chunkSize int64) error {
	if chunkSize <= 0 {
		return errors.New("server returned invalid chunk size")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return uploadChunk(ctx, apiClient, accessToken, uploadID, 0, nil)
	}

	buf := make([]byte, int(chunkSize))
	var index int32
	for {
		n, readErr := io.ReadFull(f, buf)
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if err := uploadChunk(ctx, apiClient, accessToken, uploadID, index, buf[:n]); err != nil {
			return err
		}
		index++
		if readErr == io.ErrUnexpectedEOF {
			return nil
		}
	}
}

func uploadChunk(ctx context.Context, apiClient *client.Client, accessToken, uploadID string, index int32, data []byte) error {
	sum := sha256.Sum256(data)
	_, err := apiClient.PutUploadChunk(ctx, accessToken, uploadID, index, bytes.NewReader(data), hex.EncodeToString(sum[:]))
	return err
}

func uploadIdempotencyKey(item manifest.Entry) string {
	return "cli-push:" + item.Path + ":" + item.SHA256
}

func isAPIErrorCode(err error, code string) bool {
	var apiErr *client.Error
	if errors.As(err, &apiErr) {
		if got, ok := apiErr.Code.(string); ok {
			return got == code
		}
	}
	return false
}

func readConfig(path string) (cliConfig, error) {
	if strings.TrimSpace(path) == "" {
		return cliConfig{}, errors.New("config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cliConfig{}, errors.New("not logged in; run synchub-cli login first")
		}
		return cliConfig{}, err
	}
	var cfg cliConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cliConfig{}, err
	}
	if cfg.ServerURL == "" || cfg.Tokens.AccessToken == "" {
		return cliConfig{}, errors.New("login config is incomplete; run synchub-cli login again")
	}
	return cfg, nil
}

func readWorkspaceConfig(path string) (workspaceConfig, error) {
	if strings.TrimSpace(path) == "" {
		return workspaceConfig{}, errors.New("workspace config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return workspaceConfig{}, errors.New("workspace is not initialized; run synchub-cli workspace init first")
		}
		return workspaceConfig{}, err
	}
	var cfg workspaceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return workspaceConfig{}, err
	}
	if cfg.Root == "" || cfg.RemotePath == "" {
		return workspaceConfig{}, errors.New("workspace config is incomplete; run synchub-cli workspace init again")
	}
	return cfg, nil
}

func readManifest(path string) (manifest.Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return manifest.Manifest{}, errors.New("manifest path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest.Manifest{}, err
	}
	var m manifest.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifest.Manifest{}, err
	}
	return m, nil
}

func writeConfig(path string, cfg cliConfig) error {
	return writeJSONFile(path, cfg, 0o600)
}

func writeJSONFile(path string, v any, perm os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, perm)
}

func resolveWorkspaceRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("workspace path is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace path is not a directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

func defaultRemotePath(root string) string {
	name := filepath.Base(filepath.Clean(root))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "/workspace"
	}
	return "/" + name
}

func normalizeRemotePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "", errors.New("remote path is required")
	}
	if strings.ContainsRune(p, 0) {
		return "", errors.New("remote path contains null byte")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := pathpkg.Clean(p)
	if cleaned == "." {
		return "/", nil
	}
	return cleaned, nil
}

func defaultConfigPath() string {
	if v := os.Getenv("SYNCHUB_CONFIG"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".synchub", "config.json")
	}
	return filepath.Join(dir, "SyncHub", "config.json")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
