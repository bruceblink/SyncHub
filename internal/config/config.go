package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	JWTSecret        string
	StorageBackend   string
	LocalStorageRoot string
	UploadChunkSize  int64
	UploadSessionTTL time.Duration
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	LogLevel         string
}

func Load() Config {
	return Config{
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		JWTSecret:        getEnv("JWT_SECRET", "dev-secret-change-me"),
		StorageBackend:   getEnv("STORAGE_BACKEND", "local"),
		LocalStorageRoot: getEnv("LOCAL_STORAGE_ROOT", "./.data/storage"),
		UploadChunkSize:  getEnvInt64("UPLOAD_CHUNK_SIZE", 4*1024*1024),
		UploadSessionTTL: time.Duration(getEnvInt64("UPLOAD_SESSION_TTL_SECONDS", 24*60*60)) * time.Second,
		AccessTokenTTL:   time.Duration(getEnvInt64("ACCESS_TOKEN_TTL_SECONDS", 15*60)) * time.Second,
		RefreshTokenTTL:  time.Duration(getEnvInt64("REFRESH_TOKEN_TTL_SECONDS", 30*24*60*60)) * time.Second,
		LogLevel:         getEnv("LOG_LEVEL", "info"),
	}
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
