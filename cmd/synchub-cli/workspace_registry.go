package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type workspaceRegistry struct {
	Version    int                      `json:"version"`
	UpdatedAt  time.Time                `json:"updated_at"`
	Workspaces []workspaceRegistryEntry `json:"workspaces"`
}

type workspaceRegistryEntry struct {
	Root                string    `json:"root"`
	WorkspaceConfigPath string    `json:"workspace_config_path"`
	ConfigPath          string    `json:"config_path"`
	RemotePath          string    `json:"remote_path"`
	ServerURL           string    `json:"server_url"`
	UserID              string    `json:"user_id"`
	UserEmail           string    `json:"user_email"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func registerWorkspace(configPath, workspaceConfigPath string, cfg workspaceConfig) error {
	registryPath, err := workspaceRegistryPath(configPath)
	if err != nil {
		return err
	}
	registry, err := readWorkspaceRegistryFile(registryPath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if registry.Version == 0 {
		registry.Version = 1
	}
	registry.UpdatedAt = now

	entry := workspaceRegistryEntry{
		Root:                cleanAbsPath(cfg.Root),
		WorkspaceConfigPath: cleanAbsPath(workspaceConfigPath),
		ConfigPath:          cleanAbsPath(configPath),
		RemotePath:          cfg.RemotePath,
		ServerURL:           cfg.ServerURL,
		UserID:              cfg.UserID,
		UserEmail:           cfg.UserEmail,
		UpdatedAt:           now,
	}
	replaced := false
	for i, existing := range registry.Workspaces {
		if samePath(existing.Root, entry.Root) || samePath(existing.WorkspaceConfigPath, entry.WorkspaceConfigPath) {
			registry.Workspaces[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		registry.Workspaces = append(registry.Workspaces, entry)
	}
	sort.Slice(registry.Workspaces, func(i, j int) bool {
		return registry.Workspaces[i].Root < registry.Workspaces[j].Root
	})
	return writeJSONFile(registryPath, registry, 0o600)
}

func readWorkspaceRegistry(configPath string) (workspaceRegistry, string, error) {
	registryPath, err := workspaceRegistryPath(configPath)
	if err != nil {
		return workspaceRegistry{}, "", err
	}
	registry, err := readWorkspaceRegistryFile(registryPath)
	if err != nil {
		return workspaceRegistry{}, registryPath, err
	}
	return registry, registryPath, nil
}

func readWorkspaceRegistryFile(path string) (workspaceRegistry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return workspaceRegistry{Version: 1, Workspaces: []workspaceRegistryEntry{}}, nil
		}
		return workspaceRegistry{}, err
	}
	var registry workspaceRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return workspaceRegistry{}, err
	}
	if registry.Version == 0 {
		registry.Version = 1
	}
	return registry, nil
}

func workspaceRegistryPath(configPath string) (string, error) {
	if path := strings.TrimSpace(os.Getenv("SYNCHUB_WORKSPACES")); path != "" {
		return cleanAbsPath(path), nil
	}
	if strings.TrimSpace(configPath) != "" {
		return filepath.Join(filepath.Dir(cleanAbsPath(configPath)), "workspaces.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".synchub", "workspaces.json"), nil
	}
	return filepath.Join(dir, "SyncHub", "workspaces.json"), nil
}

func cleanAbsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func samePath(left, right string) bool {
	left = cleanAbsPath(left)
	right = cleanAbsPath(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
