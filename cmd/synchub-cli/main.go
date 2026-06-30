package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-cli login --server http://localhost:8080 --email user@example.com --password password")
}

func writeConfig(path string, cfg cliConfig) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o600)
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
