package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	metadatasvc "github.com/bruceblink/SyncHub/internal/metadata"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
	"github.com/bruceblink/SyncHub/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const traceIDKey = "trace_id"

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	router   *gin.Engine
	auth     *authsvc.Service
	files    *filesvc.Service
	sync     *syncsvc.Service
	metadata *metadatasvc.Service
	db       Pinger
	storage  storage.ReadinessChecker
	metrics  *requestMetrics
	github   *authsvc.GitHubOAuth
}

func New(auth *authsvc.Service, files *filesvc.Service, db Pinger) *Server {
	return NewWithSync(auth, files, nil, db)
}

func NewWithSync(auth *authsvc.Service, files *filesvc.Service, sync *syncsvc.Service, db Pinger) *Server {
	return NewWithSyncAndStorage(auth, files, sync, db, nil)
}

func NewWithSyncAndStorage(auth *authsvc.Service, files *filesvc.Service, sync *syncsvc.Service, db Pinger, store storage.ReadinessChecker) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	metrics := newRequestMetrics()
	r.Use(gin.Recovery())
	r.Use(traceMiddleware())
	r.Use(requestMetricsMiddleware(metrics))
	r.Use(requestLogMiddleware())
	var metadataService *metadatasvc.Service
	if repository, ok := db.(metadatasvc.Repository); ok {
		metadataService = metadatasvc.NewService(repository)
	}
	s := &Server{router: r, auth: auth, files: files, sync: sync, metadata: metadataService, db: db, storage: store, metrics: metrics}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) ConfigureGitHubOAuth(github *authsvc.GitHubOAuth) {
	s.github = github
}

func traceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := strings.TrimSpace(c.GetHeader("X-Trace-ID"))
		if traceID == "" {
			traceID = strings.TrimSpace(c.GetHeader("X-Request-ID"))
		}
		if traceID == "" {
			traceID = uuid.NewString()
		}
		c.Set(traceIDKey, traceID)
		c.Header("X-Trace-ID", traceID)
		c.Next()
	}
}

func requestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		slog.InfoContext(c.Request.Context(), "api request",
			"method", c.Request.Method,
			"path", requestLogPath(c),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
			"trace_id", traceID(c),
		)
	}
}

func requestLogPath(c *gin.Context) string {
	if path := c.FullPath(); path != "" {
		return path
	}
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}

