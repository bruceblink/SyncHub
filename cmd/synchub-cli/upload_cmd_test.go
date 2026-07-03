package main

import (
	"bytes"
	"context"
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

func TestRunUploadStatusRequiresID(t *testing.T) {
	err := run(context.Background(), []string{"upload", "status"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "upload id is required") {
		t.Fatalf("error = %v, want upload id required", err)
	}
}
