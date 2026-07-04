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

func TestRunFileDeleteCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/file_1":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"readme.txt","path":"/workspace/readme.txt","node_type":"file","size":18,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
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
		"delete",
		"--path", root,
		"--config", loginConfigPath,
		"--file-id", "file_1",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file delete json: %v", err)
	}
	if !deleted {
		t.Fatal("delete endpoint was not called")
	}
	if strings.Contains(stdout.String(), "deleted:") {
		t.Fatalf("json output includes text delete output: %s", stdout.String())
	}
	var snapshot fileMutationSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode delete json: %v\n%s", err, stdout.String())
	}
	if snapshot.Action != "delete" || snapshot.File.ID != "file_1" || snapshot.File.Path != "/workspace/readme.txt" || snapshot.Source != nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
}

func TestRunFileDeleteByFileID(t *testing.T) {
	root := t.TempDir()
	registeredDevice := false
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/dir_1":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":2,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			registeredDevice = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"test-device","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/files/dir_1":
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
	if !registeredDevice {
		t.Fatal("device was not registered")
	}
	workspace, err := readWorkspaceConfig(filepath.Join(root, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if workspace.DeviceID != "dev_1" || workspace.DeviceName != "test-device" {
		t.Fatalf("workspace device = %#v", workspace)
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
