package api

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed admin_dist/*
var adminFiles embed.FS

func (s *Server) adminUI(c *gin.Context) {
	assets, err := fs.Sub(adminFiles, "admin_dist")
	if err != nil {
		fail(c, err)
		return
	}
	requestedPath := c.Param("path")
	requestedPath = strings.TrimPrefix(path.Clean("/"+requestedPath), "/")
	if requestedPath == "" || requestedPath == "." {
		requestedPath = "index.html"
	}
	if _, err := fs.Stat(assets, requestedPath); err != nil {
		requestedPath = "index.html"
	}
	if requestedPath == "index.html" {
		content, err := fs.ReadFile(assets, requestedPath)
		if err != nil {
			fail(c, err)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
		return
	}
	req := c.Request.Clone(c.Request.Context())
	req.URL.Path = "/" + requestedPath
	http.FileServer(http.FS(assets)).ServeHTTP(c.Writer, req)
}
