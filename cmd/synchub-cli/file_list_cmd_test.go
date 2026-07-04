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

func TestRunFileList(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != "dir_1" {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("cursor"); got != "dir_0" {
			t.Fatalf("cursor = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "20" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dir_2","parent_id":"dir_1","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"},{"id":"file_1","parent_id":"dir_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}],"next_cursor":"file_1"}}`))
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
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--parent-id", "dir_1",
		"--cursor", "dir_0",
		"--page-size", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 2\ndirectory docs/ path=/workspace/docs size=0 version=1 id=dir_2\nfile a.txt path=/workspace/a.txt size=5 version=2 id=file_1\nnext cursor: file_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListCanOutputJSON(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != "dir_1" {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("cursor"); got != "dir_0" {
			t.Fatalf("cursor = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "20" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"file_1","parent_id":"dir_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":2,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}],"next_cursor":"file_1"}}`))
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
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--parent-id", "dir_1",
		"--cursor", "dir_0",
		"--page-size", "20",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list json: %v", err)
	}
	if strings.Contains(stdout.String(), "files:") {
		t.Fatalf("json output includes text list output: %s", stdout.String())
	}

	var snapshot fileListSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode file list json: %v\n%s", err, stdout.String())
	}
	if snapshot.Workspace.Root != root || snapshot.Workspace.RemotePath != "/workspace" || snapshot.Workspace.UserEmail != "user@example.com" || snapshot.Workspace.DeviceID != "dev_1" {
		t.Fatalf("workspace = %#v", snapshot.Workspace)
	}
	if snapshot.ParentID != "dir_1" || snapshot.Cursor != "dir_0" || snapshot.NextCursor != "file_1" {
		t.Fatalf("snapshot cursor fields = %#v", snapshot)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "file_1" || snapshot.Items[0].Path != "/workspace/a.txt" {
		t.Fatalf("items = %#v", snapshot.Items)
	}
}

func TestRunFileHelpIncludesFileListJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"file", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli file list --path . --json") {
		t.Fatalf("file help missing list json command: %s", stdout.String())
	}
}

func TestRunFileListByRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/docs" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"dir_docs","name":"docs","path":"/workspace/docs","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files":
			if got := r.URL.Query().Get("parent_id"); got != "dir_docs" {
				t.Fatalf("parent_id = %q", got)
			}
			if got := r.URL.Query().Get("page_size"); got != "20" {
				t.Fatalf("page_size = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"file_1","parent_id":"dir_docs","name":"guide.md","path":"/workspace/docs/guide.md","node_type":"file","size":9,"sha256":"sha1","version":3,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}]}}`))
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
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/docs",
		"--page-size", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 1\nfile guide.md path=/workspace/docs/guide.md size=9 version=3 id=file_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListRootRemotePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/files" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("parent_id"); got != "" {
			t.Fatalf("parent_id = %q", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Fatalf("page_size = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dir_1","name":"workspace","path":"/workspace","node_type":"directory","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}]}}`))
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
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	want := "files: 1\ndirectory workspace/ path=/workspace size=0 version=1 id=dir_1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunFileListRejectsRemoteFilePath(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/files/by-path":
			if got := r.URL.Query().Get("path"); got != "/workspace/a.txt" {
				t.Fatalf("path = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"id":"file_1","name":"a.txt","path":"/workspace/a.txt","node_type":"file","size":5,"sha256":"sha1","version":1,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}}`))
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

	err := run(context.Background(), []string{
		"file",
		"list",
		"--path", root,
		"--config", loginConfigPath,
		"--remote-path", "/workspace/a.txt",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remote path is not a directory: /workspace/a.txt") {
		t.Fatalf("error = %v, want remote path is not a directory", err)
	}
}
