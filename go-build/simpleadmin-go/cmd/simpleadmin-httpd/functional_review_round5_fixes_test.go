package main

// 第五轮功能审查回归测试:每个测试对应本轮修复的一个功能性缺陷,
// 防止后续迭代回退。缺陷编号 R5-Bxx 与修复说明一一对应。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- R5-B1: 调度器补跑不受开机错误时钟污染 ----------

// TestSchedulerNoCatchUpOnBogusClockLastCheck 设备无电池时钟,开机初期
// LastCheck 被写成错误时钟下的"远古"值;NTP 校时把时钟向前步进数年后,
// 陈旧 LastCheck 不得触发补跑(否则设备开机约 1 分钟后意外重启)。
func TestSchedulerNoCatchUpOnBogusClockLastCheck(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	// 开机错误时钟(1970)下的一次检查,写入远古 LastCheck。
	if schedulerCheckOnce(nil, time.Date(1970, 1, 1, 0, 0, 5, 0, time.Local)) {
		t.Fatalf("错误时钟下不应触发")
	}

	// NTP 校时后时钟步进到当天 18:00(已过设定时刻 04:00):
	// LastCheck(1970)距 target 远超 48h 补跑窗口,不得补跑。
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 18, 0, 0, 0, time.Local)) {
		t.Fatalf("被开机错误时钟污染的 LastCheck 不应触发补跑重启")
	}
	if *count != 0 {
		t.Fatalf("重启动作次数 = %d, want 0", *count)
	}

	// 时钟正常后调度不受影响:次日设定时刻正常触发。
	if !schedulerCheckOnce(nil, time.Date(2026, 1, 3, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("次日设定时刻应正常触发")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}
}

// TestSchedulerCatchUpStillWorksWithRecentLastCheck 48h 护栏不得破坏
// 合法补跑:设备断电跨天,LastCheck 距 target 约 31h,仍应补跑。
func TestSchedulerCatchUpStillWorksWithRecentLastCheck(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 3, 0, 0, 0, time.Local)) {
		t.Fatalf("未到设定时刻不应触发")
	}
	if !schedulerCheckOnce(nil, time.Date(2026, 1, 3, 10, 0, 0, 0, time.Local)) {
		t.Fatalf("LastCheck 在 48h 窗口内且错过设定时刻,应补跑")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}
}

// ---------- R5-B2: 修改密码吊销其余会话 ----------

func round5DirectAPIServer(t *testing.T) (*httptest.Server, *simpleAdminServer) {
	t.Helper()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("make static dir: %v", err)
	}
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	cfg := serverConfig{staticDir: staticDir, authFile: authFile, noTLS: true, mockMode: true, directAPI: true}
	srv := &simpleAdminServer{cfg: cfg}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts, srv
}

func round5LoggedInClient(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	directAPILogin(t, ts, client)
	return client
}

