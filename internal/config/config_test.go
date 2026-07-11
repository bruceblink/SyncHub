package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadReadsDotEnvWithoutOverridingProcessEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	dotEnv := "APP_ENV=local\nDATABASE_DRIVER=postgres\nDATABASE_URL=postgres://dotenv/synchub\nHTTP_ADDR=:9999\n"
	if err := os.WriteFile(filepath.Join(".env"), []byte(dotEnv), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	for _, key := range []string{"APP_ENV", "DATABASE_DRIVER", "DATABASE_URL"} {
		oldValue, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, oldValue)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	t.Setenv("HTTP_ADDR", ":8766")

	cfg := Load()
	if cfg.DatabaseURL != "postgres://dotenv/synchub" || cfg.DatabaseDriver != "postgres" || cfg.AppEnv != "local" {
		t.Fatalf("dotenv database config = %#v", cfg)
	}
	if cfg.HTTPAddr != ":8766" {
		t.Fatalf("http addr = %q, want process environment value", cfg.HTTPAddr)
	}
}

func TestLoadDefaultsToProductionPostgres(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("UPLOAD_CLEANUP_INTERVAL_SECONDS", "")
	t.Setenv("VERSION_CLEANUP_INTERVAL_SECONDS", "")
	t.Setenv("CLEANUP_BATCH_LIMIT", "")
	t.Setenv("VERSION_RETENTION_MIN_VERSIONS", "")
	t.Setenv("VERSION_RETENTION_MAX_AGE_DAYS", "")
	t.Setenv("STORAGE_QUOTA_BYTES", "")
	t.Setenv("TRASH_RETENTION_DAYS", "")

	cfg := Load()
	if cfg.AppEnv != "production" {
		t.Fatalf("app env = %q, want production", cfg.AppEnv)
	}
	if cfg.DatabaseDriver != "postgres" {
		t.Fatalf("database driver = %q, want postgres", cfg.DatabaseDriver)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("database url = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.HTTPAddr != ":8765" {
		t.Fatalf("http addr = %q, want :8765", cfg.HTTPAddr)
	}
	if cfg.UploadCleanupInterval <= 0 {
		t.Fatalf("upload cleanup interval = %s, want positive duration", cfg.UploadCleanupInterval)
	}
	if cfg.VersionCleanupInterval != cfg.UploadCleanupInterval {
		t.Fatalf("version cleanup interval = %s, want upload cleanup interval %s", cfg.VersionCleanupInterval, cfg.UploadCleanupInterval)
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
	if cfg.StorageQuotaBytes != 0 {
		t.Fatalf("storage quota = %d, want unlimited", cfg.StorageQuotaBytes)
	}
	if cfg.TrashRetention != 30*24*time.Hour {
		t.Fatalf("trash retention = %s, want 720h", cfg.TrashRetention)
	}
}

func TestLoadTrashRetentionCanBeOverriddenOrDisabled(t *testing.T) {
	t.Setenv("TRASH_RETENTION_DAYS", "14")
	if got := Load().TrashRetention; got != 14*24*time.Hour {
		t.Fatalf("trash retention = %s, want 336h", got)
	}
	t.Setenv("TRASH_RETENTION_DAYS", "0")
	if got := Load().TrashRetention; got != 0 {
		t.Fatalf("disabled trash retention = %s, want 0", got)
	}
}

func TestLoadStorageQuotaOverride(t *testing.T) {
	t.Setenv("STORAGE_QUOTA_BYTES", "1073741824")

	cfg := Load()
	if cfg.StorageQuotaBytes != 1073741824 {
		t.Fatalf("storage quota = %d, want 1073741824", cfg.StorageQuotaBytes)
	}
}

func TestLoadDefaultsToSQLiteWithoutDatabaseURLInLocalOrTestEnvironment(t *testing.T) {
	for _, appEnv := range []string{"local", "test"} {
		t.Run(appEnv, func(t *testing.T) {
			t.Setenv("APP_ENV", appEnv)
			t.Setenv("DATABASE_DRIVER", "")
			t.Setenv("DATABASE_URL", "")

			cfg := Load()
			if cfg.DatabaseDriver != "sqlite" || cfg.DatabaseURL != "./.data/synchub.db" || !AllowsSQLite(cfg.AppEnv) {
				t.Fatalf("%s sqlite config = %#v", appEnv, cfg)
			}
		})
	}
}

func TestLoadKeepsExplicitPostgresWithoutDatabaseURLInLocalEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_URL", "")

	cfg := Load()
	if cfg.DatabaseDriver != "postgres" || cfg.DatabaseURL != "" {
		t.Fatalf("explicit postgres config = %#v", cfg)
	}
}

func TestLoadInfersPostgresFromURL(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/synchub")
	t.Setenv("DATABASE_SCHEMA", " smoke_schema ")

	cfg := Load()
	if cfg.DatabaseDriver != "postgres" {
		t.Fatalf("database driver = %q, want postgres", cfg.DatabaseDriver)
	}
	if cfg.DatabaseSchema != "smoke_schema" {
		t.Fatalf("database schema = %q, want smoke_schema", cfg.DatabaseSchema)
	}
}

func TestLoadVersionRetentionOverrides(t *testing.T) {
	t.Setenv("UPLOAD_CLEANUP_INTERVAL_SECONDS", "3600")
	t.Setenv("VERSION_CLEANUP_INTERVAL_SECONDS", "1800")
	t.Setenv("VERSION_RETENTION_MIN_VERSIONS", "7")
	t.Setenv("VERSION_RETENTION_MAX_AGE_DAYS", "14")
	t.Setenv("CLEANUP_BATCH_LIMIT", "250")

	cfg := Load()
	if cfg.VersionCleanupInterval != 30*time.Minute {
		t.Fatalf("version cleanup interval = %s, want 30m", cfg.VersionCleanupInterval)
	}
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
