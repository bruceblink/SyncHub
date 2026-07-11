package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bruceblink/SyncHub/internal/api"
	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/config"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
	workersvc "github.com/bruceblink/SyncHub/internal/worker"
	"github.com/bruceblink/SyncHub/migrations"
)

func main() {
	cfg := config.Load()
	configureLogging(cfg.LogLevel)
	ctx := context.Background()

	repo, closeRepo, err := openRepository(ctx, cfg)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer closeRepo()

	store, err := openStorage(cfg)
	if err != nil {
		slog.Error("configure storage", "error", err)
		os.Exit(1)
	}
	authService := authsvc.NewService(repo, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	fileService := filesvc.NewService(repo, store, cfg.UploadChunkSize, cfg.UploadSessionTTL)
	syncService := syncsvc.NewService(repo)
	workerService := workersvc.NewService(repo, store)
	apiServer := api.NewWithSyncAndStorage(authService, fileService, syncService, repo, store)

	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go workerService.RunUploadSessionCleanupLoop(workerCtx, cfg.UploadCleanupInterval, cfg.CleanupBatchLimit, func(err error) {
		slog.Error("upload session cleanup failed", "error", err)
	})
	go workerService.RunUploadChunkCleanupLoop(workerCtx, cfg.UploadCleanupInterval, cfg.CleanupBatchLimit, func(err error) {
		slog.Error("upload chunk cleanup failed", "error", err)
	})
	go workerService.RunFileVersionCleanupLoop(workerCtx, cfg.VersionCleanupInterval, cfg.VersionRetention.MinVersions, cfg.VersionRetention.MaxAge, cfg.CleanupBatchLimit, func(err error) {
		slog.Error("file version cleanup failed", "error", err)
	})

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: apiServer.Handler()}
	go func() {
		slog.Info("SyncHub api listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopWorker()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("api server shutdown failed", "error", err)
		os.Exit(1)
	}
}

func configureLogging(level string) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(level),
	})))
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "", "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type repository interface {
	api.Pinger
	authsvc.Repository
	filesvc.Repository
	syncsvc.Repository
	workersvc.Repository
}

type storageBackend interface {
	storage.Storage
	storage.ReadinessChecker
}

func openRepository(ctx context.Context, cfg config.Config) (repository, func(), error) {
	switch cfg.DatabaseDriver {
	case "sqlite":
		if !config.AllowsSQLite(cfg.AppEnv) {
			return nil, nil, errors.New("sqlite is only allowed when APP_ENV is development, local, or test")
		}
		if cfg.DatabaseURL == "" {
			return nil, nil, errors.New("DATABASE_URL is required for sqlite")
		}
		repo, err := db.OpenSQLite(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		return repo, func() { _ = repo.Close() }, nil
	case "postgres", "postgresql":
		if cfg.DatabaseURL == "" {
			return nil, nil, errors.New("DATABASE_URL is required for postgres")
		}
		pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseSchema)
		if err != nil {
			return nil, nil, err
		}
		if err := db.ApplyPostgresMigrations(ctx, pool, migrations.FS); err != nil {
			pool.Close()
			return nil, nil, err
		}
		return db.NewRepository(pool), pool.Close, nil
	default:
		return nil, nil, errors.New("unsupported database driver: " + cfg.DatabaseDriver)
	}
}

func openStorage(cfg config.Config) (storageBackend, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.StorageBackend)) {
	case "", "local":
		return storage.NewLocal(cfg.LocalStorageRoot), nil
	default:
		return nil, errors.New("unsupported storage backend: " + cfg.StorageBackend)
	}
}
