package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"
)

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

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("workspace path is required")
	}
	*f = append(*f, value)
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
	case "list":
		return runWorkspaceList(args[1:], stdout, stderr)
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
	var rootPaths stringListFlag
	fs.Var(&rootPaths, "path", "local workspace root; repeat for multiple workspaces")
	remotePath := fs.String("remote-path", "", "remote workspace path")
	remoteRoot := fs.String("remote-root", "", "remote parent path for multiple workspaces")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	jsonOutput := fs.Bool("json", false, "print workspace init result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pathArgs := append([]string{}, rootPaths...)
	pathArgs = append(pathArgs, fs.Args()...)
	if len(pathArgs) == 0 {
		pathArgs = []string{"."}
	}
	if len(pathArgs) > 1 && strings.TrimSpace(*workspaceConfigPath) != "" {
		return errors.New("--workspace-config can only be used with one workspace path")
	}

	loginConfig, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	results, err := buildWorkspaceInitResults(pathArgs, *remotePath, *remoteRoot, *workspaceConfigPath, loginConfig)
	if err != nil {
		return err
	}
	for _, result := range results {
		if err := writeJSONFile(result.Config, result.Workspace, 0o600); err != nil {
			return err
		}
		if err := registerWorkspace(*configPath, result.Config, result.Workspace); err != nil {
			return err
		}
	}
	if *jsonOutput {
		return writeWorkspaceInitJSON(stdout, results)
	}
	if len(results) > 1 {
		fmt.Fprintf(stdout, "workspaces initialized: %d\n", len(results))
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "workspace initialized: %s\n", result.Workspace.Root)
		fmt.Fprintf(stdout, "remote path: %s\n", result.Workspace.RemotePath)
		fmt.Fprintf(stdout, "config: %s\n", result.Config)
	}
	fmt.Fprintln(stdout, "daemon: registered for startup discovery")
	fmt.Fprintln(stdout, "startup command: synchub-cli sync daemon")
	return nil
}

func runWorkspaceList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	jsonOutput := fs.Bool("json", false, "print registered workspaces as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	registry, registryPath, err := readWorkspaceRegistry(*configPath)
	if err != nil {
		return err
	}
	snapshot := workspaceListSnapshot{
		Registry:   registryPath,
		Workspaces: workspaceListEntries(registry.Workspaces),
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(snapshot)
	}
	printWorkspaceListText(stdout, snapshot)
	return nil
}

type workspaceInitResult struct {
	Config    string          `json:"config"`
	Workspace workspaceConfig `json:"workspace"`
}

type workspaceInitSnapshot struct {
	Config    string          `json:"config"`
	Workspace workspaceConfig `json:"workspace"`
}

type workspaceInitBatchSnapshot struct {
	Workspaces []workspaceInitResult `json:"workspaces"`
}

type workspaceListSnapshot struct {
	Registry   string               `json:"registry"`
	Workspaces []workspaceListEntry `json:"workspaces"`
}

