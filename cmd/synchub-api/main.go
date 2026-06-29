package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bruceblink/SyncHub/internal/api"
	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/config"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	if pool != nil {
		defer pool.Close()
	}

	repo := db.NewRepository(pool)
	store := storage.NewLocal(cfg.LocalStorageRoot)
	authService := authsvc.NewService(repo, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	fileService := filesvc.NewService(repo, store, cfg.UploadChunkSize, cfg.UploadSessionTTL)
	apiServer := api.New(authService, fileService, repo)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("api server shutdown failed", "error", err)
		os.Exit(1)
	}
}