// TestSetPasswordDestroysOtherSessions 改密成功后其余会话必须失效
// (被窃令牌不能靠改密前签发的会话继续存活),当前操作端会话保留。
func TestSetPasswordDestroysOtherSessions(t *testing.T) {
	ts, _ := round5DirectAPIServer(t)
	clientA := round5LoggedInClient(t, ts)
	clientB := round5LoggedInClient(t, ts)

	resp, err := clientA.PostForm(ts.URL+"/api/set_password", url.Values{
		"current_password": {"admin"},
		"new_password":     {"newpass123"},
		"confirm_password": {"newpass123"},
	})
	if err != nil {
		t.Fatalf("set_password: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("set_password = %d %s, want ok:true", resp.StatusCode, body)
	}

	// 其余会话(B)已吊销:业务 API 返回 401。
	respB, err := clientB.Get(ts.URL + "/api/get_uptime")
	if err != nil {
		t.Fatalf("client B get_uptime: %v", err)
	}
	respB.Body.Close()
	if respB.StatusCode != http.StatusUnauthorized {
		t.Fatalf("改密后其余会话状态码 = %d, want 401", respB.StatusCode)
	}

	// 当前会话(A)保留,无需重新登录。
	respA, err := clientA.Get(ts.URL + "/api/get_uptime")
	if err != nil {
		t.Fatalf("client A get_uptime: %v", err)
	}
	respA.Body.Close()
	if respA.StatusCode != http.StatusOK {
		t.Fatalf("改密后当前会话状态码 = %d, want 200", respA.StatusCode)
	}
}

// ---------- R5-B3: %2e%2e 编码穿越不得绕过认证 ----------

// TestPublicAuthPathTraversalBlocked /assets/%2e%2e/index.html 的解码路径
// 命中 /assets/ 放行前缀,而 ServeMux 按转义路径分发不产生 301,静态处理器
// Clean 后读出 index.html——受保护 HTML 入口被未认证读取。规范化后必须拦截。
func TestPublicAuthPathTraversalBlocked(t *testing.T) {
	if isPublicAuthPath("/assets/../index.html") {
		t.Fatalf("穿越路径被判为公开,受保护入口会被未认证读取")
	}
	if !isPublicAuthPath("/assets/app.js") {
		t.Fatalf("正常静态资源必须放行")
	}
	if isPublicAuthPath("/index.html") || isPublicAuthPath("/") {
		t.Fatalf("HTML 入口必须受会话保护")
	}

	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(filepath.Join(staticDir, "assets"), 0755); err != nil {
		t.Fatalf("make static dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("SECRET-INDEX"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "assets", "app.js"), []byte("PUBLIC-JS"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	srv := &simpleAdminServer{cfg: serverConfig{staticDir: staticDir, authFile: authFile, noTLS: true, mockMode: true}}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// 未认证读取编码穿越路径:必须被重定向到登录页,不得返回 index.html。
	resp, err := noRedirect.Get(ts.URL + "/assets/%2e%2e/index.html")
	if err != nil {
		t.Fatalf("traversal request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("穿越请求状态码 = %d body=%q, want 303 重定向登录页", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "SECRET-INDEX") {
		t.Fatalf("未认证读取到了受保护的 index.html")
	}

	// 正常静态资源仍放行。
	resp2, err := noRedirect.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("asset request: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || string(body2) != "PUBLIC-JS" {
		t.Fatalf("静态资源 = %d %q, want 200 PUBLIC-JS", resp2.StatusCode, body2)
	}
}

// ---------- R5-B4: AT 代理自动超时封顶 ----------

// TestATProxyAutoTimeoutCapped timeout_ms=0 的组合命令按子命令累加预算
// (每段 QSCAN 计 180s),不封顶则一条本地请求可把全局 AT 锁占住数小时。
func TestATProxyAutoTimeoutCapped(t *testing.T) {
	command := "AT+QSCAN=3,1" + strings.Repeat(";+QSCAN=3,1", 5) // 自动预算 6×180s
	if atCommandTimeoutMS(command) <= atProxyMaxTimeoutMS {
		t.Fatalf("测试命令自动预算 %d 未超上限,场景无效", atCommandTimeoutMS(command))
	}
	captured := make(chan int, 1)
	exec := func(_, _ string, timeoutMS int) (string, error) {
		captured <- timeoutMS
		return "OK\r\n", nil
	}
	resp := atPipeExchange(t, `{"command":"`+command+`","timeout_ms":0}`, exec)
	if !resp.OK {
		t.Fatalf("响应 = %+v, want ok=true", resp)
	}
	if got := <-captured; got != atProxyMaxTimeoutMS {
		t.Fatalf("自动推导超时 = %d, want 封顶为 %d", got, atProxyMaxTimeoutMS)
	}
}

// ---------- R5-B5: 动作命令清单覆盖写形态 ----------

func TestATActionCommandCoversWriteForms(t *testing.T) {
	actions := []string{
		`AT+CPIN="1234"`,
		`AT+CSCA="+8613800138000"`,
		`AT+COPS=1,0,"46000"`,
		"AT&W",
		"AT+QPOWD=1",
		"AT+CMGW",
		`AT+QMBNCFG="select","mbn_file.mbn"`,
		`AT+QMBNCFG="delete","mbn_file.mbn"`,
	}
	for _, command := range actions {
		if !isATActionCommand(command) {
			t.Errorf("isATActionCommand(%q) = false, 写命令超时后会被盲目重发", command)
		}
	}
	reads := []string{"AT+CPIN?", "AT+CSCA?", "AT+COPS?", `AT+QMBNCFG="List"`, "AT+QMBNCFG", "AT+CPMS?"}
	for _, command := range reads {
		if isATActionCommand(command) {
			t.Errorf("isATActionCommand(%q) = true, 查询形态不得判为动作", command)
		}
	}
}

// ---------- R5-B6: 正常运行期动作命令优先出队 ----------

func TestATCacheWorkerPrioritizesActionInNormalMode(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var mu sync.Mutex
	order := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		if command == "AT+BLOCKING" {
			close(started)
			<-release
		}
		mu.Lock()
		order = append(order, command)
		mu.Unlock()
		return command + "\r\nOK", nil
	})

	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 8)}
	go mgr.worker()

	mgr.enqueue("AT+BLOCKING", false)
	<-started                      // worker 已被长读命令占住
	mgr.enqueue("AT+CIMI", false)  // 长读命令先入队
	mgr.enqueue("AT+CMGD=1", true) // 动作命令后入队
	close(release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "AT+BLOCKING" || order[1] != "AT+CMGD=1" || order[2] != "AT+CIMI" {
		t.Fatalf("执行顺序 = %#v, want 动作命令先于排队的读命令", order)
	}
}

// ---------- R5-B7: 溢出限流放弃返回 pending 而非空串 ----------

func TestATCacheOverflowGiveUpReturnsPending(t *testing.T) {
	prevMax := atCacheOverflowMax
	atCacheOverflowMax = 0 // 溢出执行一律限流放弃
	t.Cleanup(func() { atCacheOverflowMax = prevMax })

	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 1)}
	mgr.queue <- "AT+BLOCKED" // 占满队列且无 worker 消费
	waitTrue := true
	got := mgr.Fetch("AT+CSQ", false, &waitTrue)
	if got != atCachePendingText {
		t.Fatalf("限流放弃后 Fetch = %q, want pending 文本(空串会被页面误报为 AT 读取失败)", got)
	}
}

