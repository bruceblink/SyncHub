package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr              string
	DatabaseDriver        string
	DatabaseURL           string
	JWTSecret             string
	StorageBackend        string
	LocalStorageRoot      string
	UploadChunkSize       int64
	UploadSessionTTL      time.Duration
	UploadCleanupInterval time.Duration
	CleanupBatchLimit     int32
	VersionRetention      VersionRetentionPolicy
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	LogLevel              string
}

type VersionRetentionPolicy struct {
	MinVersions int64
	MaxAge      time.Duration
}

func Load() Config {
	databaseURL := os.Getenv("DATABASE_URL")
	databaseDriver := strings.ToLower(strings.TrimSpace(os.Getenv("DATABASE_DRIVER")))
	if databaseDriver == "" {
		databaseDriver = inferDatabaseDriver(databaseURL)
	}
	if databaseURL == "" && databaseDriver == "sqlite" {
		databaseURL = "./.data/synchub.db"
	}

	return Config{
		HTTPAddr:              getEnv("HTTP_ADDR", ":8765"),
		DatabaseDriver:        databaseDriver,
		DatabaseURL:           databaseURL,
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-me"),
		StorageBackend:        getEnv("STORAGE_BACKEND", "local"),
		LocalStorageRoot:      getEnv("LOCAL_STORAGE_ROOT", "./.data/storage"),
		UploadChunkSize:       getEnvInt64("UPLOAD_CHUNK_SIZE", 4*1024*1024),
		UploadSessionTTL:      time.Duration(getEnvInt64("UPLOAD_SESSION_TTL_SECONDS", 24*60*60)) * time.Second,
		UploadCleanupInterval: time.Duration(getEnvInt64("UPLOAD_CLEANUP_INTERVAL_SECONDS", 60*60)) * time.Second,
		CleanupBatchLimit:     int32(getEnvInt64("CLEANUP_BATCH_LIMIT", 1000)),
		VersionRetention: VersionRetentionPolicy{
			MinVersions: getEnvInt64("VERSION_RETENTION_MIN_VERSIONS", 20),
			MaxAge:      time.Duration(getEnvInt64("VERSION_RETENTION_MAX_AGE_DAYS", 30)) * 24 * time.Hour,
		},
		AccessTokenTTL:  time.Duration(getEnvInt64("ACCESS_TOKEN_TTL_SECONDS", 15*60)) * time.Second,
		RefreshTokenTTL: time.Duration(getEnvInt64("REFRESH_TOKEN_TTL_SECONDS", 30*24*60*60)) * time.Second,
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}
}

func inferDatabaseDriver(databaseURL string) string {
	lower := strings.ToLower(strings.TrimSpace(databaseURL))
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return "postgres"
	}
	return "sqlite"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
