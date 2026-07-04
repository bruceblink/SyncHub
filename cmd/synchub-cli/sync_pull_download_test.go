package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestDownloadChangeFileReplacesExistingFile(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(localPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files/file_1/content" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()

	err := downloadChangeFile(context.Background(), client.New(server.URL), "access", "file_1", localPath)
	if err != nil {
		t.Fatalf("download change file: %v", err)
	}
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(raw) != "new" {
		t.Fatalf("local file = %q, want new", string(raw))
	}
}
