package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunFileList(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != "dir_1" {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("cursor"); got != "dir_0" {
			t.Fatalf("cursor = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "20" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dir_2","parent_id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"},{"id":"file_1","parent_id":"dir_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}],"next_cursor":"file_1"}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--parent-id", "dir_1",
		"--cursor", "dir_0",
		"--page-size", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 2\ndirectory docs/ path=/workspace/docs size=0 version=1 id=dir_2\nfile a.txt path=/workspace/a.txt size=5 version=2 id=file_1\nnext cursor: file_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/docs" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_docs","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files":
			if got := r.URL.Query().Get("parent_id"); got != "dir_docs" {
				t.Fatalf("parent_id = %q", got)
			}
			if got := r.URL.Query().Get("page_size"); got != "20" {
				t.Fatalf("page_size = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"file_1","parent_id":"dir_docs","name":"guide.md","path":"/workspace/docs/guide.md","node_type":"file","size":9,"sha256":"sha1","version":3,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}]}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
		"--page-size", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 1\nfile guide.md path=/workspace/docs/guide.md size=9 version=3 id=file_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListRootRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != "" {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}]}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 1\ndirectory workspace/ path=/workspace size=0 version=1 id=dir_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListRejectsRemoteFilePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	err := run(context.Background(), []string{
		"file",
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path is not a directory: /workspace/a.txt") {
		t.Fatalf("error = %v, want remote path is not a directory", err)
	}
}

func TestRunFileDownloadByRemotePath(t *testing.T) {
	root := t.TempDir()
	content := []byte("downloaded content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/docs/readme.txt" {
				t.Fatalf("path = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/docs/readme.txt","node_type":"file","size":18,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs/readme.txt",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download: %v", err)
	}
	outputPath := filepath.Join(root, "docs", "readme.txt")
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("downloaded content = %q, want %q", string(raw), string(content))
	}
	want := fmt.Sprintf("downloaded: /workspace/docs/readme.txt\noutput: %s\nbytes: %d\n", outputPath, len(content))
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDownloadByFileIDToOutputDirectory(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	content := []byte("by id")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"notes.txt","path":"/archive/notes.txt","node_type":"file","size":5,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--output", outputDir,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download: %v", err)
	}
	outputPath := filepath.Join(outputDir, "notes.txt")
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("downloaded content = %q, want %q", string(raw), string(content))
	}
	want := fmt.Sprintf("downloaded: /archive/notes.txt\noutput: %s\nbytes: %d\n", outputPath, len(content))
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDownloadSupportsRangeAndETag(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "partial.txt")
	content := []byte("part")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/readme.txt","node_type":"file","size":10,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			if got := r.Header.Get("Range"); got != "bytes=0-3" {
				t.Fatalf("range = %q", got)
			}
			if got := r.Header.Get("If-None-Match"); got != `"old"` {
				t.Fatalf("if-none-match = %q", got)
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Range", "bytes 0-3/10")
			w.Header().Set("ETag", `"new"`)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--output", outputPath,
		"--range", "bytes=0-3",
		"--if-none-match", `"old"`,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download range: %v", err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read partial file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("partial content = %q, want %q", string(raw), string(content))
	}
	want := fmt.Sprintf("downloaded: /workspace/readme.txt\noutput: %s\nbytes: %d\ncontent range: bytes 0-3/10\netag: \"new\"\n", outputPath, len(content))
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDownloadNotModifiedDoesNotWriteOutput(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "cached.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/readme.txt","node_type":"file","size":10,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/content":
			if got := r.Header.Get("If-None-Match"); got != `"cached"` {
				t.Fatalf("if-none-match = %q", got)
			}
			w.Header().Set("ETag", `"cached"`)
			w.WriteHeader(http.StatusNotModified)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--output", outputPath,
		"--if-none-match", `"cached"`,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download not modified: %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("output was written or stat failed: %v", err)
	}
	want := "not modified: /workspace/readme.txt\netag: \"cached\"\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDownloadRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/by-path" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.URL.Query().Get("path"); got != "/workspace/docs" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_docs","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	err := run(context.Background(), []string{
		"file",
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path is not a file: /workspace/docs") {
		t.Fatalf("error = %v, want remote path is not a file", err)
	}
}

func TestRunFileDeleteByRemotePath(t *testing.T) {
	root := t.TempDir()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/docs/readme.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/docs/readme.txt","node_type":"file","size":18,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/files/file_1":
			var req struct {
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode delete request: %v", err)
			}
			if req.DeviceID != "dev_1" {
				t.Fatalf("delete device id = %q", req.DeviceID)
			}
			deleted = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:        1,
		Root:           root,
		RemotePath:     "/workspace",
		ServerURL:      server.URL,
		UserID:         "u1",
		UserEmail:      "user@example.com",
		DeviceID:       "dev_1",
		DeviceName:     "laptop",
		DevicePlatform: "windows",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"delete",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs/readme.txt",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete endpoint was not called")
	}
	want := "deleted: /workspace/docs/readme.txt\nid: file_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDeleteByFileID(t *testing.T) {
	root := t.TempDir()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/dir_1":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/files/dir_1":
			deleted = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"delete",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "dir_1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete endpoint was not called")
	}
	want := "deleted: /workspace/docs\nid: dir_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileDeleteRejectsAmbiguousTarget(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	err := run(context.Background(), []string{
		"file",
		"delete",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--remote-path", "/workspace/a.txt",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path and file id cannot both be set") {
		t.Fatalf("error = %v, want ambiguous target error", err)
	}
}