// ---------- R5-B8: DMZ 状态只认 IPv4 记录 ----------

func TestParseNetworkConfigStatusDMZPrefersIPv4Record(t *testing.T) {
	// 固件按地址族各回一行;IPv6 行(恒禁用)不得覆盖 IPv4 的启用状态。
	raw := `+QMAP: "DMZ",1,4,"192.168.1.50"` + "\r\n" + `+QMAP: "DMZ",0,6` + "\r\nOK"
	data := parseNetworkConfigStatusAT(raw)
	if data["dmzMode"] != "1" {
		t.Fatalf("dmzMode = %v, want 1(IPv6 记录覆盖了 IPv4 启用态)", data["dmzMode"])
	}
	if data["dmzIP"] != "192.168.1.50" {
		t.Fatalf("dmzIP = %v, want 192.168.1.50", data["dmzIP"])
	}
	// 行序颠倒同样稳定。
	raw2 := `+QMAP: "DMZ",0,6` + "\r\n" + `+QMAP: "DMZ",1,4,"192.168.1.50"` + "\r\nOK"
	if data2 := parseNetworkConfigStatusAT(raw2); data2["dmzMode"] != "1" || data2["dmzIP"] != "192.168.1.50" {
		t.Fatalf("行序颠倒解析 = %v/%v, want 1/192.168.1.50", data2["dmzMode"], data2["dmzIP"])
	}
	// 无地址族字段的旧形态按 IPv4 处理(兼容)。
	raw3 := `+QMAP: "DMZ",1` + "\r\nOK"
	if data3 := parseNetworkConfigStatusAT(raw3); data3["dmzMode"] != "1" {
		t.Fatalf("无地址族形态 dmzMode = %v, want 1", data3["dmzMode"])
	}
}

// ---------- R5-B9: LAN IP 语义校验 ----------

