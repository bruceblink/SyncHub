package main

import (
	"context"
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
	Version    int       `json:"version"`
	Root       string    `json:"root"`
	RemotePath string    `json:"remote_path"`
	ServerURL  string    `json:"server_url"`
	UserID     string    `json:"user_id"`
	UserEmail  string    `json:"user_email"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
}

func printWorkspaceUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli workspace init --path . --remote-path /workspace")
}

func printManifestUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli manifest scan --path .")
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
