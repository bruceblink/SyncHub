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

	"github.com/bruceblink/SyncHub/pkg/client"
)

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

func TestRunFileDownloadByRemotePathCanOutputJSON(t *testing.T) {
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

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
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
		"download",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs/readme.txt",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download json: %v", err)
	}
	if strings.Contains(stdout.String(), "downloaded:") {
		t.Fatalf("json output includes text download output: %s", stdout.String())
	}
	outputPath := filepath.Join(root, "docs", "readme.txt")
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("downloaded content = %q, want %q", string(raw), string(content))
	}

	var snapshot fileDownloadSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode download json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.File.ID != "file_1" || snapshot.File.Path != "/workspace/docs/readme.txt" {
		t.Fatalf("file = %#v", snapshot.File)
	}
	if snapshot.Output != outputPath || snapshot.Bytes != int64(len(content)) || snapshot.NotModified || snapshot.StatusCode != http.StatusOK {
		t.Fatalf("snapshot = %#v", snapshot)
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

func TestRunFileDownloadRangeCanOutputJSON(t *testing.T) {
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download range json: %v", err)
	}

	var snapshot fileDownloadSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode download json: %v\n%s", err, stdout.String())
	}
	if snapshot.Output != outputPath || snapshot.Bytes != int64(len(content)) || snapshot.NotModified || snapshot.StatusCode != http.StatusPartialContent {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.ETag != `"new"` || snapshot.ContentRange != "bytes 0-3/10" {
		t.Fatalf("range metadata = %#v", snapshot)
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

func TestRunFileDownloadNotModifiedCanOutputJSON(t *testing.T) {
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file download not modified json: %v", err)
	}
	if strings.Contains(stdout.String(), "not modified:") {
		t.Fatalf("json output includes text not-modified output: %s", stdout.String())
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("output was written or stat failed: %v", err)
	}

	var snapshot fileDownloadSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode download json: %v\n%s", err, stdout.String())
	}
	if snapshot.Output != outputPath || snapshot.Bytes != 0 || !snapshot.NotModified || snapshot.StatusCode != http.StatusNotModified {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.ETag != `"cached"` {
		t.Fatalf("etag = %q", snapshot.ETag)
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