func (s *Server) routes() {
	s.router.GET("/healthz", func(c *gin.Context) { ok(c, gin.H{"status": "ok"}) })
	s.router.GET("/readyz", func(c *gin.Context) {
		checks := gin.H{}
		if s.db != nil {
			if err := s.db.Ping(c.Request.Context()); err != nil {
				fail(c, domain.E(domain.CodeInternal, "database is not ready", err))
				return
			}
			checks["database"] = gin.H{"status": "ready"}
		}
		if s.storage != nil {
			if err := s.storage.Ping(c.Request.Context()); err != nil {
				fail(c, domain.E(domain.CodeInternal, "storage is not ready", err))
				return
			}
			checks["storage"] = gin.H{"status": "ready"}
		}
		data := gin.H{"status": "ready"}
		if len(checks) > 0 {
			data["checks"] = checks
		}
		ok(c, data)
	})
	s.router.GET("/version", func(c *gin.Context) {
		ok(c, gin.H{"name": version.Name, "version": version.Version})
	})
	s.router.GET("/metrics", s.metricsHandler)
	s.router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/app/") })
	s.router.GET("/docs", swaggerRedirect)
	s.router.GET("/swagger", swaggerRedirect)
	s.router.GET("/swagger/", swaggerUI)
	s.router.GET("/swagger/openapi.yaml", swaggerSpec)
	s.router.GET("/app", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/app/") })
	s.router.GET("/app/*path", s.adminUI)

	v1 := s.router.Group("/api/v1")
	v1.POST("/auth/register", s.register)
	v1.POST("/auth/login", s.login)
	v1.POST("/auth/refresh", s.refresh)
	v1.POST("/auth/logout", s.logout)
	v1.GET("/auth/providers", s.authProviders)
	v1.GET("/auth/github", s.githubLogin)
	v1.GET("/auth/github/callback", s.githubCallback)
	v1.POST("/auth/oauth/exchange", s.oauthExchange)
	v1.GET("/billing/plans", s.billingPlans)
	v1.OPTIONS("/metadata/capabilities", s.metadataCORSMiddleware())
	v1.GET("/metadata/capabilities", s.metadataCORSMiddleware(), s.metadataCapabilities)

	protected := v1.Group("")
	protected.Use(s.requireAuth())
	protected.GET("/account/usage", s.usage)
	protected.GET("/account/subscription", s.subscription)
	protected.GET("/account/billing", s.billingOverview)
	protected.POST("/account/subscription/cancel", s.cancelSubscription)
	protected.POST("/account/subscription/resume", s.resumeSubscription)
	protected.GET("/account/api-keys", s.listAPIKeys)
	protected.POST("/account/api-keys", s.createAPIKey)
	protected.DELETE("/account/api-keys/:id", s.revokeAPIKey)
	protected.DELETE("/account", s.deleteAccount)

	syncAPI := v1.Group("")
	syncAPI.Use(s.requireSyncKey())
	syncAPI.GET("/files/:id", s.getFile)
	syncAPI.GET("/files/:id/versions", s.listFileVersions)
	syncAPI.POST("/files/:id/versions/:version/restore", s.restoreFileVersion)
	syncAPI.POST("/files/:id/versions/:version/pin", s.pinFileVersion)
	syncAPI.DELETE("/files/:id/versions/:version/pin", s.unpinFileVersion)
	syncAPI.GET("/files/by-path", s.getFileByPath)
	syncAPI.GET("/files", s.listFiles)
	syncAPI.GET("/files/search", s.searchFiles)
	syncAPI.GET("/trash", s.listTrash)
	syncAPI.POST("/trash/:id/restore", s.restoreTrash)
	syncAPI.DELETE("/trash/:id", s.purgeTrash)
	syncAPI.POST("/files/directories", s.createDirectory)
	syncAPI.PATCH("/files/:id", s.moveFile)
	syncAPI.DELETE("/files/:id", s.deleteFile)
	syncAPI.GET("/files/:id/content", s.download)
	syncAPI.POST("/uploads", s.initUpload)
	syncAPI.PUT("/uploads/:id/chunks/:index", s.putChunk)
	syncAPI.GET("/uploads/:id", s.uploadStatus)
	syncAPI.DELETE("/uploads/:id", s.abortUpload)
	syncAPI.POST("/uploads/:id/commit", s.commitUpload)
	syncAPI.GET("/devices", s.listDevices)
	syncAPI.POST("/devices", s.registerDevice)
	syncAPI.DELETE("/devices/:id", s.revokeDevice)
	syncAPI.POST("/devices/:id/heartbeat", s.heartbeatDevice)
	syncAPI.GET("/activity", s.listActivity)
	syncAPI.GET("/sync/changes", s.listChanges)
	syncAPI.POST("/sync/ack", s.ackChanges)
	syncAPI.GET("/sync/conflicts", s.listSyncConflicts)
	syncAPI.PATCH("/sync/conflicts/:id", s.resolveSyncConflict)

	metadataAPI := v1.Group("/metadata/:application")
	metadataAPI.Use(s.metadataCORSMiddleware())
	metadataAPI.OPTIONS("/:collection", func(c *gin.Context) {})
	metadataAPI.Use(s.requireMetadataKey())
	metadataAPI.GET("/:collection", s.getMetadataDocument)
	metadataAPI.PUT("/:collection", s.putMetadataDocument)
}

func (s *Server) billingPlans(c *gin.Context) {
	ok(c, gin.H{"items": []gin.H{
		{"id": "free", "name": "Free", "currency": "USD", "unit_amount": 0, "billing_interval": "month", "features": []string{"application_metadata_sync", "api_key_management"}},
		{"id": "pro", "name": "Pro", "currency": "USD", "unit_amount": 500, "billing_interval": "month", "features": []string{"application_metadata_sync", "api_key_management", "priority_support"}},
	}})
}

func (s *Server) metadataCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Trace-ID")
		c.Header("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) metadataCapabilities(c *gin.Context) {
	ok(c, metadatasvc.MetadataCapabilities())
}

func (s *Server) subscription(c *gin.Context) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	subscription, err := s.metadata.Subscription(c.Request.Context(), userID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, subscriptionDTO(subscription))
}

