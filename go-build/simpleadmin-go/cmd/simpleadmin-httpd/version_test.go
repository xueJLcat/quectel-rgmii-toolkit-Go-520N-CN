package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderStaticHTMLContentReplacesPlaceholder(t *testing.T) {
	in := `<script src="js/simpleadmin-ui.js?v=` + appVersionPlaceholder + `"></script>`
	want := `<script src="js/simpleadmin-ui.js?v=` + appVersion + `"></script>`
	if got := renderStaticHTMLContent(in); got != want {
		t.Fatalf("renderStaticHTMLContent = %q, want %q", got, want)
	}
	if got := renderStaticHTMLContent("no placeholder"); got != "no placeholder" {
		t.Fatalf("content without placeholder must be unchanged, got %q", got)
	}
}

func TestStaticFileHandlerInjectsVersion(t *testing.T) {
	staticDir := t.TempDir()
	indexHTML := `<!doctype html><link href="css/styles.css?v=` + appVersionPlaceholder + `"/>`
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(indexHTML), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "plain.txt"), []byte(appVersionPlaceholder), 0644); err != nil {
		t.Fatalf("write plain.txt: %v", err)
	}

	server := &simpleAdminServer{cfg: serverConfig{staticDir: staticDir}}
	handler := server.staticFileHandler()

	for _, path := range []string{"/", "/index.html"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, appVersionPlaceholder) {
			t.Fatalf("GET %s still contains placeholder: %s", path, body)
		}
		if !strings.Contains(body, "?v="+appVersion) {
			t.Fatalf("GET %s missing injected version %q: %s", path, appVersion, body)
		}
	}

	// 非 HTML 资源不做替换
	req := httptest.NewRequest(http.MethodGet, "/plain.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != appVersionPlaceholder {
		t.Fatalf("non-html content changed: %q", got)
	}
}

func TestLoginPageServesVersionedHTML(t *testing.T) {
	staticDir := t.TempDir()
	loginHTML := `<!doctype html><link href="css/Poppins.css?v=` + appVersionPlaceholder + `"/>`
	if err := os.WriteFile(filepath.Join(staticDir, "login.html"), []byte(loginHTML), 0644); err != nil {
		t.Fatalf("write login.html: %v", err)
	}

	server := &simpleAdminServer{cfg: serverConfig{staticDir: staticDir}, sessions: map[string]time.Time{}}
	req := httptest.NewRequest(http.MethodGet, "/login.html", nil)
	rec := httptest.NewRecorder()
	server.handleLoginPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login.html = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, appVersionPlaceholder) || !strings.Contains(body, "?v="+appVersion) {
		t.Fatalf("login.html version substitution failed: %s", body)
	}
}
