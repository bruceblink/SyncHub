package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bruceblink/SyncHub/internal/config"
)

func TestOpenRepositoryUsesSQLite(t *testing.T) {
	repo, closeRepo, err := openRepository(context.Background(), config.Config{
		DatabaseDriver: "sqlite",
		DatabaseURL:    filepath.Join(t.TempDir(), "synchub.db"),
	})
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	defer closeRepo()

	if err := repo.Ping(context.Background()); err != nil {
		t.Fatalf("ping sqlite repository: %v", err)
	}
}

func TestOpenRepositoryRequiresPostgresURL(t *testing.T) {
	_, _, err := openRepository(context.Background(), config.Config{DatabaseDriver: "postgres"})
	if err == nil {
		t.Fatal("expected postgres without database url to fail")
	}
}
