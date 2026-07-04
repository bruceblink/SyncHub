package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunSyncOncePushesAndPulls(t *testing.T) {
	root := t.TempDir()
	content := []byte("sync once")
	if err := os.WriteFile(filepath.Join(root, "once.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	registeredDevice := false
	listedChanges := false
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads":
			var req client.InitUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode init upload request: %v", err)
			}
			if req.Path != "/workspace/once.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) || req.DeviceID != "dev_1" {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_once","path":"/workspace/once.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_once/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":9,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_once/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":1,"change_id":2}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			registeredDevice = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"test-device","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			listedChanges = true
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[],"next_cursor":0}}`))
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
		"once",
		"--path", root,
		"--config", loginConfigPath,
		"--device-name", "test-device",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once: %v", err)
	}
	for _, want := range []string{"uploaded: 1 files", "pulled: 0 files", "cursor: 0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %s", want, stdout.String())
		}
	}
	if !registeredDevice || !listedChanges {
		t.Fatalf("sync once did not pull: registered=%v listed=%v", registeredDevice, listedChanges)
	}
	if _, err := readManifest(filepath.Join(root, ".synchub", "manifest.json")); err != nil {
		t.Fatalf("read manifest after sync once: %v", err)
	}
}

func TestRunSyncOnceCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	content := []byte("sync once")
	if err := os.WriteFile(filepath.Join(root, "once.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
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
			if req.Path != "/workspace/once.txt" || req.Size != int64(len(content)) || req.SHA256 != testSHA(content) || req.DeviceID != "dev_1" {
				t.Fatalf("unexpected init upload request: %#v", req)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_once","path":"/workspace/once.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/uploads/upl_once/chunks/0":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":9,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_once/commit":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_1","version":3,"change_id":4}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dev_1","name":"test-device","platform":"windows","last_applied_change_id":0,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[],"next_cursor":0}}`))
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
		"sync",
		"once",
		"--path", root,
		"--config", loginConfigPath,
		"--device-name", "test-device",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once json: %v", err)
	}
	if strings.Contains(stdout.String(), "uploaded:") || strings.Contains(stdout.String(), "pulled:") {
		t.Fatalf("json output includes text sync once output: %s", stdout.String())
	}

	var snapshot syncOnceSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode sync once json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.DryRun {
		t.Fatalf("dry_run = true")
	}
	if snapshot.Push == nil || snapshot.Push.Summary.Uploaded != 1 || len(snapshot.Push.Uploads) != 1 || snapshot.Push.Uploads[0].Version != 3 {
		t.Fatalf("push = %#v", snapshot.Push)
	}
	if snapshot.Pull == nil || snapshot.Pull.Skipped || snapshot.Pull.Result == nil || snapshot.Pull.Result.Summary.Changes != 0 || snapshot.Pull.Result.Cursor != 0 {
		t.Fatalf("pull = %#v", snapshot.Pull)
	}
	if _, err := readManifest(filepath.Join(root, ".synchub", "manifest.json")); err != nil {
		t.Fatalf("read manifest after sync once: %v", err)
	}
}

func TestRunSyncOnceDryRunPreviewsPushAndPull(t *testing.T) {
	root := t.TempDir()
	content := []byte("local preview")
	if err := os.WriteFile(filepath.Join(root, "local.txt"), content, 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sync/changes":
			if got := r.URL.Query().Get("device_id"); got != "dev_1" {
				t.Fatalf("device_id = %q", got)
			}
			if got := r.URL.Query().Get("after_change_id"); got != "4" {
				t.Fatalf("after_change_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":5,"file_id":"file_1","event_type":"update","version":2,"path":"/workspace/remote.txt","created_at":"2026-06-30T00:02:00Z"}],"next_cursor":5}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/devices",
			r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/uploads"),
			r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/chunks/"),
			r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/files/"),
			r.Method == http.MethodPost && r.URL.Path == "/api/v1/sync/ack":
			t.Fatalf("dry run must not mutate remote state: %s %s", r.Method, r.URL.String())
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	workspacePath := filepath.Join(root, ".synchub", "workspace.json")
	if err := writeJSONFile(workspacePath, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		LastAppliedChangeID: 4,
	}, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	manifestPath := filepath.Join(root, ".synchub", "manifest.json")
	if err := writeJSONFile(manifestPath, manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest before dry run: %v", err)
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
	err = run(context.Background(), []string{
		"sync",
		"once",
		"--path", root,
		"--config", loginConfigPath,
		"--dry-run",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once dry run: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"create /workspace/local.txt size=13 base_version=-",
		"uploaded: 1 files",
		"update /workspace/remote.txt version=2 id=5",
		"current cursor: 4",
		"next cursor: 5",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after dry run: %v", err)
	}
	if !bytes.Equal(afterManifest, beforeManifest) {
		t.Fatalf("dry run changed manifest")
	}
	var workspace workspaceConfig
	workspaceRaw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace config: %v", err)
	}
	if err := json.Unmarshal(workspaceRaw, &workspace); err != nil {
		t.Fatalf("decode workspace config: %v", err)
	}
	if workspace.LastAppliedChangeID != 4 {
		t.Fatalf("last applied change id = %d, want 4", workspace.LastAppliedChangeID)
	}
}

func TestRunSyncOnceDryRunSkipsPullWhenDeviceMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("local preview"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry run without device must not call server: %s %s", r.Method, r.URL.String())
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

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"once",
		"--path", root,
		"--config", filepath.Join(root, ".synchub", "missing-login.json"),
		"--dry-run",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once dry run without device: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"create /workspace/local.txt size=13 base_version=-",
		"uploaded: 1 files",
		"pull dry run skipped: workspace device is not registered",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncOnceDryRunWithoutDeviceCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("local preview"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("dry run without device must not call server: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  server.URL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	})

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"once",
		"--path", root,
		"--config", filepath.Join(root, ".synchub", "missing-login.json"),
		"--dry-run",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync once dry run json without device: %v", err)
	}
	if strings.Contains(stdout.String(), "pull dry run skipped") {
		t.Fatalf("json output includes text skip output: %s", stdout.String())
	}

	var snapshot syncOnceSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode sync once dry-run json: %v\n%s", err, stdout.String())
	}
	if !snapshot.DryRun {
		t.Fatalf("dry_run = false")
	}
	if snapshot.Push == nil || !snapshot.Push.DryRun || snapshot.Push.Summary.Uploaded != 1 || len(snapshot.Push.Uploads) != 1 || snapshot.Push.Uploads[0].Path != "/workspace/local.txt" {
		t.Fatalf("push = %#v", snapshot.Push)
	}
	if snapshot.Pull == nil || !snapshot.Pull.Skipped || snapshot.Pull.Reason != "workspace device is not registered" || snapshot.Pull.Result != nil {
		t.Fatalf("pull = %#v", snapshot.Pull)
	}
}
