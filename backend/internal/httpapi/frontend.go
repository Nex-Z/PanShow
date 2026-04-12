package httpapi

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"panshow/backend/internal/web"

	"github.com/gin-gonic/gin"
)

func (api *API) registerFrontend(router *gin.Engine) {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return
	}

	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		requestPath := cleanFrontendPath(c.Request.URL.Path)
		if isBackendRoute(requestPath) {
			c.Status(http.StatusNotFound)
			return
		}
		if serveFrontendFile(c, dist, requestPath) {
			return
		}
		if strings.HasPrefix(requestPath, "/assets/") {
			c.Status(http.StatusNotFound)
			return
		}
		if isFrontendAppRoute(requestPath) {
			serveFrontendIndex(c, dist)
			return
		}

		serveFrontendIndexWithStatus(c, dist, http.StatusNotFound)
	})
}

func cleanFrontendPath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(rawPath, "/"))
}

func isBackendRoute(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") || requestPath == "/healthz"
}

func isFrontendAppRoute(requestPath string) bool {
	return requestPath == "/" || requestPath == "/r2" || strings.HasPrefix(requestPath, "/r2/")
}

func serveFrontendFile(c *gin.Context, dist fs.FS, requestPath string) bool {
	name := strings.TrimPrefix(requestPath, "/")
	if name == "" || name == "." || !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(dist, name)
	if err != nil || info.IsDir() {
		return false
	}
	serveFrontendContent(c, dist, name, info)
	return true
}

func serveFrontendIndex(c *gin.Context, dist fs.FS) {
	serveFrontendIndexWithStatus(c, dist, http.StatusOK)
}

func serveFrontendIndexWithStatus(c *gin.Context, dist fs.FS, status int) {
	info, err := fs.Stat(dist, "index.html")
	if err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	if status == http.StatusOK {
		serveFrontendContent(c, dist, "index.html", info)
		return
	}
	serveFrontendContentWithStatus(c, dist, "index.html", status)
}

func serveFrontendContent(c *gin.Context, dist fs.FS, name string, info fs.FileInfo) {
	content, err := fs.ReadFile(dist, name)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), bytes.NewReader(content))
}

func serveFrontendContentWithStatus(c *gin.Context, dist fs.FS, name string, status int) {
	content, err := fs.ReadFile(dist, name)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	c.Writer.WriteHeader(status)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = c.Writer.Write(content)
}
