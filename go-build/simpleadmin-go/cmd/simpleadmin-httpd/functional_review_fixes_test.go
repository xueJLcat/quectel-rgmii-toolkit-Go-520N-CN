package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- B1: AT 执行失败保留上次成功缓存 ----------

// TestATCacheRunFailurePreservesLastGoodResponse 执行失败(含部分输出)不得
// 覆盖上次成功的响应:负缓存窗口内 Fetch 返回的是最后成功数据而不是
// 截断的半截内容。
func TestATCacheRunFailurePreservesLastGoodResponse(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return "+CMGL: 1,1,\"+86138\",...\n(截断)", errors.New("timeout waiting for OK/ERROR")
	})

	mgr := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{
			"AT+CMGL=4": {command: "AT+CMGL=4", response: "+CMGL: 1,1,\"+86138\"...\n+CMGL: 2,1,\"+86139\"...\nOK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
		},
		queue: make(chan string, 4),
	}
	mgr.run("AT+CMGL=4")

	entry := mgr.entries["AT+CMGL=4"]
	if strings.Contains(entry.response, "截断") {
		t.Fatalf("失败的部分输出覆盖了上次成功响应: %q", entry.response)
	}
	if !strings.Contains(entry.response, "OK") {
		t.Fatalf("上次成功响应未保留: %q", entry.response)
	}
	if entry.errorText == "" {
		t.Fatalf("失败仍应记录 errorText")
	}

	// 负缓存窗口内快照读取应得到最后成功值。
	_, response, _, _, _ := mgr.snapshot("AT+CMGL=4")
	if got := mgr.responseOrPending(true, response, entry.errorText, false); !strings.Contains(got, "+CMGL: 2") {
		t.Fatalf("负缓存窗口内应返回最后成功响应,实际: %q", got)
	}
}

// TestATCacheFirstFailureStillSurfacesError 无历史数据的条目首次失败时,
// 早退分支仍经 errorText 返回错误信息(行为不变)。
func TestATCacheFirstFailureStillSurfacesError(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return "", errors.New("all AT device candidates failed")
	})
	mgr := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{"ATI": {command: "ATI", lastRequested: time.Now()}},
		queue:   make(chan string, 4),
	}
	mgr.run("ATI")
	entry := mgr.entries["ATI"]
	if got := mgr.responseOrPending(!entry.updatedAt.IsZero(), entry.response, entry.errorText, false); got != "all AT device candidates failed" {
		t.Fatalf("首次失败应返回错误文本,实际: %q", got)
	}
}

// ---------- B2: 无效扫描不得推进短信 webhook 高水位 ----------

// TestNotifyNewSMSInvalidScanKeepsWatermark 无效扫描(截断列表)即使解析出
// 消息也不得推进/拉低高水位:否则下一次有效扫描会把更高索引的既有消息
// 全部当成新到达重推(重复通知)。
func TestNotifyNewSMSInvalidScanKeepsWatermark(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	// 首扫:有效完整列表,建立基线水位 3。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "a", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "b", 2),
		smsWebhookTestMessage("+8613800000003", "26/08/29,10:02:00+32", "c", 3),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	// 无效扫描:超时截断只剩索引 1(部分输出),不得拉低水位。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "a", 1),
	}, false)
	expectNoSMSWebhookPosts(t, ch)
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != 3 {
		t.Fatalf("无效扫描后水位应保持 3,实际: %v", status["lastNotifiedIndex"])
	}

	// 下一次有效扫描:完整列表,无新到达,不得重推索引 2/3。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "a", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "b", 2),
		smsWebhookTestMessage("+8613800000003", "26/08/29,10:02:00+32", "c", 3),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	// 真正的新消息仍正常通知。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000004", "26/08/29,10:03:00+32", "d", 4),
	}, true)
	records := waitForSMSWebhookPosts(t, ch, 1)
	if !strings.Contains(string(records[0].body), "+8613800000004") {
		t.Fatalf("应通知新消息 4,实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}

// TestNotifyNewSMSInvalidFirstScanDoesNotBaseline 无效首扫(即使带消息)
// 不得消耗初始化:基线必须由首次有效扫描建立,避免漏报。
func TestNotifyNewSMSInvalidFirstScanDoesNotBaseline(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "存量", 1),
	}, false)
	expectNoSMSWebhookPosts(t, ch)
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != -1 {
		t.Fatalf("无效首扫后应保持未初始化,实际: %v", status["lastNotifiedIndex"])
	}
}

// ---------- B3: 短信列表接口暴露 pending/error ----------

