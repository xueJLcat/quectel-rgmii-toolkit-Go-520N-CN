package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newRateLimitTestServer(t *testing.T) *simpleAdminServer {
	t.Helper()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("make static dir: %v", err)
	}
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	return &simpleAdminServer{cfg: serverConfig{authFile: authFile, staticDir: staticDir}}
}

func postLoginFromAddr(t *testing.T, srv *simpleAdminServer, remoteAddr, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("username=admin&password="+password))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	rr := httptest.NewRecorder()
	srv.routes().ServeHTTP(rr, req)
	return rr
}

func TestLoginRateLimitLocksAfterFiveFailures(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const addr = "10.0.0.1:1234"
	for i := 0; i < loginMaxFailures; i++ {
		if rr := postLoginFromAddr(t, srv, addr, "wrong"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401, body = %s", i+1, rr.Code, rr.Body.String())
		}
	}
	rr := postLoginFromAddr(t, srv, addr, "wrong")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want 429, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		RetryAfter int    `json:"retry_after"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 429 body %q: %v", rr.Body.String(), err)
	}
	if body.OK || body.RetryAfter <= 0 || body.RetryAfter > int(loginInitialLock/time.Second) {
		t.Fatalf("429 body = %q, want retry_after in (0, %d]", rr.Body.String(), int(loginInitialLock/time.Second))
	}
}

func TestLoginRateLimitRejectsCorrectPasswordWhileLocked(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const addr = "10.0.0.2:1234"
	for i := 0; i < loginMaxFailures; i++ {
		postLoginFromAddr(t, srv, addr, "wrong")
	}
	if rr := postLoginFromAddr(t, srv, addr, "admin"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password while locked status = %d, want 429, body = %s", rr.Code, rr.Body.String())
	}
}

func TestLoginRateLimitIsolatesRemoteIPs(t *testing.T) {
	srv := newRateLimitTestServer(t)
	for i := 0; i < loginMaxFailures; i++ {
		postLoginFromAddr(t, srv, "10.0.0.3:1234", "wrong")
	}
	if rr := postLoginFromAddr(t, srv, "10.0.0.3:1234", "admin"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("locked IP status = %d, want 429", rr.Code)
	}
	if rr := postLoginFromAddr(t, srv, "10.0.0.4:1234", "admin"); rr.Code != http.StatusOK {
		t.Fatalf("other IP status = %d, want 200, body = %s", rr.Code, rr.Body.String())
	}
}

func TestLoginRateLimitStateNotSharedAcrossServers(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const addr = "10.0.0.5:1234"
	for i := 0; i < loginMaxFailures; i++ {
		postLoginFromAddr(t, srv, addr, "wrong")
	}
	if rr := postLoginFromAddr(t, srv, addr, "admin"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want 429", rr.Code)
	}
	fresh := &simpleAdminServer{cfg: srv.cfg}
	if rr := postLoginFromAddr(t, fresh, addr, "admin"); rr.Code != http.StatusOK {
		t.Fatalf("fresh server status = %d, want 200, body = %s", rr.Code, rr.Body.String())
	}
}

func TestLoginRateLimitSuccessResetsFailures(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const addr = "10.0.0.6:1234"
	for i := 0; i < loginMaxFailures-1; i++ {
		if rr := postLoginFromAddr(t, srv, addr, "wrong"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("failure status = %d, want 401", rr.Code)
		}
	}
	if rr := postLoginFromAddr(t, srv, addr, "admin"); rr.Code != http.StatusOK {
		t.Fatalf("success status = %d, want 200, body = %s", rr.Code, rr.Body.String())
	}
	for i := 0; i < loginMaxFailures-1; i++ {
		if rr := postLoginFromAddr(t, srv, addr, "wrong"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("post-reset failure %d status = %d, want 401", i+1, rr.Code)
		}
	}
}

func expireLoginLock(srv *simpleAdminServer, remoteIP string) {
	srv.loginLimitMu.Lock()
	defer srv.loginLimitMu.Unlock()
	if rec, ok := srv.loginAttempts[remoteIP]; ok {
		rec.lockedUntil = time.Now().Add(-time.Second)
	}
}

func currentLoginLockPeriod(srv *simpleAdminServer, remoteIP string) time.Duration {
	srv.loginLimitMu.Lock()
	defer srv.loginLimitMu.Unlock()
	if rec, ok := srv.loginAttempts[remoteIP]; ok {
		return rec.lockPeriod
	}
	return 0
}

func TestLoginRateLimitEscalatesAndCapsLockPeriod(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const ip = "10.0.0.7"
	addr := ip + ":1234"
	failUntilLocked := func() {
		t.Helper()
		for i := 0; i < loginMaxFailures; i++ {
			if rr := postLoginFromAddr(t, srv, addr, "wrong"); rr.Code != http.StatusUnauthorized {
				t.Fatalf("failure status = %d, want 401, body = %s", rr.Code, rr.Body.String())
			}
		}
	}

	failUntilLocked()
	if got := currentLoginLockPeriod(srv, ip); got != loginInitialLock {
		t.Fatalf("first lock period = %v, want %v", got, loginInitialLock)
	}
	expireLoginLock(srv, ip)
	failUntilLocked()
	if got := currentLoginLockPeriod(srv, ip); got != loginInitialLock*2 {
		t.Fatalf("second lock period = %v, want %v", got, loginInitialLock*2)
	}
	for i := 0; i < 8; i++ {
		expireLoginLock(srv, ip)
		failUntilLocked()
	}
	if got := currentLoginLockPeriod(srv, ip); got != loginMaxLock {
		t.Fatalf("capped lock period = %v, want %v", got, loginMaxLock)
	}
}

func TestLoginRateLimitCleansIdleRecords(t *testing.T) {
	srv := newRateLimitTestServer(t)
	const ip = "10.0.0.8"
	for i := 0; i < loginMaxFailures-1; i++ {
		postLoginFromAddr(t, srv, ip+":1234", "wrong")
	}
	srv.loginLimitMu.Lock()
	if rec, ok := srv.loginAttempts[ip]; ok {
		rec.lastFailure = time.Now().Add(-loginIdleTimeout - time.Minute)
	}
	srv.loginLimitMu.Unlock()

	if rr := postLoginFromAddr(t, srv, "10.0.0.9:1234", "admin"); rr.Code != http.StatusOK {
		t.Fatalf("unrelated login status = %d, want 200", rr.Code)
	}
	srv.loginLimitMu.Lock()
	_, ok := srv.loginAttempts[ip]
	srv.loginLimitMu.Unlock()
	if ok {
		t.Fatalf("idle record for %s was not cleaned up", ip)
	}
}
