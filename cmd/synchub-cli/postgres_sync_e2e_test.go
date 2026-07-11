package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/api"
	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
	"github.com/bruceblink/SyncHub/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTwoDeviceSyncFlow(t *testing.T) {
	server := newPostgresCLITestServer(t)
	defer server.Close()

	deviceOneRoot := t.TempDir()
	deviceTwoRoot := t.TempDir()
	deviceOneConfig := filepath.Join(deviceOneRoot, ".synchub", "login.json")
	deviceTwoConfig := filepath.Join(deviceTwoRoot, ".synchub", "login.json")

	runCLI(t, "register",
		"--server", server.URL,
		"--email", "two-device@example.com",
		"--password", "password123",
		"--config", deviceOneConfig,
	)
	runCLI(t, "workspace", "init",
		"--path", deviceOneRoot,
		"--remote-path", "/workspace",
		"--config", deviceOneConfig,
	)
	if err := os.WriteFile(filepath.Join(deviceOneRoot, "shared.txt"), []byte("from device one"), 0o644); err != nil {
		t.Fatalf("write device one file: %v", err)
	}
	runCLI(t, "sync", "once",
		"--path", deviceOneRoot,
		"--config", deviceOneConfig,
		"--device-name", "device-one",
		"--platform", "test",
	)

	runCLI(t, "login",
		"--server", server.URL,
		"--email", "two-device@example.com",
		"--password", "password123",
		"--config", deviceTwoConfig,
	)
	runCLI(t, "workspace", "init",
		"--path", deviceTwoRoot,
		"--remote-path", "/workspace",
		"--config", deviceTwoConfig,
	)
	runCLI(t, "sync", "once",
		"--path", deviceTwoRoot,
		"--config", deviceTwoConfig,
		"--device-name", "device-two",
		"--platform", "test",
	)
	assertFileContent(t, filepath.Join(deviceTwoRoot, "shared.txt"), "from device one")

	if err := os.WriteFile(filepath.Join(deviceTwoRoot, "shared.txt"), []byte("from device two"), 0o644); err != nil {
		t.Fatalf("write device two file: %v", err)
	}
	runCLI(t, "sync", "once",
		"--path", deviceTwoRoot,
		"--config", deviceTwoConfig,
		"--device-name", "device-two",
		"--platform", "test",
	)
	runCLI(t, "sync", "once",
		"--path", deviceOneRoot,
		"--config", deviceOneConfig,
		"--device-name", "device-one",
		"--platform", "test",
	)
	assertFileContent(t, filepath.Join(deviceOneRoot, "shared.txt"), "from device two")

	if err := os.Rename(filepath.Join(deviceTwoRoot, "shared.txt"), filepath.Join(deviceTwoRoot, "renamed.txt")); err != nil {
		t.Fatalf("rename device two file: %v", err)
	}
	runCLI(t, "sync", "once",
		"--path", deviceTwoRoot,
		"--config", deviceTwoConfig,
		"--device-name", "device-two",
		"--platform", "test",
	)
	runCLI(t, "sync", "once",
		"--path", deviceOneRoot,
		"--config", deviceOneConfig,
		"--device-name", "device-one",
		"--platform", "test",
	)
	assertFileMissing(t, filepath.Join(deviceOneRoot, "shared.txt"))
	assertFileContent(t, filepath.Join(deviceOneRoot, "renamed.txt"), "from device two")

	if err := os.Remove(filepath.Join(deviceTwoRoot, "renamed.txt")); err != nil {
		t.Fatalf("delete device two file: %v", err)
	}
	runCLI(t, "sync", "once",
		"--path", deviceTwoRoot,
		"--config", deviceTwoConfig,
		"--device-name", "device-two",
		"--platform", "test",
	)
	runCLI(t, "sync", "once",
		"--path", deviceOneRoot,
		"--config", deviceOneConfig,
		"--device-name", "device-one",
		"--platform", "test",
	)
	assertFileMissing(t, filepath.Join(deviceOneRoot, "renamed.txt"))
}

func newPostgresCLITestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(adminPool.Close)
	schema := "synchub_cli_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
	})
	pool, err := db.Connect(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("connect test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.ApplyPostgresMigrations(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := db.NewRepository(pool)

	store := storage.NewLocal(t.TempDir())
	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, store, 4*1024*1024, 24*time.Hour)
	syncService := syncsvc.NewService(repo)
	server := api.NewWithSyncAndStorage(authService, fileService, syncService, repo, store)
	return httptest.NewServer(server.Handler())
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("synchub-cli %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("%s = %q, want %q", path, string(raw), want)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists or stat failed: %v", path, err)
	}
}
