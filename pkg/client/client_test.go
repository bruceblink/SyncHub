package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access","refresh_token":"refresh","expires_in":900}}}`))
	}))
	defer server.Close()

	data, err := New(server.URL).Login(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if data.User.Email != "user@example.com" || data.Tokens.AccessToken != "access" {
		t.Fatalf("unexpected login data: %#v", data)
	}
}

func TestLoginReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"AUTH_INVALID_CREDENTIALS","message":"invalid email or password"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).Login(context.Background(), "user@example.com", "wrong-password")
	if err == nil {
		t.Fatal("expected login error")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Message != "invalid email or password" {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

func TestInitUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/uploads" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "upload-1" {
			t.Fatalf("idempotency key = %q", got)
		}
		var req InitUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Path != "/workspace/a.txt" || req.Size != 5 || req.SHA256 != "hash" || req.ChunkSize != 2 {
			t.Fatalf("unexpected request body: %#v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":2,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
	}))
	defer server.Close()

	session, err := New(server.URL).InitUpload(context.Background(), "access-token", InitUploadRequest{
		Path:      "/workspace/a.txt",
		Size:      5,
		SHA256:    "hash",
		ChunkSize: 2,
	}, "upload-1")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	if session.UploadID != "upl_1" || session.ChunkSize != 2 || session.Status != "pending" {
		t.Fatalf("unexpected upload session: %#v", session)
	}
}

func TestPutUploadChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/uploads/upl_1/chunks/2" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("content type = %q", got)
		}
		if got := r.Header.Get("X-Chunk-Sha256"); got != "chunk-hash" {
			t.Fatalf("chunk checksum = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != "hello" {
			t.Fatalf("chunk body = %q", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":2,"size":5,"sha256":"chunk-hash"}}`))
	}))
	defer server.Close()

	chunk, err := New(server.URL).PutUploadChunk(context.Background(), "access-token", "upl_1", 2, bytes.NewBufferString("hello"), "chunk-hash")
	if err != nil {
		t.Fatalf("put upload chunk: %v", err)
	}
	if chunk.ChunkIndex != 2 || chunk.Size != 5 || chunk.SHA256 != "chunk-hash" {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

func TestUploadStatusAndCommitUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/uploads/upl_1":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":4,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[{"chunk_index":0,"size":4,"sha256":"chunk-hash"}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":3,"change_id":8}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	apiClient := New(server.URL)
	session, err := apiClient.UploadStatus(context.Background(), "access-token", "upl_1")
	if err != nil {
		t.Fatalf("upload status: %v", err)
	}
	if len(session.UploadedChunks) != 1 || session.UploadedChunks[0].ChunkIndex != 0 {
		t.Fatalf("unexpected upload status: %#v", session)
	}

	commit, err := apiClient.CommitUpload(context.Background(), "access-token", "upl_1")
	if err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if commit.FileID != "file_1" || commit.Version != 3 || commit.ChangeID != 8 {
		t.Fatalf("unexpected commit data: %#v", commit)
	}
}
