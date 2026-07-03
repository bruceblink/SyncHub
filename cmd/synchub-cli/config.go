package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

type cliConfig struct {
	ServerURL            string           `json:"server_url"`
	User                 client.User      `json:"user"`
	Tokens               client.TokenPair `json:"tokens"`
	AccessTokenExpiresAt time.Time        `json:"access_token_expires_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

func readConfig(path string) (cliConfig, error) {
	if strings.TrimSpace(path) == "" {
		return cliConfig{}, errors.New("config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cliConfig{}, errors.New("not logged in; run synchub-cli register or synchub-cli login first")
		}
		return cliConfig{}, err
	}
	var cfg cliConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cliConfig{}, err
	}
	if cfg.ServerURL == "" || cfg.Tokens.AccessToken == "" {
		return cliConfig{}, errors.New("login config is incomplete; run synchub-cli register or synchub-cli login again")
	}
	return cfg, nil
}

func readConfigWithRefresh(ctx context.Context, path string) (cliConfig, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return cliConfig{}, err
	}
	if !shouldRefreshAccessToken(cfg, time.Now().UTC()) {
		return cfg, nil
	}
	if strings.TrimSpace(cfg.Tokens.RefreshToken) == "" {
		return cliConfig{}, errors.New("refresh token is missing; run synchub-cli login again")
	}
	tokens, err := client.New(cfg.ServerURL).Refresh(ctx, cfg.Tokens.RefreshToken)
	if err != nil {
		return cliConfig{}, err
	}
	cfg.Tokens = tokens
	cfg.AccessTokenExpiresAt = tokens.AccessTokenExpiresAt(time.Now().UTC())
	cfg.UpdatedAt = time.Now().UTC()
	if err := writeConfig(path, cfg); err != nil {
		return cliConfig{}, err
	}
	return cfg, nil
}

func shouldRefreshAccessToken(cfg cliConfig, now time.Time) bool {
	if cfg.AccessTokenExpiresAt.IsZero() {
		return false
	}
	return !cfg.AccessTokenExpiresAt.After(now.Add(time.Minute))
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
