package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "zbims_session"
	sessionDuration   = 24 * time.Hour
)

const (
	loginMaxFailures = 5
	loginInitialLock = 30 * time.Second
	loginMaxLock     = 5 * time.Minute
	loginIdleTimeout = 15 * time.Minute
)

type loginAttemptRecord struct {
	failures    int
	lockedUntil time.Time
	lastFailure time.Time
	lockPeriod  time.Duration
}

func (s *simpleAdminServer) sessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAuthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if s.isRequestAuthenticated(r) {
			s.renewSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}
		s.writeAuthRequired(w, r)
	})
}

// sessionCookieRenewInterval 是浏览器 Cookie 续期的最小间隔(声明为变量便于
// 测试注入)。服务端会话每次校验都滑动续期,但 Cookie 的 Expires 只在登录
// 时设定;若不重签,持续活跃的用户仍会在登录 24h 后被浏览器清除 Cookie 而
// 登出,滑动续期形同虚设。间隔内不重签,避免每个响应都携带 Set-Cookie。
var sessionCookieRenewInterval = time.Hour

// renewSessionCookie 会话校验通过后按节流策略重签 Cookie:以服务端已续期
// 的会话过期时间重新设定浏览器 Cookie 的 Expires/MaxAge,使"持续活跃则
// 不过期"的滑动语义真正生效(覆盖静态资源、页面与 /api/ws 握手等一切
// HTTP 请求;WS 长连接内的帧无法设置 HttpOnly Cookie,由前端定期 HTTP
// 心跳兜底)。
func (s *simpleAdminServer) renewSessionCookie(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	now := time.Now()
	s.sessionMu.Lock()
	expiresAt, ok := s.sessions[cookie.Value]
	if !ok || !expiresAt.After(now) {
		s.sessionMu.Unlock()
		return
	}
	if s.sessionCookieIssuedAt == nil {
		s.sessionCookieIssuedAt = make(map[string]time.Time)
	}
	if last, seen := s.sessionCookieIssuedAt[cookie.Value]; seen && now.Sub(last) < sessionCookieRenewInterval {
		s.sessionMu.Unlock()
		return
	}
	s.sessionCookieIssuedAt[cookie.Value] = now
	s.sessionMu.Unlock()
	setSessionCookie(w, r, cookie.Value, expiresAt)
}

func isPublicAuthPath(path string) bool {
	switch path {
	case "/login.html", "/logout.html", "/api/login", "/api/logout", "/api/module_model", "/favicon.ico":
		return true
	}
	// 登录页自身的静态资源放行:未认证时 /css/、/fonts/、/assets/ 若被 302 成登录页
	// HTML,登录页会字体降级、样式错乱甚至脚本失效。/assets/ 为 React 前端(Vite)
	// 的内容寻址构建产物(JS/CSS 分块),登录页与主应用共用,不含会话数据;
	// index.html 等 HTML 入口仍受会话保护(favicon.ico 已在上方按精确路径放行)。
	return strings.HasPrefix(path, "/css/") ||
		strings.HasPrefix(path, "/fonts/") ||
		strings.HasPrefix(path, "/assets/")
}

func (s *simpleAdminServer) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isRequestAuthenticated(r) {
		target := sanitizeLoginNext(r.URL.Query().Get("next"))
		if target == "" {
			target = "/"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	s.serveVersionedHTML(w, r, "login.html")
}

// sanitizeLoginNext 校验登录页携带的 next 跳转目标,与前端 login.html 的
// nextURL() 语义一致:只接受以单个斜杠开头的同站路径(拒绝 // 开头的
// 协议相对跳转,防止开放重定向),另接受 # 开头的同页锚点(前端登录成功
// 后的 hash 还原流程,形如 #sms,统一补 / 前缀后跳转)。其余(绝对 URL、
// 反斜杠、控制字符等)一律视为无效返回空串,由调用方退回 /。
func sanitizeLoginNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	for i := 0; i < len(next); i++ {
		c := next[i]
		if c <= ' ' || c == 0x7f || c == '\\' {
			return ""
		}
	}
	if strings.HasPrefix(next, "#") {
		return "/" + next
	}
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return ""
}