func (s *Server) billingOverview(c *gin.Context) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	subscription, err := s.metadata.Subscription(c.Request.Context(), userID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{
		"subscription":                subscriptionDTO(subscription),
		"payment_provider_configured": subscription.Provider != nil,
	})
}

func (s *Server) cancelSubscription(c *gin.Context) {
	s.updateSubscriptionCancellation(c, true)
}

func (s *Server) resumeSubscription(c *gin.Context) {
	s.updateSubscriptionCancellation(c, false)
}

func (s *Server) updateSubscriptionCancellation(c *gin.Context, cancel bool) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	subscription, err := s.metadata.UpdateSubscriptionCancellation(c.Request.Context(), userID(c), cancel)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, subscriptionDTO(subscription))
}

func (s *Server) listAPIKeys(c *gin.Context) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	keys, err := s.metadata.ListAPIKeys(c.Request.Context(), userID(c))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, apiKeyDTO(key))
	}
	ok(c, gin.H{"items": items})
}

func (s *Server) createAPIKey(c *gin.Context) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	var req struct {
		Name        string `json:"name"`
		Application string `json:"application"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	key, secret, err := s.metadata.CreateAPIKey(c.Request.Context(), userID(c), req.Name, req.Application)
	if err != nil {
		fail(c, err)
		return
	}
	created(c, gin.H{"api_key": apiKeyDTO(key), "secret": secret})
}

func (s *Server) revokeAPIKey(c *gin.Context) {
	if s.metadata == nil {
		fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
		return
	}
	if err := s.metadata.RevokeAPIKey(c.Request.Context(), userID(c), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"revoked": true})
}

func (s *Server) deleteAccount(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	if err := s.auth.DeleteAccount(c.Request.Context(), userID(c), req.Email, req.Password); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s *Server) getMetadataDocument(c *gin.Context) {
	document, err := s.metadata.GetDocument(c.Request.Context(), userID(c), c.Param("application"), c.Param("collection"))
	if err != nil {
		if domain.ErrorCodeOf(err) == domain.CodeNotFound {
			ok(c, gin.H{"payload": json.RawMessage("null"), "version": 0})
			return
		}
		fail(c, err)
		return
	}
	ok(c, metadataDocumentDTO(document))
}

func (s *Server) putMetadataDocument(c *gin.Context) {
	var req struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	document, err := s.metadata.PutDocument(c.Request.Context(), userID(c), c.Param("application"), c.Param("collection"), req.Payload)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, metadataDocumentDTO(document))
}

func (s *Server) register(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	user, tokens, err := s.auth.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		fail(c, err)
		return
	}
	created(c, gin.H{"user": userDTO(user), "tokens": tokens})
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	user, tokens, err := s.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"user": userDTO(user), "tokens": tokens})
}

func (s *Server) refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	tokens, err := s.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, tokens)
}

func (s *Server) logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := s.auth.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{})
}

func (s *Server) authProviders(c *gin.Context) {
	ok(c, gin.H{"github": s.github != nil && s.github.Enabled()})
}

func (s *Server) githubLogin(c *gin.Context) {
	if s.github == nil || !s.github.Enabled() {
		fail(c, domain.E(domain.CodeNotFound, "GitHub login is not configured", nil))
		return
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		fail(c, domain.E(domain.CodeInternal, "failed to create OAuth state", err))
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	http.SetCookie(c.Writer, &http.Cookie{
		Name: "synchub_oauth_state", Value: state, Path: "/api/v1/auth/github",
		MaxAge: 600, HttpOnly: true, Secure: c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, s.github.AuthorizationURL(state))
}

func (s *Server) githubCallback(c *gin.Context) {
	if s.github == nil || !s.github.Enabled() {
		s.redirectOAuthResult(c, "", "GitHub login is not configured")
		return
	}
	stateCookie, err := c.Request.Cookie("synchub_oauth_state")
	http.SetCookie(c.Writer, &http.Cookie{Name: "synchub_oauth_state", Value: "", Path: "/api/v1/auth/github", MaxAge: -1, HttpOnly: true, Secure: c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode})
	if err != nil || stateCookie.Value == "" || stateCookie.Value != c.Query("state") {
		s.redirectOAuthResult(c, "", "invalid OAuth state")
		return
	}
	identity, err := s.github.Exchange(c.Request.Context(), c.Query("code"))
	if err != nil {
		s.redirectOAuthResult(c, "", err.Error())
		return
	}
	code, err := s.auth.CompleteOAuthLogin(c.Request.Context(), "github", identity.UserID, identity.Email, identity.Login, identity.AvatarURL)
	if err != nil {
		s.redirectOAuthResult(c, "", err.Error())
		return
	}
	s.redirectOAuthResult(c, code, "")
}

func (s *Server) redirectOAuthResult(c *gin.Context, code, message string) {
	query := url.Values{}
	if code != "" {
		query.Set("oauth_code", code)
	}
	if message != "" {
		query.Set("oauth_error", message)
	}
	c.Redirect(http.StatusFound, "/app/?"+query.Encode())
}

func (s *Server) oauthExchange(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	user, tokens, err := s.auth.ExchangeOAuthLoginCode(c.Request.Context(), req.Code)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"user": userDTO(user), "tokens": tokens})
}

func (s *Server) getFile(c *gin.Context) {
	node, err := s.files.GetByID(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileDTO(node))
}

func (s *Server) getFileByPath(c *gin.Context) {
	node, err := s.files.GetByPath(c.Request.Context(), userID(c), c.Query("path"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileDTO(node))
}

func (s *Server) listFiles(c *gin.Context) {
	var parentID *string
	if v := c.Query("parent_id"); v != "" {
		parentID = &v
	}
	limit := int32(100)
	if raw := c.Query("page_size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = int32(parsed)
		}
	}
	result, err := s.files.List(c.Request.Context(), userID(c), parentID, c.Query("cursor"), limit)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]any, 0, len(result.Items))
	for _, node := range result.Items {
		data = append(data, fileDTO(node))
	}
	ok(c, gin.H{"items": data, "next_cursor": result.NextCursor})
}

func (s *Server) searchFiles(c *gin.Context) {
	limit := int32(100)
	if raw := c.Query("page_size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = int32(parsed)
		}
	}
	result, err := s.files.Search(c.Request.Context(), userID(c), c.Query("q"), c.Query("cursor"), limit)
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(result.Items))
	for _, node := range result.Items {
		items = append(items, fileDTO(node))
	}
	ok(c, gin.H{"items": items, "next_cursor": result.NextCursor, "retention_days": s.files.TrashRetentionDays()})
}

func (s *Server) usage(c *gin.Context) {
	usage, err := s.files.Usage(c.Request.Context(), userID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"file_count": usage.FileCount, "bytes_used": usage.BytesUsed, "quota_bytes": usage.QuotaBytes})
}

func (s *Server) listTrash(c *gin.Context) {
	limit := int32(100)
	if raw := c.Query("page_size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = int32(parsed)
		}
	}
	result, err := s.files.ListDeleted(c.Request.Context(), userID(c), c.Query("cursor"), limit)
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(result.Items))
	for _, node := range result.Items {
		items = append(items, fileDTO(node))
	}
	ok(c, gin.H{"items": items, "next_cursor": result.NextCursor})
}

func (s *Server) restoreTrash(c *gin.Context) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
			return
		}
	}
	node, err := s.files.RestoreDeleted(c.Request.Context(), userID(c), c.Param("id"), optionalString(req.DeviceID))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileDTO(node))
}

func (s *Server) purgeTrash(c *gin.Context) {
	if err := s.files.PurgeDeleted(c.Request.Context(), userID(c), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"purged": true})
}

func (s *Server) listFileVersions(c *gin.Context) {
	limit64, err := parseInt64Query(c, "limit", 100)
	if err != nil {
		fail(c, err)
		return
	}
	versions, err := s.files.Versions(c.Request.Context(), userID(c), c.Param("id"), int32(limit64))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(versions))
	for _, version := range versions {
		items = append(items, fileVersionDTO(version))
	}
	ok(c, gin.H{"items": items})
}

func (s *Server) restoreFileVersion(c *gin.Context) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
			return
		}
	}
	version, err := parsePositiveInt64Param(c, "version")
	if err != nil {
		fail(c, err)
		return
	}
	node, changeID, err := s.files.RestoreVersion(c.Request.Context(), userID(c), c.Param("id"), version, optionalString(req.DeviceID))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"file": fileDTO(node), "change_id": changeID})
}

func (s *Server) pinFileVersion(c *gin.Context) {
	version, err := parsePositiveInt64Param(c, "version")
	if err != nil {
		fail(c, err)
		return
	}
	pinned, err := s.files.PinVersion(c.Request.Context(), userID(c), c.Param("id"), version)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileVersionDTO(pinned))
}

func (s *Server) unpinFileVersion(c *gin.Context) {
	version, err := parsePositiveInt64Param(c, "version")
	if err != nil {
		fail(c, err)
		return
	}
	unpinned, err := s.files.UnpinVersion(c.Request.Context(), userID(c), c.Param("id"), version)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileVersionDTO(unpinned))
}

func (s *Server) createDirectory(c *gin.Context) {
	var req struct {
		Path     string `json:"path"`
		DeviceID string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	node, err := s.files.CreateDirectory(c.Request.Context(), userID(c), req.Path, optionalString(req.DeviceID))
	if err != nil {
		fail(c, err)
		return
	}
	created(c, fileDTO(node))
}

func (s *Server) moveFile(c *gin.Context) {
	var req struct {
		Path        string `json:"path"`
		DeviceID    string `json:"device_id"`
		BaseVersion *int64 `json:"base_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	node, err := s.files.Move(c.Request.Context(), userID(c), c.Param("id"), req.Path, req.BaseVersion, optionalString(req.DeviceID))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileDTO(node))
}