func TestRunFileMoveByRemotePath(t *testing.T) {
	root := t.TempDir()
	moved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/readme.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/readme.txt","node_type":"file","size":18,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/files/file_1":
			var req struct {
				Path     string `json:"path"`
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode move request: %v", err)
			}
			if req.Path != "/workspace/docs/readme.txt" || req.DeviceID != "dev_1" {
				t.Fatalf("move request = %#v", req)
			}
			moved = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/docs/readme.txt","node_type":"file","size":18,"sha256":"sha1","version":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:        1,
		Root:           root,
		RemotePath:     "/workspace",
		ServerURL:      server.URL,
		UserID:         "u1",
		UserEmail:      "user@example.com",
		DeviceID:       "dev_1",
		DeviceName:     "laptop",
		DevicePlatform: "windows",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"move",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/readme.txt",
		"--to", "/workspace/docs/readme.txt",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file move: %v", err)
	}
	if !moved {
		t.Fatal("move endpoint was not called")
	}
	want := "moved: /workspace/readme.txt -> /workspace/docs/readme.txt\nid: file_1\nversion: 3\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileMoveRequiresTargetPath(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	err := run(context.Background(), []string{
		"file",
		"move",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/readme.txt",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path is required") {
		t.Fatalf("error = %v, want target path error", err)
	}
}

func TestRunFileMoveByFileID(t *testing.T) {
	root := t.TempDir()
	moved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/dir_1":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/files/dir_1":
			var req struct {
				Path     string `json:"path"`
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode move request: %v", err)
			}
			if req.Path != "/archive/docs" || req.DeviceID != "" {
				t.Fatalf("move request = %#v", req)
			}
			moved = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"docs","path":"/archive/docs","node_type":"directory","version":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"move",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "dir_1",
		"--to", "/archive/docs",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file move: %v", err)
	}
	if !moved {
		t.Fatal("move endpoint was not called")
	}
	want := "moved: /workspace/docs -> /archive/docs\nid: dir_1\nversion: 3\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileMkdir(t *testing.T) {
	root := t.TempDir()
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/files/directories" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		var req struct {
			Path     string `json:"path"`
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mkdir request: %v", err)
		}
		if req.Path != "/workspace/docs" || req.DeviceID != "dev_1" {
			t.Fatalf("mkdir request = %#v", req)
		}
		created = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_docs","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:        1,
		Root:           root,
		RemotePath:     "/workspace",
		ServerURL:      server.URL,
		UserID:         "u1",
		UserEmail:      "user@example.com",
		DeviceID:       "dev_1",
		DeviceName:     "laptop",
		DevicePlatform: "windows",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"mkdir",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file mkdir: %v", err)
	}
	if !created {
		t.Fatal("mkdir endpoint was not called")
	}
	want := "created directory: /workspace/docs\nid: dir_docs\nversion: 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileMkdirRequiresRemotePath(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspaceConfig(t, root)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: "http://localhost:8765",
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	err := run(context.Background(), []string{
		"file",
		"mkdir",
		"--path", root,
		"--config", loginConfigPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path is required") {
		t.Fatalf("error = %v, want remote path error", err)
	}
}

func TestRunFileVersionsByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1/versions":
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("limit = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"ver_2","file_id":"file_1","version":2,"size":6,"sha256":"sha2","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:02:00Z"},{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}]}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"versions",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--limit", "2",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	want := "file: /workspace/a.txt\nfile id: file_1\nversions: 2\nv2 size=6 sha256=sha2 pinned=2026-06-30T00:03:00Z created=2026-06-30T00:02:00Z id=ver_2\nv1 size=5 sha256=sha1 pinned=- created=2026-06-30T00:01:00Z id=ver_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileVersionsByFileID(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1/versions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[]}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"versions",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	want := "file id: file_1\nversions: 0\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileRestoreByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":6,"sha256":"sha2","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:02:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/file_1/versions/1/restore":
			var req struct {
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode restore request: %v", err)
			}
			if req.DeviceID != "dev_1" {
				t.Fatalf("restore device id = %q", req.DeviceID)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:04:00Z"},"change_id":9}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:        1,
		Root:           root,
		RemotePath:     "/workspace",
		ServerURL:      server.URL,
		UserID:         "u1",
		UserEmail:      "user@example.com",
		DeviceID:       "dev_1",
		DeviceName:     "laptop",
		DevicePlatform: "windows",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"restore",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file restore: %v", err)
	}
	want := "restored: /workspace/a.txt\nversion: 3\nchange id: 9\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFilePinByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/file_1/versions/1/pin":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":"2026-06-30T00:03:00Z","created_at":"2026-06-30T00:01:00Z"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"pin",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file pin: %v", err)
	}
	want := "file: /workspace/a.txt\npinned: file_1 v1\npinned at: 2026-06-30T00:03:00Z\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileUnpinByFileID(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/files/file_1/versions/1/pin" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"ver_1","file_id":"file_1","version":1,"size":5,"sha256":"sha1","pinned_at":null,"created_at":"2026-06-30T00:01:00Z"}}`))
	}))
	defer server.Close()

	writeTestWorkspaceConfigWithServer(t, root, server.URL)
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL: server.URL,
		User:      clientUser("u1", "user@example.com"),
		Tokens:    client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"file",
		"unpin",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--version", "1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file unpin: %v", err)
	}
	want := "unpinned: file_1 v1\npinned at: -\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
