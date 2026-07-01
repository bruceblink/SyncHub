package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
	"github.com/gin-gonic/gin"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	router *gin.Engine
	auth   *authsvc.Service
	files  *filesvc.Service
	sync   *syncsvc.Service
	db     Pinger
}

func New(auth *authsvc.Service, files *filesvc.Service, db Pinger) *Server {
	return NewWithSync(auth, files, nil, db)
}

func NewWithSync(auth *authsvc.Service, files *filesvc.Service, sync *syncsvc.Service, db Pinger) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	s := &Server{router: r, auth: auth, files: files, sync: sync, db: db}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) routes() {
	s.router.GET("/healthz", func(c *gin.Context) { ok(c, gin.H{"status": "ok"}) })
	s.router.GET("/readyz", func(c *gin.Context) {
		if s.db != nil {
			if err := s.db.Ping(c.Request.Context()); err != nil {
				fail(c, domain.E(domain.CodeInternal, "database is not ready", err))
				return
			}
		}
		ok(c, gin.H{"status": "ready"})
	})
	s.router.GET("/swagger", swaggerRedirect)
	s.router.GET("/swagger/", swaggerUI)
	s.router.GET("/swagger/openapi.yaml", swaggerSpec)

	v1 := s.router.Group("/api/v1")
	v1.POST("/auth/register", s.register)
	v1.POST("/auth/login", s.login)
	v1.POST("/auth/refresh", s.refresh)
	v1.POST("/auth/logout", s.logout)

	protected := v1.Group("")
	protected.Use(s.requireAuth())
	protected.GET("/files/:id", s.getFile)
	protected.GET("/files/:id/versions", s.listFileVersions)
	protected.POST("/files/:id/versions/:version/restore", s.restoreFileVersion)
	protected.GET("/files/by-path", s.getFileByPath)
	protected.GET("/files", s.listFiles)
	protected.POST("/files/directories", s.createDirectory)
	protected.PATCH("/files/:id", s.moveFile)
	protected.DELETE("/files/:id", s.deleteFile)
	protected.GET("/files/:id/content", s.download)
	protected.POST("/uploads", s.initUpload)
	protected.PUT("/uploads/:id/chunks/:index", s.putChunk)
	protected.GET("/uploads/:id", s.uploadStatus)
	protected.POST("/uploads/:id/commit", s.commitUpload)
	protected.POST("/devices", s.registerDevice)
	protected.POST("/devices/:id/heartbeat", s.heartbeatDevice)
	protected.GET("/sync/changes", s.listChanges)
	protected.POST("/sync/ack", s.ackChanges)
	protected.GET("/sync/conflicts", s.listSyncConflicts)
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
	nodes, err := s.files.List(c.Request.Context(), userID(c), parentID, limit)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]any, 0, len(nodes))
	for _, node := range nodes {
		data = append(data, fileDTO(node))
	}
	ok(c, gin.H{"items": data})
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
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version <= 0 {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid version", err))
		return
	}
	node, changeID, err := s.files.RestoreVersion(c.Request.Context(), userID(c), c.Param("id"), version)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"file": fileDTO(node), "change_id": changeID})
}

func (s *Server) createDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	node, err := s.files.CreateDirectory(c.Request.Context(), userID(c), req.Path)
	if err != nil {
		fail(c, err)
		return
	}
	created(c, fileDTO(node))
}

func (s *Server) moveFile(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	node, err := s.files.Move(c.Request.Context(), userID(c), c.Param("id"), req.Path)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, fileDTO(node))
}

func (s *Server) deleteFile(c *gin.Context) {
	if err := s.files.Delete(c.Request.Context(), userID(c), c.Param("id")); err != nil {
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, domain.E(domain.CodeInvalidArgument, "invalid request body", err))
		return
	}
	session, err := s.files.InitUpload(c.Request.Context(), userID(c), req.Path, req.Size, req.SHA256, req.ChunkSize, req.BaseVersion, c.GetHeader("Idempotency-Key"))
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

func (s *Server) heartbeatDevice(c *gin.Context) {
	if s.sync == nil {
		fail(c, domain.E(domain.CodeInternal, "sync service is not configured", nil))
		return
	}
	device, err := s.sync.Heartbeat(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, deviceDTO(device))
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

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(c, domain.E(domain.CodeUnauthenticated, "missing bearer token", nil))
			c.Abort()
			return
		}
		id, err := s.auth.VerifyAccessToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			fail(c, err)
			c.Abort()
			return
		}
		c.Set("user_id", id)
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

func userDTO(user domain.User) gin.H {
	return gin.H{"id": user.ID, "email": user.Email, "status": user.Status}
}

func fileDTO(node domain.FileNode) gin.H {
	return gin.H{"id": node.ID, "parent_id": node.ParentID, "name": node.Name, "path": node.Path, "node_type": node.NodeType, "size": node.Size, "sha256": node.SHA256, "version": node.Version, "created_at": node.CreatedAt, "updated_at": node.UpdatedAt}
}

func fileVersionDTO(version domain.FileVersion) gin.H {
	return gin.H{"id": version.ID, "file_id": version.FileID, "version": version.Version, "size": version.Size, "sha256": version.SHA256, "created_at": version.CreatedAt}
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
	return gin.H{"id": device.ID, "name": device.Name, "platform": device.Platform, "last_seen_at": device.LastSeenAt, "last_applied_change_id": device.LastAppliedChangeID, "created_at": device.CreatedAt, "updated_at": device.UpdatedAt}
}

func changeEventDTO(event domain.ChangeEvent) gin.H {
	return gin.H{"id": event.ID, "file_id": event.FileID, "event_type": event.EventType, "version": event.Version, "path": event.Path, "old_path": event.OldPath, "source_device_id": event.SourceDeviceID, "created_at": event.CreatedAt}
}

func syncConflictDTO(conflict domain.SyncConflict) gin.H {
	return gin.H{"id": conflict.ID, "file_id": conflict.FileID, "path": conflict.Path, "local_version": conflict.LocalVersion, "remote_version": conflict.RemoteVersion, "resolution": conflict.Resolution, "created_at": conflict.CreatedAt, "resolved_at": conflict.ResolvedAt}
}