func (s *Server) deleteFile(c *gin.Context) {
	var req struct {
		DeviceID    string `json:"device_id"`
		BaseVersion *int64 `json:"base_version"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := s.files.Delete(c.Request.Context(), userID(c), c.Param("id"), req.BaseVersion, optionalString(req.DeviceID)); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{})
}

func (s *Server) initUpload(c *gin.Context) {
	var req struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		SHA256      string `json:"sha256"`
		ChunkSize   int64  `json:"chunk_size"`
		BaseVersion *int64 `json:"base_version"`
		DeviceID    string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	session, err := s.files.InitUpload(c.Request.Context(), userID(c), req.Path, req.Size, req.SHA256, req.ChunkSize, req.BaseVersion, c.GetHeader("Idempotency-Key"), req.DeviceID)
	if err != nil {
		fail(c, err)
		return
	}
	created(c, uploadDTO(session, nil))
}

func (s *Server) putChunk(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid chunk index", err))
		return
	}
	checksum := c.GetHeader("X-Chunk-Sha256")
	chunk, err := s.files.PutChunk(c.Request.Context(), userID(c), c.Param("id"), int32(index), c.Request.Body, checksum)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, chunkDTO(chunk))
}

func (s *Server) uploadStatus(c *gin.Context) {
	session, chunks, err := s.files.UploadStatus(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, uploadDTO(session, chunks))
}

func (s *Server) abortUpload(c *gin.Context) {
	session, err := s.files.AbortUpload(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, uploadDTO(session, nil))
}

func (s *Server) commitUpload(c *gin.Context) {
	node, changeID, err := s.files.CommitUpload(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"file_id": node.ID, "version": node.Version, "change_id": changeID})
}

func (s *Server) download(c *gin.Context) {
	br, err := parseRange(c.GetHeader("Range"))
	if err != nil {
		fail(c, err)
		return
	}

	node, err := s.files.GetByID(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Accept-Ranges", "bytes")
	etag := fileETag(node)
	if etag != "" {
		c.Header("ETag", etag)
		if ifNoneMatch(c.GetHeader("If-None-Match"), etag) {
			c.Status(http.StatusNotModified)
			return
		}
	}

	rc, info, node, err := s.files.Download(c.Request.Context(), userID(c), c.Param("id"), br)
	if err != nil {
		fail(c, err)
		return
	}
	defer rc.Close()

	status := http.StatusOK
	contentLength := info.Size
	if br != nil {
		resolved, err := resolveByteRange(info.Size, br)
		if err != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", info.Size))
			fail(c, err)
			return
		}
		status = http.StatusPartialContent
		contentLength = resolved.Length
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", resolved.Start, resolved.End, info.Size))
	}
	c.DataFromReader(status, contentLength, "application/octet-stream", rc, nil)
}

func (s *Server) registerDevice(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	var req struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	device, err := s.sync.RegisterDevice(c.Request.Context(), userID(c), req.Name, req.Platform)
	if err != nil {
		fail(c, err)
		return
	}
	created(c, deviceDTO(device))
}

func (s *Server) listDevices(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	limit64, err := parseInt64Query(c, "limit", 100)
	if err != nil {
		fail(c, err)
		return
	}
	devices, err := s.sync.Devices(c.Request.Context(), userID(c), int32(limit64))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(devices))
	for _, device := range devices {
		items = append(items, deviceDTO(device))
	}
	ok(c, gin.H{"items": items})
}

func (s *Server) revokeDevice(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	if err := s.sync.RevokeDevice(c.Request.Context(), userID(c), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{})
}

func (s *Server) heartbeatDevice(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	var req struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	device, err := s.sync.Heartbeat(c.Request.Context(), userID(c), c.Param("id"), req.Status, req.Error)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, deviceDTO(device))
}

func (s *Server) listActivity(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	beforeEventID, err := parseInt64Query(c, "before_event_id", 0)
	if err != nil {
		fail(c, err)
		return
	}
	limit, err := parseInt64Query(c, "limit", 50)
	if err != nil {
		fail(c, err)
		return
	}
	events, err := s.sync.Activity(c.Request.Context(), userID(c), c.Query("file_id"), beforeEventID, int32(limit))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(events))
	var nextCursor int64
	for _, event := range events {
		items = append(items, changeEventDTO(event))
		nextCursor = event.ID
	}
	ok(c, gin.H{"items": items, "next_cursor": nextCursor})
}

func (s *Server) listChanges(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	afterChangeID, err := parseInt64Query(c, "after_change_id", 0)
	if err != nil {
		fail(c, err)
		return
	}
	limit64, err := parseInt64Query(c, "limit", 500)
	if err != nil {
		fail(c, err)
		return
	}
	events, err := s.sync.Changes(c.Request.Context(), userID(c), c.Query("device_id"), afterChangeID, int32(limit64))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(events))
	var nextCursor int64
	for _, event := range events {
		items = append(items, changeEventDTO(event))
		nextCursor = event.ID
	}
	ok(c, gin.H{"items": items, "next_cursor": nextCursor})
}

func (s *Server) ackChanges(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	var req struct {
		DeviceID            string `json:"device_id"`
		LastAppliedChangeID int64  `json:"last_applied_change_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	device, err := s.sync.Ack(c.Request.Context(), userID(c), req.DeviceID, req.LastAppliedChangeID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, deviceDTO(device))
}

func (s *Server) listSyncConflicts(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	limit64, err := parseInt64Query(c, "limit", 100)
	if err != nil {
		fail(c, err)
		return
	}
	conflicts, err := s.sync.Conflicts(c.Request.Context(), userID(c), c.Query("resolution"), int32(limit64))
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]any, 0, len(conflicts))
	for _, conflict := range conflicts {
		items = append(items, syncConflictDTO(conflict))
	}
	ok(c, gin.H{"items": items})
}

