package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunUploadStatus(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/uploads/upl_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":4,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[{"chunk_index":0,"size":4,"sha256":"sha0"},{"chunk_index":2,"size":3,"sha256":"sha2"}]}}`))
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
		"upload",
		"status",
		"--path", root,
		"--config", loginConfigPath,
		"--id", "upl_1",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("upload status: %v", err)
	}
	want := "upload: upl_1\npath: /workspace/a.txt\nstatus: pending\nchunk size: 4\nexpires at: 2026-06-30T00:00:00Z\nuploaded chunks: 2\nchunk 0 size=4 sha256=sha0\nchunk 2 size=3 sha256=sha2\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunUploadStatusCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/uploads/upl_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":4,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[{"chunk_index":0,"size":4,"sha256":"sha0"}]}}`))
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
		"upload",
		"status",
		"--path", root,
		"--config", loginConfigPath,
		"--id", "upl_1",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("upload status json: %v", err)
	}
	if strings.Contains(stdout.String(), "upload:") {
		t.Fatalf("json output includes text upload output: %s", stdout.String())
	}

	var snapshot uploadStatusSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode upload status json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.Upload.UploadID != "upl_1" || snapshot.Upload.Path != "/workspace/a.txt" || snapshot.Upload.Status != "pending" {
		t.Fatalf("upload = %#v", snapshot.Upload)
	}
	if len(snapshot.Upload.UploadedChunks) != 1 || snapshot.Upload.UploadedChunks[0].ChunkIndex != 0 {
		t.Fatalf("chunks = %#v", snapshot.Upload.UploadedChunks)
	}
}

func TestRunUploadStatusRequiresID(t *testing.T) {
	err := run(context.Background(), []string{"upload", "status"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "upload id is required") {
		t.Fatalf("error = %v, want upload id required", err)
	}
}

func TestRunUploadHelpIncludesStatusJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"upload", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("upload help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli upload status --path . --id upl_1 --json") {
		t.Fatalf("upload help missing status json command: %s", stdout.String())
	}
}