func TestLANIPSemanticValidation(t *testing.T) {
	valid := []string{"192.168.1.1", "10.0.0.254", "172.16.0.1"}
	for _, ip := range valid {
		if !isUnicastIPv4(ip) {
			t.Errorf("isUnicastIPv4(%q) = false, want true", ip)
		}
	}
	invalid := []string{"0.0.0.0", "255.255.255.255", "224.0.0.1", "127.0.0.1", "169.254.1.1", "192.168.001.001", "::1", "abc", ""}
	for _, ip := range invalid {
		if isUnicastIPv4(ip) {
			t.Errorf("isUnicastIPv4(%q) = true, want false", ip)
		}
	}
	if !lanIPRangeOrdered("192.168.1.100", "192.168.1.200") {
		t.Errorf("start<end 应合法")
	}
	if !lanIPRangeOrdered("192.168.1.100", "192.168.1.100") {
		t.Errorf("start==end 应合法")
	}
	if lanIPRangeOrdered("192.168.1.200", "192.168.1.100") {
		t.Errorf("start>end 应非法")
	}

	// handler 层:非法三元组必须 400,不下发固件。
	s := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	for _, query := range []string{
		"action=lanip&start=192.168.1.200&end=192.168.1.100&gateway=192.168.1.1", // start>end
		"action=lanip&start=192.168.1.100&end=192.168.1.200&gateway=0.0.0.0",     // 非单播网关
		"action=lanip&start=224.0.0.5&end=192.168.1.200&gateway=192.168.1.1",     // 组播池起点
		"action=dmz&enabled=true&ip=255.255.255.255",                             // 广播 DMZ 目标
	} {
		rr := httptest.NewRecorder()
		s.handleNetworkConfigData(rr, httptest.NewRequest(http.MethodPost, "/api/network_config_data?"+query, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("query=%q 状态码 = %d, want 400", query, rr.Code)
		}
	}
}

// ---------- R5-B10: APN 拒绝非法输入而非静默剥离 ----------

func TestNetworkSettingsCommandsRejectsInvalidAPN(t *testing.T) {
	if _, err := networkSettingsCommands("IPV4V6", "###", "", ""); err == nil {
		t.Fatalf("全非法字符 APN 应拒绝,不得写成空 APN")
	}
	if _, err := networkSettingsCommands("IPV4V6", "ct net", "", ""); err == nil {
		t.Fatalf("含空格 APN 应拒绝,不得静默改写")
	}
	if _, err := networkSettingsCommands("IPV4V6", strings.Repeat("a", apnMaxLength+1), "", ""); err == nil {
		t.Fatalf("超长 APN 应拒绝")
	}
	commands, err := networkSettingsCommands("IPV4V6", "ctnet", "", "")
	if err != nil || len(commands) != 1 || commands[0] != `+CGDCONT=1,"IPV4V6","ctnet"` {
		t.Fatalf("合法 APN = %#v %v, want 单条 CGDCONT", commands, err)
	}
}

// ---------- R5-B11: 防火墙端口前导零归一化 ----------

func TestFirewallPortNormalizationBlocksLeadingZeroBypass(t *testing.T) {
	// 同端口 block+accept(前导零形态)必须检出冲突:否则 ACCEPT 080 先行,
	// DROP 80 永不可达,用户以为封了端口实际全网放行。
	if _, err := validateFirewallRules([]firewallRule{
		{Port: "80", Action: firewallActionBlock},
		{Port: "080", Action: firewallActionAccept},
	}); err == nil {
		t.Fatalf("080/80 冲突未被检出")
	}
	ports, err := parseFirewallPorts("80,080,0080")
	if err != nil || len(ports) != 1 || ports[0] != "80" {
		t.Fatalf("parseFirewallPorts = %#v %v, want 归一化去重后 [80]", ports, err)
	}
	rules, err := parseFirewallRuleLines("block 080\nblock 80")
	if err != nil || len(rules) != 1 || rules[0].Port != "80" {
		t.Fatalf("parseFirewallRuleLines = %#v %v, want 归一化去重后单条 block 80", rules, err)
	}
	if normalizeFirewallPort("0") != "" || normalizeFirewallPort("65536") != "" || normalizeFirewallPort("8a") != "" {
		t.Fatalf("非法端口归一化应为空串")
	}
	if normalizeFirewallPort("080") != "80" || normalizeFirewallPort("65535") != "65535" {
		t.Fatalf("合法端口归一化错误")
	}
}

// ---------- R5-B12: status6 mock 模式不触内核 ----------

func TestFirewallStatus6MockModeDoesNotTouchKernel(t *testing.T) {
	overrideFirewallRuntime(t, func(command string, args []string) (string, error) {
		t.Fatalf("mock 模式不得执行真实命令: %s %v", command, args)
		return "", nil
	})
	rr := callFirewallAPI(t, true, "action=status6")
	data := decodeFirewallBody(t, rr)
	if data["ok"] != true {
		t.Fatalf("status6 mock ok = %v, want true", data["ok"])
	}
	chains, _ := data["chains"].([]any)
	if len(chains) != 3 {
		t.Fatalf("status6 mock 链数量 = %d, want 3(INPUT/FORWARD/OUTPUT)", len(chains))
	}
}

// ---------- R5-B13: delete_all 后 webhook 不漏推复用索引的新短信 ----------

func TestSMSWebhookResetAfterDeleteAllPushesReusedIndices(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.invalid/hook"}); err != nil {
		t.Fatalf("writeSMSWebhookConfig: %v", err)
	}
	posted := installSMSWebhookRecorder(t)

	msg := func(storage string, index int) map[string]any {
		return map[string]any{"storage": storage, "indices": []int{index}, "sender": "10001", "date": "26/09/07", "text": "hi"}
	}
	// 建立基线并推进高水位到 5。
	notifyNewSMSByWebhookStorages([]map[string]any{msg("ME", 5)}, map[string]bool{"ME": true})
	notifyNewSMSByWebhookStorages([]map[string]any{msg("ME", 5)}, map[string]bool{"ME": true})

	// delete_all 成功后重置水位。
	resetSMSWebhookState("ME")

	// 清空后一个轮询周期内到达两条新短信,模块复用低索引 1、2。
	notifyNewSMSByWebhookStorages([]map[string]any{msg("ME", 1), msg("ME", 2)}, map[string]bool{"ME": true})

	// 两条都必须推送(旧实现只补发最大索引那条,索引 1 永久漏推)。
	gotIndexes := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case record := <-posted:
			var payload map[string]any
			if err := json.Unmarshal(record.body, &payload); err != nil {
				t.Fatalf("webhook 负载非 JSON: %v", err)
			}
			gotIndexes[fmt.Sprintf("%v", payload["index"])] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("delete_all 后新短信漏推:已收到 %d/2 条", i)
		}
	}
	if !gotIndexes["1"] || !gotIndexes["2"] {
		t.Fatalf("推送索引 = %v, want 1 与 2 都推送", gotIndexes)
	}
}