func (s *Server) resolveSyncConflict(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	conflict, err := s.sync.ResolveConflict(c.Request.Context(), userID(c), c.Param("id"), req.Resolution)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, syncConflictDTO(conflict))
}

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(c, domain.E(domain.CodeUnauthenticated, "missing bearer token", nil))
			c.Abort()
			return
		}
		id, err := s.auth.VerifyAccessToken(c.Request.Context(), strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			fail(c, err)
			c.Abort()
			return
		}
		c.Set("user_id", id)
		c.Next()
	}
}

func (s *Server) requireMetadataKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.metadata == nil {
			fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
			c.Abort()
			return
		}
		key := strings.TrimSpace(c.GetHeader("X-API-Key"))
		userID, err := s.metadata.Authorize(c.Request.Context(), key, c.Param("application"))
		if err != nil {
			fail(c, err)
			c.Abort()
			return
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

func (s *Server) requireSyncKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		var userID string
		var err error
		if key := strings.TrimSpace(c.GetHeader("X-API-Key")); key != "" {
			if s.metadata == nil {
				fail(c, domain.E(domain.CodeInternal, "metadata service is not configured", nil))
				c.Abort()
				return
			}
			userID, err = s.metadata.Authorize(c.Request.Context(), key, "synchub-desktop")
		} else {
			header := c.GetHeader("Authorization")
			if s.auth == nil || !strings.HasPrefix(header, "Bearer ") {
				err = domain.E(domain.CodeUnauthenticated, "missing sync credentials", nil)
			} else {
				userID, err = s.auth.VerifyAccessToken(c.Request.Context(), strings.TrimPrefix(header, "Bearer "))
			}
		}
		if err != nil {
			fail(c, err)
			c.Abort()
			return
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

func userID(c *gin.Context) string {
	v, _ := c.Get("user_id")
	id, _ := v.(string)
	return id
}

func parseRange(raw string) (*storage.ByteRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "bytes=") {
		return nil, domain.E(domain.CodeInvalidArgument, "invalid range header", nil)
	}
	parts := strings.Split(strings.TrimPrefix(raw, "bytes="), "-")
	if len(parts) != 2 || parts[0] == "" {
		return nil, domain.E(domain.CodeInvalidArgument, "invalid range header", nil)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 {
		return nil, domain.E(domain.CodeInvalidArgument, "invalid range header", err)
	}
	var end *int64
	if parts[1] != "" {
		parsed, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || parsed < start {
			return nil, domain.E(domain.CodeInvalidArgument, "invalid range header", err)
		}
		end = &parsed
	}
	return &storage.ByteRange{Start: start, End: end}, nil
}

type resolvedByteRange struct {
	Start  int64
	End    int64
	Length int64
}

func resolveByteRange(size int64, br *storage.ByteRange) (resolvedByteRange, error) {
	if br == nil {
		return resolvedByteRange{Start: 0, End: size - 1, Length: size}, nil
	}
	if size <= 0 || br.Start >= size {
		return resolvedByteRange{}, domain.E(domain.CodeRangeNotSatisfiable, "range not satisfiable", nil)
	}
	end := size - 1
	if br.End != nil && *br.End < end {
		end = *br.End
	}
	return resolvedByteRange{Start: br.Start, End: end, Length: end - br.Start + 1}, nil
}

func fileETag(node domain.FileNode) string {
	if node.SHA256 == nil {
		return ""
	}
	return fmt.Sprintf(`"%s-%d"`, *node.SHA256, node.Version)
}

func ifNoneMatch(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || normalizeETag(candidate) == normalizeETag(etag) {
			return true
		}
	}
	return false
}

func normalizeETag(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "W/")
	return value
}

