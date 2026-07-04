package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunSyncPushMovesRenamedManifestFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("same content")
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), content, 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	remoteVersion := int64(3)
	moved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			if req.Path != "/workspace" {
				t.Fatalf("directory path = %q", req.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/old.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"old.txt","path":"/workspace/old.txt","node_type":"file","version":3}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/files/file_1":
			var req struct {
				Path        string `json:"path"`
				DeviceID    string `json:"device_id"`
				BaseVersion *int64 `json:"base_version"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode move request: %v", err)
			}
			if req.Path != "/workspace/renamed.txt" {
				t.Fatalf("move path = %q", req.Path)
			}
			if req.DeviceID != "dev_1" || req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
				t.Fatalf("move request = %#v", req)
			}
			moved = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"renamed.txt","path":"/workspace/renamed.txt","node_type":"file","version":4}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len(content)), SHA256: testSHA(content), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
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
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	want := "uploaded: 0 files\nmoved: 1 files\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if !moved {
		t.Fatal("file was not moved")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].Path != "/workspace/renamed.txt" || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 4 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushKeepsRenamedManifestFileOnConflict(t *testing.T) {
	root := t.TempDir()
	content := []byte("same content")
	if err := os.WriteFile(filepath.Join(root, "renamed.txt"), content, 0o644); err != nil {
		t.Fatalf("write renamed file: %v", err)
	}
	remoteVersion := int64(3)
	uploaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/old.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"old.txt","path":"/workspace/old.txt","node_type":"file","version":4}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/files/file_1":
			var req struct {
				Path        string `json:"path"`
				BaseVersion *int64 `json:"base_version"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode move request: %v", err)
			}
			if req.Path != "/workspace/renamed.txt" || req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
				t.Fatalf("move request = %#v", req)
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"FILE_CONFLICT","message":"base version conflict"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			uploaded = true
			t.Fatalf("renamed file should not be uploaded after move conflict")
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
		DeviceID:   "dev_1",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/old.txt", RelativePath: "old.txt", Size: int64(len(content)), SHA256: testSHA(content), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
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
		"sync",
		"push",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	want := "uploaded: 0 files\nconflicts kept: 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if uploaded {
		t.Fatal("renamed file was uploaded after move conflict")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].Path != "/workspace/old.txt" || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != remoteVersion {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}
