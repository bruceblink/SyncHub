package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
)

func TestPostgresRepositoryUploadDownloadFlow(t *testing.T) {
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "postgres@example.com",
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

	content := []byte("hello postgres")
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

func TestPostgresTrashListsAndRestoresDeletedDirectory(t *testing.T) {
	repo := newTestRepository(t)
	server := New(authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour), filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 1024, time.Hour), repo)
	register := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "trash@example.com", "password": "password123"})
	var auth struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, register, &auth)
	token := auth.Data.Tokens.AccessToken
	created := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/deleted"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var node struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, created, &node)
	child := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/deleted/nested"})
	if child.Code != http.StatusCreated {
		t.Fatalf("create child = %d: %s", child.Code, child.Body.String())
	}
	deleted := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+node.Data.ID, token, map[string]any{"base_version": node.Data.Version})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	trash := doJSON(t, server, http.MethodGet, "/api/v1/trash", token, nil)
	if trash.Code != http.StatusOK || !bytes.Contains(trash.Body.Bytes(), []byte(`"path":"/deleted"`)) || bytes.Contains(trash.Body.Bytes(), []byte(`"path":"/deleted/nested"`)) {
		t.Fatalf("trash = %d: %s", trash.Code, trash.Body.String())
	}
	restored := doJSON(t, server, http.MethodPost, "/api/v1/trash/"+node.Data.ID+"/restore", token, map[string]any{})
	if restored.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restored.Code, restored.Body.String())
	}
	active := doJSON(t, server, http.MethodGet, "/api/v1/files", token, nil)
	if active.Code != http.StatusOK || !bytes.Contains(active.Body.Bytes(), []byte(`"path":"/deleted"`)) {
		t.Fatalf("active = %d: %s", active.Code, active.Body.String())
	}
	nested := doJSON(t, server, http.MethodGet, "/api/v1/files/by-path?path=/deleted/nested", token, nil)
	if nested.Code != http.StatusOK {
		t.Fatalf("nested = %d: %s", nested.Code, nested.Body.String())
	}
}

func TestPostgresTrashPurgeDeletesOnlySelectedDirectoryTree(t *testing.T) {
	repo := newTestRepository(t)
	server := New(authsvc.NewService(repo, "test-secret", time.Minute, time.Hour), filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 1024, time.Hour), repo)
	register := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "purge@example.com", "password": "password123"})
	var auth struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, register, &auth)
	token := auth.Data.Tokens.AccessToken

	created := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/deleted"})
	var root struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, created, &root)
	if child := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/deleted/nested"}); child.Code != http.StatusCreated {
		t.Fatalf("create child = %d: %s", child.Code, child.Body.String())
	}
	similar := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/deleted-copy"})
	if similar.Code != http.StatusCreated {
		t.Fatalf("create similar = %d: %s", similar.Code, similar.Body.String())
	}
	if deleted := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+root.Data.ID, token, map[string]any{"base_version": root.Data.Version}); deleted.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}

	purged := doJSON(t, server, http.MethodDelete, "/api/v1/trash/"+root.Data.ID, token, nil)
	if purged.Code != http.StatusOK || !bytes.Contains(purged.Body.Bytes(), []byte(`"purged":true`)) {
		t.Fatalf("purge = %d: %s", purged.Code, purged.Body.String())
	}
	if restore := doJSON(t, server, http.MethodPost, "/api/v1/trash/"+root.Data.ID+"/restore", token, map[string]any{}); restore.Code != http.StatusNotFound {
		t.Fatalf("restore purged = %d: %s", restore.Code, restore.Body.String())
	}
	if repeated := doJSON(t, server, http.MethodDelete, "/api/v1/trash/"+root.Data.ID, token, nil); repeated.Code != http.StatusNotFound {
		t.Fatalf("repeat purge = %d: %s", repeated.Code, repeated.Body.String())
	}
	if unaffected := doJSON(t, server, http.MethodGet, "/api/v1/files/by-path?path=/deleted-copy", token, nil); unaffected.Code != http.StatusOK {
		t.Fatalf("similar path affected = %d: %s", unaffected.Code, unaffected.Body.String())
	}
}