// ---------- R5-B14: 发送短信段数上限 ----------

func TestSendSMSBusinessRejectsTooManySegments(t *testing.T) {
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(t.TempDir(), "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	s := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	message := strings.Repeat("测", smsVendorSegmentUnits*(smsMaxSendSegments+1)) // 11 段
	result := s.sendSMSBusiness("+8613800138000", message)
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("超长短信不应被接受: %v", result)
	}
	if _, isValidation := result["validation"]; !isValidation {
		t.Fatalf("段数超限应标记为校验失败(400): %v", result)
	}
	if !strings.Contains(fmt.Sprintf("%v", result["error"]), "短信过长") {
		t.Fatalf("错误信息 = %v, want 含 短信过长", result["error"])
	}
	// 上限之内仍可发送(mock 模式)。
	okMessage := strings.Repeat("测", smsVendorSegmentUnits*smsMaxSendSegments) // 恰好 10 段
	if result2 := s.sendSMSBusiness("+8613800138000", okMessage); result2["ok"] != true {
		t.Fatalf("10 段以内应正常发送: %v", result2)
	}
}

// ---------- R5-B15: ME 无短信中心时合并 SM 侧数据 ----------

func TestMergeSMSListDualFillsServiceCentersFromSM(t *testing.T) {
	me := map[string]any{"messages": []map[string]any{}, "serviceCenters": []string{}}
	sm := map[string]any{
		"messages":       []map[string]any{{"sender": "10001", "storage": "SM"}},
		"serviceCenters": []string{"+8613800138000"},
	}
	merged, ok := mergeSMSListDual(me, sm)
	if !ok {
		t.Fatalf("SM 有有效消息,应返回 true")
	}
	centers, _ := merged["serviceCenters"].([]string)
	if len(centers) != 1 || centers[0] != "+8613800138000" {
		t.Fatalf("serviceCenters = %v, want SM 侧短信中心被合并(旧实现判键存在而非判空,永不生效)", centers)
	}
	// ME 自有短信中心时不被覆盖。
	me2 := map[string]any{"messages": []map[string]any{}, "serviceCenters": []string{"+8613900139000"}}
	sm2 := map[string]any{"messages": []map[string]any{{"storage": "SM"}}, "serviceCenters": []string{"+8613800138000"}}
	merged2, _ := mergeSMSListDual(me2, sm2)
	if centers2, _ := merged2["serviceCenters"].([]string); len(centers2) != 1 || centers2[0] != "+8613900139000" {
		t.Fatalf("ME 已有短信中心被覆盖: %v", centers2)
	}
}

