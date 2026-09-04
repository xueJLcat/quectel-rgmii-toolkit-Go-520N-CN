package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWebSocketBusinessAPIRejectedAfterSessionInvalidated 验证 WS 分发逐请求复核会话:
// 升级握手只鉴权一次,会话被登出销毁后,同一存活连接再调用业务 API 必须收到
// {id, status:401, error:"login required"} 帧(前端既有 401 → 跳登录页逻辑随之生效),
// 而不是像旧实现那样持续放行。
func TestWebSocketBusinessAPIRejectedAfterSessionInvalidated(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := wsFixesDial(t, ts, client)

	ws.writeJSONRequest("pre1", "POST", "/api/dashboard_data", "")
	resp := ws.readResponse(5 * time.Second)
	if resp["id"] != "pre1" {
		t.Fatalf("pre-logout response id = %v, want pre1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("pre-logout status = %v, want 200 (resp=%v)", status, resp)
	}

	logoutResp, err := client.PostForm(ts.URL+"/api/logout", url.Values{})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logoutResp.StatusCode)
	}

	ws.writeJSONRequest("post1", "POST", "/api/dashboard_data", "")
	resp = ws.readResponse(5 * time.Second)
	if resp["id"] != "post1" {
		t.Fatalf("post-logout response id = %v, want post1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "401" {
		t.Fatalf("post-logout status = %v, want 401 (resp=%v)", status, resp)
	}
	if errMsg := fmt.Sprint(resp["error"]); errMsg != "login required" {
		t.Fatalf("post-logout error = %q, want %q", errMsg, "login required")
	}
}

// TestWebSocketPublicEndpointStillServedAfterSessionInvalidated 验证公开端点不受复核影响:
// 会话失效后经同一存活连接调用 /api/module_model 仍正常应答。
func TestWebSocketPublicEndpointStillServedAfterSessionInvalidated(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := wsFixesDial(t, ts, client)

	logoutResp, err := client.PostForm(ts.URL+"/api/logout", url.Values{})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logoutResp.StatusCode)
	}

	ws.writeJSONRequest("pub1", "GET", "/api/module_model", "")
	resp := ws.readResponse(5 * time.Second)
	if resp["id"] != "pub1" {
		t.Fatalf("module_model response id = %v, want pub1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("module_model status = %v, want 200 (resp=%v)", status, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["body"]), mockModuleModel) {
		t.Fatalf("module_model body = %v, want contain %s", resp["body"], mockModuleModel)
	}
}

// TestLoginStaticAssetsServedWithoutAuth 验证登录页静态资源未认证时不被 302 到登录页:
// /css/、/fonts/、/favicon.ico 放行;同时守护放行面没有扩大——/js/ 等其它资源仍需登录。
func TestLoginStaticAssetsServedWithoutAuth(t *testing.T) {
	staticDir := e2eStaticDir(t)
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	srv := &simpleAdminServer{cfg: serverConfig{authFile: authFile, staticDir: staticDir}}
	handler := srv.routes()

	for _, path := range []string{"/css/tailwind.css", "/fonts/poppins-v21-400-latin.woff2", "/favicon.ico"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if loc := rr.Header().Get("Location"); strings.Contains(loc, "/login.html") {
			t.Fatalf("%s redirected to login page: %d Location=%s", path, rr.Code, loc)
		}
		if rr.Code == http.StatusFound || rr.Code == http.StatusSeeOther || rr.Code == http.StatusTemporaryRedirect {
			t.Fatalf("%s status = %d, want not redirected", path, rr.Code)
		}
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/js/simpleadmin-api.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/login.html") {
		t.Fatalf("/js/simpleadmin-api.js = %d Location=%s, want redirect to login page", rr.Code, rr.Header().Get("Location"))
	}
}

// TestLoginPageAuthenticatedHonorsNext 验证已登录访问 /login.html?next=... 不再丢弃 next:
// 合法 next 跳转到对应位置(# 锚点补 / 前缀),非法 next(// 开头、绝对 URL)退回 /。
func TestLoginPageAuthenticatedHonorsNext(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("make static dir: %v", err)
	}
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	srv := &simpleAdminServer{cfg: serverConfig{authFile: authFile, staticDir: staticDir}}
	handler := srv.routes()
	token, err := srv.createSession(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	cases := []struct {
		name    string
		target  string
		wantLoc string
	}{
		{"fragment next", "/login.html?next=%23sms", "/#sms"},
		{"path next", "/login.html?next=%2Findex.html%3Ftab%3D1", "/index.html?tab=1"},
		{"no next", "/login.html", "/"},
		{"protocol relative rejected", "/login.html?next=%2F%2Fevil.com", "/"},
		{"absolute url rejected", "/login.html?next=https%3A%2F%2Fevil.com", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303 redirect, body = %s", rr.Code, rr.Body.String())
			}
			if loc := rr.Header().Get("Location"); loc != tc.wantLoc {
				t.Fatalf("Location = %q, want %q", loc, tc.wantLoc)
			}
		})
	}
}

// TestPurgeExpiredSessionsAndAttempts 单元验证周期清理函数:
// 过期会话删除、未过期保留;未锁定且空闲超 loginIdleTimeout 的失败记录删除,
// 近期失败或仍在锁定期的记录保留(与惰性清理条件一致)。
func TestPurgeExpiredSessionsAndAttempts(t *testing.T) {
	now := time.Now()
	srv := &simpleAdminServer{
		sessions: map[string]time.Time{
			"expired-token": now.Add(-time.Minute),
			"valid-token":   now.Add(time.Hour),
		},
		loginAttempts: map[string]*loginAttemptRecord{
			"10.0.0.1": {lastFailure: now.Add(-time.Hour)},
			"10.0.0.2": {lastFailure: now.Add(-time.Minute)},
			"10.0.0.3": {lastFailure: now.Add(-time.Hour), lockedUntil: now.Add(time.Minute)},
		},
	}

	srv.startSessionCleanup()
	srv.startSessionCleanup()

	srv.purgeExpiredSessionsAndAttempts(now)

	if _, ok := srv.sessions["expired-token"]; ok {
		t.Fatalf("expired session not removed")
	}
	if _, ok := srv.sessions["valid-token"]; !ok {
		t.Fatalf("valid session removed")
	}
	if _, ok := srv.loginAttempts["10.0.0.1"]; ok {
		t.Fatalf("idle unlocked failure record not removed")
	}
	if _, ok := srv.loginAttempts["10.0.0.2"]; !ok {
		t.Fatalf("recent failure record removed")
	}
	if _, ok := srv.loginAttempts["10.0.0.3"]; !ok {
		t.Fatalf("still locked failure record removed")
	}
}
