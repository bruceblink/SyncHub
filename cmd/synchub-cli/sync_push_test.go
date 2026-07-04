package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunSyncPushUploadsManifestFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(root, "nested", "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var dirs []string
	var chunks []string
	committed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/refresh" {
			var req struct {
				RefreshToken string `json:"refresh_token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode refresh request: %v", err)
			}
			if req.RefreshToken != "refresh" {
				t.Fatalf("refresh token = %q", req.RefreshToken)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"access_token":"access-new","refresh_token":"refresh-new","expires_in":900}}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-new" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode directory request: %v", err)
			}
			dirs = append(dirs, req.Path)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"dir","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			if got := r.Header.Get("Idempotency-Key"); got == "" {
				t.Fatal("missing idempotency key")
			}
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/nested/a.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) || req.DeviceID != "dev_1" {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/nested/a.txt","chunk_size":3,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/uploads/upl_1/chunks/"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if got := r.Header.Get("X-Chunk-Sha256"); got != testSHA(body) {
				t.Fatalf("chunk sha = %q, want %q", got, testSHA(body))
			}
			chunks = append(chunks, string(body))
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":3,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			committed = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
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
			{Path: "/workspace/nested/a.txt", RelativePath: "nested/a.txt", Size: int64(len(content)), SHA256: testSHA(content)},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(-time.Minute),
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
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(dirs) != 2 || dirs[0] != "/workspace" || dirs[1] != "/workspace/nested" {
		t.Fatalf("dirs = %#v", dirs)
	}
	if len(chunks) != 2 || chunks[0] != "hel" || chunks[1] != "lo" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if !committed {
		t.Fatal("upload was not committed")
	}
	refreshedConfig, err := readConfig(loginConfigPath)
	if err != nil {
		t.Fatalf("read refreshed config: %v", err)
	}
	if refreshedConfig.Tokens.AccessToken != "access-new" || refreshedConfig.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("refreshed tokens = %#v", refreshedConfig.Tokens)
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 1 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(root, "nested", "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	committed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"dir","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/nested/a.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) || req.DeviceID != "dev_1" {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/nested/a.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_1/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":5,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			committed = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":9,"change_id":10}}`))
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
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
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
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push json: %v", err)
	}
	if strings.Contains(stdout.String(), "uploaded:") {
		t.Fatalf("json output includes text push output: %s", stdout.String())
	}

	var snapshot syncPushSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode push json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.DryRun {
		t.Fatalf("dry_run = true")
	}
	if snapshot.Summary.Changes != 1 || snapshot.Summary.Uploaded != 1 || snapshot.Summary.Deleted != 0 || snapshot.Summary.Moved != 0 || snapshot.Summary.ConflictsKept != 0 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
	if len(snapshot.Uploads) != 1 || snapshot.Uploads[0].Action != "create" || snapshot.Uploads[0].Path != "/workspace/nested/a.txt" || snapshot.Uploads[0].RelativePath != "nested/a.txt" || snapshot.Uploads[0].Size != int64(len(content)) || snapshot.Uploads[0].SHA256 != testSHA(content) || snapshot.Uploads[0].Version != 9 {
		t.Fatalf("uploads = %#v", snapshot.Uploads)
	}
	if len(snapshot.Deletes) != 0 || len(snapshot.Moves) != 0 {
		t.Fatalf("deletes/moves = %#v %#v", snapshot.Deletes, snapshot.Moves)
	}
	if !committed {
		t.Fatal("upload was not committed")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 9 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushSkipsAlreadyUploadedChunks(t *testing.T) {
	root := t.TempDir()
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var chunkIndexes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_resume","path":"/workspace/a.txt","chunk_size":3,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[{"chunk_index":0,"size":3,"sha256":"` + testSHA([]byte("hel")) + `"}]}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/uploads/upl_resume/chunks/"):
			chunkIndexes = append(chunkIndexes, pathpkg.Base(r.URL.Path))
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != "lo" {
				t.Fatalf("chunk body = %q, want lo", string(body))
			}
			if got := r.Header.Get("X-Chunk-Sha256"); got != testSHA([]byte("lo")) {
				t.Fatalf("chunk sha = %q, want %q", got, testSHA([]byte("lo")))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":1,"size":2,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_resume/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
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
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(chunkIndexes) != 1 || chunkIndexes[0] != "1" {
		t.Fatalf("uploaded chunk indexes = %#v, want [1]", chunkIndexes)
	}
}

func TestRunSyncPushDiscoversNewLocalFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("new file")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), content, 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	committed := false
	registeredDevice := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
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
				t.Fatalf("decode device request: %v", err)
			}
			if req.Name != "push-device" || req.Platform != "test-os" {
				t.Fatalf("device request = %#v", req)
			}
			registeredDevice = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"push-device","platform":"test-os","last_applied_change_id":4,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/new.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			if req.DeviceID != "dev_1" {
				t.Fatalf("device id = %q, want dev_1", req.DeviceID)
			}
			if req.BaseVersion != nil {
				t.Fatalf("base version = %#v, want nil", req.BaseVersion)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/new.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_1/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":8,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			committed = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
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
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
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
		"--device-name", "push-device",
		"--platform", "test-os",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync push: %v", err)
	}
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !committed {
		t.Fatal("upload was not committed")
	}
	if !registeredDevice {
		t.Fatal("device was not registered")
	}
	var workspace workspaceConfig
	workspaceRaw, err := os.ReadFile(filepath.Join(root, ".synchub", "workspace.json"))
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.DeviceID != "dev_1" || workspace.DeviceName != "push-device" || workspace.DevicePlatform != "test-os" || workspace.LastAppliedChangeID != 4 {
		t.Fatalf("workspace device = %#v", workspace)
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].Path != "/workspace/new.txt" || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 1 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushSkipsUnchangedManifestFiles(t *testing.T) {
	root := t.TempDir()
	content := []byte("unchanged")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	remoteVersion := int64(5)
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Now().UTC(),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len(content)), SHA256: testSHA(content), RemoteVersion: &remoteVersion},
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
	if stdout.String() != "uploaded: 0 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != remoteVersion {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}

func TestRunSyncPushWritesManifestForEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
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
	if stdout.String() != "uploaded: 0 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	m, err := readManifest(filepath.Join(root, ".synchub", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Items) != 0 || m.Root != root || m.RemotePath != "/workspace" {
		t.Fatalf("manifest = %#v", m)
	}
}

func TestRunSyncPushSendsManifestRemoteVersion(t *testing.T) {
	root := t.TempDir()
	content := []byte("local change")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	remoteVersion := int64(7)
	baseVersionSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/directories":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/a.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			if req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
				t.Fatalf("base version = %#v, want %d", req.BaseVersion, remoteVersion)
			}
			baseVersionSeen = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_1","path":"/workspace/a.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_1/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":12,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_1/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":8,"change_id":9}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
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
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("old content")), SHA256: testSHA([]byte("old content")), RemoteVersion: &remoteVersion},
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
	if stdout.String() != "uploaded: 1 files\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !baseVersionSeen {
		t.Fatal("base version was not sent")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != 8 {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}