func TestPostgresSearchFilesMatchesNameAndPathWithoutDeletedItems(t *testing.T) {
	repo := newTestRepository(t)
	server := New(authsvc.NewService(repo, "test-secret", time.Minute, time.Hour), filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 1024, time.Hour), repo)
	registered := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "search@example.com", "password": "password123"})
	var auth struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registered, &auth)
	token := auth.Data.Tokens.AccessToken
	for _, path := range []string{"/reports", "/reports/q1-budget", "/archive"} {
		response := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": path})
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	search := doJSON(t, server, http.MethodGet, "/api/v1/files/search?q=budget", token, nil)
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte(`"path":"/reports/q1-budget"`)) {
		t.Fatalf("search = %d: %s", search.Code, search.Body.String())
	}
	byPath := doJSON(t, server, http.MethodGet, "/api/v1/files/search?q=reports", token, nil)
	if byPath.Code != http.StatusOK || !bytes.Contains(byPath.Body.Bytes(), []byte(`"path":"/reports/q1-budget"`)) {
		t.Fatalf("path search = %d: %s", byPath.Code, byPath.Body.String())
	}
	var item struct {
		Data struct {
			Items []struct {
				ID      string `json:"id"`
				Path    string `json:"path"`
				Version int64  `json:"version"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, search, &item)
	deleted := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+item.Data.Items[0].ID, token, map[string]any{"base_version": item.Data.Items[0].Version})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	search = doJSON(t, server, http.MethodGet, "/api/v1/files/search?q=budget", token, nil)
	if search.Code != http.StatusOK || bytes.Contains(search.Body.Bytes(), []byte(`q1-budget`)) {
		t.Fatalf("deleted search = %d: %s", search.Code, search.Body.String())
	}
}

func TestPostgresUsageCountsActiveFilesOnly(t *testing.T) {
	repo := newTestRepository(t)
	server := New(authsvc.NewService(repo, "test-secret", time.Minute, time.Hour), filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 1024, time.Hour), repo)
	registered := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "usage@example.com", "password": "password123"})
	var auth struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registered, &auth)
	token := auth.Data.Tokens.AccessToken
	created := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/docs"})
	if created.Code != http.StatusCreated {
		t.Fatalf("directory = %d", created.Code)
	}
	content := []byte("usage bytes")
	checksum := sha256.Sum256(content)
	init := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{"path": "/docs/file.txt", "size": len(content), "sha256": hex.EncodeToString(checksum[:]), "chunk_size": len(content)})
	var upload struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, init, &upload)
	putChunk(t, server, token, upload.Data.UploadID, content, hex.EncodeToString(checksum[:]))
	committed := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+upload.Data.UploadID+"/commit", token, map[string]any{})
	if committed.Code != http.StatusOK {
		t.Fatalf("commit = %d", committed.Code)
	}
	var commit struct {
		Data struct {
			FileID  string `json:"file_id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, committed, &commit)
	usage := doJSON(t, server, http.MethodGet, "/api/v1/account/usage", token, nil)
	if usage.Code != http.StatusOK || !bytes.Contains(usage.Body.Bytes(), []byte(`"file_count":1`)) || !bytes.Contains(usage.Body.Bytes(), []byte(`"bytes_used":11`)) {
		t.Fatalf("usage = %d: %s", usage.Code, usage.Body.String())
	}
	deleted := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+commit.Data.FileID, token, map[string]any{"base_version": commit.Data.Version})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	usage = doJSON(t, server, http.MethodGet, "/api/v1/account/usage", token, nil)
	if usage.Code != http.StatusOK || !bytes.Contains(usage.Body.Bytes(), []byte(`"file_count":0`)) || !bytes.Contains(usage.Body.Bytes(), []byte(`"bytes_used":0`)) {
		t.Fatalf("usage after delete = %d: %s", usage.Code, usage.Body.String())
	}
}

func TestPostgresUploadEnforcesConfiguredStorageQuota(t *testing.T) {
	repo := newTestRepository(t)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 1024, time.Hour).WithStorageQuota(10)
	server := New(authsvc.NewService(repo, "test-secret", time.Minute, time.Hour), fileService, repo)
	registered := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "quota@example.com", "password": "password123"})
	var auth struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registered, &auth)
	token := auth.Data.Tokens.AccessToken

	usage := doJSON(t, server, http.MethodGet, "/api/v1/account/usage", token, nil)
	if usage.Code != http.StatusOK || !bytes.Contains(usage.Body.Bytes(), []byte(`"quota_bytes":10`)) {
		t.Fatalf("usage = %d: %s", usage.Code, usage.Body.String())
	}
	checksum := sha256.Sum256([]byte("eleven bytes"))
	init := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path": "/too-large.txt", "size": 11, "sha256": hex.EncodeToString(checksum[:]), "chunk_size": 11,
	})
	if init.Code != http.StatusRequestEntityTooLarge || !bytes.Contains(init.Body.Bytes(), []byte(`"code":"STORAGE_QUOTA_EXCEEDED"`)) {
		t.Fatalf("over quota upload = %d: %s", init.Code, init.Body.String())
	}
}

