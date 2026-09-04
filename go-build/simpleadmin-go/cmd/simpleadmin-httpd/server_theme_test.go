package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTheme(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dark", "dark"},
		{"DARK", "dark"},
		{" dark ", "dark"},
		{"light", "light"},
		{"Light", "light"},
		{"", ""},
		{"auto", ""},
		{"blue", ""},
	}
	for _, c := range cases {
		if got := normalizeTheme(c.in); got != c.want {
			t.Errorf("normalizeTheme(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReadThemeConfig(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.json")
	if theme, err := readThemeConfig(missing); theme != defaultTheme || !os.IsNotExist(err) {
		t.Fatalf("missing file: theme=%q err=%v, want %q + ErrNotExist", theme, err, defaultTheme)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if theme, err := readThemeConfig(corrupt); theme != defaultTheme || err == nil {
		t.Fatalf("corrupt file: theme=%q err=%v, want %q + error", theme, err, defaultTheme)
	}

	unsupported := filepath.Join(dir, "unsupported.json")
	if err := os.WriteFile(unsupported, []byte(`{"theme":"auto"}`), 0644); err != nil {
		t.Fatalf("write unsupported: %v", err)
	}
	if theme, err := readThemeConfig(unsupported); theme != defaultTheme || err != nil {
		t.Fatalf("unsupported value: theme=%q err=%v, want %q without error", theme, err, defaultTheme)
	}

	darkFile := filepath.Join(dir, "dark.json")
	if err := os.WriteFile(darkFile, []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatalf("write dark: %v", err)
	}
	if theme, err := readThemeConfig(darkFile); theme != "dark" || err != nil {
		t.Fatalf("dark value: theme=%q err=%v, want dark without error", theme, err)
	}
}

func TestWriteThemeConfigAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "theme.json")

	if err := writeThemeConfig(path, "auto"); err == nil {
		t.Fatal("writeThemeConfig must reject unsupported theme")
	}

	if err := writeThemeConfig(path, "dark"); err != nil {
		t.Fatalf("writeThemeConfig(dark): %v", err)
	}
	theme, err := readThemeConfig(path)
	if err != nil || theme != "dark" {
		t.Fatalf("round trip: theme=%q err=%v, want dark", theme, err)
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, "nested", ".simpleadmin.theme.*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temp files not cleaned up: %v", leftovers)
	}
}

func TestHandleGetSetTheme(t *testing.T) {
	dir := t.TempDir()
	server := &simpleAdminServer{cfg: serverConfig{staticDir: dir}}

	req := httptest.NewRequest(http.MethodGet, "/api/get_theme", nil)
	rec := httptest.NewRecorder()
	server.handleGetTheme(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET default = %d, want 200", rec.Code)
	}
	var cfg themeConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || cfg.Theme != defaultTheme {
		t.Fatalf("GET default body = %q, want theme %q", rec.Body.String(), defaultTheme)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/set_theme?theme=dark", nil)
	rec = httptest.NewRecorder()
	server.handleSetTheme(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SET query = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if theme, err := readThemeConfig(server.themeConfigPath()); err != nil || theme != "dark" {
		t.Fatalf("persisted theme = %q err=%v, want dark", theme, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/set_theme", strings.NewReader(`{"theme":"light"}`))
	rec = httptest.NewRecorder()
	server.handleSetTheme(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SET body = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if theme, err := readThemeConfig(server.themeConfigPath()); err != nil || theme != "light" {
		t.Fatalf("persisted theme = %q err=%v, want light", theme, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/set_theme?theme=auto", nil)
	rec = httptest.NewRecorder()
	server.handleSetTheme(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("SET invalid = %d, want 400", rec.Code)
	}
}

func TestStaticFileHandlerInjectsDefaultTheme(t *testing.T) {
	staticDir := t.TempDir()
	indexHTML := `<!doctype html><html data-bs-theme="` + themePlaceholder + `" lang="zh-CN"><body>` + appVersionPlaceholder + `</body></html>`
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(indexHTML), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(staticDir, "config"), 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "config", "get_theme.json"), []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatalf("write theme config: %v", err)
	}

	server := &simpleAdminServer{cfg: serverConfig{staticDir: staticDir}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.staticFileHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, themePlaceholder) {
		t.Fatalf("theme placeholder not replaced: %s", body)
	}
	if !strings.Contains(body, `data-bs-theme="dark"`) {
		t.Fatalf("expected injected dark theme: %s", body)
	}
	if strings.Contains(body, appVersionPlaceholder) || !strings.Contains(body, appVersion) {
		t.Fatalf("version substitution broken: %s", body)
	}
}

func TestLoginPageInjectsDefaultTheme(t *testing.T) {
	staticDir := t.TempDir()
	loginHTML := `<!doctype html><html data-bs-theme="` + themePlaceholder + `" lang="zh-CN"></html>`
	if err := os.WriteFile(filepath.Join(staticDir, "login.html"), []byte(loginHTML), 0644); err != nil {
		t.Fatalf("write login.html: %v", err)
	}
	if err := writeThemeConfig(filepath.Join(staticDir, "config", "get_theme.json"), "dark"); err != nil {
		t.Fatalf("write theme config: %v", err)
	}

	server := &simpleAdminServer{cfg: serverConfig{staticDir: staticDir}, sessions: map[string]time.Time{}}
	req := httptest.NewRequest(http.MethodGet, "/login.html", nil)
	rec := httptest.NewRecorder()
	server.handleLoginPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login.html = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-bs-theme="dark"`) || strings.Contains(body, themePlaceholder) {
		t.Fatalf("login page theme substitution failed: %s", body)
	}
}

// TestE2EThemeDefaultViaGateway 走真实 /api/ws 网关与真实前端目录:
// 只读校验默认主题(仓库内为 light),不写入真实配置文件;
// 写入路径由 TestHandleGetSetTheme 在临时目录覆盖。
func TestE2EThemeDefaultViaGateway(t *testing.T) {
	ts, client := e2eTestServer(t)

	resp := e2eWebSocketCall(t, ts, client, "theme-get", "GET", "/api/get_theme", "")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("get_theme via gateway status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	body := fmt.Sprint(resp["body"])
	var cfg themeConfig
	if err := json.Unmarshal([]byte(body), &cfg); err != nil || cfg.Theme != defaultTheme {
		t.Fatalf("get_theme via gateway body = %q, want theme %q", body, defaultTheme)
	}

	for _, page := range []string{"/", "/login.html"} {
		pageResp, err := client.Get(ts.URL + page)
		if err != nil {
			t.Fatalf("GET %s: %v", page, err)
		}
		data, _ := io.ReadAll(pageResp.Body)
		pageResp.Body.Close()
		text := string(data)
		if strings.Contains(text, themePlaceholder) {
			t.Fatalf("GET %s still contains theme placeholder", page)
		}
		if !strings.Contains(text, `data-bs-theme="`+defaultTheme+`"`) {
			t.Fatalf("GET %s missing injected default theme: %.200s", page, text)
		}
	}
}