func parseInt64Query(c *gin.Context, name string, fallback int64) (int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, domain.E(domain.CodeInvalidArgument, "invalid "+name, err)
	}
	return parsed, nil
}

func parsePositiveInt64Param(c *gin.Context, name string) (int64, error) {
	parsed, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, domain.E(domain.CodeInvalidArgument, "invalid "+name, err)
	}
	return parsed, nil
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func userDTO(user domain.User) gin.H {
	return gin.H{"id": user.ID, "email": user.Email, "status": user.Status}
}

func fileDTO(node domain.FileNode) gin.H {
	return gin.H{"id": node.ID, "parent_id": node.ParentID, "name": node.Name, "path": node.Path, "node_type": node.NodeType, "size": node.Size, "sha256": node.SHA256, "version": node.Version, "created_at": node.CreatedAt, "updated_at": node.UpdatedAt}
}

func fileVersionDTO(version domain.FileVersion) gin.H {
	return gin.H{"id": version.ID, "file_id": version.FileID, "version": version.Version, "size": version.Size, "sha256": version.SHA256, "pinned_at": version.PinnedAt, "created_at": version.CreatedAt}
}

func uploadDTO(session domain.UploadSession, chunks []domain.UploadChunk) gin.H {
	items := make([]any, 0, len(chunks))
	for _, chunk := range chunks {
		items = append(items, chunkDTO(chunk))
	}
	return gin.H{"upload_id": session.ID, "path": session.TargetPath, "chunk_size": session.ChunkSize, "expires_at": session.ExpiresAt, "status": session.Status, "uploaded_chunks": items}
}

