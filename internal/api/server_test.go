package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	server := New(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
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
