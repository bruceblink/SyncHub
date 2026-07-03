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

func TestRunSyncDevicesShowsRegisteredDevices(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/devices" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dev_1","name":"laptop","platform":"windows","last_seen_at":"2026-06-30T00:03:00Z","last_applied_change_id":9,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:04:00Z"},{"id":"dev_2","name":"desktop","platform":"linux","last_applied_change_id":2,"created_at":"2026-06-30T00:01:00Z","updated_at":"2026-06-30T00:02:00Z"}]}}`))
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
		"sync",
		"devices",
		"--path", root,
		"--config", loginConfigPath,
		"--limit", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync devices: %v", err)
	}
	want := "devices: 2\n* dev_1 name=laptop platform=windows cursor=9 last_seen=2026-06-30T00:03:00Z updated=2026-06-30T00:04:00Z\n- dev_2 name=desktop platform=linux cursor=2 last_seen=- updated=2026-06-30T00:02:00Z\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunSyncDevices(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/devices" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "25" {
			t.Fatalf("limit = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dev_1","name":"laptop","platform":"windows","last_seen_at":"2026-06-30T00:01:00Z","last_applied_change_id":7,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"},{"id":"dev_2","name":"desktop","platform":"linux","last_applied_change_id":3,"created_at":"2026-06-30T00:02:00Z","updated_at":"2026-06-30T00:02:00Z"}]}}`))
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
		"sync",
		"devices",
		"--path", root,
		"--config", loginConfigPath,
		"--limit", "25",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync devices: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"devices: 2",
		"* dev_1 name=laptop platform=windows cursor=7 last_seen=2026-06-30T00:01:00Z updated=2026-06-30T00:01:00Z",
		"- dev_2 name=desktop platform=linux cursor=3 last_seen=- updated=2026-06-30T00:02:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}