func TestSMSListMetaOnlyCarriesPendingAndError(t *testing.T) {
	data := map[string]any{
		"messages":       []map[string]any{{"sender": "+86138", "indices": []int{1}, "storage": "ME"}},
		"serviceCenters": []string{"+8613800100500"},
		"pending":        true,
		"error":          "AT 数据读取失败，请检查模块或稍后重试",
	}
	meta := smsListMetaOnly(data)
	if meta["pending"] != true {
		t.Fatalf("list_meta 应透传 pending: %#v", meta)
	}
	if meta["error"] == nil {
		t.Fatalf("list_meta 应透传 error: %#v", meta)
	}
}

// ---------- B4: 邻区扫描不参与周期刷新 ----------

func TestNeighbourScanSuppressedFromPeriodicRefresh(t *testing.T) {
	command := scanModeCommand("Neighbour Scan")
	if command == "" {
		t.Fatalf("scanModeCommand(Neighbour Scan) 不应为空")
	}
	if !isNeighbourScanCommand(command) {
		t.Fatalf("邻区扫描命令应被识别为页面专属命令")
	}
	if !isPeriodicRefreshSuppressedATCommand(command) {
		t.Fatalf("邻区扫描命令必须排除在周期刷新之外")
	}
	// 仪表盘组合命令同样含 +QENG,不得被误判。
	dashboard := pageATCommand(atKeyDashboard)
	if isNeighbourScanCommand(dashboard) {
		t.Fatalf("仪表盘命令被误判为邻区扫描")
	}

	// 最近请求过的邻区扫描条目不得进入周期刷新列表。
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{
		command: {command: command, response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
	}}
	if got := mgr.cachedReadCommandsNeedingRefresh(); len(got) != 0 {
		t.Fatalf("邻区扫描不得参与周期刷新: %#v", got)
	}
}

// ---------- B5: 定时重启失败跨分钟仍可重试 ----------

// TestSchedulerRebootFailureRetriesAfterMinutePasses 失败发生在设定分钟内、
// 下一轮调度已离开该分钟时,补跑分支必须继续重试(旧实现把 LastCheck
// 写到失败时刻,补跑条件 LastCheck<target 永不成立,重启被推迟到次日)。
// 前置一次 04:00 之前的检查以建立早于目标时刻的 LastCheck,模拟生产中
// 30 秒周期检查一直在运行。
func TestSchedulerRebootFailureRetriesAfterMinutePasses(t *testing.T) {
	withTempSchedulerEnv(t)
	response := "AT+CFUN=1,1\r\n+CME ERROR: unknown error"
	count := withRebootResponse(t, &response)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	// 04:00 之前的周期检查:不触发,仅记录 LastCheck=03:59:30(<target)。
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 3, 59, 30, 0, time.Local)) {
		t.Fatalf("目标时刻前不应触发")
	}
	if *count != 0 {
		t.Fatalf("目标时刻前不应执行重启, count = %d", *count)
	}

	// 04:00:45 触发但终结错误 → 失败;不得把 LastCheck 推进到目标时刻之后。
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 0, 45, 0, time.Local)) {
		t.Fatalf("终结错误响应不得计为已触发")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1", *count)
	}

	// 下一轮已离开 04:00 分钟(04:01:15),补跑分支必须继续重试。
	response = "AT+CFUN=1,1\r\nOK"
	if !schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 1, 15, 0, time.Local)) {
		t.Fatalf("失败后跨分钟的下一轮应补跑重试")
	}
	if *count != 2 {
		t.Fatalf("action count = %d, want 2", *count)
	}
	if persisted, inMemory := lastRunDates(); persisted != "2026-01-02" || inMemory != "2026-01-02" {
		t.Fatalf("重试成功后应写入触发记录: persisted=%q inMemory=%q", persisted, inMemory)
	}
}

// ---------- B6: 会话 Cookie 续期 ----------

