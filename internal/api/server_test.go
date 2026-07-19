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
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
	metadatasvc "github.com/bruceblink/SyncHub/internal/metadata"
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

func TestBillingPlansArePublic(t *testing.T) {
	server := New(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("billing plans status = %d body = %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"id":"free"`) || !strings.Contains(body, `"id":"pro"`) {
		t.Fatalf("billing plans body = %s", body)
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

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rootRec, rootReq)
	if rootRec.Code != http.StatusMovedPermanently {
		t.Fatalf("root redirect status = %d", rootRec.Code)
	}
	if got := rootRec.Header().Get("Location"); got != "/app/" {
		t.Fatalf("root redirect location = %q", got)
	}

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

func TestMetadataCORSPermitsBrowserPreflight(t *testing.T) {
	server := New(nil, nil, nil)

	allowed := httptest.NewRequest(http.MethodOptions, "/api/v1/metadata/kvideo/favorites", nil)
	allowed.Header.Set("Origin", "https://kvideo.example.com")
	allowedRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusNoContent || allowedRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("CORS preflight response = %d headers=%v", allowedRec.Code, allowedRec.Header())
	}

}

func TestMetadataCapabilitiesArePublicAndMatchClientContract(t *testing.T) {
	server := New(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/capabilities", nil)
	req.Header.Set("Origin", "https://third-party.example")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("capabilities response = %d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	var body struct {
		Data metadatasvc.Capabilities `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if got := body.Data.Applications["kvideo"].Collections; len(got) != 2 || got[0] != "watch-history" || got[1] != "favorites" {
		t.Fatalf("KVideo collections = %#v", got)
	}
	if got := body.Data.Applications["latestnews"].Collections; len(got) != 3 || got[0] != "reading-history" || got[1] != "favorites" || got[2] != "preferences" {
		t.Fatalf("LatestNews collections = %#v", got)
	}
}

func TestGitHubLoginRequiresConfigurationAndSetsStateCookie(t *testing.T) {
	server := New(nil, nil, nil)

	providers := httptest.NewRecorder()
	server.Handler().ServeHTTP(providers, httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil))
	if providers.Code != http.StatusOK || !strings.Contains(providers.Body.String(), `"github":false`) {
		t.Fatalf("disabled providers response = %d %s", providers.Code, providers.Body.String())
	}

	server.ConfigureGitHubOAuth(&authsvc.GitHubOAuth{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://sync.example/callback"})
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/api/v1/auth/github", nil))
	if login.Code != http.StatusFound || !strings.HasPrefix(login.Header().Get("Location"), "https://github.com/login/oauth/authorize?") {
		t.Fatalf("GitHub login response = %d location=%q", login.Code, login.Header().Get("Location"))
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "synchub_oauth_state" || cookies[0].Value == "" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("OAuth state cookies = %#v", cookies)
	}
}

func TestGitHubCallbackRejectsMissingStateBeforeProviderExchange(t *testing.T) {
	server := New(nil, nil, nil)
	server.ConfigureGitHubOAuth(&authsvc.GitHubOAuth{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://sync.example/callback"})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?code=code&state=missing", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "oauth_error=invalid+OAuth+state") {
		t.Fatalf("invalid callback response = %d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSyncEndpointsAcceptDesktopAPIKeyOrAccountSession(t *testing.T) {
	apiKey := "shk_desktop_test"
	jwtSecret := "sync-web-admin-test-secret"
	repo := &syncKeyRepository{keyHash: authsvc.TokenHash(apiKey)}
	server := New(authsvc.NewService(nil, jwtSecret, 15*time.Minute, 24*time.Hour), nil, repo)

	missingCredentials := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	missingCredentialsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingCredentialsRec, missingCredentials)
	if missingCredentialsRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing sync credentials status = %d body = %s", missingCredentialsRec.Code, missingCredentialsRec.Body.String())
	}

	invalidSession := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	invalidSession.Header.Set("Authorization", "Bearer invalid-session")
	invalidSessionRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidSessionRec, invalidSession)
	if invalidSessionRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid account session status = %d body = %s", invalidSessionRec.Code, invalidSessionRec.Body.String())
	}

	validKey := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	validKey.Header.Set("X-API-Key", apiKey)
	validKeyRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(validKeyRec, validKey)
	if validKeyRec.Code != http.StatusInternalServerError || !strings.Contains(validKeyRec.Body.String(), "sync service is not configured") {
		t.Fatalf("valid API key should pass auth middleware: status=%d body=%s", validKeyRec.Code, validKeyRec.Body.String())
	}

	accountSession := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	accountSession.Header.Set("Authorization", "Bearer "+testAccessToken(t, jwtSecret, "user-1"))
	accountSessionRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(accountSessionRec, accountSession)
	if accountSessionRec.Code != http.StatusInternalServerError || !strings.Contains(accountSessionRec.Body.String(), "sync service is not configured") {
		t.Fatalf("account session should pass auth middleware: status=%d body=%s", accountSessionRec.Code, accountSessionRec.Body.String())
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

type syncKeyRepository struct{ keyHash string }

func (r *syncKeyRepository) Ping(context.Context) error { return nil }
func (r *syncKeyRepository) CreateAPIKey(context.Context, string, string, string, string, string) (domain.APIKey, error) {
	return domain.APIKey{}, errors.New("not implemented")
}
func (r *syncKeyRepository) ListAPIKeys(context.Context, string) ([]domain.APIKey, error) {
	return nil, errors.New("not implemented")
}
func (r *syncKeyRepository) RevokeAPIKey(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (r *syncKeyRepository) GetAPIKeyBySecretHash(_ context.Context, hash string) (domain.APIKey, error) {
	if hash != r.keyHash {
		return domain.APIKey{}, domain.E(domain.CodeNotFound, "api key not found", nil)
	}
	return domain.APIKey{ID: "desktop-key", UserID: "user-1", Application: "synchub-desktop"}, nil
}
func (r *syncKeyRepository) TouchAPIKey(context.Context, string) error { return nil }
func (r *syncKeyRepository) GetSubscription(context.Context, string) (domain.Subscription, error) {
	return domain.Subscription{Plan: "free", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (r *syncKeyRepository) UpdateSubscriptionCancellation(context.Context, string, bool) (domain.Subscription, error) {
	return domain.Subscription{}, errors.New("not implemented")
}
func (r *syncKeyRepository) GetMetadataDocument(context.Context, string, string, string) (domain.MetadataDocument, error) {
	return domain.MetadataDocument{}, errors.New("not implemented")
}
func (r *syncKeyRepository) PutMetadataDocument(context.Context, string, string, string, []byte) (domain.MetadataDocument, error) {
	return domain.MetadataDocument{}, errors.New("not implemented")
}

var _ metadatasvc.Repository = (*syncKeyRepository)(nil)

func (c *fakeReadinessChecker) Ping(ctx context.Context) error {
	_ = ctx
	c.calls++
	return c.err
}
