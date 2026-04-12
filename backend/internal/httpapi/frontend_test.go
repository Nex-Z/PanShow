package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
)

func TestServeFrontendIndexDoesNotRedirectToDotSlash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	serveFrontendIndex(ctx, fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<!doctype html><html></html>"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}
}

func TestServeFrontendIndexCanKeepNotFoundStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/missing", nil)

	serveFrontendIndexWithStatus(ctx, fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<!doctype html><html></html>"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	}, http.StatusNotFound)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "<!doctype html>") {
		t.Fatalf("body = %q, want frontend index", body)
	}
}
