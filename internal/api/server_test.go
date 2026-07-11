package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	server := New(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Trace-ID", "trace-health")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != float64(0) || body.Message != "ok" {
		t.Fatalf("unexpected response: %#v", body)
	}
	if body.TraceID != "trace-health" {
		t.Fatalf("trace id = %q, want trace-health", body.TraceID)
	}
	if got := rec.Header().Get("X-Trace-ID"); got != "trace-health" {
		t.Fatalf("trace header = %q, want trace-health", got)
	}
}

func TestReadyzChecksDatabaseAndStorage(t *testing.T) {
	db := &fakePinger{}
	store := &fakeReadinessChecker{}
	server := NewWithSyncAndStorage(nil, nil, nil, db, store)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body = %s", rec.Code, rec.Body.String())
	}
	if db.calls != 1 {
		t.Fatalf("database ping calls = %d, want 1", db.calls)
	}
	if store.calls != 1 {
		t.Fatalf("storage ping calls = %d, want 1", store.calls)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
			Checks map[string]struct {
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "ready" || body.Data.Checks["database"].Status != "ready" || body.Data.Checks["storage"].Status != "ready" {
		t.Fatalf("readiness data = %#v", body.Data)
	}
}

func TestVersionEndpoint(t *testing.T) {
	server := New(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("version status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code float64 `json:"code"`
		Data struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body.Code != 0 || body.Data.Name != "SyncHub" || body.Data.Version == "" {
		t.Fatalf("version response = %#v", body)
	}
}

func TestRequestLogIncludesTraceAndStatus(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	server := New(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Trace-ID", "trace-log")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode request log: %v; raw = %s", err, logs.String())
	}
	if entry["msg"] != "api request" {
		t.Fatalf("log msg = %v, want api request", entry["msg"])
	}
	if entry["method"] != http.MethodGet {
		t.Fatalf("method = %v, want GET", entry["method"])
	}
	if entry["path"] != "/healthz" {
		t.Fatalf("path = %v, want /healthz", entry["path"])
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status = %v, want 200", entry["status"])
	}
	if entry["trace_id"] != "trace-log" {
		t.Fatalf("trace_id = %v, want trace-log", entry["trace_id"])
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("duration_ms missing from log: %#v", entry)
	}
}

func TestMetricsEndpointExportsRequestCounters(t *testing.T) {
	server := NewWithSyncAndStorage(nil, nil, nil, &fakePinger{}, &fakeReadinessChecker{err: errors.New("storage unavailable")})

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d body = %s", healthRec.Code, healthRec.Body.String())
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusInternalServerError {
		t.Fatalf("ready status = %d body = %s", readyRec.Code, readyRec.Body.String())
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body = %s", metricsRec.Code, metricsRec.Body.String())
	}
	if got := metricsRec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("metrics content type = %q", got)
	}
	body := metricsRec.Body.String()
	if !strings.Contains(body, `synchub_http_requests_total{method="GET",path="/healthz",status="200"} 1`) {
		t.Fatalf("metrics missing health counter: %s", body)
	}
	if !strings.Contains(body, `synchub_http_requests_total{method="GET",path="/readyz",status="500"} 1`) {
		t.Fatalf("metrics missing readiness error counter: %s", body)
	}
	if !strings.Contains(body, `synchub_http_requests_by_status_class_total{status_class="2xx"} 1`) {
		t.Fatalf("metrics missing 2xx status class counter: %s", body)
	}
	if !strings.Contains(body, `synchub_http_requests_by_status_class_total{status_class="5xx"} 1`) {
		t.Fatalf("metrics missing 5xx status class counter: %s", body)
	}
	if strings.Contains(body, `path="/metrics"`) {
		t.Fatalf("metrics endpoint should not count itself: %s", body)
	}
	if !strings.Contains(body, `synchub_http_request_duration_seconds_total{method="GET",path="/healthz",status="200"}`) {
		t.Fatalf("metrics missing duration counter: %s", body)
	}
}

func TestReadyzFailsWhenStorageIsNotReady(t *testing.T) {
	store := &fakeReadinessChecker{err: errors.New("storage unavailable")}
	server := NewWithSyncAndStorage(nil, nil, nil, &fakePinger{}, store)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body = %s", rec.Code, rec.Body.String())
	}
	var body Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(body.Message, "storage is not ready") {
		t.Fatalf("unexpected response: %#v", body)
	}
	if body.TraceID == "" {
		t.Fatalf("trace id was not set in error response: %#v", body)
	}
}

func TestSwaggerDocs(t *testing.T) {
	server := New(nil, nil, nil)

	uiReq := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	uiRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uiRec, uiReq)
	if uiRec.Code != http.StatusOK {
		t.Fatalf("swagger ui status = %d body = %s", uiRec.Code, uiRec.Body.String())
	}
	if got := uiRec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("swagger ui content type = %q", got)
	}

	specReq := httptest.NewRequest(http.MethodGet, "/swagger/openapi.yaml", nil)
	specRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(specRec, specReq)
	if specRec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d body = %s", specRec.Code, specRec.Body.String())
	}
	if got := specRec.Header().Get("Content-Type"); got != "application/yaml; charset=utf-8" {
		t.Fatalf("openapi content type = %q", got)
	}
	if body := specRec.Body.String(); !strings.Contains(body, "openapi: 3.0.3") || !strings.Contains(body, "/api/v1/auth/register:") {
		t.Fatalf("unexpected openapi body: %s", body)
	}

	redirectReq := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	redirectRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(redirectRec, redirectReq)
	if redirectRec.Code != http.StatusMovedPermanently {
		t.Fatalf("swagger redirect status = %d", redirectRec.Code)
	}
	if got := redirectRec.Header().Get("Location"); got != "/swagger/" {
		t.Fatalf("swagger redirect location = %q", got)
	}
}

func TestAdminUI(t *testing.T) {
	server := New(nil, nil, nil)

	redirectReq := httptest.NewRequest(http.MethodGet, "/app", nil)
	redirectRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(redirectRec, redirectReq)
	if redirectRec.Code != http.StatusMovedPermanently {
		t.Fatalf("admin redirect status = %d", redirectRec.Code)
	}
	if got := redirectRec.Header().Get("Location"); got != "/app/" {
		t.Fatalf("admin redirect location = %q", got)
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/app/", nil)
	uiRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uiRec, uiReq)
	if uiRec.Code != http.StatusOK {
		t.Fatalf("admin ui status = %d body = %s", uiRec.Code, uiRec.Body.String())
	}
	if got := uiRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("admin ui content type = %q", got)
	}
	if !strings.Contains(uiRec.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("admin ui did not include React root: %s", uiRec.Body.String())
	}
}

type fakePinger struct {
	calls int
	err   error
}

func (p *fakePinger) Ping(ctx context.Context) error {
	_ = ctx
	p.calls++
	return p.err
}

type fakeReadinessChecker struct {
	calls int
	err   error
}

func (c *fakeReadinessChecker) Ping(ctx context.Context) error {
	_ = ctx
	c.calls++
	return c.err
}
