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
	StorageQuotaBytes      int64
	UploadChunkSize        int64
	UploadSessionTTL       time.Duration
	UploadCleanupInterval  time.Duration
	VersionCleanupInterval time.Duration
	ObjectGCInterval       time.Duration
	CleanupBatchLimit      int32
	VersionRetention       VersionRetentionPolicy
	TrashRetention         time.Duration
	AccessTokenTTL         time.Duration
	RefreshTokenTTL        time.Duration
	GitHubOAuthClientID    string
	GitHubOAuthSecret      string
	GitHubOAuthRedirectURL string
	LogLevel               string
}

type VersionRetentionPolicy struct {
	MinVersions int64
	MaxAge      time.Duration
}

func Load() Config {
	_ = godotenv.Load()
	databaseURL := os.Getenv("DATABASE_URL")
	databaseDriver := "postgres"
	appEnv := normalizeAppEnv(os.Getenv("APP_ENV"))

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
		StorageQuotaBytes:      getEnvNonNegativeInt64("STORAGE_QUOTA_BYTES", 0),
		UploadChunkSize:        getEnvInt64("UPLOAD_CHUNK_SIZE", 4*1024*1024),
		UploadSessionTTL:       time.Duration(getEnvInt64("UPLOAD_SESSION_TTL_SECONDS", 24*60*60)) * time.Second,
		UploadCleanupInterval:  uploadCleanupInterval,
		VersionCleanupInterval: time.Duration(getEnvInt64("VERSION_CLEANUP_INTERVAL_SECONDS", int64(uploadCleanupInterval/time.Second))) * time.Second,
		ObjectGCInterval:       time.Duration(getEnvInt64("OBJECT_GC_INTERVAL_SECONDS", 60*60)) * time.Second,
		CleanupBatchLimit:      int32(getEnvInt64("CLEANUP_BATCH_LIMIT", 1000)),
		VersionRetention: VersionRetentionPolicy{
			MinVersions: getEnvInt64("VERSION_RETENTION_MIN_VERSIONS", 20),
			MaxAge:      time.Duration(getEnvNonNegativeInt64("VERSION_RETENTION_MAX_AGE_DAYS", 30)) * 24 * time.Hour,
		},
		TrashRetention:         time.Duration(getEnvNonNegativeInt64("TRASH_RETENTION_DAYS", 30)) * 24 * time.Hour,
		AccessTokenTTL:         time.Duration(getEnvInt64("ACCESS_TOKEN_TTL_SECONDS", 15*60)) * time.Second,
		RefreshTokenTTL:        time.Duration(getEnvInt64("REFRESH_TOKEN_TTL_SECONDS", 30*24*60*60)) * time.Second,
		GitHubOAuthClientID:    strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_ID")),
		GitHubOAuthSecret:      strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_SECRET")),
		GitHubOAuthRedirectURL: getEnv("GITHUB_OAUTH_REDIRECT_URL", "https://sync.likanug.app/api/v1/auth/github/callback"),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
	}
}

func normalizeAppEnv(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "production"
	}
	return value
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