func chunkDTO(chunk domain.UploadChunk) gin.H {
	return gin.H{"chunk_index": chunk.ChunkIndex, "size": chunk.Size, "sha256": chunk.SHA256}
}

func deviceDTO(device domain.Device) gin.H {
	return gin.H{"id": device.ID, "name": device.Name, "platform": device.Platform, "last_seen_at": device.LastSeenAt, "last_sync_at": device.LastSyncAt, "last_sync_status": device.LastSyncStatus, "last_sync_error": device.LastSyncError, "last_applied_change_id": device.LastAppliedChangeID, "created_at": device.CreatedAt, "updated_at": device.UpdatedAt}
}

func changeEventDTO(event domain.ChangeEvent) gin.H {
	return gin.H{"id": event.ID, "file_id": event.FileID, "event_type": event.EventType, "version": event.Version, "path": event.Path, "old_path": event.OldPath, "source_device_id": event.SourceDeviceID, "created_at": event.CreatedAt}
}

func syncConflictDTO(conflict domain.SyncConflict) gin.H {
	return gin.H{"id": conflict.ID, "file_id": conflict.FileID, "path": conflict.Path, "local_version": conflict.LocalVersion, "remote_version": conflict.RemoteVersion, "resolution": conflict.Resolution, "created_at": conflict.CreatedAt, "resolved_at": conflict.ResolvedAt}
}

func subscriptionDTO(subscription domain.Subscription) gin.H {
	return gin.H{
		"plan": subscription.Plan, "status": subscription.Status,
		"currency": subscription.Currency, "unit_amount": subscription.UnitAmount,
		"billing_interval": subscription.BillingInterval, "expires_at": subscription.ExpiresAt,
		"current_period_end":   subscription.CurrentPeriodEnd,
		"cancel_at_period_end": subscription.CancelAtPeriodEnd,
		"provider":             subscription.Provider,
	}
}

func apiKeyDTO(key domain.APIKey) gin.H {
	return gin.H{"id": key.ID, "name": key.Name, "application": key.Application, "key_prefix": key.KeyPrefix, "last_used_at": key.LastUsedAt, "revoked_at": key.RevokedAt, "created_at": key.CreatedAt}
}

func metadataDocumentDTO(document domain.MetadataDocument) gin.H {
	return gin.H{"payload": json.RawMessage(document.Payload), "version": document.Version, "updated_at": document.UpdatedAt}
}
