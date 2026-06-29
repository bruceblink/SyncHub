package config

import "testing"

func TestLoadDefaultsToSQLite(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "")

	cfg := Load()
	if cfg.DatabaseDriver != "sqlite" {
		t.Fatalf("database driver = %q, want sqlite", cfg.DatabaseDriver)
	}
	if cfg.DatabaseURL != "./.data/synchub.db" {
		t.Fatalf("database url = %q, want default sqlite path", cfg.DatabaseURL)
	}
}

func TestLoadInfersPostgresFromURL(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/synchub")

	cfg := Load()
	if cfg.DatabaseDriver != "postgres" {
		t.Fatalf("database driver = %q, want postgres", cfg.DatabaseDriver)
	}
}
