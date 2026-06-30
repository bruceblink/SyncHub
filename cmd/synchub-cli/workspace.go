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
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := pathpkg.Clean(p)
	if cleaned == "." {
		return "/", nil
	}
	return cleaned, nil
}
