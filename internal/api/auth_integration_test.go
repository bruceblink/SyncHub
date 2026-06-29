package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthRegisterAndLogin(t *testing.T) {
	repo := newTestRepository(t)
	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerBody := []byte(`{"email":"user@example.com","password":"password123"}`)
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

	loginBody := []byte(`{"email":"user@example.com","password":"password123"}`)
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
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	recreateSchema(t, pool)
	return db.NewRepository(pool)
}

func recreateSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	root := findRepoRoot(t)
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000001_init.down.sql"))
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000001_init.up.sql"))
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	ctx := context.Background()
	_, _ = pool.Exec(ctx, string(down))
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
