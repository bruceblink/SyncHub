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

func TestRunFileMkdirCanOutputJSON(t *testing.T) {
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
		"mkdir",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file mkdir json: %v", err)
	}
	if !created {
		t.Fatal("mkdir endpoint was not called")
	}
	if strings.Contains(stdout.String(), "created directory:") {
		t.Fatalf("json output includes text mkdir output: %s", stdout.String())
	}
	var snapshot fileMutationSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode mkdir json: %v\n%s", err, stdout.String())
	}
	if snapshot.Action != "mkdir" || snapshot.File.ID != "dir_docs" || snapshot.File.Path != "/workspace/docs" || snapshot.File.Version != 1 || snapshot.Source != nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
}

func TestRunFileMkdirRegistersDeviceWhenMissing(t *testing.T) {
	root := t.TempDir()
	registeredDevice := false
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			registeredDevice = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"test-device","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
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
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_docs","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
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
		"mkdir",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file mkdir: %v", err)
	}
	if !registeredDevice {
		t.Fatal("device was not registered")
	}
	if !created {
		t.Fatal("mkdir endpoint was not called")
	}
	workspace, err := readWorkspaceConfig(filepath.Join(root, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if workspace.DeviceID != "dev_1" || workspace.DeviceName != "test-device" || workspace.DevicePlatform != "windows" {
		t.Fatalf("workspace device = %#v", workspace)
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

func TestRunFileHelpIncludesMutationJSONCommands(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"file", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"synchub-cli file delete --path . --file-id file_1 --json",
		"synchub-cli file move --path . --file-id file_1 --to /workspace/docs/readme.txt --json",
		"synchub-cli file mkdir --path . --remote-path /workspace/docs --json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("file help missing %q: %s", want, out)
		}
	}
}
