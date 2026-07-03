package config

import (
	"testing"
	"time"
)

func TestLoadDefaultsToSQLite(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("UPLOAD_CLEANUP_INTERVAL_SECONDS", "")
	t.Setenv("CLEANUP_BATCH_LIMIT", "")
	t.Setenv("VERSION_RETENTION_MIN_VERSIONS", "")
	t.Setenv("VERSION_RETENTION_MAX_AGE_DAYS", "")

	cfg := Load()
	if cfg.DatabaseDriver != "sqlite" {
		t.Fatalf("database driver = %q, want sqlite", cfg.DatabaseDriver)
	}
	if cfg.DatabaseURL != "./.data/synchub.db" {
		t.Fatalf("database url = %q, want default sqlite path", cfg.DatabaseURL)
	}
	if cfg.HTTPAddr != ":8765" {
		t.Fatalf("http addr = %q, want :8765", cfg.HTTPAddr)
	}
	if cfg.UploadCleanupInterval <= 0 {
		t.Fatalf("upload cleanup interval = %s, want positive duration", cfg.UploadCleanupInterval)
	}
	if cfg.CleanupBatchLimit != 1000 {
		t.Fatalf("cleanup batch limit = %d, want 1000", cfg.CleanupBatchLimit)
	}
	if cfg.VersionRetention.MinVersions != 20 {
		t.Fatalf("version retention min versions = %d, want 20", cfg.VersionRetention.MinVersions)
	}
	if cfg.VersionRetention.MaxAge != 30*24*time.Hour {
		t.Fatalf("version retention max age = %s, want 720h", cfg.VersionRetention.MaxAge)
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

func TestLoadVersionRetentionOverrides(t *testing.T) {
	t.Setenv("VERSION_RETENTION_MIN_VERSIONS", "7")
	t.Setenv("VERSION_RETENTION_MAX_AGE_DAYS", "14")
	t.Setenv("CLEANUP_BATCH_LIMIT", "250")

	cfg := Load()
	if cfg.CleanupBatchLimit != 250 {
		t.Fatalf("cleanup batch limit = %d, want 250", cfg.CleanupBatchLimit)
	}
	if cfg.VersionRetention.MinVersions != 7 {
		t.Fatalf("version retention min versions = %d, want 7", cfg.VersionRetention.MinVersions)
	}
	if cfg.VersionRetention.MaxAge != 14*24*time.Hour {
		t.Fatalf("version retention max age = %s, want 336h", cfg.VersionRetention.MaxAge)
	}
}

func TestLoadVersionRetentionMaxAgeCanDisableCleanup(t *testing.T) {
	t.Setenv("VERSION_RETENTION_MAX_AGE_DAYS", "0")

	cfg := Load()
	if cfg.VersionRetention.MaxAge != 0 {
		t.Fatalf("version retention max age = %s, want disabled", cfg.VersionRetention.MaxAge)
	}
}
