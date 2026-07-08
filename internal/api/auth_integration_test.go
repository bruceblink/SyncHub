package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	"github.com/bruceblink/SyncHub/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthRegisterAndLogin(t *testing.T) {
	repo := newTestRepository(t)
	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	email := "user-" + uuid.NewString() + "@example.com"
	registerBody := []byte(fmt.Sprintf(`{"email":%q,"password":"password123"}`, email))
	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registerRec.Code, registerRec.Body.String())
	}
	var registerResp struct {
		Code    float64 `json:"code"`
		Message string  `json:"message"`
		Data    struct {
			Tokens struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registerRec.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if registerResp.Data.Tokens.AccessToken == "" || registerResp.Data.Tokens.RefreshToken == "" {
		t.Fatalf("register response missing tokens: %#v", registerResp)
	}

	loginBody := []byte(fmt.Sprintf(`{"email":%q,"password":"password123"}`, email))
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", loginRec.Code, loginRec.Body.String())
	}
}

func newTestRepository(t *testing.T) *db.Repository {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "synchub_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = make(map[string]string)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("%s,public", schema)
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "set search_path to "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	var activeSchema string
	if err := pool.QueryRow(ctx, "select current_schema()").Scan(&activeSchema); err != nil {
		t.Fatalf("read active test schema: %v", err)
	}
	if activeSchema != schema {
		t.Fatalf("active test schema = %q, want %q", activeSchema, schema)
	}
	if err := db.ApplyPostgresMigrations(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db.NewRepository(pool)
}