// ---------- R5-B16: 半八度地址按规范映射 */#/a/b/c ----------

func TestDecodePDUSemiOctetsMapsSpecialNibbles(t *testing.T) {
	// TS 23.040 §9.1.2.5:10='*' 11='#' 12='a' 13='b' 14='c' 15=填充。
	if got := decodePDUSemiOctets([]byte{0xBA}, 2); got != "*#" {
		t.Fatalf("*# 业务号解码 = %q, want %q(旧实现静默丢弃 */# 半八度)", got, "*#")
	}
	if got := decodePDUSemiOctets([]byte{0xDC, 0xE4}, 4); got != "ab4c" {
		t.Fatalf("a/b/c 半八度解码 = %q, want ab4c", got)
	}
	// 填充符 F 仍被丢弃,数字解码行为不变。
	if got := decodePDUSemiOctets([]byte{0x21, 0x43, 0xF5}, 5); got != "12345" {
		t.Fatalf("数字+填充解码 = %q, want 12345", got)
	}
}

// ---------- R5-B17: set_ttl 严格校验拒绝负数 ----------

func TestSetTTLRejectsNegativeAndMalformedValues(t *testing.T) {
	ts, client, _ := ttlFixesMockServer(t)
	for _, value := range []string{"-1", "6.5", "1 2", "abc", "999"} {
		resp, err := client.Get(ts.URL + "/api/set_ttl?ttlvalue=" + url.QueryEscape(value))
		if err != nil {
			t.Fatalf("GET set_ttl(%q): %v", value, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("set_ttl(%q) 响应非 JSON: %s", value, body)
		}
		// 旧实现 stripNonDigits 把 "-1" 洗成 TTL=1(出站包一跳即丢,等效断网)
		// 且返回 ok:true;必须按非法输入拒绝。
		if data["ok"] != false {
			t.Fatalf("set_ttl(%q) = %v, want ok:false", value, data)
		}
	}
	// 合法值不受影响。
	resp, err := client.Get(ts.URL + "/api/set_ttl?ttlvalue=65")
	if err != nil {
		t.Fatalf("GET set_ttl(65): %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil || data["ok"] != true {
		t.Fatalf("set_ttl(65) = %s, want ok:true", body)
	}
}

// ---------- R5-B18: 连通性探测并行拨号保留冗余 ----------

func TestDialAnyTargetParallelKeepsRedundancy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	// 占位目标:监听后立即关闭,拨号快速失败(模拟首目标不可达)。
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dead: %v", err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dialTargetTimeout)
	defer cancel()
	// 首目标不可达时,总预算内后续目标仍有机会成功(串行实现下 2s 预算
	// 会被首目标的 1.5s 超时挤占,末尾目标名存实亡)。
	if !dialAnyTarget(ctx, []string{deadAddr, ln.Addr().String()}) {
		t.Fatalf("备用目标可达时应判定在线")
	}
	if dialAnyTarget(context.Background(), []string{deadAddr}) {
		t.Fatalf("全部目标不可达时应判定离线")
	}
	if dialAnyTarget(context.Background(), nil) {
		t.Fatalf("空目标列表应判定离线")
	}
}

// ---------- R5-B19: 总览流量解析 +QGDCNT(全制式总量) ----------

func TestParseDashboardUsesQGDCNTTotalCounters(t *testing.T) {
	// LTE 驻留:QGDNRCNT 恒 0,QGDCNT 有真实流量;旧实现丢弃 QGDCNT 行,
	// "累计流量"恒为 0。两个计数器字段序一致:<bytes_sent>,<bytes_recv>
	// (RG520N 手册 §9.7 与 EC2x 手册 §10.15)。
	raw := "+QSIMSTAT: 0,1\r\n+QGDNRCNT: 0,0\r\n+QGDCNT: 1000,5000\r\nOK"
	data := parseDashboardAT(raw)
	if data["nr_rx_bytes"] != int64(5000) || data["nr_tx_bytes"] != int64(1000) {
		t.Fatalf("LTE 驻留流量 = rx %v / tx %v, want rx=5000 tx=1000", data["nr_rx_bytes"], data["nr_tx_bytes"])
	}
	// 双计数器在场:逐方向取 max(总量 ≥ NR 分量,单侧被重置也不倒退)。
	raw2 := "+QSIMSTAT: 0,1\r\n+QGDNRCNT: 100,200\r\n+QGDCNT: 150,300\r\nOK"
	data2 := parseDashboardAT(raw2)
	if data2["nr_rx_bytes"] != int64(300) || data2["nr_tx_bytes"] != int64(150) {
		t.Fatalf("合并流量 = rx %v / tx %v, want rx=300 tx=150", data2["nr_rx_bytes"], data2["nr_tx_bytes"])
	}
}

// ---------- R5-B20: SIM 新鲜证据优先于陈旧缺卡证据 ----------

func TestParseDeviceInfoFreshSIMEvidenceOverridesStaleAbsent(t *testing.T) {
	// 静态批(10 分钟缓存)残留无卡开机时的 +CME ERROR: 10,
	// 新鲜批(3 秒缓存)显示热插卡后已在位:必须判"已插卡"。
	raw := strings.Join([]string{
		"Quectel",
		"RG520N-CN",
		"+CGSN: 860000000000000",
		"+CME ERROR: 10",
		"+QSIMSTAT: 0,1",
		"+CPIN: READY",
		"OK",
	}, "\r\n")
	data := parseDeviceInfoAT(raw)
	if data["simInserted"] != true || data["simStatus"] != "已插卡" {
		t.Fatalf("热插卡后 = %v/%v, want 已插卡(陈旧缺卡证据粘滞压制了新鲜证据)", data["simStatus"], data["simInserted"])
	}
	// CME 码精确匹配:100~107(unknown/invalid command 类)不再误判缺卡。
	if isSIMAbsentEvidence("+CME ERROR: 100") || isSIMAbsentEvidence("+CME ERROR: 107") {
		t.Fatalf("CME 100/107 被误判为缺卡证据")
	}
	if !isSIMAbsentEvidence("+CME ERROR: 10") || !isSIMAbsentEvidence("+CME ERROR: 310") {
		t.Fatalf("CME 10/310 必须判为缺卡证据")
	}
	// 拔卡方向不回归:新鲜 QSIMSTAT=0 仍压制陈旧静态批里的 ICCID/IMSI。
	raw2 := strings.Join([]string{
		"+ICCID: 89860317245913983015",
		"460030912345678",
		"+QSIMSTAT: 0,0",
		"OK",
	}, "\r\n")
	if data2 := parseDeviceInfoAT(raw2); data2["simInserted"] != false || data2["simStatus"] != "未插卡" {
		t.Fatalf("拔卡后 = %v/%v, want 未插卡", data2["simStatus"], data2["simInserted"])
	}
}

// ---------- R5-B21: 看门狗自愈动作被拒绝时不进入冷却 ----------

func TestWatchdogRejectedRebootRetriesNextTick(t *testing.T) {
	_, actionCalls, online := setupWatchdogTest(t)
	// 覆盖为本轮返回明确终结错误:模块拒绝了重启,自愈实际未发生。
	watchdogRebootAction = func(s *simpleAdminServer) string {
		*actionCalls++
		return "+CME ERROR: 4"
	}
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 1, CooldownMinutes: 30})
	*online = false

	watchdogTick(nil)
	if *actionCalls != 1 {
		t.Fatalf("action calls = %d, want 1", *actionCalls)
	}
	// 被明确拒绝的动作不得记录冷却:否则恢复被推迟一个完整冷却周期(30 分钟)。
	if status := currentWatchdogStatus(); status["lastActionTime"] != "" {
		t.Fatalf("被拒绝的动作记录了冷却时间: %v", status["lastActionTime"])
	}
	watchdogTick(nil)
	if *actionCalls != 2 {
		t.Fatalf("被拒绝后下一轮应重试,action calls = %d, want 2", *actionCalls)
	}
}

