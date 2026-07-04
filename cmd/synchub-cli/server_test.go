package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
