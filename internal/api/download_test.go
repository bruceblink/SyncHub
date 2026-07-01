package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	"github.com/golang-jwt/jwt/v5"
)

func TestDownloadSupportsETagAndRange(t *testing.T) {
	server, token, node, etag := newDownloadTestServer(t)

	full := serveDownload(t, server, token, node.ID, "", "")
	if full.Code != http.StatusOK {
		t.Fatalf("full download status = %d body = %s", full.Code, full.Body.String())
	}
	if got := full.Body.String(); got != "0123456789" {
		t.Fatalf("full body = %q", got)
	}
	if got := full.Header().Get("ETag"); got != etag {
		t.Fatalf("etag = %q, want %q", got, etag)
	}
	if got := full.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("accept-ranges = %q", got)
	}

	notModified := serveDownload(t, server, token, node.ID, "", etag)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("if-none-match status = %d body = %s", notModified.Code, notModified.Body.String())
	}
	if notModified.Body.Len() != 0 {
		t.Fatalf("304 body length = %d", notModified.Body.Len())
	}

	bounded := serveDownload(t, server, token, node.ID, "bytes=2-4", "")
	if bounded.Code != http.StatusPartialContent {
		t.Fatalf("bounded range status = %d body = %s", bounded.Code, bounded.Body.String())
	}
	if got := bounded.Body.String(); got != "234" {
		t.Fatalf("bounded range body = %q", got)
	}
	if got := bounded.Header().Get("Content-Range"); got != "bytes 2-4/10" {
		t.Fatalf("bounded content-range = %q", got)
	}
	if got := bounded.Header().Get("Content-Length"); got != "3" {
		t.Fatalf("bounded content-length = %q", got)
	}

	openEnded := serveDownload(t, server, token, node.ID, "bytes=7-", "")
	if openEnded.Code != http.StatusPartialContent {
		t.Fatalf("open-ended range status = %d body = %s", openEnded.Code, openEnded.Body.String())
	}
	if got := openEnded.Body.String(); got != "789" {
		t.Fatalf("open-ended range body = %q", got)
	}
	if got := openEnded.Header().Get("Content-Range"); got != "bytes 7-9/10" {
		t.Fatalf("open-ended content-range = %q", got)
	}
	if got := openEnded.Header().Get("Content-Length"); got != "3" {
		t.Fatalf("open-ended content-length = %q", got)
	}
}

func TestDownloadRejectsUnsatisfiableRange(t *testing.T) {
	server, token, node, _ := newDownloadTestServer(t)

	rec := serveDownload(t, server, token, node.ID, "bytes=99-", "")
	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes */10" {
		t.Fatalf("content-range = %q", got)
	}
}

func newDownloadTestServer(t *testing.T) (*Server, string, domain.FileNode, string) {
	t.Helper()

	ctx := context.Background()
	userID := "user-1"
	content := []byte("0123456789")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	key := "objects/download-test/file"
	store := storage.NewLocal(t.TempDir())
	if err := store.PutChunk(ctx, key, bytes.NewReader(content), sha); err != nil {
		t.Fatalf("put test object: %v", err)
	}

	node := domain.FileNode{
		ID:         "file-1",
		UserID:     userID,
		Name:       "file.txt",
		Path:       "/file.txt",
		NodeType:   domain.NodeTypeFile,
		Size:       int64(len(content)),
		SHA256:     &sha,
		StorageKey: &key,
		Version:    3,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	repo := &downloadFileRepo{node: node}

	secret := "test-secret"
	authService := authsvc.NewService(nil, secret, 15*time.Minute, 24*time.Hour)
	fileService := filesvc.NewService(repo, store, 4*1024*1024, 24*time.Hour)
	return New(authService, fileService, nil), testAccessToken(t, secret, userID), node, fileETag(node)
}

func testAccessToken(t *testing.T, secret, userID string) string {
	t.Helper()

	now := time.Now()
	claims := authsvc.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return token
}

func serveDownload(t *testing.T, server *Server, token, fileID, byteRange, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+fileID+"/content", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

type downloadFileRepo struct {
	node domain.FileNode
}

func (r *downloadFileRepo) CreateDirectory(ctx context.Context, userID, path, name string, parentID *string) (domain.FileNode, error) {
	_, _, _, _ = ctx, userID, path, name
	_ = parentID
	return domain.FileNode{}, errNotImplemented()
}

func (r *downloadFileRepo) GetFileByID(ctx context.Context, userID, fileID string) (domain.FileNode, error) {
	_ = ctx
	if r.node.UserID != userID || r.node.ID != fileID {
		return domain.FileNode{}, domain.E(domain.CodeFileNotFound, "file not found", nil)
	}
	return r.node, nil
}

func (r *downloadFileRepo) GetFileByPath(ctx context.Context, userID, path string) (domain.FileNode, error) {
	_ = ctx
	if r.node.UserID != userID || r.node.Path != path {
		return domain.FileNode{}, domain.E(domain.CodeFileNotFound, "file not found", nil)
	}
	return r.node, nil
}

func (r *downloadFileRepo) ListFiles(ctx context.Context, userID string, parentID *string, limit int32) ([]domain.FileNode, error) {
	_, _, _, _ = ctx, userID, parentID, limit
	return nil, errNotImplemented()
}

func (r *downloadFileRepo) ListFileVersions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error) {
	_, _, _, _ = ctx, userID, fileID, limit
	return nil, errNotImplemented()
}

func (r *downloadFileRepo) RestoreFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileNode, int64, error) {
	_, _, _, _ = ctx, userID, fileID, version
	return domain.FileNode{}, 0, errNotImplemented()
}

func (r *downloadFileRepo) MoveFile(ctx context.Context, userID, fileID, newPath, newName string, newParentID *string) (domain.FileNode, error) {
	_, _, _, _, _ = ctx, userID, fileID, newPath, newName
	_ = newParentID
	return domain.FileNode{}, errNotImplemented()
}

func (r *downloadFileRepo) DeleteFile(ctx context.Context, userID, fileID string) error {
	_, _, _ = ctx, userID, fileID
	return errNotImplemented()
}

func (r *downloadFileRepo) CreateUploadSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	_, _ = ctx, s
	return domain.UploadSession{}, errNotImplemented()
}

func (r *downloadFileRepo) GetUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	_, _, _ = ctx, userID, uploadID
	return domain.UploadSession{}, errNotImplemented()
}

func (r *downloadFileRepo) PutUploadChunk(ctx context.Context, uploadID string, chunkIndex, size int32, sha256sum, storageKey string) (domain.UploadChunk, error) {
	_, _, _, _, _, _ = ctx, uploadID, chunkIndex, size, sha256sum, storageKey
	return domain.UploadChunk{}, errNotImplemented()
}

func (r *downloadFileRepo) ListUploadChunks(ctx context.Context, uploadID string) ([]domain.UploadChunk, error) {
	_, _ = ctx, uploadID
	return nil, errNotImplemented()
}

func (r *downloadFileRepo) CommitUpload(ctx context.Context, userID, uploadID, storageKey string) (domain.FileNode, int64, error) {
	_, _, _, _ = ctx, userID, uploadID, storageKey
	return domain.FileNode{}, 0, errNotImplemented()
}

func errNotImplemented() error {
	return domain.E(domain.CodeInternal, "not implemented", io.ErrUnexpectedEOF)
}
