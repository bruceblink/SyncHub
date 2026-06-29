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
	"github.com/gin-gonic/gin"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	router *gin.Engine
	auth   *authsvc.Service
	files  *filesvc.Service
	db     Pinger
}

func New(auth *authsvc.Service, files *filesvc.Service, db Pinger) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	s := &Server{router: r, auth: auth, files: files, db: db}
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

	v1 := s.router.Group("/api/v1")
	v1.POST("/auth/register", s.register)
	v1.POST("/auth/login", s.login)
	v1.POST("/auth/refresh", s.refresh)
	v1.POST("/auth/logout", s.logout)

	protected := v1.Group("")
	protected.Use(s.requireAuth())
	protected.GET("/files/:id", s.getFile)
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
	session, err := s.files.InitUpload(c.Request.Context(), userID(c), req.Path, req.Size, req.SHA256, req.ChunkSize, req.BaseVersion)
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
	rc, info, node, err := s.files.Download(c.Request.Context(), userID(c), c.Param("id"), br)
	if err != nil {
		fail(c, err)
		return
	}
	defer rc.Close()
	if node.SHA256 != nil {
		c.Header("ETag", fmt.Sprintf(`"%s-%d"`, *node.SHA256, node.Version))
	}
	status := http.StatusOK
	if br != nil {
		status = http.StatusPartialContent
		if br.End != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", br.Start, *br.End, info.Size))
		}
	}
	c.DataFromReader(status, info.Size, "application/octet-stream", rc, nil)
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

func userDTO(user domain.User) gin.H {
	return gin.H{"id": user.ID, "email": user.Email, "status": user.Status}
}

func fileDTO(node domain.FileNode) gin.H {
	return gin.H{"id": node.ID, "parent_id": node.ParentID, "name": node.Name, "path": node.Path, "node_type": node.NodeType, "size": node.Size, "sha256": node.SHA256, "version": node.Version, "created_at": node.CreatedAt, "updated_at": node.UpdatedAt}
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
