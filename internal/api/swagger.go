package api

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed openapi.yaml swagger_index.html
var swaggerFiles embed.FS

func swaggerRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/swagger/")
}

func swaggerUI(c *gin.Context) {
	html, err := swaggerFiles.ReadFile("swagger_index.html")
	if err != nil {
		fail(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func swaggerSpec(c *gin.Context) {
	spec, err := swaggerFiles.ReadFile("openapi.yaml")
	if err != nil {
		fail(c, err)
		return
	}
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", spec)
}