func TestPostgresUploadConflictRecordsSyncConflict(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	syncService := syncsvc.NewService(repo)
	server := NewWithSync(authService, fileService, syncService, repo)

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

	conflictsResp := doJSON(t, server, http.MethodGet, "/api/v1/sync/conflicts?resolution=pending&limit=10", token, nil)
	if conflictsResp.Code != http.StatusOK {
		t.Fatalf("conflicts status = %d body = %s", conflictsResp.Code, conflictsResp.Body.String())
	}
	var conflictsBody struct {
		Data struct {
			Items []struct {
				ID            string `json:"id"`
				Path          string `json:"path"`
				Resolution    string `json:"resolution"`
				LocalVersion  *int64 `json:"local_version"`
				RemoteVersion *int64 `json:"remote_version"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, conflictsResp, &conflictsBody)
	if len(conflictsBody.Data.Items) != 1 || conflictsBody.Data.Items[0].ID != conflicts[0].ID {
		t.Fatalf("conflict response = %#v", conflictsBody.Data.Items)
	}
	if conflictsBody.Data.Items[0].Path != "/workspace/conflict.txt" || conflictsBody.Data.Items[0].Resolution != domain.ConflictResolutionPending {
		t.Fatalf("conflict response item = %#v", conflictsBody.Data.Items[0])
	}

	resolveResp := doJSON(t, server, http.MethodPatch, "/api/v1/sync/conflicts/"+conflicts[0].ID, token, map[string]any{
		"resolution": domain.ConflictResolutionKeepBoth,
	})
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve conflict status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolveBody struct {
		Data struct {
			ID         string     `json:"id"`
			Resolution string     `json:"resolution"`
			ResolvedAt *time.Time `json:"resolved_at"`
		} `json:"data"`
	}
	decodeBody(t, resolveResp, &resolveBody)
	if resolveBody.Data.ID != conflicts[0].ID || resolveBody.Data.Resolution != domain.ConflictResolutionKeepBoth || resolveBody.Data.ResolvedAt == nil {
		t.Fatalf("resolve conflict response = %#v", resolveBody.Data)
	}

	conflicts, err = repo.ListSyncConflicts(ctx, registerBody.Data.User.ID, domain.ConflictResolutionPending, 10)
	if err != nil {
		t.Fatalf("list pending sync conflicts after resolve: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("pending conflicts after resolve = %#v, want none", conflicts)
	}
}

func TestPostgresMoveAndDeleteConflictRecordsSyncConflict(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	syncService := syncsvc.NewService(repo)
	server := NewWithSync(authService, fileService, syncService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "move-delete-conflict@example.com",
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

	first := []byte("first")
	firstSHA := sha256.Sum256(first)
	firstUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/conflict-action.txt",
		"size":       len(first),
		"sha256":     hex.EncodeToString(firstSHA[:]),
		"chunk_size": len(first),
	})
	if firstUpload.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d body = %s", firstUpload.Code, firstUpload.Body.String())
	}
	var firstUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, firstUpload, &firstUploadBody)
	putChunk(t, server, token, firstUploadBody.Data.UploadID, first, hex.EncodeToString(firstSHA[:]))
	firstCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+firstUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if firstCommit.Code != http.StatusOK {
		t.Fatalf("first commit status = %d body = %s", firstCommit.Code, firstCommit.Body.String())
	}
	var firstCommitBody struct {
		Data struct {
			FileID  string `json:"file_id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, firstCommit, &firstCommitBody)

	second := []byte("second")
	secondSHA := sha256.Sum256(second)
	secondUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":         "/workspace/conflict-action.txt",
		"size":         len(second),
		"sha256":       hex.EncodeToString(secondSHA[:]),
		"chunk_size":   len(second),
		"base_version": firstCommitBody.Data.Version,
	})
	if secondUpload.Code != http.StatusCreated {
		t.Fatalf("second upload status = %d body = %s", secondUpload.Code, secondUpload.Body.String())
	}
	var secondUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, secondUpload, &secondUploadBody)
	putChunk(t, server, token, secondUploadBody.Data.UploadID, second, hex.EncodeToString(secondSHA[:]))
	secondCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+secondUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if secondCommit.Code != http.StatusOK {
		t.Fatalf("second commit status = %d body = %s", secondCommit.Code, secondCommit.Body.String())
	}
	var secondCommitBody struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, secondCommit, &secondCommitBody)

	moveResp := doJSON(t, server, http.MethodPatch, "/api/v1/files/"+firstCommitBody.Data.FileID, token, map[string]any{
		"path":         "/workspace/moved-conflict-action.txt",
		"base_version": firstCommitBody.Data.Version,
	})
	if moveResp.Code != http.StatusConflict {
		t.Fatalf("stale move status = %d body = %s", moveResp.Code, moveResp.Body.String())
	}
	deleteResp := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+firstCommitBody.Data.FileID, token, map[string]any{
		"base_version": firstCommitBody.Data.Version,
	})
	if deleteResp.Code != http.StatusConflict {
		t.Fatalf("stale delete status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}

	node, err := repo.GetFileByID(ctx, registerBody.Data.User.ID, firstCommitBody.Data.FileID)
	if err != nil {
		t.Fatalf("get file after rejected actions: %v", err)
	}
	if node.Path != "/workspace/conflict-action.txt" || node.Version != secondCommitBody.Data.Version || node.DeletedAt != nil {
		t.Fatalf("file after rejected actions = %#v", node)
	}
	conflictsResp := doJSON(t, server, http.MethodGet, "/api/v1/sync/conflicts?resolution=pending&limit=10", token, nil)
	if conflictsResp.Code != http.StatusOK {
		t.Fatalf("conflicts status = %d body = %s", conflictsResp.Code, conflictsResp.Body.String())
	}
	var conflictsBody struct {
		Data struct {
			Items []struct {
				Path          string `json:"path"`
				LocalVersion  *int64 `json:"local_version"`
				RemoteVersion *int64 `json:"remote_version"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, conflictsResp, &conflictsBody)
	if len(conflictsBody.Data.Items) != 2 {
		t.Fatalf("conflicts = %#v, want two", conflictsBody.Data.Items)
	}
	for _, conflict := range conflictsBody.Data.Items {
		if conflict.Path != "/workspace/conflict-action.txt" {
			t.Fatalf("conflict path = %q", conflict.Path)
		}
		if conflict.LocalVersion == nil || *conflict.LocalVersion != firstCommitBody.Data.Version {
			t.Fatalf("local version = %#v", conflict.LocalVersion)
		}
		if conflict.RemoteVersion == nil || *conflict.RemoteVersion != secondCommitBody.Data.Version {
			t.Fatalf("remote version = %#v", conflict.RemoteVersion)
		}
	}
}

func TestPostgresFileVersionHistory(t *testing.T) {
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "versions@example.com",
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
	token := registerBody.Data.Tokens.AccessToken

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	first := []byte("first")
	firstSHA := sha256.Sum256(first)
	firstUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/history.txt",
		"size":       len(first),
		"sha256":     hex.EncodeToString(firstSHA[:]),
		"chunk_size": len(first),
	})
	if firstUpload.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d body = %s", firstUpload.Code, firstUpload.Body.String())
	}
	var firstUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, firstUpload, &firstUploadBody)
	putChunk(t, server, token, firstUploadBody.Data.UploadID, first, hex.EncodeToString(firstSHA[:]))
	firstCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+firstUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if firstCommit.Code != http.StatusOK {
		t.Fatalf("first commit status = %d body = %s", firstCommit.Code, firstCommit.Body.String())
	}
	var firstCommitBody struct {
		Data struct {
			FileID  string `json:"file_id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, firstCommit, &firstCommitBody)
	if firstCommitBody.Data.FileID == "" || firstCommitBody.Data.Version != 1 {
		t.Fatalf("first commit data = %#v", firstCommitBody.Data)
	}

	second := []byte("second")
	secondSHA := sha256.Sum256(second)
	secondUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":         "/workspace/history.txt",
		"size":         len(second),
		"sha256":       hex.EncodeToString(secondSHA[:]),
		"chunk_size":   len(second),
		"base_version": firstCommitBody.Data.Version,
	})
	if secondUpload.Code != http.StatusCreated {
		t.Fatalf("second upload status = %d body = %s", secondUpload.Code, secondUpload.Body.String())
	}
	var secondUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, secondUpload, &secondUploadBody)
	putChunk(t, server, token, secondUploadBody.Data.UploadID, second, hex.EncodeToString(secondSHA[:]))
	secondCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+secondUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if secondCommit.Code != http.StatusOK {
		t.Fatalf("second commit status = %d body = %s", secondCommit.Code, secondCommit.Body.String())
	}

	versionsResp := doJSON(t, server, http.MethodGet, "/api/v1/files/"+firstCommitBody.Data.FileID+"/versions?limit=10", token, nil)
	if versionsResp.Code != http.StatusOK {
		t.Fatalf("versions status = %d body = %s", versionsResp.Code, versionsResp.Body.String())
	}
	var versionsBody struct {
		Data struct {
			Items []struct {
				ID      string `json:"id"`
				FileID  string `json:"file_id"`
				Version int64  `json:"version"`
				Size    int64  `json:"size"`
				SHA256  string `json:"sha256"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, versionsResp, &versionsBody)
	if len(versionsBody.Data.Items) != 2 {
		t.Fatalf("versions = %#v, want two", versionsBody.Data.Items)
	}
	if versionsBody.Data.Items[0].Version != 2 || versionsBody.Data.Items[0].SHA256 != hex.EncodeToString(secondSHA[:]) || versionsBody.Data.Items[0].Size != int64(len(second)) {
		t.Fatalf("latest version = %#v", versionsBody.Data.Items[0])
	}
	if versionsBody.Data.Items[1].Version != 1 || versionsBody.Data.Items[1].SHA256 != hex.EncodeToString(firstSHA[:]) || versionsBody.Data.Items[1].Size != int64(len(first)) {
		t.Fatalf("initial version = %#v", versionsBody.Data.Items[1])
	}
	if versionsBody.Data.Items[0].FileID != firstCommitBody.Data.FileID || versionsBody.Data.Items[1].FileID != firstCommitBody.Data.FileID {
		t.Fatalf("version file ids = %#v", versionsBody.Data.Items)
	}
}

func TestPostgresListFilesPagination(t *testing.T) {
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "list-pagination@example.com",
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
	token := registerBody.Data.Tokens.AccessToken

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}
	var createDirBody struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, createDirResp, &createDirBody)
	if createDirBody.Data.ID == "" {
		t.Fatal("workspace id is empty")
	}
	docsResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace/docs"})
	if docsResp.Code != http.StatusCreated {
		t.Fatalf("create docs status = %d body = %s", docsResp.Code, docsResp.Body.String())
	}

	content := []byte("readme")
	sum := sha256.Sum256(content)
	uploadResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/readme.txt",
		"size":       len(content),
		"sha256":     hex.EncodeToString(sum[:]),
		"chunk_size": len(content),
	})
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body = %s", uploadResp.Code, uploadResp.Body.String())
	}
	var uploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, uploadResp, &uploadBody)
	putChunk(t, server, token, uploadBody.Data.UploadID, content, hex.EncodeToString(sum[:]))
	commitResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if commitResp.Code != http.StatusOK {
		t.Fatalf("commit status = %d body = %s", commitResp.Code, commitResp.Body.String())
	}

	firstResp := doJSON(t, server, http.MethodGet, "/api/v1/files?parent_id="+createDirBody.Data.ID+"&page_size=1", token, nil)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first list status = %d body = %s", firstResp.Code, firstResp.Body.String())
	}
	var firstBody struct {
		Data struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
	}
	decodeBody(t, firstResp, &firstBody)
	if len(firstBody.Data.Items) != 1 || firstBody.Data.Items[0].Name != "docs" || firstBody.Data.NextCursor == "" {
		t.Fatalf("first page = %#v", firstBody.Data)
	}

	secondResp := doJSON(t, server, http.MethodGet, "/api/v1/files?parent_id="+createDirBody.Data.ID+"&cursor="+firstBody.Data.NextCursor+"&page_size=1", token, nil)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second list status = %d body = %s", secondResp.Code, secondResp.Body.String())
	}
	var secondBody struct {
		Data struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
	}
	decodeBody(t, secondResp, &secondBody)
	if len(secondBody.Data.Items) != 1 || secondBody.Data.Items[0].Name != "readme.txt" || secondBody.Data.NextCursor != "" {
		t.Fatalf("second page = %#v", secondBody.Data)
	}
}

func TestPostgresPinFileVersion(t *testing.T) {
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	server := New(authService, fileService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "pin-version@example.com",
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
	token := registerBody.Data.Tokens.AccessToken

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	content := []byte("pin me")
	sum := sha256.Sum256(content)
	uploadResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/pin.txt",
		"size":       len(content),
		"sha256":     hex.EncodeToString(sum[:]),
		"chunk_size": len(content),
	})
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body = %s", uploadResp.Code, uploadResp.Body.String())
	}
	var uploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, uploadResp, &uploadBody)
	putChunk(t, server, token, uploadBody.Data.UploadID, content, hex.EncodeToString(sum[:]))
	commitResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if commitResp.Code != http.StatusOK {
		t.Fatalf("commit status = %d body = %s", commitResp.Code, commitResp.Body.String())
	}
	var commitBody struct {
		Data struct {
			FileID  string `json:"file_id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, commitResp, &commitBody)

	pinResp := doJSON(t, server, http.MethodPost, "/api/v1/files/"+commitBody.Data.FileID+"/versions/1/pin", token, map[string]any{})
	if pinResp.Code != http.StatusOK {
		t.Fatalf("pin status = %d body = %s", pinResp.Code, pinResp.Body.String())
	}
	var pinBody struct {
		Data struct {
			ID       string     `json:"id"`
			Version  int64      `json:"version"`
			PinnedAt *time.Time `json:"pinned_at"`
		} `json:"data"`
	}
	decodeBody(t, pinResp, &pinBody)
	if pinBody.Data.ID == "" || pinBody.Data.Version != 1 || pinBody.Data.PinnedAt == nil {
		t.Fatalf("pin body = %#v", pinBody.Data)
	}

	versionsResp := doJSON(t, server, http.MethodGet, "/api/v1/files/"+commitBody.Data.FileID+"/versions?limit=10", token, nil)
	if versionsResp.Code != http.StatusOK {
		t.Fatalf("versions status = %d body = %s", versionsResp.Code, versionsResp.Body.String())
	}
	var versionsBody struct {
		Data struct {
			Items []struct {
				Version  int64      `json:"version"`
				PinnedAt *time.Time `json:"pinned_at"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, versionsResp, &versionsBody)
	if len(versionsBody.Data.Items) != 1 || versionsBody.Data.Items[0].Version != 1 || versionsBody.Data.Items[0].PinnedAt == nil {
		t.Fatalf("versions after pin = %#v", versionsBody.Data.Items)
	}

	unpinResp := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+commitBody.Data.FileID+"/versions/1/pin", token, nil)
	if unpinResp.Code != http.StatusOK {
		t.Fatalf("unpin status = %d body = %s", unpinResp.Code, unpinResp.Body.String())
	}
	var unpinBody struct {
		Data struct {
			ID       string     `json:"id"`
			Version  int64      `json:"version"`
			PinnedAt *time.Time `json:"pinned_at"`
		} `json:"data"`
	}
	decodeBody(t, unpinResp, &unpinBody)
	if unpinBody.Data.ID == "" || unpinBody.Data.Version != 1 || unpinBody.Data.PinnedAt != nil {
		t.Fatalf("unpin body = %#v", unpinBody.Data)
	}

	versionsResp = doJSON(t, server, http.MethodGet, "/api/v1/files/"+commitBody.Data.FileID+"/versions?limit=10", token, nil)
	if versionsResp.Code != http.StatusOK {
		t.Fatalf("versions after unpin status = %d body = %s", versionsResp.Code, versionsResp.Body.String())
	}
	decodeBody(t, versionsResp, &versionsBody)
	if len(versionsBody.Data.Items) != 1 || versionsBody.Data.Items[0].PinnedAt != nil {
		t.Fatalf("versions after unpin = %#v", versionsBody.Data.Items)
	}
}

func TestPostgresRestoreFileVersion(t *testing.T) {
	repo := newTestRepository(t)

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, storage.NewLocal(t.TempDir()), 4*1024*1024, 24*time.Hour)
	syncService := syncsvc.NewService(repo)
	server := NewWithSync(authService, fileService, syncService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "restore@example.com",
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
	token := registerBody.Data.Tokens.AccessToken

	deviceResp := doJSON(t, server, http.MethodPost, "/api/v1/devices", token, map[string]any{
		"name":     "restore-device",
		"platform": "test",
	})
	if deviceResp.Code != http.StatusCreated {
		t.Fatalf("register device status = %d body = %s", deviceResp.Code, deviceResp.Body.String())
	}
	var deviceBody struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, deviceResp, &deviceBody)

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	first := []byte("first version")
	firstSHA := sha256.Sum256(first)
	firstUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/restore.txt",
		"size":       len(first),
		"sha256":     hex.EncodeToString(firstSHA[:]),
		"chunk_size": len(first),
	})
	if firstUpload.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d body = %s", firstUpload.Code, firstUpload.Body.String())
	}
	var firstUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, firstUpload, &firstUploadBody)
	putChunk(t, server, token, firstUploadBody.Data.UploadID, first, hex.EncodeToString(firstSHA[:]))
	firstCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+firstUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if firstCommit.Code != http.StatusOK {
		t.Fatalf("first commit status = %d body = %s", firstCommit.Code, firstCommit.Body.String())
	}
	var firstCommitBody struct {
		Data struct {
			FileID  string `json:"file_id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	decodeBody(t, firstCommit, &firstCommitBody)

	second := []byte("second version")
	secondSHA := sha256.Sum256(second)
	secondUpload := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":         "/workspace/restore.txt",
		"size":         len(second),
		"sha256":       hex.EncodeToString(secondSHA[:]),
		"chunk_size":   len(second),
		"base_version": firstCommitBody.Data.Version,
	})
	if secondUpload.Code != http.StatusCreated {
		t.Fatalf("second upload status = %d body = %s", secondUpload.Code, secondUpload.Body.String())
	}
	var secondUploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, secondUpload, &secondUploadBody)
	putChunk(t, server, token, secondUploadBody.Data.UploadID, second, hex.EncodeToString(secondSHA[:]))
	secondCommit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+secondUploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if secondCommit.Code != http.StatusOK {
		t.Fatalf("second commit status = %d body = %s", secondCommit.Code, secondCommit.Body.String())
	}

	restoreResp := doJSON(t, server, http.MethodPost, "/api/v1/files/"+firstCommitBody.Data.FileID+"/versions/1/restore", token, map[string]any{
		"device_id": deviceBody.Data.ID,
	})
	if restoreResp.Code != http.StatusOK {
		t.Fatalf("restore status = %d body = %s", restoreResp.Code, restoreResp.Body.String())
	}
	var restoreBody struct {
		Data struct {
			File struct {
				ID      string `json:"id"`
				Version int64  `json:"version"`
				Size    int64  `json:"size"`
				SHA256  string `json:"sha256"`
			} `json:"file"`
			ChangeID int64 `json:"change_id"`
		} `json:"data"`
	}
	decodeBody(t, restoreResp, &restoreBody)
	if restoreBody.Data.File.ID != firstCommitBody.Data.FileID || restoreBody.Data.File.Version != 3 || restoreBody.Data.ChangeID == 0 {
		t.Fatalf("restore body = %#v", restoreBody.Data)
	}
	if restoreBody.Data.File.SHA256 != hex.EncodeToString(firstSHA[:]) || restoreBody.Data.File.Size != int64(len(first)) {
		t.Fatalf("restored file metadata = %#v", restoreBody.Data.File)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+firstCommitBody.Data.FileID+"/content", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+token)
	downloadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d body = %s", downloadRec.Code, downloadRec.Body.String())
	}
	if got := downloadRec.Body.String(); got != string(first) {
		t.Fatalf("download body = %q, want %q", got, string(first))
	}

	versionsResp := doJSON(t, server, http.MethodGet, "/api/v1/files/"+firstCommitBody.Data.FileID+"/versions?limit=10", token, nil)
	if versionsResp.Code != http.StatusOK {
		t.Fatalf("versions status = %d body = %s", versionsResp.Code, versionsResp.Body.String())
	}
	var versionsBody struct {
		Data struct {
			Items []struct {
				Version int64  `json:"version"`
				SHA256  string `json:"sha256"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, versionsResp, &versionsBody)
	if len(versionsBody.Data.Items) != 3 {
		t.Fatalf("versions = %#v, want three", versionsBody.Data.Items)
	}
	if versionsBody.Data.Items[0].Version != 3 || versionsBody.Data.Items[0].SHA256 != hex.EncodeToString(firstSHA[:]) {
		t.Fatalf("restored version item = %#v", versionsBody.Data.Items[0])
	}

	changesReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/changes?device_id="+deviceBody.Data.ID+"&after_change_id=0&limit=20", nil)
	changesReq.Header.Set("Authorization", "Bearer "+token)
	changesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(changesRec, changesReq)
	if changesRec.Code != http.StatusOK {
		t.Fatalf("changes status = %d body = %s", changesRec.Code, changesRec.Body.String())
	}
	var changesBody struct {
		Data struct {
			Items []struct {
				EventType      string  `json:"event_type"`
				Path           string  `json:"path"`
				Version        *int64  `json:"version"`
				SourceDeviceID *string `json:"source_device_id"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, changesRec, &changesBody)
	if len(changesBody.Data.Items) == 0 {
		t.Fatalf("changes missing restore event: %#v", changesBody.Data.Items)
	}
	last := changesBody.Data.Items[len(changesBody.Data.Items)-1]
	if last.EventType != "restore" || last.Path != "/workspace/restore.txt" || last.Version == nil || *last.Version != 3 {
		t.Fatalf("last change = %#v", last)
	}
	if last.SourceDeviceID == nil || *last.SourceDeviceID != deviceBody.Data.ID {
		t.Fatalf("restore source device id = %#v, want %s", last.SourceDeviceID, deviceBody.Data.ID)
	}
}

func TestPostgresUploadInitIdempotencyKey(t *testing.T) {
	repo := newTestRepository(t)

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

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", registerBody.Data.Tokens.AccessToken, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

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

func TestPostgresAbortUploadIsIdempotentAndCleansStagingChunks(t *testing.T) {
	repo := newTestRepository(t)
	storageRoot := t.TempDir()
	server := New(
		authsvc.NewService(repo, "test-secret", time.Minute, time.Hour),
		filesvc.NewService(repo, storage.NewLocal(storageRoot), 1024, time.Hour),
		repo,
	)

	registered := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": "abort-upload@example.com", "password": "password123",
	})
	if registered.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registered.Code, registered.Body.String())
	}
	var auth struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registered, &auth)
	content := []byte("unfinished upload")
	sum := sha256.Sum256(content)
	initialized := doJSON(t, server, http.MethodPost, "/api/v1/uploads", auth.Data.Tokens.AccessToken, map[string]any{
		"path": "/unfinished.txt", "size": len(content), "sha256": hex.EncodeToString(sum[:]), "chunk_size": len(content),
	})
	if initialized.Code != http.StatusCreated {
		t.Fatalf("init status = %d body = %s", initialized.Code, initialized.Body.String())
	}
	var upload struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, initialized, &upload)
	putChunk(t, server, auth.Data.Tokens.AccessToken, upload.Data.UploadID, content, hex.EncodeToString(sum[:]))
	stagingPath := filepath.Join(storageRoot, "staging", auth.Data.User.ID, upload.Data.UploadID, "0")
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("staging chunk before abort: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		aborted := doJSON(t, server, http.MethodDelete, "/api/v1/uploads/"+upload.Data.UploadID, auth.Data.Tokens.AccessToken, nil)
		if aborted.Code != http.StatusOK {
			t.Fatalf("abort attempt %d status = %d body = %s", attempt+1, aborted.Code, aborted.Body.String())
		}
	}
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging chunk after abort still exists or stat failed: %v", err)
	}
	status := doJSON(t, server, http.MethodGet, "/api/v1/uploads/"+upload.Data.UploadID, auth.Data.Tokens.AccessToken, nil)
	if status.Code != http.StatusOK || !bytes.Contains(status.Body.Bytes(), []byte(`"status":"aborted"`)) {
		t.Fatalf("aborted upload status = %d body = %s", status.Code, status.Body.String())
	}
	commit := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+upload.Data.UploadID+"/commit", auth.Data.Tokens.AccessToken, nil)
	if commit.Code != http.StatusGone {
		t.Fatalf("aborted commit status = %d body = %s", commit.Code, commit.Body.String())
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