// TestSessionCookieRenewedAfterThrottle 持续活跃时浏览器 Cookie 必须按节流
// 间隔重签(滑动续期),否则登录 24h 后 Cookie 过期、服务端续期形同虚设。
func TestSessionCookieRenewedAfterThrottle(t *testing.T) {
	srv := newRateLimitTestServer(t)
	oldRenew := sessionCookieRenewInterval
	sessionCookieRenewInterval = time.Hour
	t.Cleanup(func() { sessionCookieRenewInterval = oldRenew })

	login := postLoginFromAddr(t, srv, "10.0.0.2:9999", "admin")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", login.Code)
	}
	cookies := login.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("登录未下发会话 Cookie")
	}
	issuedLogin := session.Expires

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Value})
		rr := httptest.NewRecorder()
		srv.routes().ServeHTTP(rr, req)
		return rr
	}

	// 节流窗口内的请求不重签 Cookie。
	if rr := get(); rr.Code != http.StatusOK {
		t.Fatalf("authenticated GET status = %d, want 200", rr.Code)
	} else {
		for _, c := range rr.Result().Cookies() {
			if c.Name == sessionCookieName {
				t.Fatalf("节流窗口内不应重签 Cookie")
			}
		}
	}

	// 把签发时刻回拨到节流窗口之外,下一请求应重签并延长过期时间。
	srv.sessionMu.Lock()
	srv.sessionCookieIssuedAt[session.Value] = time.Now().Add(-2 * sessionCookieRenewInterval)
	srv.sessionMu.Unlock()

	rr := get()
	if rr.Code != http.StatusOK {
		t.Fatalf("authenticated GET status = %d, want 200", rr.Code)
	}
	var renewed *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			renewed = c
		}
	}
	if renewed == nil {
		t.Fatalf("节流窗口过后应重签 Cookie")
	}
	if renewed.Value != session.Value {
		t.Fatalf("续期应沿用原会话令牌")
	}
	// 同一秒内完成时两次过期时间可能相等,只要求不早于登录签发值。
	if renewed.Expires.Before(issuedLogin) {
		t.Fatalf("重签 Cookie 过期时间不应早于登录时: %s < %s", renewed.Expires, issuedLogin)
	}
}

// TestSessionKeepaliveEndpointRenewsCookieInGatewayMode 前端会话保活心跳
// 命中的 /api/get_uptime 必须在默认 WS 网关模式下也注册为 HTTP 路由:
// HttpOnly Cookie 无法经 WS 帧续期,保活只能走普通 HTTP;端点轻量
// (只读运行时长),会话中间件完成滑动续期与 Cookie 重签。
func TestSessionKeepaliveEndpointRenewsCookieInGatewayMode(t *testing.T) {
	srv := newRateLimitTestServer(t) // directAPI=false,WS 网关模式
	oldRenew := sessionCookieRenewInterval
	sessionCookieRenewInterval = time.Hour
	t.Cleanup(func() { sessionCookieRenewInterval = oldRenew })

	login := postLoginFromAddr(t, srv, "10.0.0.3:9999", "admin")
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("登录未下发会话 Cookie")
	}

	// 回拨签发时刻越过节流窗口,模拟心跳到达时已需续期。
	srv.sessionMu.Lock()
	srv.sessionCookieIssuedAt[session.Value] = time.Now().Add(-2 * sessionCookieRenewInterval)
	srv.sessionMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/get_uptime", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Value})
	rr := httptest.NewRecorder()
	srv.routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("网关模式保活端点 status = %d, want 200", rr.Code)
	}
	var renewed *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			renewed = c
		}
	}
	if renewed == nil {
		t.Fatalf("保活端点必须重签 Cookie(续期依赖此路径)")
	}

	// 未登录访问保活端点不得泄露数据,须 401。
	anon := httptest.NewRequest(http.MethodGet, "/api/get_uptime", nil)
	rrAnon := httptest.NewRecorder()
	srv.routes().ServeHTTP(rrAnon, anon)
	if rrAnon.Code != http.StatusUnauthorized {
		t.Fatalf("未登录保活端点 status = %d, want 401", rrAnon.Code)
	}
}

// ---------- B9: 读取失败不记录全零趋势点 ----------

func TestRecordDashboardMetricsSkipsErrorData(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	errorData := map[string]any{
		"error":            "AT 数据读取失败，请检查模块或稍后重试",
		"signalPercentage": 0,
		"nr_rx_bytes":      int64(0),
		"nr_tx_bytes":      int64(0),
	}
	recordDashboardMetricsIfReady(errorData)
	if points := snapshotMetricsHistory(); len(points) != 0 {
		t.Fatalf("error 数据不得记录趋势点,实际 %d 个", len(points))
	}

	pendingData := map[string]any{"pending": true}
	recordDashboardMetricsIfReady(pendingData)
	if points := snapshotMetricsHistory(); len(points) != 0 {
		t.Fatalf("pending 数据不得记录趋势点,实际 %d 个", len(points))
	}

	recordDashboardMetricsIfReady(map[string]any{
		"cpuUsagePercent":  10,
		"signalPercentage": 80,
		"nr_rx_bytes":      int64(1),
		"nr_tx_bytes":      int64(1),
	})
	if points := snapshotMetricsHistory(); len(points) != 1 {
		t.Fatalf("就绪数据应记录 1 个趋势点,实际 %d 个", len(points))
	}
}