// ---------- R5-B22: 回显定位必须锚定行首 ----------

func TestFindEchoMarkerAnchoredToLineStart(t *testing.T) {
	// 短命令 "AT" 不得在 "STATUS" 等长单词内部误命中:否则陈旧数据被
	// 当作"回显之后的本命令响应"截断保留。
	buf := "+QENG: \"servingcell\",\"STATUS\"\r\nAT\r\nOK\r\n"
	want := strings.Index(buf, "\nAT\r") + 1
	if got := findEchoMarker([]byte(buf), "AT"); got != want {
		t.Fatalf("findEchoMarker = %d, want %d(行首锚定)", got, want)
	}
	if got := findEchoMarker([]byte("STATUS ONLY\r\nOK"), "AT"); got != -1 {
		t.Fatalf("无行首回显时 = %d, want -1", got)
	}
}

// ---------- R5-B23: 禁用 IP 透传后台流程去重 ----------

func TestIPPassthroughDisableDedup(t *testing.T) {
	endIPPassthroughDisable()
	t.Cleanup(endIPPassthroughDisable)
	if !beginIPPassthroughDisable() {
		t.Fatalf("首次占用应成功")
	}
	if beginIPPassthroughDisable() {
		t.Fatalf("在途时重复提交应被拒绝(否则并发协程重复下发配置与重启)")
	}
	endIPPassthroughDisable()
	if !beginIPPassthroughDisable() {
		t.Fatalf("流程结束后应允许再次提交")
	}
}

