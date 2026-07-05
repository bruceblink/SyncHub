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
)

func TestRunServerStatusShowsPublicHealthEndpoints(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"name":"SyncHub","version":"0.1.0"}}`))
		case "/healthz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ok"}}`))
		case "/readyz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "status", "--server", server.URL}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server status: %v", err)
	}
	want := "server: " + server.URL + "\nversion: SyncHub 0.1.0\nhealth: ok\nready: ready\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if strings.Join(requests, ",") != "/version,/healthz,/readyz" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRunServerStatusCanOutputJSON(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"name":"SyncHub","version":"0.1.0"}}`))
		case "/healthz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ok"}}`))
		case "/readyz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "status", "--server", server.URL, "--json"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server status json: %v", err)
	}
	if strings.Contains(stdout.String(), "server:") {
		t.Fatalf("json output includes text status output: %s", stdout.String())
	}
	var snapshot serverStatusSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode server status json: %v\n%s", err, stdout.String())
	}
	if snapshot.Server != server.URL || snapshot.Version.Name != "SyncHub" || snapshot.Version.Version != "0.1.0" {
		t.Fatalf("snapshot version = %#v", snapshot)
	}
	if snapshot.Health.Status != "ok" || snapshot.Ready.Status != "ready" {
		t.Fatalf("snapshot status = %#v", snapshot)
	}
	if strings.Join(requests, ",") != "/version,/healthz,/readyz" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRunServerStatusReportsReadinessFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"name":"SyncHub","version":"0.1.0"}}`))
		case "/healthz":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ok"}}`))
		case "/readyz":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"database is not ready"}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	err := run(context.Background(), []string{"server", "status", "--server", server.URL}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "readiness check failed: database is not ready") {
		t.Fatalf("error = %v, want readiness failure", err)
	}
}

func TestRunServerWaitRetriesUntilReady(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/readyz" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"database is not ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"server",
		"wait",
		"--server", server.URL,
		"--timeout", "1s",
		"--interval", "1ms",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server wait: %v", err)
	}
	if calls != 2 {
		t.Fatalf("ready calls = %d, want 2", calls)
	}
	want := "server ready: " + server.URL + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunServerWaitCanOutputJSON(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/readyz" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"database is not ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"status":"ready"}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"server",
		"wait",
		"--server", server.URL,
		"--timeout", "1s",
		"--interval", "1ms",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server wait json: %v", err)
	}
	if strings.Contains(stdout.String(), "server ready:") {
		t.Fatalf("json output includes text wait output: %s", stdout.String())
	}
	var snapshot serverWaitSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode server wait json: %v\n%s", err, stdout.String())
	}
	if snapshot.Server != server.URL || snapshot.Ready.Status != "ready" || snapshot.Attempts != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunServerWaitTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/readyz" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"database is not ready"}`))
	}))
	defer server.Close()

	err := run(context.Background(), []string{
		"server",
		"wait",
		"--server", server.URL,
		"--timeout", "1ms",
		"--interval", "1ms",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "server was not ready before timeout") || !strings.Contains(err.Error(), "database is not ready") {
		t.Fatalf("error = %v, want timeout with readiness reason", err)
	}
}

func TestRunServerMetricsPrintsPrometheusText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/metrics" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte("# TYPE synchub_http_requests_total counter\nsynchub_http_requests_total 1\n"))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "metrics", "--server", server.URL}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server metrics: %v", err)
	}
	want := "# TYPE synchub_http_requests_total counter\nsynchub_http_requests_total 1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunServerMetricsReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"metrics unavailable"}`))
	}))
	defer server.Close()

	err := run(context.Background(), []string{"server", "metrics", "--server", server.URL}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "metrics check failed: metrics unavailable") {
		t.Fatalf("error = %v, want metrics failure", err)
	}
}

func TestRunServerOpenAPIPrintsSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swagger/openapi.yaml" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("openapi: 3.0.3\ninfo:\n  title: SyncHub API\n"))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "openapi", "--server", server.URL}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server openapi: %v", err)
	}
	want := "openapi: 3.0.3\ninfo:\n  title: SyncHub API\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunServerOpenAPICanOutputJSON(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: SyncHub API\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swagger/openapi.yaml" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(spec))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "openapi", "--server", server.URL, "--json"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server openapi json: %v", err)
	}
	if strings.HasPrefix(stdout.String(), "openapi:") {
		t.Fatalf("json output includes raw spec output: %s", stdout.String())
	}
	var snapshot serverOpenAPISnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode server openapi json: %v\n%s", err, stdout.String())
	}
	if snapshot.Server != server.URL || snapshot.Bytes != len([]byte(spec)) || snapshot.Spec != spec || snapshot.Output != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunServerOpenAPIWritesSpecToOutputFile(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: SyncHub API\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swagger/openapi.yaml" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(spec))
	}))
	defer server.Close()

	outputPath := filepath.Join(t.TempDir(), "generated", "openapi.yaml")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("old spec"), 0o644); err != nil {
		t.Fatalf("write old output: %v", err)
	}
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"server",
		"openapi",
		"--server", server.URL,
		"--output", outputPath,
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server openapi output: %v", err)
	}
	wantStdout := "openapi written: " + outputPath + "\n"
	if stdout.String() != wantStdout {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantStdout)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read openapi output: %v", err)
	}
	if string(raw) != spec {
		t.Fatalf("output file = %q, want %q", string(raw), spec)
	}
}

func TestRunServerOpenAPIOutputCanOutputJSON(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: SyncHub API\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/swagger/openapi.yaml" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(spec))
	}))
	defer server.Close()

	outputPath := filepath.Join(t.TempDir(), "generated", "openapi.yaml")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"server",
		"openapi",
		"--server", server.URL,
		"--output", outputPath,
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server openapi output json: %v", err)
	}
	if strings.Contains(stdout.String(), "openapi written:") {
		t.Fatalf("json output includes text output: %s", stdout.String())
	}
	var snapshot serverOpenAPISnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode server openapi output json: %v\n%s", err, stdout.String())
	}
	if snapshot.Server != server.URL || snapshot.Output != outputPath || snapshot.Bytes != len([]byte(spec)) || snapshot.Spec != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read openapi output: %v", err)
	}
	if string(raw) != spec {
		t.Fatalf("output file = %q, want %q", string(raw), spec)
	}
}

func TestRunServerOpenAPIReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"INTERNAL","message":"openapi unavailable"}`))
	}))
	defer server.Close()

	err := run(context.Background(), []string{"server", "openapi", "--server", server.URL}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "openapi check failed: openapi unavailable") {
		t.Fatalf("error = %v, want openapi failure", err)
	}
}

func TestRunServerHelpIncludesStatusJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", "help"}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"synchub-cli server status --server http://localhost:8765 --json",
		"synchub-cli server wait --server http://localhost:8765 --timeout 30s --json",
		"synchub-cli server openapi --server http://localhost:8765 --output ./openapi.yaml --json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("server help missing %q: %s", want, out)
		}
	}
}
