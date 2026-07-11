package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/internal/config"
)

func TestOpenRepositoryRequiresPostgresURL(t *testing.T) {
	_, _, err := openRepository(context.Background(), config.Config{DatabaseDriver: "postgres"})
	if err == nil {
		t.Fatal("expected postgres without database url to fail")
	}
}

func TestOpenStorageUsesLocalBackend(t *testing.T) {
	store, err := openStorage(config.Config{
		StorageBackend:   "LOCAL",
		LocalStorageRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
}

func TestOpenStorageRejectsUnsupportedBackend(t *testing.T) {
	_, err := openStorage(config.Config{StorageBackend: "s3"})
	if err == nil || !strings.Contains(err.Error(), "unsupported storage backend: s3") {
		t.Fatalf("error = %v, want unsupported storage backend", err)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want slog.Level
	}{
		{name: "debug", raw: "debug", want: slog.LevelDebug},
		{name: "info default", raw: "", want: slog.LevelInfo},
		{name: "info mixed case", raw: " INFO ", want: slog.LevelInfo},
		{name: "warn", raw: "warn", want: slog.LevelWarn},
		{name: "warning", raw: "warning", want: slog.LevelWarn},
		{name: "error", raw: "error", want: slog.LevelError},
		{name: "invalid falls back to info", raw: "verbose", want: slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseLogLevel(tt.raw); got != tt.want {
				t.Fatalf("parseLogLevel(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
