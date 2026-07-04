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
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func TestRunSyncPushKeepsConflictCopy(t *testing.T) {
	oldNow := syncPushNow
	syncPushNow = func() time.Time {
		return time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC)
	}
	defer func() {
		syncPushNow = oldNow
	}()

	root := t.TempDir()
	content := []byte("local conflict")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	remoteVersion := int64(7)
	conflictPath := "/workspace/a.conflict-dev_1-20260630T010203.000000000Z.txt"
	var initPaths []string
	conflictCommitted := false
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
			if req.Size != int64(len(content)) || req.SHA256 != testSHA(content) {
				t.Fatalf("unexpected upload content metadata: %#v", req)
			}
			initPaths = append(initPaths, req.Path)
			switch req.Path {
			case "/workspace/a.txt":
				if req.BaseVersion == nil || *req.BaseVersion != remoteVersion {
					t.Fatalf("base version = %#v, want %d", req.BaseVersion, remoteVersion)
				}
				_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_original","path":"/workspace/a.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
			case conflictPath:
				if req.BaseVersion != nil {
					t.Fatalf("conflict copy base version = %#v, want nil", req.BaseVersion)
				}
				_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"upload_id":"upl_conflict","path":"/workspace/a.conflict-dev_1-20260630T010203.000000000Z.txt","chunk_size":1024,"expires_at":"2026-06-30T00:00:00Z","status":"pending","uploaded_chunks":[]}}`))
			default:
				t.Fatalf("upload path = %q", req.Path)
			}
		case r.Method == http.MethodPut && (r.URL.Path == "/api/v1/uploads/upl_original/chunks/0" || r.URL.Path == "/api/v1/uploads/upl_conflict/chunks/0"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read chunk: %v", err)
			}
			if string(body) != string(content) {
				t.Fatalf("chunk body = %q", string(body))
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"chunk_index":0,"size":14,"sha256":"ok"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_original/commit":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"FILE_CONFLICT","message":"base version conflict"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/uploads/upl_conflict/commit":
			conflictCommitted = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"file_id":"file_conflict","version":1,"change_id":10}}`))
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
	want := "uploaded: 1 files\nconflicts kept: 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if len(initPaths) != 2 || initPaths[0] != "/workspace/a.txt" || initPaths[1] != conflictPath {
		t.Fatalf("init paths = %#v", initPaths)
	}
	if !conflictCommitted {
		t.Fatal("conflict copy was not committed")
	}
	updatedManifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("read updated manifest: %v", err)
	}
	if len(updatedManifest.Items) != 1 || updatedManifest.Items[0].RemoteVersion == nil || *updatedManifest.Items[0].RemoteVersion != remoteVersion {
		t.Fatalf("updated manifest = %#v", updatedManifest.Items)
	}
}
