package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", envOrDefault("SYNCHUB_SERVER", defaultServerURL), "SyncHub API server URL")
	email := fs.String("email", os.Getenv("SYNCHUB_EMAIL"), "login email")
	password := fs.String("password", os.Getenv("SYNCHUB_PASSWORD"), "login password")
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	jsonOutput := fs.Bool("json", false, "print login result as JSON")
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
	return writeAuthConfig(stdout, *configPath, apiClient, data, "login", "logged in as", *jsonOutput)
}

func runRegister(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", envOrDefault("SYNCHUB_SERVER", defaultServerURL), "SyncHub API server URL")
	email := fs.String("email", os.Getenv("SYNCHUB_EMAIL"), "register email")
	password := fs.String("password", os.Getenv("SYNCHUB_PASSWORD"), "register password")
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	jsonOutput := fs.Bool("json", false, "print register result as JSON")
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
	data, err := apiClient.Register(ctx, *email, *password)
	if err != nil {
		return err
	}
	return writeAuthConfig(stdout, *configPath, apiClient, data, "register", "registered as", *jsonOutput)
}

func runLogout(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	jsonOutput := fs.Bool("json", false, "print logout result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Tokens.RefreshToken) == "" {
		return errors.New("refresh token is missing; run synchub-cli login again")
	}
	if err := client.New(cfg.ServerURL).Logout(ctx, cfg.Tokens.RefreshToken); err != nil {
		return err
	}
	if err := os.Remove(*configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if *jsonOutput {
		return writeAuthCommandJSON(stdout, authCommandSnapshot{
			Action: "logout",
			Server: cfg.ServerURL,
			Config: *configPath,
			User:   cfg.User,
		})
	}
	fmt.Fprintln(stdout, "logged out")
	fmt.Fprintf(stdout, "config removed: %s\n", *configPath)
	return nil
}

func writeAuthConfig(stdout io.Writer, configPath string, apiClient *client.Client, data client.LoginData, action, successPrefix string, jsonOutput bool) error {
	now := time.Now().UTC()
	accessTokenExpiresAt := data.Tokens.AccessTokenExpiresAt(now)
	cfg := cliConfig{
		ServerURL:            apiClient.BaseURL,
		User:                 data.User,
		Tokens:               data.Tokens,
		AccessTokenExpiresAt: accessTokenExpiresAt,
		UpdatedAt:            now,
	}
	if err := writeConfig(configPath, cfg); err != nil {
		return err
	}
	if jsonOutput {
		return writeAuthCommandJSON(stdout, authCommandSnapshot{
			Action:               action,
			Server:               apiClient.BaseURL,
			Config:               configPath,
			User:                 data.User,
			AccessTokenExpiresAt: &accessTokenExpiresAt,
		})
	}
	fmt.Fprintf(stdout, "%s %s\n", successPrefix, data.User.Email)
	fmt.Fprintf(stdout, "config: %s\n", configPath)
	return nil
}

type authCommandSnapshot struct {
	Action               string      `json:"action"`
	Server               string      `json:"server"`
	Config               string      `json:"config"`
	User                 client.User `json:"user"`
	AccessTokenExpiresAt *time.Time  `json:"access_token_expires_at,omitempty"`
}

func writeAuthCommandJSON(stdout io.Writer, snapshot authCommandSnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}
