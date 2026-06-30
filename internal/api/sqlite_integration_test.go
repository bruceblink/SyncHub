package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/db"
	"github.com/bruceblink/SyncHub/internal/domain"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
)

func TestSQLiteRepositoryUploadDownloadFlow(t *testing.T) {
	ctx := context.Background()
	repo, err := db.OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "sqlite@example.com",
		"password": "password123",
	})
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registerResp.Code, registerResp.Body.String())
	}
	var registerBody struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registerResp, &registerBody)
	if registerBody.Data.Tokens.AccessToken == "" {
		t.Fatal("register response missing access token")
	}
	token := registerBody.Data.Tokens.AccessToken

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{
		"path": "/workspace",
	})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	content := []byte("hello sqlite")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])

	uploadResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/readme.txt",
		"size":       len(content),
		"sha256":     sha,
		"chunk_size": len(content),
	})
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("init upload status = %d body = %s", uploadResp.Code, uploadResp.Body.String())
	}
	var uploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, uploadResp, &uploadBody)
	if uploadBody.Data.UploadID == "" {
		t.Fatal("upload response missing upload id")
	}

	chunkReq := httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/chunks/0", bytes.NewReader(content))
	chunkReq.Header.Set("Authorization", "Bearer "+token)
	chunkReq.Header.Set("Content-Type", "application/octet-stream")
	chunkReq.Header.Set("X-Chunk-Sha256", sha)
	chunkRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(chunkRec, chunkReq)
	if chunkRec.Code != http.StatusOK {
		t.Fatalf("put chunk status = %d body = %s", chunkRec.Code, chunkRec.Body.String())
	}

	commitResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if commitResp.Code != http.StatusOK {
		t.Fatalf("commit upload status = %d body = %s", commitResp.Code, commitResp.Body.String())
	}
	var commitBody struct {
		Data struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	decodeBody(t, commitResp, &commitBody)
	if commitBody.Data.FileID == "" {
		t.Fatal("commit response missing file id")
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+commitBody.Data.FileID+"/content", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+token)
	downloadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d body = %s", downloadRec.Code, downloadRec.Body.String())
	}
	if got := downloadRec.Body.String(); got != string(content) {
		t.Fatalf("download body = %q", got)
	}
}

func TestSQLiteUploadConflictRecordsSyncConflict(t *testing.T) {
	ctx := context.Background()
	repo, err := db.OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "conflict@example.com",
		"password": "password123",
	})
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registerResp.Code, registerResp.Body.String())
	}
	var registerBody struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registerResp, &registerBody)
	token := registerBody.Data.Tokens.AccessToken

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	initial := []byte("initial")
	initialSHA := sha256.Sum256(initial)
	initialUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/conflict.txt",
		"size":       len(initial),
		"sha256":     hex.EncodeToString(initialSHA[:]),
		"chunk_size": len(initial),
	})
	if initialUpload.Code != http.StatusCreated {
		t.Fatalf("initial upload status = %d body = %s", initialUpload.Code, initialUpload.Body.String())
	}
	var initialUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, initialUpload, &initialUploadBody)
	putChunk(t, server, token, initialUploadBody.Data.UploadID, initial, hex.EncodeToString(initialSHA[:]))
	initialCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+initialUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if initialCommit.Code != http.StatusOK {
		t.Fatalf("initial commit status = %d body = %s", initialCommit.Code, initialCommit.Body.String())
	}

	conflicting := []byte("conflicting")
	conflictingSHA := sha256.Sum256(conflicting)
	conflictUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":         "/workspace/conflict.txt",
		"size":         len(conflicting),
		"sha256":       hex.EncodeToString(conflictingSHA[:]),
		"chunk_size":   len(conflicting),
		"base_version": 0,
	})
	if conflictUpload.Code != http.StatusCreated {
		t.Fatalf("conflict upload status = %d body = %s", conflictUpload.Code, conflictUpload.Body.String())
	}
	var conflictUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, conflictUpload, &conflictUploadBody)
	putChunk(t, server, token, conflictUploadBody.Data.UploadID, conflicting, hex.EncodeToString(conflictingSHA[:]))
	conflictCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+conflictUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if conflictCommit.Code != http.StatusConflict {
		t.Fatalf("conflict commit status = %d body = %s", conflictCommit.Code, conflictCommit.Body.String())
	}

	conflicts, err := repo.ListSyncConflicts(ctx, registerBody.Data.User.ID, domain.ConflictResolutionPending, 10)
	if err != nil {
		t.Fatalf("list sync conflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one", conflicts)
	}
	if conflicts[0].Path != "/workspace/conflict.txt" {
		t.Fatalf("conflict path = %q", conflicts[0].Path)
	}
	if conflicts[0].LocalVersion == nil || *conflicts[0].LocalVersion != 0 {
		t.Fatalf("local version = %#v", conflicts[0].LocalVersion)
	}
	if conflicts[0].RemoteVersion == nil || *conflicts[0].RemoteVersion != 1 {
		t.Fatalf("remote version = %#v", conflicts[0].RemoteVersion)
	}
}

func TestSQLiteUploadInitIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	repo, err := db.OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "idempotent@example.com",
		"password": "password123",
	})
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registerResp.Code, registerResp.Body.String())
	}
	var registerBody struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registerResp, &registerBody)

	sum := sha256.Sum256([]byte("hello"))
	body := map[string]any{
		"path":       "/workspace/idempotent.txt",
		"size":       5,
		"sha256":     hex.EncodeToString(sum[:]),
		"chunk_size": 5,
	}
	first := doJSONWithHeaders(t, server, http.MethodPost, "/api/v1/uploads", registerBody.Data.Tokens.AccessToken, body, map[string]string{
		"Idempotency-Key": "upload-init-1",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first init status = %d body = %s", first.Code, first.Body.String())
	}
	second := doJSONWithHeaders(t, server, http.MethodPost, "/api/v1/uploads", registerBody.Data.Tokens.AccessToken, body, map[string]string{
		"Idempotency-Key": "upload-init-1",
	})
	if second.Code != http.StatusCreated {
		t.Fatalf("second init status = %d body = %s", second.Code, second.Body.String())
	}

	var firstBody, secondBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, first, &firstBody)
	decodeBody(t, second, &secondBody)
	if firstBody.Data.UploadID == "" || firstBody.Data.UploadID != secondBody.Data.UploadID {
		t.Fatalf("upload ids = %q and %q, want same non-empty id", firstBody.Data.UploadID, secondBody.Data.UploadID)
	}
}

func putChunk(t *testing.T, server *Server, token, uploadID string, content []byte, checksum string) {
	t.Helper()
	chunkReq := httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+uploadID+"/chunks/0", bytes.NewReader(content))
	chunkReq.Header.Set("Authorization", "Bearer "+token)
	chunkReq.Header.Set("Content-Type", "application/octet-stream")
	chunkReq.Header.Set("X-Chunk-Sha256", checksum)
	chunkRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(chunkRec, chunkReq)
	if chunkRec.Code != http.StatusOK {
		t.Fatalf("put chunk status = %d body = %s", chunkRec.Code, chunkRec.Body.String())
	}
}

func doJSON(t *testing.T, server *Server, method, target, token string, body any) *httptest.ResponseRecorder {
	return doJSONWithHeaders(t, server, method, target, token, body, nil)
}

func doJSONWithHeaders(t *testing.T, server *Server, method, target, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()

	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v body = %s", err, rec.Body.String())
	}
}
