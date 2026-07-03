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
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

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
