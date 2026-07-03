package main

import (
	"context"
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
	return writeAuthConfig(stdout, *configPath, apiClient, data, "logged in as")
}

func runRegister(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", envOrDefault("SYNCHUB_SERVER", defaultServerURL), "SyncHub API server URL")
	email := fs.String("email", os.Getenv("SYNCHUB_EMAIL"), "register email")
	password := fs.String("password", os.Getenv("SYNCHUB_PASSWORD"), "register password")
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
	data, err := apiClient.Register(ctx, *email, *password)
	if err != nil {
		return err
	}
	return writeAuthConfig(stdout, *configPath, apiClient, data, "registered as")
}

func runLogout(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
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
	fmt.Fprintln(stdout, "logged out")
	fmt.Fprintf(stdout, "config removed: %s\n", *configPath)
	return nil
}

func writeAuthConfig(stdout io.Writer, configPath string, apiClient *client.Client, data client.LoginData, successPrefix string) error {
	now := time.Now().UTC()
	cfg := cliConfig{
		ServerURL:            apiClient.BaseURL,
		User:                 data.User,
		Tokens:               data.Tokens,
		AccessTokenExpiresAt: data.Tokens.AccessTokenExpiresAt(now),
		UpdatedAt:            now,
	}
	if err := writeConfig(configPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s %s\n", successPrefix, data.User.Email)
	fmt.Fprintf(stdout, "config: %s\n", configPath)
	return nil
}
