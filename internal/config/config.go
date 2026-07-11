package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr               string
	AppEnv                 string
	DatabaseDriver         string
	DatabaseURL            string
	DatabaseSchema         string
	JWTSecret              string
	StorageBackend         string
	LocalStorageRoot       string
	UploadChunkSize        int64
	UploadSessionTTL       time.Duration
	UploadCleanupInterval  time.Duration
	VersionCleanupInterval time.Duration
	CleanupBatchLimit      int32
	VersionRetention       VersionRetentionPolicy
	AccessTokenTTL         time.Duration
	RefreshTokenTTL        time.Duration
	LogLevel               string
}

type VersionRetentionPolicy struct {
	MinVersions int64
	MaxAge      time.Duration
}

func Load() Config {
	_ = godotenv.Load()
	databaseURL := os.Getenv("DATABASE_URL")
	databaseDriver := strings.ToLower(strings.TrimSpace(os.Getenv("DATABASE_DRIVER")))
	appEnv := normalizeAppEnv(os.Getenv("APP_ENV"))
	if databaseDriver == "" {
		if databaseURL != "" {
			databaseDriver = inferDatabaseDriver(databaseURL)
		} else if AllowsSQLite(appEnv) {
			databaseDriver = "sqlite"
		} else {
			databaseDriver = "postgres"
		}
	}
	if databaseURL == "" && databaseDriver == "sqlite" && AllowsSQLite(appEnv) {
		databaseURL = "./.data/synchub.db"
	}

	uploadCleanupInterval := time.Duration(getEnvInt64("UPLOAD_CLEANUP_INTERVAL_SECONDS", 60*60)) * time.Second

	return Config{
		HTTPAddr:               getEnv("HTTP_ADDR", ":8765"),
		AppEnv:                 appEnv,
		DatabaseDriver:         databaseDriver,
		DatabaseURL:            databaseURL,
		DatabaseSchema:         strings.TrimSpace(os.Getenv("DATABASE_SCHEMA")),
		JWTSecret:              getEnv("JWT_SECRET", "dev-secret-change-me"),
		StorageBackend:         getEnv("STORAGE_BACKEND", "local"),
		LocalStorageRoot:       getEnv("LOCAL_STORAGE_ROOT", "./.data/storage"),
		UploadChunkSize:        getEnvInt64("UPLOAD_CHUNK_SIZE", 4*1024*1024),
		UploadSessionTTL:       time.Duration(getEnvInt64("UPLOAD_SESSION_TTL_SECONDS", 24*60*60)) * time.Second,
		UploadCleanupInterval:  uploadCleanupInterval,
		VersionCleanupInterval: time.Duration(getEnvInt64("VERSION_CLEANUP_INTERVAL_SECONDS", int64(uploadCleanupInterval/time.Second))) * time.Second,
		CleanupBatchLimit:      int32(getEnvInt64("CLEANUP_BATCH_LIMIT", 1000)),
		VersionRetention: VersionRetentionPolicy{
			MinVersions: getEnvInt64("VERSION_RETENTION_MIN_VERSIONS", 20),
			MaxAge:      time.Duration(getEnvNonNegativeInt64("VERSION_RETENTION_MAX_AGE_DAYS", 30)) * 24 * time.Hour,
		},
		AccessTokenTTL:  time.Duration(getEnvInt64("ACCESS_TOKEN_TTL_SECONDS", 15*60)) * time.Second,
		RefreshTokenTTL: time.Duration(getEnvInt64("REFRESH_TOKEN_TTL_SECONDS", 30*24*60*60)) * time.Second,
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}
}

func AllowsSQLite(appEnv string) bool {
	switch normalizeAppEnv(appEnv) {
	case "development", "local", "test":
		return true
	default:
		return false
	}
}

func normalizeAppEnv(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "production"
	}
	return value
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

func getEnvNonNegativeInt64(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}