// ---------- R5-B24: 广播写失败必须关闭连接 ----------

// round5CloseTrackingConn 记录 Close 是否被调用:net.Pipe 的 Close 恒返回
// nil 且二次 Close 不报错,无法用"二次 Close 报错"判定广播路径是否关闭了
// 连接,必须显式跟踪。
type round5CloseTrackingConn struct {
	net.Conn
	closed int32
}

func (c *round5CloseTrackingConn) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return c.Conn.Close()
}

func TestBroadcastClosesConnOnWriteFailure(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	tracker := &round5CloseTrackingConn{Conn: serverConn}
	ws := &apiWebSocketConn{conn: tracker, br: bufio.NewReader(serverConn)}
	registerAPIWebSocketClient(ws)
	clientConn.Close() // 对端关闭,写帧必然失败

	broadcastAPIWebSocketEvent("test_event", map[string]string{"k": "v"})

	apiWebSocketEventHub.mu.Lock()
	remaining := len(apiWebSocketEventHub.clients)
	apiWebSocketEventHub.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("写失败客户端未注销, hub 残留 %d", remaining)
	}
	// 半截帧已污染字节流,连接必须被广播路径关闭而不是带病存活
	// (旧实现只注销不关闭)。
	if atomic.LoadInt32(&tracker.closed) != 1 {
		t.Fatalf("写失败后广播路径未关闭连接")
	}
}

// ---------- R5-B25: 历史采样 JSON 保留合法零值 ----------

func TestMetricsHistoryPointKeepsZeroValues(t *testing.T) {
	data, err := json.Marshal(metricsHistoryPoint{Timestamp: 1})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 空闲期速率 0、无信号期 0% 是合法观测值,不得被 omitempty 吞掉
	// (前端把字段缺失映射为 null,趋势图出现断线缺口而非贴零曲线)。
	for _, field := range []string{`"cpu":0`, `"ram":0`, `"sig":0`, `"rsrp":0`, `"rx":0`, `"tx":0`, `"rxr":0`, `"txr":0`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("JSON 缺少零值字段 %s: %s", field, data)
		}
	}
}

// ---------- R5-B26: 短信发送命令必须有可用回显标记 ----------

func TestSMSSendCommandHasEchoMarker(t *testing.T) {
	command := buildSMSVendorSendCommand("00310038003600310033003800", 1, 1, 1)
	if atPayloadEchoMarker(command) == "" {
		t.Fatalf("短信发送命令应派生非空回显标记,否则事务无回显关联保护")
	}
}