type workspaceListEntry struct {
	Root                string    `json:"root"`
	WorkspaceConfigPath string    `json:"workspace_config_path"`
	ConfigPath          string    `json:"config_path"`
	RemotePath          string    `json:"remote_path"`
	ServerURL           string    `json:"server_url"`
	UserEmail           string    `json:"user_email"`
	Available           bool      `json:"available"`
	Reason              string    `json:"reason,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func writeWorkspaceInitJSON(stdout io.Writer, results []workspaceInitResult) error {
	if len(results) == 0 {
		return errors.New("workspace init result is empty")
	}
	if len(results) > 1 {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(workspaceInitBatchSnapshot{Workspaces: results})
	}
	result := results[0]
	snapshot := workspaceInitSnapshot{
		Config:    result.Config,
		Workspace: result.Workspace,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func workspaceListEntries(entries []workspaceRegistryEntry) []workspaceListEntry {
	result := make([]workspaceListEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, workspaceListEntryForRegistry(entry))
	}
	return result
}

func workspaceListEntryForRegistry(entry workspaceRegistryEntry) workspaceListEntry {
	root := cleanAbsPath(entry.Root)
	workspaceConfigPath := cleanAbsPath(entry.WorkspaceConfigPath)
	if workspaceConfigPath == "" && root != "" {
		workspaceConfigPath = defaultWorkspaceConfigPath(root)
	}
	item := workspaceListEntry{
		Root:                root,
		WorkspaceConfigPath: workspaceConfigPath,
		ConfigPath:          cleanAbsPath(entry.ConfigPath),
		RemotePath:          entry.RemotePath,
		ServerURL:           entry.ServerURL,
		UserEmail:           entry.UserEmail,
		Available:           true,
		UpdatedAt:           entry.UpdatedAt,
	}
	if root == "" {
		item.Available = false
		item.Reason = "workspace root is empty"
		return item
	}
	if info, err := os.Stat(root); err != nil {
		item.Available = false
		item.Reason = err.Error()
		return item
	} else if !info.IsDir() {
		item.Available = false
		item.Reason = "workspace root is not a directory"
		return item
	}
	if _, err := readWorkspaceConfig(workspaceConfigPath); err != nil {
		item.Available = false
		item.Reason = err.Error()
	}
	return item
}

func printWorkspaceListText(stdout io.Writer, snapshot workspaceListSnapshot) {
	fmt.Fprintf(stdout, "registry: %s\n", snapshot.Registry)
	fmt.Fprintf(stdout, "workspaces: %d\n", len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		fmt.Fprintf(stdout, "workspace: %s\n", workspace.Root)
		fmt.Fprintf(stdout, "remote path: %s\n", workspace.RemotePath)
		fmt.Fprintf(stdout, "user: %s\n", workspace.UserEmail)
		fmt.Fprintf(stdout, "server: %s\n", workspace.ServerURL)
		fmt.Fprintf(stdout, "config: %s\n", workspace.ConfigPath)
		fmt.Fprintf(stdout, "workspace config: %s\n", workspace.WorkspaceConfigPath)
		if workspace.Available {
			fmt.Fprintln(stdout, "status: ok")
			continue
		}
		fmt.Fprintln(stdout, "status: missing")
		if strings.TrimSpace(workspace.Reason) != "" {
			fmt.Fprintf(stdout, "reason: %s\n", workspace.Reason)
		}
	}
}

func buildWorkspaceInitResults(pathArgs []string, remotePath, remoteRoot, workspaceConfigPath string, loginConfig cliConfig) ([]workspaceInitResult, error) {
	if strings.TrimSpace(remotePath) != "" && strings.TrimSpace(remoteRoot) != "" {
		return nil, errors.New("--remote-path and --remote-root cannot be used together")
	}
	multiple := len(pathArgs) > 1
	if multiple && strings.TrimSpace(remotePath) != "" {
		return nil, errors.New("--remote-path can only be used with one workspace path; use --remote-root for multiple paths")
	}

	results := make([]workspaceInitResult, 0, len(pathArgs))
	roots := make([]string, 0, len(pathArgs))
	remotePaths := make(map[string]string, len(pathArgs))
	now := time.Now().UTC()
	for _, rootPath := range pathArgs {
		root, err := resolveWorkspaceRoot(rootPath)
		if err != nil {
			return nil, err
		}
		for _, existing := range roots {
			if samePath(existing, root) {
				return nil, fmt.Errorf("duplicate workspace path: %s", root)
			}
		}
		remote, err := initRemotePath(root, remotePath, remoteRoot)
		if err != nil {
			return nil, err
		}
		if existing, ok := remotePaths[remote]; ok {
			return nil, fmt.Errorf("remote path %s is already used by %s", remote, existing)
		}
		outputPath := workspaceConfigPath
		if strings.TrimSpace(outputPath) == "" {
			outputPath = defaultWorkspaceConfigPath(root)
		}
		cfg := workspaceConfig{
			Version:    1,
			Root:       root,
			RemotePath: remote,
			ServerURL:  loginConfig.ServerURL,
			UserID:     loginConfig.User.ID,
			UserEmail:  loginConfig.User.Email,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		roots = append(roots, root)
		remotePaths[remote] = root
		results = append(results, workspaceInitResult{
			Config:    outputPath,
			Workspace: cfg,
		})
	}
	return results, nil
}

func initRemotePath(root, remotePath, remoteRoot string) (string, error) {
	if strings.TrimSpace(remotePath) != "" {
		return normalizeRemotePath(remotePath)
	}
	if strings.TrimSpace(remoteRoot) == "" {
		return normalizeRemotePath(defaultRemotePath(root))
	}
	parent, err := normalizeRemotePath(remoteRoot)
	if err != nil {
		return "", err
	}
	return normalizeRemotePath(pathpkg.Join(parent, filepath.Base(filepath.Clean(root))))
}

func loadWorkspace(rootPath, workspaceConfigPath string) (string, workspaceConfig, string, error) {
	root, err := resolveWorkspaceRoot(rootPath)
	if err != nil {
		return "", workspaceConfig{}, "", err
	}
	configPath := workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = defaultWorkspaceConfigPath(root)
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

func defaultWorkspaceConfigPath(root string) string {
	return filepath.Join(root, ".synchub", "workspace.json")
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

func writeWorkspaceConfig(path string, cfg workspaceConfig) error {
	cfg.UpdatedAt = time.Now().UTC()
	return writeJSONFile(path, cfg, 0o600)
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
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", errors.New("remote path traversal is not allowed")
		}
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
