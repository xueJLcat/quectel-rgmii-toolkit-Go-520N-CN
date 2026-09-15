package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type simpleAdminServer struct {
	cfg       serverConfig
	sessionMu sync.Mutex
	sessions  map[string]time.Time
	// sessionCookieIssuedAt 记录每个会话最近一次签发/续期 Cookie 的时刻,
	// 用于节流续期(见 renewSessionCookie),受 sessionMu 保护。
	sessionCookieIssuedAt map[string]time.Time

	loginLimitMu  sync.Mutex
	loginAttempts map[string]*loginAttemptRecord

	cleanupOnce sync.Once
}

func (s *simpleAdminServer) routes() http.Handler {
	s.startSessionCleanup()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	if !s.cfg.directAPI {
		mux.HandleFunc("/api/module_model", s.handleModuleModel)
		// 前端会话保活心跳端点:网关模式下业务 API 只经 WS 网关分发,而
		// HttpOnly 会话 Cookie 无法经 WS 帧续期。保活命中这个轻量 HTTP
		// 端点(仅读系统运行时长,不产生 AT 流量),由会话中间件完成滑动
		// 续期与 Cookie 重签(见 renewSessionCookie),持续活跃不再整点登出。
		mux.HandleFunc("/api/get_uptime", s.handleGetUptime)
	}
	mux.HandleFunc("/login.html", s.handleLoginPage)
	mux.HandleFunc("/logout.html", s.handleLogoutPage)
	mux.HandleFunc("/api/ws", s.handleAPIWebSocket)
	mux.HandleFunc("/api/console/ws", s.handleNativeConsoleWebSocket)
	if s.cfg.directAPI {
		s.registerNativeAPIRoutes(mux)
	} else {
		mux.HandleFunc("/api/", s.handleAPINotFound)
	}
	mux.HandleFunc("/cgi-bin/", s.handleLegacyCGINotFound)
	mux.HandleFunc("/console", s.handleNativeConsole)
	mux.HandleFunc("/console/", s.handleNativeConsole)
	mux.Handle("/", s.staticFileHandler())
	return s.sessionAuth(mux)
}

// staticFileHandler 在 http.FileServer 基础上为 HTML 页面注入当前版本号
// (页面内的 ?v=__SA_VERSION__ 占位符被替换为构建注入的 appVersion),
// 并把 data-bs-theme 上的 __SA_THEME__ 占位符替换为设备保存的默认主题。
func (s *simpleAdminServer) staticFileHandler() http.Handler {
	fs := http.FileServer(http.Dir(s.cfg.staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathLower := strings.ToLower(r.URL.Path)
		isHTML := strings.HasSuffix(pathLower, ".html") || r.URL.Path == "/"
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && isHTML {
			rel := filepath.Clean("/" + r.URL.Path)
			if rel == "/" {
				rel = "/index.html"
			}
			if data, err := os.ReadFile(filepath.Join(s.cfg.staticDir, rel)); err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				http.ServeContent(w, r, filepath.Base(rel), time.Time{}, strings.NewReader(s.renderHTMLForServer(string(data))))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

// serveVersionedHTML 输出静态目录中带版本号占位符替换的 HTML 页面。
func (s *simpleAdminServer) serveVersionedHTML(w http.ResponseWriter, r *http.Request, relPath string) {
	data, err := os.ReadFile(filepath.Join(s.cfg.staticDir, relPath))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, relPath, time.Time{}, strings.NewReader(s.renderHTMLForServer(string(data))))
}

func (s *simpleAdminServer) nativeAPIHandlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/api/get_atcache":         s.handleGetATCache,
		"/api/get_atcommand":       s.handleGetATCommand,
		"/api/user_atcommand":      s.handleGetATCommand,
		"/api/dashboard_data":      s.handleDashboardData,
		"/api/mock_at":             s.handleMockATPayload,
		"/api/module_model":        s.handleModuleModel,
		"/api/device_info_data":    s.handleDeviceInfoData,
		"/api/diag_data":           handleDiagData,
		"/api/network_data":        s.handleNetworkData,
		"/api/at_data":             s.handleATData,
		"/api/network_config_data": s.handleNetworkConfigData,
		"/api/network_detail":      s.handleNetworkDetail,
		"/api/firewall_data":       s.handleFirewallData,
		"/api/system_data":         s.handleSystemData,
		"/api/sms_data":            s.handleSMSData,
		"/api/signal_data":         s.handleSignalData,
		"/api/get_ping":            s.handleGetPing,
		"/api/get_sms":             s.handleGetSMS,
		"/api/get_ttl_status":      s.handleGetTTLStatus,
		"/api/get_uptime":          s.handleGetUptime,
		"/api/history_data":        s.handleHistoryData,
		"/api/get_watchdog":        s.handleGetWatchdog,
		"/api/set_watchdog":        s.handleSetWatchdog,
		"/api/get_scheduler":       s.handleGetScheduler,
		"/api/set_scheduler":       s.handleSetScheduler,
		"/api/get_timesync":        s.handleGetTimeSync,
		"/api/set_timesync":        s.handleSetTimeSync,
		"/api/timesync_now":        s.handleTimeSyncNow,
		"/api/system_monitor":      s.handleSystemMonitor,
		"/api/get_sms_webhook":     s.handleGetSMSWebhook,
		"/api/set_sms_webhook":     s.handleSetSMSWebhook,
		"/api/get_sms_serverchan":  s.handleGetSMSServerChan,
		"/api/set_sms_serverchan":  s.handleSetSMSServerChan,
		"/api/test_sms_forward":    s.handleTestSMSForward,
		"/api/get_language":        s.handleGetLanguage,
		"/api/get_theme":           s.handleGetTheme,
		"/api/send_sms":            s.handleSendSMS,
		"/api/set_ttl":             s.handleSetTTL,
		"/api/set_language":        s.handleSetLanguage,
		"/api/set_theme":           s.handleSetTheme,
		"/api/set_password":        s.handleSetPassword,
	}
}

func (s *simpleAdminServer) registerNativeAPIRoutes(mux *http.ServeMux) {
	for path, handler := range s.nativeAPIHandlers() {
		mux.HandleFunc(path, handler)
	}
	mux.HandleFunc("/api/", s.handleAPINotFound)
}

func (s *simpleAdminServer) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusNotFound, "unsupported api endpoint: "+r.URL.Path)
}

func (s *simpleAdminServer) handleLegacyCGINotFound(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusNotFound, "unsupported legacy cgi-bin endpoint: "+r.URL.Path)
}