func (s *simpleAdminServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	remoteIP := loginRemoteIP(r)
	if remaining := s.loginLockedRemaining(remoteIP); remaining > 0 {
		retryAfter := int((remaining + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too many failed attempts", "retry_after": retryAfter})
		return
	}
	auth, err := loadAuthConfig(s.cfg.authFile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "auth config error"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid form"})
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if !constantTimeEqual(username, auth.Username) || !constantTimeEqual(password, auth.Password) {
		s.recordLoginFailure(remoteIP)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid credentials"})
		return
	}
	s.resetLoginAttempts(remoteIP)
	expiresAt := time.Now().Add(sessionDuration)
	token, err := s.createSession(expiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "session error"})
		return
	}
	s.markSessionCookieIssued(token)
	setSessionCookie(w, r, token, expiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": "/"})
}

// markSessionCookieIssued 记录会话 Cookie 已签发,作为续期节流的基准时刻。
func (s *simpleAdminServer) markSessionCookieIssued(token string) {
	s.sessionMu.Lock()
	if s.sessionCookieIssuedAt == nil {
		s.sessionCookieIssuedAt = make(map[string]time.Time)
	}
	s.sessionCookieIssuedAt[token] = time.Now()
	s.sessionMu.Unlock()
}

func loginRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

func (s *simpleAdminServer) loginLockedRemaining(remoteIP string) time.Duration {
	now := time.Now()
	s.loginLimitMu.Lock()
	defer s.loginLimitMu.Unlock()
	for ip, rec := range s.loginAttempts {
		if !rec.lockedUntil.After(now) && now.Sub(rec.lastFailure) >= loginIdleTimeout {
			delete(s.loginAttempts, ip)
		}
	}
	rec, ok := s.loginAttempts[remoteIP]
	if !ok || !rec.lockedUntil.After(now) {
		return 0
	}
	return rec.lockedUntil.Sub(now)
}

func (s *simpleAdminServer) recordLoginFailure(remoteIP string) {
	now := time.Now()
	s.loginLimitMu.Lock()
	defer s.loginLimitMu.Unlock()
	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttemptRecord)
	}
	rec, ok := s.loginAttempts[remoteIP]
	if !ok {
		rec = &loginAttemptRecord{}
		s.loginAttempts[remoteIP] = rec
	}
	rec.lastFailure = now
	if rec.lockedUntil.After(now) {
		return
	}
	rec.failures++
	if rec.failures < loginMaxFailures {
		return
	}
	period := rec.lockPeriod * 2
	if period < loginInitialLock {
		period = loginInitialLock
	}
	if period > loginMaxLock {
		period = loginMaxLock
	}
	rec.lockPeriod = period
	rec.lockedUntil = now.Add(period)
	rec.failures = 0
}

func (s *simpleAdminServer) resetLoginAttempts(remoteIP string) {
	s.loginLimitMu.Lock()
	defer s.loginLimitMu.Unlock()
	delete(s.loginAttempts, remoteIP)
}

func (s *simpleAdminServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.destroyRequestSession(r)
	clearSessionCookie(w, r)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	if r.Method == http.MethodGet || strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/login.html", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": "/login.html"})
}

func (s *simpleAdminServer) handleLogoutPage(w http.ResponseWriter, r *http.Request) {
	s.destroyRequestSession(r)
	clearSessionCookie(w, r)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	http.Redirect(w, r, "/login.html", http.StatusSeeOther)
}

func (s *simpleAdminServer) writeAuthRequired(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/cgi-bin/") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "login required"})
		return
	}
	loginURL := "/login.html"
	if r.URL.Path != "/" || r.URL.RawQuery != "" {
		loginURL += "?next=" + url.QueryEscape(r.URL.RequestURI())
	}
	http.Redirect(w, r, loginURL, http.StatusSeeOther)
}

func (s *simpleAdminServer) isRequestAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.validateSession(cookie.Value)
}

func (s *simpleAdminServer) createSession(expiresAt time.Time) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	s.sessionMu.Lock()
	if s.sessions == nil {
		s.sessions = make(map[string]time.Time)
	}
	s.sessions[token] = expiresAt
	s.sessionMu.Unlock()
	return token, nil
}

func (s *simpleAdminServer) validateSession(token string) bool {
	now := time.Now()
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessions == nil {
		return false
	}
	expiresAt, ok := s.sessions[token]
	if !ok {
		return false
	}
	if !expiresAt.After(now) {
		delete(s.sessions, token)
		return false
	}
	s.sessions[token] = now.Add(sessionDuration)
	return true
}

func (s *simpleAdminServer) destroyRequestSession(r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	s.sessionMu.Lock()
	if s.sessions != nil {
		delete(s.sessions, cookie.Value)
	}
	if s.sessionCookieIssuedAt != nil {
		delete(s.sessionCookieIssuedAt, cookie.Value)
	}
	s.sessionMu.Unlock()
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// sessionCleanupInterval 是会话与登录失败记录周期清理的运行间隔。
const sessionCleanupInterval = time.Hour

// startSessionCleanup 启动周期清理协程;每个服务实例至多启动一次
// (测试中 routes() 可能被多次调用)。
func (s *simpleAdminServer) startSessionCleanup() {
	s.cleanupOnce.Do(func() {
		go s.sessionCleanupLoop()
	})
}

func (s *simpleAdminServer) sessionCleanupLoop() {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.purgeExpiredSessionsAndAttempts(time.Now())
	}
}

// purgeExpiredSessionsAndAttempts 删除过期会话与已无需保留的登录失败记录
// (未处于锁定且距上次失败已超过 loginIdleTimeout,与 loginLockedRemaining
// 的惰性清理条件一致)。惰性清理保持不变,本函数仅作为每小时兜底。
func (s *simpleAdminServer) purgeExpiredSessionsAndAttempts(now time.Time) {
	s.sessionMu.Lock()
	for token, expiresAt := range s.sessions {
		if !expiresAt.After(now) {
			delete(s.sessions, token)
		}
	}
	// 同步清理 Cookie 续期节流记录:登出路径会删除对应条目,但会话自然过期
	// 不经过登出,孤儿条目会永久残留,长跑进程中随累计登录次数无界增长。
	// 判定口径与续期路径一致:只保留仍有有效会话的记录。
	for token := range s.sessionCookieIssuedAt {
		if _, ok := s.sessions[token]; !ok {
			delete(s.sessionCookieIssuedAt, token)
		}
	}
	s.sessionMu.Unlock()

	s.loginLimitMu.Lock()
	for ip, rec := range s.loginAttempts {
		if !rec.lockedUntil.After(now) && now.Sub(rec.lastFailure) >= loginIdleTimeout {
			delete(s.loginAttempts, ip)
		}
	}
	s.loginLimitMu.Unlock()
}
