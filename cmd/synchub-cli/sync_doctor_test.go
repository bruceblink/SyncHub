package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRunSyncDoctorShowsHealthyWorkspace(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/devices":
			if got := r.Header.Get("Authorization"); got != "Bearer access" {
				t.Fatalf("authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":"dev_1","name":"laptop","platform":"windows","last_applied_change_id":7,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:01:00Z"}]}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:             1,
		Root:                root,
		RemotePath:          "/workspace",
		ServerURL:           server.URL,
		UserID:              "u1",
		UserEmail:           "user@example.com",
		DeviceID:            "dev_1",
		DeviceName:          "laptop",
		DevicePlatform:      "windows",
		LastAppliedChangeID: 7,
	})
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	remoteVersion := int64(2)
	if err := writeJSONFile(filepath.Join(root, ".synchub", "manifest.json"), manifest.Manifest{
		Version:     1,
		Root:        root,
		RemotePath:  "/workspace",
		GeneratedAt: time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC),
		Items: []manifest.Entry{
			{Path: "/workspace/a.txt", RelativePath: "a.txt", Size: int64(len("alpha")), SHA256: testSHA([]byte("alpha")), RemoteVersion: &remoteVersion},
		},
	}, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"doctor",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync doctor: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"sync doctor: ok",
		"[ok] workspace root: " + root,
		"[ok] workspace config:",
		"[ok] login config:",
		"[ok] server ready: " + server.URL,
		"[ok] auth: token accepted; devices=1",
		"[ok] device: dev_1 name=laptop platform=windows cursor=7",
		"[ok] manifest:",
		"[ok] daemon: not paused",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func TestRunSyncDoctorWarnsForMissingDeviceAndManifest(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/devices":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[]}}`))
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
	if err := writeJSONFile(filepath.Join(root, ".synchub", "daemon-control.json"), syncAgentControl{
		Version:   1,
		Paused:    true,
		UpdatedAt: time.Date(2026, 7, 4, 1, 2, 3, 0, time.UTC),
	}, 0o600); err != nil {
		t.Fatalf("write daemon control: %v", err)
	}
	loginConfigPath := filepath.Join(root, ".synchub", "login.json")
	if err := writeConfig(loginConfigPath, cliConfig{
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"doctor",
		"--path", root,
		"--config", loginConfigPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("sync doctor warning-only report should exit successfully: %v", err)
	}
	var report syncDoctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor json: %v\n%s", err, stdout.String())
	}
	if !report.OK {
		t.Fatalf("report ok = false: %#v", report)
	}
	if !doctorReportHasCheck(report, "device", syncDoctorStatusWarn) {
		t.Fatalf("missing device warning: %#v", report.Checks)
	}
	if !doctorReportHasCheck(report, "manifest", syncDoctorStatusWarn) {
		t.Fatalf("missing manifest warning: %#v", report.Checks)
	}
	if !doctorReportHasCheck(report, "daemon", syncDoctorStatusWarn) {
		t.Fatalf("missing daemon warning: %#v", report.Checks)
	}
	if len(report.Next) == 0 {
		t.Fatalf("expected next steps: %#v", report)
	}
}

func TestRunSyncDoctorFailsOnAuthError(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/devices":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED","message":"invalid token"}`))
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
		ServerURL:            server.URL,
		User:                 clientUser("u1", "user@example.com"),
		Tokens:               client.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
		AccessTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write login config: %v", err)
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"sync",
		"doctor",
		"--path", root,
		"--config", loginConfigPath,
	}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected sync doctor auth failure")
	}
	out := stdout.String()
	for _, want := range []string{
		"sync doctor: failed",
		"[fail] auth: invalid token",
		"[skipped] device: auth check did not pass",
		"synchub-cli login --server " + server.URL,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q: %s", want, out)
		}
	}
}

func doctorReportHasCheck(report syncDoctorReport, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
