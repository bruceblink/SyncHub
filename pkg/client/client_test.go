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

func TestRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/refresh" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		if req.RefreshToken != "refresh-old" {
			t.Fatalf("refresh token = %q", req.RefreshToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"access_token":"access-new","refresh_token":"refresh-new","expires_in":900}}`))
	}))
	defer server.Close()

	tokens, err := New(server.URL).Refresh(context.Background(), "refresh-old")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tokens.AccessToken != "access-new" || tokens.RefreshToken != "refresh-new" || tokens.ExpiresIn != 900 {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
}

func TestLogout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/logout" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode logout request: %v", err)
		}
		if req.RefreshToken != "refresh" {
			t.Fatalf("refresh token = %q", req.RefreshToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer server.Close()

	if err := New(server.URL).Logout(context.Background(), "refresh"); err != nil {
		t.Fatalf("logout: %v", err)
	}
}

func TestRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/register" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access","refresh_token":"refresh","expires_in":900}}}`))
	}))
	defer server.Close()

	data, err := New(server.URL).Register(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if data.User.Email != "user@example.com" || data.Tokens.AccessToken != "access" {
		t.Fatalf("unexpected register data: %#v", data)
	}
}

func TestNewDefaultsToLocalMVPServer(t *testing.T) {
	client := New("")
	if client.BaseURL != "http://localhost:8765" {
		t.Fatalf("base url = %q, want http://localhost:8765", client.BaseURL)
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

func TestCreateDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/files/directories" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Path != "/workspace" {
			t.Fatalf("path = %q", req.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).CreateDirectory(context.Background(), "access-token", "/workspace")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if node.ID != "dir_1" || node.Path != "/workspace" || node.NodeType != "directory" {
		t.Fatalf("unexpected node: %#v", node)
	}
}

func TestCreateDirectoryWithDevice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/files/directories" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Path     string `json:"path"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Path != "/workspace" || req.DeviceID != "dev_1" {
			t.Fatalf("request body = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).CreateDirectoryWithDevice(context.Background(), "access-token", "/workspace", "dev_1")
	if err != nil {
		t.Fatalf("create directory with device: %v", err)
	}
	if node.ID != "dir_1" || node.Path != "/workspace" || node.NodeType != "directory" {
		t.Fatalf("unexpected node: %#v", node)
	}
}

func TestGetFileByPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/by-path" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("path"); got != "/workspace/a b.txt" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a b.txt","path":"/workspace/a b.txt","node_type":"file","version":3}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).GetFileByPath(context.Background(), "access-token", "/workspace/a b.txt")
	if err != nil {
		t.Fatalf("get file by path: %v", err)
	}
	if node.ID != "file_1" || node.Path != "/workspace/a b.txt" || node.Version != 3 {
		t.Fatalf("unexpected node: %#v", node)
	}
}

func TestGetFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","version":3}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).GetFile(context.Background(), "access-token", "file_1")
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if node.ID != "file_1" || node.Path != "/workspace/a.txt" || node.Version != 3 {
		t.Fatalf("unexpected node: %#v", node)
	}
}

func TestListFiles(t *testing.T) {
	parentID := "dir_1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != parentID {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("cursor"); got != "file_0" {
			t.Fatalf("cursor = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "20" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dir_2","parent_id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"},{"id":"file_1","parent_id":"dir_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}],"next_cursor":"file_1"}}`))
	}))
	defer server.Close()

	files, err := New(server.URL).ListFiles(context.Background(), "access-token", &parentID, "file_0", 20)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files.Items) != 2 {
		t.Fatalf("file count = %d", len(files.Items))
	}
	if files.Items[0].ID != "dir_2" || files.Items[0].NodeType != "directory" || files.Items[0].ParentID == nil || *files.Items[0].ParentID != parentID {
		t.Fatalf("unexpected first item: %#v", files.Items[0])
	}
	if files.Items[1].ID != "file_1" || files.Items[1].Size != 5 || files.Items[1].SHA256 == nil || *files.Items[1].SHA256 != "sha1" {
		t.Fatalf("unexpected second item: %#v", files.Items[1])
	}
	if files.NextCursor != "file_1" {
		t.Fatalf("next cursor = %q, want file_1", files.NextCursor)
	}
}

func TestListFileVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1/versions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"ver_2","file_id":"file_1","version":2,"size":6,"sha256":"sha2","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:02:00Z"},{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}]}}`))
	}))
	defer server.Close()

	versions, err := New(server.URL).ListFileVersions(context.Background(), "access-token", "file_1", 20)
	if err != nil {
		t.Fatalf("list file versions: %v", err)
	}
	if len(versions.Items) != 2 {
		t.Fatalf("version count = %d", len(versions.Items))
	}
	if versions.Items[0].ID != "ver_2" || versions.Items[0].Version != 2 || versions.Items[0].SHA256 != "sha2" {
		t.Fatalf("unexpected first version: %#v", versions.Items[0])
	}
	if versions.Items[0].PinnedAt == nil || versions.Items[1].PinnedAt != nil {
		t.Fatalf("unexpected pinned state: %#v", versions.Items)
	}
}

func TestRestoreFileVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/files/file_1/versions/2/restore" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":4,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"},"change_id":9}}`))
	}))
	defer server.Close()

	restored, err := New(server.URL).RestoreFileVersion(context.Background(), "access-token", "file_1", 2)
	if err != nil {
		t.Fatalf("restore file version: %v", err)
	}
	if restored.File.ID != "file_1" || restored.File.Version != 4 || restored.ChangeID != 9 {
		t.Fatalf("unexpected restore data: %#v", restored)
	}
}

func TestPinAndUnpinFileVersion(t *testing.T) {
	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/files/file_1/versions/1/pin", body: `{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:01:00Z"}}`},
		{method: http.MethodDelete, path: "/api/v1/files/file_1/versions/1/pin", body: `{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}}`},
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if index >= len(requests) {
			t.Fatalf("unexpected extra request = %s %s", r.Method, r.URL.Path)
		}
		expected := requests[index]
		index++
		if r.Method != expected.method || r.URL.Path != expected.path {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(expected.body))
	}))
	defer server.Close()

	client := New(server.URL)
	pinned, err := client.PinFileVersion(context.Background(), "access-token", "file_1", 1)
	if err != nil {
		t.Fatalf("pin file version: %v", err)
	}
	if pinned.ID != "ver_1" || pinned.PinnedAt == nil {
		t.Fatalf("unexpected pinned version: %#v", pinned)
	}
	unpinned, err := client.UnpinFileVersion(context.Background(), "access-token", "file_1", 1)
	if err != nil {
		t.Fatalf("unpin file version: %v", err)
	}
	if unpinned.ID != "ver_1" || unpinned.PinnedAt != nil {
		t.Fatalf("unexpected unpinned version: %#v", unpinned)
	}
	if index != len(requests) {
		t.Fatalf("request count = %d, want %d", index, len(requests))
	}
}

func TestDeleteFile(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/files/file_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		deleted = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer server.Close()

	if err := New(server.URL).DeleteFile(context.Background(), "access-token", "file_1"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if !deleted {
		t.Fatal("file was not deleted")
	}
}

func TestDeleteFileWithDevice(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/files/file_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.DeviceID != "dev_1" {
			t.Fatalf("device id = %q", req.DeviceID)
		}
		deleted = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer server.Close()

	if err := New(server.URL).DeleteFileWithDevice(context.Background(), "access-token", "file_1", "dev_1"); err != nil {
		t.Fatalf("delete file with device: %v", err)
	}
	if !deleted {
		t.Fatal("file was not deleted")
	}
}

func TestMoveFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/files/file_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Path != "/workspace/renamed.txt" {
			t.Fatalf("path = %q", req.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"renamed.txt","path":"/workspace/renamed.txt","node_type":"file","version":4}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).MoveFile(context.Background(), "access-token", "file_1", "/workspace/renamed.txt")
	if err != nil {
		t.Fatalf("move file: %v", err)
	}
	if node.ID != "file_1" || node.Path != "/workspace/renamed.txt" || node.Version != 4 {
		t.Fatalf("unexpected node: %#v", node)
	}
}

func TestMoveFileWithDevice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/files/file_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Path     string `json:"path"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Path != "/workspace/renamed.txt" || req.DeviceID != "dev_1" {
			t.Fatalf("request body = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"renamed.txt","path":"/workspace/renamed.txt","node_type":"file","version":4}}`))
	}))
	defer server.Close()

	node, err := New(server.URL).MoveFileWithDevice(context.Background(), "access-token", "file_1", "/workspace/renamed.txt", "dev_1")
	if err != nil {
		t.Fatalf("move file with device: %v", err)
	}
	if node.ID != "file_1" || node.Path != "/workspace/renamed.txt" || node.Version != 4 {
		t.Fatalf("unexpected node: %#v", node)
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
		if req.Path != "/workspace/a.txt" || req.Size != 5 || req.SHA256 != "hash" || req.ChunkSize != 2 || req.DeviceID != "dev_1" {
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
		DeviceID:  "dev_1",
	}, "upload-1")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	if session.UploadID != "upl_1" || session.ChunkSize != 2 || session.Status != "pending" {
		t.Fatalf("unexpected upload session: %#v", session)
	}
}

func TestDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1/content" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Range"); got != "bytes=1-3" {
			t.Fatalf("range = %q", got)
		}
		if got := r.Header.Get("If-None-Match"); got != `"etag"` {
			t.Fatalf("if-none-match = %q", got)
		}
		w.Header().Set("ETag", `"etag2"`)
		w.Header().Set("Content-Range", "bytes 1-3/5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("ell"))
	}))
	defer server.Close()

	result, err := New(server.URL).DownloadFile(context.Background(), "access-token", "file_1", DownloadOptions{
		Range:       "bytes=1-3",
		IfNoneMatch: `"etag"`,
	})
	if err != nil {
		t.Fatalf("download file: %v", err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if result.StatusCode != http.StatusPartialContent || result.ETag != `"etag2"` || result.ContentRange != "bytes 1-3/5" || string(body) != "ell" {
		t.Fatalf("unexpected download result: %#v body=%q", result, string(body))
	}
}

func TestDeviceAndChangeMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			var req struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode register device: %v", err)
			}
			if req.Name != "laptop" || req.Platform != "windows" {
				t.Fatalf("register request = %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices/dev_1/heartbeat":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_seen_at":"2026-06-30T00:01:00Z","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			if got := r.URL.Query().Get("after_change_id"); got != "7" {
				t.Fatalf("after_change_id = %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Fatalf("limit = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":8,"file_id":"file_1","event_type":"create","version":1,"path":"/workspace/a.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":8}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			var req struct {
				DeviceID            string `json:"device_id"`
				LastAppliedChangeID int64  `json:"last_applied_change_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ack: %v", err)
			}
			if req.DeviceID != "dev_1" || req.LastAppliedChangeID != 8 {
				t.Fatalf("ack request = %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":8,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:03:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	apiClient := New(server.URL)
	device, err := apiClient.RegisterDevice(context.Background(), "access-token", "laptop", "windows")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if device.ID != "dev_1" || device.Name != "laptop" {
		t.Fatalf("unexpected device: %#v", device)
	}

	heartbeat, err := apiClient.HeartbeatDevice(context.Background(), "access-token", "dev_1")
	if err != nil {
		t.Fatalf("heartbeat device: %v", err)
	}
	if heartbeat.LastSeenAt == nil {
		t.Fatalf("heartbeat missing last_seen_at: %#v", heartbeat)
	}

	changes, err := apiClient.ListChanges(context.Background(), "access-token", "dev_1", 7, 50)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if changes.NextCursor != 8 || len(changes.Items) != 1 || changes.Items[0].Path != "/workspace/a.txt" {
		t.Fatalf("unexpected changes: %#v", changes)
	}

	acked, err := apiClient.AckChanges(context.Background(), "access-token", "dev_1", changes.NextCursor)
	if err != nil {
		t.Fatalf("ack changes: %v", err)
	}
	if acked.LastAppliedChangeID != 8 {
		t.Fatalf("unexpected acked device: %#v", acked)
	}
}

func TestListSyncConflicts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/conflicts" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("resolution"); got != "pending" {
			t.Fatalf("resolution = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "25" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"conf_1","file_id":"file_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"pending","created_at":"2026-06-30T00:00:00Z"}]}}`))
	}))
	defer server.Close()

	conflicts, err := New(server.URL).ListSyncConflicts(context.Background(), "access-token", "pending", 25)
	if err != nil {
		t.Fatalf("list sync conflicts: %v", err)
	}
	if len(conflicts.Items) != 1 {
		t.Fatalf("conflict count = %d", len(conflicts.Items))
	}
	conflict := conflicts.Items[0]
	if conflict.ID != "conf_1" || conflict.Path != "/workspace/a.txt" || conflict.Resolution != "pending" {
		t.Fatalf("unexpected conflict: %#v", conflict)
	}
	if conflict.LocalVersion == nil || *conflict.LocalVersion != 1 {
		t.Fatalf("local version = %#v", conflict.LocalVersion)
	}
	if conflict.RemoteVersion == nil || *conflict.RemoteVersion != 2 {
		t.Fatalf("remote version = %#v", conflict.RemoteVersion)
	}
}

func TestResolveSyncConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sync/conflicts/conf_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Resolution string `json:"resolution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Resolution != "keep_both" {
			t.Fatalf("resolution = %q", req.Resolution)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"conf_1","file_id":"file_1","path":"/workspace/a.txt","local_version":1,"remote_version":2,"resolution":"keep_both","created_at":"2026-06-30T00:00:00Z","resolved_at":"2026-06-30T00:01:00Z"}}`))
	}))
	defer server.Close()

	conflict, err := New(server.URL).ResolveSyncConflict(context.Background(), "access-token", "conf_1", "keep_both")
	if err != nil {
		t.Fatalf("resolve sync conflict: %v", err)
	}
	if conflict.ID != "conf_1" || conflict.Resolution != "keep_both" || conflict.ResolvedAt == nil {
		t.Fatalf("unexpected conflict: %#v", conflict)
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
