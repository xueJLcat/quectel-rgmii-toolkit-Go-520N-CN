// functional_review_round7_fixes_test.go 覆盖第七轮功能审查修复:
// R7-B1  isATActionCommand 必须识别 payload 形态(AT&F\r\n),重试防护不再被击穿
// R7-B2  delete_indices 每个参数值都按逗号展开,混合传参不再拼出错误索引
// R7-B3  密码首尾空白对称校验 + 认证文件空密码拒绝(自愈重置)
// R7-B4  WS 握手 101 必须送达中间件已重签的 Set-Cookie
// R7-B5  并发同一动作命令每次执行(pendingForce 补跑,不再静默合并)
// R7-B6  五字段 PDU 头的非 DELIVER 条目必须跳过(PDU 长度不得当日期)
// R7-B7  delete_indices 后复用槽位的新短信必须补发 webhook(释放槽位登记)
// R7-B8  GSM7 septet 数按实际数据钳制,UCS2 按 UDL 截断
// R7-B9  发送失败不得把真实运行器错误吞成 UNKNOWN
// R7-B10 双存储删除失败时错误汇总必须保留
// R7-B11 set_password 当前密码校验复用登录限流
// R7-B12 syncWatchdogPoller 配置读取与启停决定同一临界区
// R7-B13 timesync 向前大步进(固件构建日期时钟)必须放行
// R7-B14 metrics 历史向前断层(超保留期)重置时间轴
// R7-B15 CPU 基线陈旧上限:长时间无人访问后首屏重测
// R7-B16 WS 网关拒绝路径同步写回(背压,不再无界派生写协程)
// R7-B17 尚未生效的托管证书必须判不匹配(触发重签自愈)
// R7-B18 监听服务器必须带 ReadHeaderTimeout
// R7-B19 setNativeTTLWithStatus 事务互斥
// R7-B20 dns_upstream_set"应用→写状态"同一临界区
// R7-B21 mac_bind_set 单播 IPv4 语义校验(前导零/非单播拒绝)
// R7-B22 开机补齐状态条目复验(AT 注入防护)
// R7-B23 LTE 锁小区多值+分隔符混用展开与空段拒绝
// R7-B24 DNAT 内网地址单播校验
// R7-B25 mac_bind_set 状态持久化失败如实报错
// R7-B26 读缓存失效代数:在途读结果不得按新鲜提交
// R7-B27 requiredTLSCertificateIPs 只含稳定地址
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- R7-B1: 动作命令 payload 形态识别 ----------

func TestATActionCommandDetectsPayloadForms(t *testing.T) {
	// 运行器重试防护处传入的是带 \r\n 的 payload(at_runner.go),旧实现
	// 精确匹配分支被尾缀击穿:恢复出厂超时后被盲目重发(执行两次)。
	for _, payload := range []string{"AT&F\r\n", "AT&W\r\n", "at&f\r\n", " AT&F ", "AT+CGMI;AT&F\r\n", "AT+CGMI;AT&W\r\n"} {
		if !isATActionCommand(payload) {
			t.Errorf("isATActionCommand(%q) = false, 动作命令超时后会被运行器盲目重发", payload)
		}
	}
	// 查询形态豁免不受净化影响。
	for _, query := range []string{"AT+COPS=?\r\n", "AT+CSQ\r\n"} {
		if isATActionCommand(query) {
			t.Errorf("isATActionCommand(%q) = true, 查询形态不得判为动作", query)
		}
	}
}

// ---------- R7-B2: delete_indices 混合传参拆分 ----------

func TestSMSDeleteIndicesSplitsEveryCommaValue(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	origRun := smsDeleteRunForTest
	commands := []string{}
	smsDeleteRunForTest = func(s *simpleAdminServer, command string) string {
		commands = append(commands, command)
		return command + "\r\nOK\r\n"
	}
	oldFreed := smsWebhookFreedSlots
	smsWebhookFreedSlots = map[string]map[int]bool{}
	t.Cleanup(func() {
		smsDeleteRunForTest = origRun
		smsWebhookFreedSlots = oldFreed
	})

	form := url.Values{"action": {"delete_indices"}, "indices": {"1,2", "3"}}
	req := httptest.NewRequest(http.MethodPost, "/api/sms_data", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleSMSData(rec, req)
	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (%s)", err, rec.Body.String())
	}
	if ok, _ := data["ok"].(bool); !ok {
		t.Fatalf("ok = false, error=%v", data["error"])
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{`+CMGD=1`, `+CMGD=2`, `+CMGD=3`} {
		found := false
		for _, cmd := range commands {
			if strings.HasSuffix(cmd, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("缺少删除命令 %q, 实际: %v", want, joined)
		}
	}
	for _, cmd := range commands {
		if strings.Contains(cmd, "+CMGD=12") {
			t.Fatalf(`混合传参把 "1,2" 拼成错误索引 12(静默删除错误短信): %v`, commands)
		}
	}
	if _, leaked := data["succeededTokens"]; leaked {
		t.Fatalf("内部键 succeededTokens 泄漏进 HTTP 响应: %v", data)
	}
	// 成功删除的槽位已登记(供 webhook 复用槽位补发)。
	for _, idx := range []int{1, 2, 3} {
		if !smsWebhookFreedSlots["ME"][idx] {
			t.Fatalf("ME:%d 未登记为释放槽位: %v", idx, smsWebhookFreedSlots)
		}
	}
}

// ---------- R7-B3: 密码空白对称与空密码拒绝 ----------

func TestPasswordWhitespaceSymmetryAndEmptyPasswordRejection(t *testing.T) {
	// 写入端:首尾空白必须拒绝(读取端整行 TrimSpace,写入不拒绝即静默变异;
	// 纯空白密码会退化为空密码)。
	for _, pw := range []string{"abc ", " abc", " ", "        ", "abc\t"} {
		if err := validateNewPassword(pw); err == nil {
			t.Errorf("validateNewPassword(%q) = nil, want 拒绝首尾空白", pw)
		}
	}
	if err := validateNewPassword("a b c"); err != nil {
		t.Errorf("内部空白密码合法,不得拒绝: %v", err)
	}

	// 读取端:空密码认证文件必须判不可解析,否则任何人以空密码登录成功。
	dir := t.TempDir()
	path := filepath.Join(dir, "simpleadmin.auth")
	for _, content := range []string{"admin:\n", "admin:        \n", "admin: \n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write auth file: %v", err)
		}
		if _, err := loadAuthConfig(path); err == nil {
			t.Fatalf("loadAuthConfig(%q) 成功, want 拒绝空密码(零口令认证后门)", content)
		}
		// ensureAuthFile 走损坏自愈:备份+重置默认凭据。
		if err := ensureAuthFile(path); err != nil {
			t.Fatalf("ensureAuthFile(%q): %v", content, err)
		}
		auth, err := loadAuthConfig(path)
		if err != nil || auth.Password != "admin" {
			t.Fatalf("自愈后应为默认凭据: auth=%+v err=%v", auth, err)
		}
		_ = os.Remove(path)
	}
}

// ---------- R7-B4: WS 握手送达重签 Cookie ----------

func TestWebSocketHandshakeDeliversRenewedCookie(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	srv := &simpleAdminServer{cfg: serverConfig{
		staticDir: e2eStaticDir(t),
		authFile:  authFile,
		ttlFile:   filepath.Join(dir, "ttlvalue"),
		noTLS:     true,
		mockMode:  true,
	}}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginResp, err := client.PostForm(ts.URL+"/api/login", url.Values{"username": {"admin"}, "password": {"admin"}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	u, _ := url.Parse(ts.URL)
	token := ""
	for _, c := range jar.Cookies(u) {
		if c.Name == sessionCookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatalf("登录后无会话 Cookie")
	}
	// 把重签节流基准拨回 2 小时前:下一个已认证请求(此处为 WS 握手)必须重签。
	srv.sessionMu.Lock()
	srv.sessionCookieIssuedAt[token] = time.Now().Add(-2 * time.Hour)
	srv.sessionMu.Unlock()

	host := ts.Listener.Addr().String()
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /api/ws HTTP/1.1\r\nHost: %s\r\nOrigin: http://%s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nCookie: %s=%s\r\n\r\n",
		host, host, sessionCookieName, token)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("WS 握手状态行 = %q, want 101", statusLine)
	}
	foundCookie := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		if strings.HasPrefix(line, "Set-Cookie:") && strings.Contains(line, sessionCookieName) {
			foundCookie = true
		}
	}
	if !foundCookie {
		t.Fatalf("WS 握手 101 未携带重签的 Set-Cookie:续期额度被消耗而 Cookie 从未送达," +
			"只走 WS 的客户端持续活跃 24h 后仍被浏览器清 Cookie 登出")
	}
}

// ---------- R7-B5: 并发动作命令每次执行 ----------

func TestConcurrentActionCommandExecutesEveryTime(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		return command + "\r\nOK", nil
	})
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 8)}

	d1 := mgr.enqueue("AT&W", true)
	d2 := mgr.enqueue("AT&W", true) // 并入在途执行,pendingForce=1
	mgr.run("AT&W")                 // 第一次执行完成 → 补跑重新入队,waiters 不唤醒

	select {
	case <-d1:
		t.Fatalf("第一次执行完成即唤醒等待者:第二个动作请求被静默合并(未执行却报成功)")
	default:
	}
	select {
	case <-d2:
		t.Fatalf("第二个请求的等待者被提前唤醒")
	default:
	}

	select {
	case cmd := <-mgr.queue:
		if cmd != "AT&W" {
			t.Fatalf("补跑入队命令 = %q, want AT&W", cmd)
		}
		mgr.run(cmd)
	default:
		t.Fatalf("补跑未重新入队")
	}

	for i, done := range []<-chan struct{}{d1, d2} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("补跑完成后等待者 %d 未被唤醒", i)
		}
	}
	mu.Lock()
	got := runs
	mu.Unlock()
	if got != 2 {
		t.Fatalf("并发同一动作命令执行了 %d 次, want 2(动作命令契约:每次执行)", got)
	}
}

// ---------- R7-B6: 五字段 PDU 头跳过非 DELIVER ----------

func TestParseSMSListSkipsNonDeliverPDUFiveFieldHeader(t *testing.T) {
	pdu := buildMockSMSDeliverPDU("+8613800138000", "不会显示")
	statusReport := pdu[:2] + "06" + pdu[4:] // DELIVER → STATUS-REPORT,解析必失败
	// 标准五字段头 `+CMGL: <idx>,<stat>,[<oa>],[<alpha>],<len>`:parts[4] 是
	// PDU 长度。旧闸门只判 date=="",长度字符串非空即绕过,产出 date="28"、
	// 正文为整条 PDU 十六进制的乱码假短信并被 webhook 推送。
	for _, raw := range []string{
		fmt.Sprintf("+CMGL: 1,2,\"+8613800138000\",,%d\r\n%s\r\nOK\r\n", len(statusReport)/2-1, statusReport),
		fmt.Sprintf("+CMGL: 1,2,\"+8613800138000\",\"张三\",%d\r\n%s\r\nOK\r\n", len(statusReport)/2-1, statusReport),
	} {
		data := parseSMSListAT(raw, "ME")
		messages, _ := data["messages"].([]map[string]any)
		if len(messages) != 0 {
			t.Fatalf("messages = %#v, want empty(五字段头非 DELIVER 必须跳过)", messages)
		}
	}

	// 同形态头的合法 DELIVER 保留(PDU 解析成功,不经过闸门)。
	deliver := buildMockSMSDeliverPDU("+8610001", "正常短信")
	raw := fmt.Sprintf("+CMGL: 2,1,\"+8610001\",,%d\r\n%s\r\nOK\r\n", len(deliver)/2-1, deliver)
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 1 || stringValue(messages[0]["text"]) != "正常短信" {
		t.Fatalf("messages = %#v, want 1 条正常 DELIVER", messages)
	}

	// 文本模式十六进制正文(头部带日期)仍按原有逻辑解码,闸门不误杀。
	textMode := "+CMGL: 3,1,\"+8610002\",\"26/08/30,10:20:30+32\"\r\n4F60597D\r\nOK"
	data = parseSMSListAT(textMode, "ME")
	messages, _ = data["messages"].([]map[string]any)
	if len(messages) != 1 || stringValue(messages[0]["text"]) != "你好" {
		t.Fatalf("messages = %#v, want 文本模式 hex 正文解码为 你好", messages)
	}
}

// ---------- R7-B7: 释放槽位补发 ----------

func TestSMSWebhookNotifiesReusedSlotAfterPartialDelete(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	msg := func(idx int, text string) map[string]any {
		return map[string]any{"sender": fmt.Sprintf("+86138000%05d", idx), "date": "26/09/07,10:00:00+32", "text": text, "indices": []int{idx}, "storage": "ME"}
	}
	base := []map[string]any{msg(1, "a"), msg(2, "b"), msg(3, "c"), msg(4, "d"), msg(5, "e")}
	notifyNewSMSByWebhook(base, true) // 首扫建基线(水位 5)
	expectNoSMSWebhookPosts(t, ch)

	// 删除中间索引 3(生产路径由 delete_indices handler 登记)。
	markSMSWebhookSlotsFreed([]string{"ME:3"})
	// 删除后的扫描:最大索引仍为 5,回绕启发式不触发,无新消息。
	notifyNewSMSByWebhook([]map[string]any{msg(1, "a"), msg(2, "b"), msg(4, "d"), msg(5, "e")}, true)
	expectNoSMSWebhookPosts(t, ch)

	// 新短信落入被复用的槽位 3:高水位机制识别不了(idx ≤ 5),必须按登记补发。
	withNew := []map[string]any{msg(1, "a"), msg(2, "b"), msg(3, "NEW"), msg(4, "d"), msg(5, "e")}
	notifyNewSMSByWebhook(withNew, true)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 3 || payload["text"] != "NEW" {
		t.Fatalf("复用槽位的新短信通知内容不符: %s", records[0].body)
	}

	// 登记已消耗:同一列表再扫不重复推送。
	notifyNewSMSByWebhook(withNew, true)
	expectNoSMSWebhookPosts(t, ch)
}

// ---------- R7-B8: PDU 解码钳制 ----------

func TestDecodePDUUserDataClampsToActualData(t *testing.T) {
	// 截断的 GSM7:UDL 声明 10 个 septet,实际数据只支撑 2 个;
	// 越界位按 0 拼字会伪造 '@' 填充(旧实现输出 "Aa@@@@@@@@"）。
	text, _, _, _ := decodePDUUserData(0x00, 0x00, 10, []byte{0xC1, 0x30})
	if text != "Aa" {
		t.Fatalf("gsm7 = %q, want Aa(septet 数必须按实际数据长度钳制)", text)
	}
	// UCS2 按 UDL 截断:声明 4 个八位组(2 字符),尾部多余字节不得参与解码。
	ucs, _, _, _ := decodePDUUserData(0x00, 0x08, 4, []byte{0x4F, 0x60, 0x59, 0x7D, 0x00, 0x41})
	if ucs != "你好" {
		t.Fatalf("ucs2 = %q, want 你好(按 UDL 截断)", ucs)
	}
}

// ---------- R7-B9: 发送失败透出真实错误 ----------

func TestSendSMSBusinessSurfacesRunnerError(t *testing.T) {
	old := runSMSTransaction
	runSMSTransaction = func(sendCmd, message string) (string, error) {
		// 已捕获部分输出(回显)但等提示符超时:resp 非空 + err 非哨兵。
		return sendCmd + "\r\nAT+CMGS=...\r\n", errors.New("read SMS prompt failed: timeout waiting for SMS prompt")
	}
	t.Cleanup(func() { runSMSTransaction = old })
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return "460110000000000\r\nOK", nil // IMSI 读取
	})
	t.Cleanup(func() { atCommandCache.Start(true) })

	srv := &simpleAdminServer{cfg: serverConfig{mockMode: false}}
	result := srv.sendSMSBusiness("+8613800138000", "hello")
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("发送应失败: %v", result)
	}
	errText := fmt.Sprint(result["error"])
	if errText == "UNKNOWN" || !strings.Contains(errText, "timeout waiting for SMS prompt") {
		t.Fatalf("error = %q, want 真实运行器错误文本(不得吞成 UNKNOWN)", errText)
	}
	// 有明确 CMS ERROR 时仍按错误码归类(不被 err 文本覆盖)。
	runSMSTransaction = func(sendCmd, message string) (string, error) {
		return sendCmd + "\r\n+CMS ERROR: 331\r\n", errors.New("transaction failed")
	}
	result = srv.sendSMSBusiness("+8613800138000", "hello")
	if got := fmt.Sprint(result["error"]); !strings.Contains(got, "CMS 331") {
		t.Fatalf("error = %q, want CMS 331 归类保留", got)
	}
}

// ---------- R7-B10: 双存储删除错误汇总 ----------

func TestDeleteIndicesAggregatesBothStorageErrors(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	origRun := smsDeleteRunForTest
	smsDeleteRunForTest = func(s *simpleAdminServer, command string) string { return "ERROR" }
	t.Cleanup(func() { smsDeleteRunForTest = origRun })

	result := srv.deleteSMSIndicesByToken([]string{"ME:1", "SM:2"})
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("全部失败时 ok 应为 false")
	}
	errText := fmt.Sprint(result["error"])
	if !strings.Contains(errText, "(ME)") || !strings.Contains(errText, "(SM)") {
		t.Fatalf("error = %q, want 双存储失败详情都保留(先失败方不得被覆盖)", errText)
	}
}

// ---------- R7-B11: set_password 限流 ----------

func TestSetPasswordRateLimitsCurrentPasswordBruteForce(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	srv := &simpleAdminServer{cfg: serverConfig{authFile: authFile}}
	post := func(remoteAddr, current string) int {
		form := url.Values{"current_password": {current}, "new_password": {"NewPass123"}, "confirm_password": {"NewPass123"}}
		req := httptest.NewRequest(http.MethodPost, "/api/set_password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		srv.handleSetPassword(rec, req)
		return rec.Code
	}

	const attacker = "192.168.1.50:54321"
	for i := 0; i < loginMaxFailures; i++ {
		if code := post(attacker, "wrong"); code != http.StatusForbidden {
			t.Fatalf("第 %d 次错误当前密码 = %d, want 403", i+1, code)
		}
	}
	if code := post(attacker, "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("锁定后 set_password = %d, want 429(当前密码校验必须复用登录限流,"+
			"否则被窃会话可无限爆破,绕过登录侧 5 次锁定)", code)
	}
	if remaining := srv.loginLockedRemaining("192.168.1.50"); remaining <= 0 {
		t.Fatalf("set_password 失败未计入登录限流")
	}

	// 当前密码正确即清零计数(与登录成功同口径),且不受其他 IP 锁定影响。
	const user = "192.168.1.60:1234"
	post(user, "wrong")
	post(user, "wrong")
	if code := post(user, "admin"); code != http.StatusOK {
		t.Fatalf("正确当前密码 = %d, want 200", code)
	}
	if remaining := srv.loginLockedRemaining("192.168.1.60"); remaining != 0 {
		t.Fatalf("验证通过后失败计数应清零, remaining = %s", remaining)
	}
}

// ---------- R7-B12: watchdog 同步临界区 ----------

func TestSyncWatchdogPollerReadsConfigUnderLock(t *testing.T) {
	setupWatchdogTest(t)
	oldWait := watchdogPollWait
	watchdogPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		return false
	}
	t.Cleanup(func() {
		watchdogPollWait = oldWait
		stopWatchdogPoller()
	})

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: false, FailThreshold: 5, CooldownMinutes: 30})
	// 持有启停锁发起同步:新实现的"读配置→决定→启停"整体在锁内,阻塞期间
	// 写入 enabled → 解锁后 sync 必须读到最新配置并启动轮询器。旧实现锁外
	// 读配置,并发交错会留下"配置已启用但轮询器停止"的静默失效状态。
	watchdogPollerMu.Lock()
	done := make(chan struct{})
	go func() {
		syncWatchdogPoller(nil)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 5, CooldownMinutes: 30})
	watchdogPollerMu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("syncWatchdogPoller 未返回")
	}
	if !watchdogPollerRunning() {
		t.Fatalf("配置读取与启停决定不在同一临界区:解锁后应按最新配置(enabled)启动轮询器")
	}
}

// ---------- R7-B13: timesync 向前大步进放行 ----------

func TestRunTimeSyncOnceAllowsHugeForwardOffsetBuildDateClock(t *testing.T) {
	withTempTimeSyncEnv(t)
	// 模拟固件构建日期时钟:本地时钟"合理"(晚于 2020 下界)但落后真实时间
	// 数年(2026 年运行 2024 年构建的固件即触发),NTP 向前偏差远超
	// timeSyncMaxStep。固定下界判定是时间炸弹:拒绝会让无电池时钟设备的
	// 校时在其最主要场景(纠正开机大偏差)永久失效。
	queries, applied := withFakeTimeSync(t, 800*24*time.Hour, nil, nil)
	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("向前大步进被拒绝(构建日期时钟永久无法校时): %v", result)
	}
	if *queries != 1 || applied.IsZero() {
		t.Fatalf("queries=%d applied=%v, want 校时已写入", *queries, applied)
	}
}

// ---------- R7-B14: metrics 向前断层重置 ----------

func TestMetricsHistoryForwardClockStepResets(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	// 预填 1970 年代采样点(冷启动校时前记录),随后时钟向前步进到真实年代。
	metricsHistory.mu.Lock()
	metricsHistory.points = append(metricsHistory.points,
		metricsHistoryPoint{Timestamp: 1000}, metricsHistoryPoint{Timestamp: 1060})
	metricsHistory.last = time.Now().Add(-2 * time.Minute)
	metricsHistory.mu.Unlock()

	recordMetricsHistoryFromDashboard(map[string]any{"signalPercentage": 50})
	points := snapshotMetricsHistory()
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1(向前断层必须丢弃旧年代时间轴,否则趋势图 x 轴横跨约 56 年)", len(points))
	}
	if points[0].Timestamp < 1000000000 {
		t.Fatalf("留存点仍是旧年代: %d", points[0].Timestamp)
	}
}

func TestMetricsHistoryLegitimateGapStillAppends(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	// 页面关闭数小时后重开的合法断档(远小于 24h 保留期)必须正常追加,
	// 不得被向前断层判定误杀。
	now := time.Now()
	metricsHistory.mu.Lock()
	metricsHistory.points = append(metricsHistory.points,
		metricsHistoryPoint{Timestamp: now.Add(-3 * time.Hour).Unix(), RxBytesTotal: 100},
		metricsHistoryPoint{Timestamp: now.Add(-3*time.Hour + time.Minute).Unix(), RxBytesTotal: 200})
	metricsHistory.last = now.Add(-2 * time.Minute)
	metricsHistory.mu.Unlock()

	recordMetricsHistoryFromDashboard(map[string]any{"signalPercentage": 60, "nr_rx_bytes": int64(300)})
	points := snapshotMetricsHistory()
	if len(points) != 3 {
		t.Fatalf("points = %d, want 3(合法断档追加,不重置)", len(points))
	}
}

// ---------- R7-B15: CPU 基线时效 ----------

func TestCPUUsageBaselineStalenessRemeasures(t *testing.T) {
	if _, ok := readCPUTimesSnapshot(); !ok {
		t.Skip("/proc/stat 不可用")
	}
	systemMetricsMu.Lock()
	oldTimes, oldAt, oldHas := lastCPUTimes, lastCPUTimesAt, hasLastCPUTimes
	systemMetricsMu.Unlock()
	t.Cleanup(func() {
		systemMetricsMu.Lock()
		lastCPUTimes, lastCPUTimesAt, hasLastCPUTimes = oldTimes, oldAt, oldHas
		systemMetricsMu.Unlock()
	})

	// 新鲜基线(正常轮询节奏):不触发 120ms 重测。
	systemMetricsMu.Lock()
	snap, _ := readCPUTimesSnapshot()
	lastCPUTimes, lastCPUTimesAt, hasLastCPUTimes = snap, time.Now(), true
	systemMetricsMu.Unlock()
	start := time.Now()
	currentCPUUsagePercent()
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("新鲜基线触发了重测: %s", elapsed)
	}

	// 陈旧基线(长时间无人访问后首屏):必须重测,不得以整个空闲区间的
	// 平均值冒充当前占用。
	systemMetricsMu.Lock()
	lastCPUTimesAt = time.Now().Add(-time.Hour)
	systemMetricsMu.Unlock()
	start = time.Now()
	currentCPUUsagePercent()
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Fatalf("陈旧基线未触发重测: %s(首屏显示的是小时级平均值)", elapsed)
	}
}

// ---------- R7-B16: WS 拒绝路径背压 ----------

func TestAPIWebSocketRejectPathAppliesBackpressure(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ws := &apiWebSocketConn{conn: server, br: bufio.NewReader(server), dispatchSem: make(chan struct{}, 1)}
	ws.dispatchSem <- struct{}{} // 在途分发额度已满
	registerAPIWebSocketClient(ws)
	defer unregisterAPIWebSocketClient(ws)

	srv := &simpleAdminServer{}
	base := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	done := make(chan struct{})
	go func() {
		srv.dispatchAPIWebSocketRequestAsync(base, ws, []byte(`{"id":"r1","path":"/api/get_uptime"}`))
		close(done)
	}()

	// 同步写回:对端不读时调用必须阻塞在写超时上形成背压;旧实现立即返回
	// 并每帧派生一个写协程,洪泛下协程与缓冲无界堆积(dispatchSem 想消除
	// 的 OOM 风险从拒绝分支漏回来)。
	select {
	case <-done:
		t.Fatalf("拒绝路径在无读者时立即返回:写回已脱离读循环,洪泛下无界堆积")
	case <-time.After(200 * time.Millisecond):
	}

	// 读走拒绝帧后调用返回,帧内容为 503 + 请求 id。
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		t.Fatalf("read frame header: %v", err)
	}
	length := int(header[1] & 0x7f)
	if length == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(client, ext); err != nil {
			t.Fatalf("read ext length: %v", err)
		}
		length = int(ext[0])<<8 | int(ext[1])
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(client, payload); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("拒绝帧写出后调用未返回")
	}
	var resp apiWebSocketResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("拒绝帧不是合法 JSON: %v (%s)", err, payload)
	}
	if resp.ID != "r1" || resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("拒绝帧 = %+v, want id=r1 status=503", resp)
	}
}

// ---------- R7-B17: 尚未生效的托管证书重签自愈 ----------

func TestServerCertificateMatchesRejectsNotYetValidManagedCert(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey, err := ensureLocalCACertificate(filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatalf("ensureLocalCACertificate: %v", err)
	}
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	build := func(notBefore time.Time) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		tmpl := x509.Certificate{
			SerialNumber:          big.NewInt(42),
			Subject:               pkix.Name{Organization: []string{"ZBIMS"}, CommonName: "ZBIMS Local HTTPS"},
			DNSNames:              requiredTLSCertificateDNSNames(),
			IPAddresses:           requiredTLSCertificateIPs(),
			NotBefore:             notBefore,
			NotAfter:              time.Now().AddDate(1, 0, 0),
			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
		}
		der, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatalf("create certificate: %v", err)
		}
		if err := writeCertificatePEM(certPath, der); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := writeRSAPrivateKeyPEM(keyPath, key); err != nil {
			t.Fatalf("write key: %v", err)
		}
	}

	// 时钟先被拨快期间签发的托管证书(NotBefore 在未来),时钟校回后必须
	// 判不匹配触发重签;旧实现只查 NotAfter 侧,HTTPS 瘫痪且重启永不自愈。
	build(time.Now().Add(24 * time.Hour))
	if serverCertificateMatches(certPath, keyPath, caCert) {
		t.Fatalf("尚未生效的托管证书被判匹配:重签自愈永不触发")
	}
	// 对照:NotBefore 已过的同模板证书应匹配(不误伤正常路径)。
	build(time.Now().Add(-time.Hour))
	if !serverCertificateMatches(certPath, keyPath, caCert) {
		t.Fatalf("正常托管证书被判不匹配:会触发无意义重签")
	}
}

// ---------- R7-B18: 监听超时 ----------

func TestNewHTTPServerHasReadHeaderTimeout(t *testing.T) {
	srv := newHTTPServer(":0", nil)
	if srv.ReadHeaderTimeout <= 0 {
		t.Fatalf("监听服务器缺少 ReadHeaderTimeout:慢速请求头攻击可耗尽设备连接资源")
	}
}

// ---------- R7-B19: TTL 事务互斥 ----------

func TestSetNativeTTLTransactionHoldsMutex(t *testing.T) {
	dir := t.TempDir()
	old := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = old })

	nativeTTLMu.Lock()
	done := make(chan struct{})
	go func() {
		setNativeTTLWithStatus(0)
		close(done)
	}()
	select {
	case <-done:
		t.Fatalf("setNativeTTLWithStatus 未等待事务锁即返回:并发 set_ttl 交错会在内核残留重复托管规则、状态文件与生效值背离")
	case <-time.After(150 * time.Millisecond):
	}
	nativeTTLMu.Unlock()
	select {
	case <-done:
	case <-time.After(45 * time.Second):
		t.Fatalf("释放锁后 setNativeTTLWithStatus 未返回")
	}
}

// ---------- R7-B20: dns_upstream_set 事务临界区 ----------

func TestDNSUpstreamSetHoldsTransactionLock(t *testing.T) {
	dir := t.TempDir()
	oldState := runtimeDNSUpstreamStateFile
	runtimeDNSUpstreamStateFile = filepath.Join(dir, "dns_upstream.conf")
	t.Cleanup(func() { runtimeDNSUpstreamStateFile = oldState })

	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	form := url.Values{"action": {"dns_upstream_set"}, "enabled": {"1"}, "servers": {"1.1.1.1"}}
	req := httptest.NewRequest(http.MethodPost, "/api/network_config_data", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	dnsUpstreamMu.Lock()
	done := make(chan struct{})
	var rec *httptest.ResponseRecorder
	go func() {
		rec = httptest.NewRecorder()
		srv.handleNetworkConfigData(rec, req)
		close(done)
	}()
	select {
	case <-done:
		t.Fatalf("dns_upstream_set 未持有事务锁即返回:并发保存交错会让状态文件与实际生效配置背离,开机自愈按旧状态静默回滚")
	case <-time.After(150 * time.Millisecond):
	}
	dnsUpstreamMu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("释放锁后处理器未返回")
	}

	state, err := readDNSUpstreamState()
	if err != nil || !state.Enabled || len(state.Servers) != 1 || state.Servers[0] != "1.1.1.1" {
		t.Fatalf("状态未持久化: state=%+v err=%v", state, err)
	}
}

// ---------- R7-B21: mac_bind_set 单播校验 ----------

func TestMacBindSetRejectsNonUnicastAndLeadingZeroIPs(t *testing.T) {
	ts, client := e2eTestServer(t)
	// 前导零形态固件回读时被 net.ParseIP 拒绝 → 绑定在列表不可见,删除走
	// 幂等路径清状态后固件侧永久残留僵尸租约;非单播地址写入 dhcp_hosts
	// 产生垃圾条目;字符串冲突检测也被前导零绕过。与 dmz/lanip 同口径拒绝。
	for _, ip := range []string{"192.168.010.010", "0.0.0.0", "224.0.0.1", "255.255.255.255", "127.0.0.1", "169.254.1.1"} {
		resp := e2eWebSocketCall(t, ts, client, "mb:"+ip, "POST", "/api/network_config_data",
			"action=mac_bind_set&mac=AA:BB:CC:DD:EE:FF&ip="+url.QueryEscape(ip))
		if status := fmt.Sprint(resp["status"]); status != "400" {
			t.Fatalf("ip=%s status = %v, want 400 (body=%v)", ip, resp["status"], resp["body"])
		}
	}
}

// ---------- R7-B22: 开机补齐状态条目复验 ----------

func TestMacBindStateEntryValidRejectsInjection(t *testing.T) {
	cases := []struct {
		entry macBindEntry
		want  bool
	}{
		{macBindEntry{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.10"}, true},
		{macBindEntry{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.5"}, true},
		// 污染的状态文件把 AT 子命令藏进 MAC 字段:补齐路径未复验即拼命令,
		// 模块按分号复合执行(重启循环/恢复出厂)。
		{macBindEntry{MAC: `AA:BB:CC:DD:EE:FF";+CFUN=1,1;"`, IP: "1.1.1.1"}, false},
		{macBindEntry{MAC: `AA:BB:CC:DD:EE:FF","1.1.1.1";AT&F;"`, IP: "1.1.1.1"}, false},
		{macBindEntry{MAC: "not-a-mac", IP: "192.168.1.10"}, false},
		{macBindEntry{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.010.1"}, false},
		{macBindEntry{MAC: "AA:BB:CC:DD:EE:FF", IP: "0.0.0.0"}, false},
		{macBindEntry{MAC: "AA:BB:CC:DD:EE:FF", IP: ""}, false},
	}
	for _, tc := range cases {
		if got := macBindStateEntryValid(tc.entry); got != tc.want {
			t.Fatalf("macBindStateEntryValid(%+v) = %v, want %v", tc.entry, got, tc.want)
		}
	}
}

// ---------- R7-B23: LTE 锁小区参数展开 ----------

func TestLockScannedCellsLTEExpandsEverySeparator(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK", nil
	})
	s := networkDataTestServer()
	call := func(query url.Values) (string, error) {
		req := httptest.NewRequest(http.MethodGet, "/api/network_data?"+query.Encode(), nil)
		return s.lockScannedCells(req)
	}

	// 多值+逗号混用:旧实现不拆分 "1,2",stripNonDigits 洗成 "12" 后静默
	// 锁定错误频点;展开后 earfcn 3 个 vs pci 2 个,数量不一致必须拒绝。
	resp, err := call(url.Values{"mode": {"LTE Only"}, "earfcn": {"1,2", "3"}, "pci": {"4", "5"}})
	if err == nil {
		t.Fatalf("混用形态被接受(resp=%q):错误频点 12 已下发", resp)
	}
	mu.Lock()
	for _, cmd := range executed {
		if strings.Contains(cmd, ",12,") {
			t.Fatalf("分隔符被拼成错误频点 12: %v", executed)
		}
	}
	mu.Unlock()

	// 数量一致的逗号串正常展开。
	executed = nil
	if _, err := call(url.Values{"mode": {"LTE Only"}, "earfcn": {"1,2"}, "pci": {"3,4"}}); err != nil {
		t.Fatalf("合法逗号串被拒绝: %v", err)
	}
	mu.Lock()
	want := `AT+QNWLOCK="common/4g",2,1,3,2,4`
	if len(executed) == 0 || executed[len(executed)-1] != want {
		t.Fatalf("executed = %v, want %s", executed, want)
	}
	mu.Unlock()

	// 空段拒绝:旧实现拼出 `...,2,1850,1,,2` 形态的畸形命令。
	if _, err := call(url.Values{"mode": {"LTE Only"}, "earfcn": {"1850,"}, "pci": {"1,2"}}); err == nil {
		t.Fatalf("空 earfcn 段未被拒绝")
	}

	// lockLTEManual:多值+分号混用,每个值都展开。
	executed = nil
	req := httptest.NewRequest(http.MethodGet, "/api/network_data?"+url.Values{"pairs": {"1850,1", "1900,2;2000,3"}}.Encode(), nil)
	if _, err := s.lockLTEManual(req); err != nil {
		t.Fatalf("lockLTEManual 混用形态被拒绝: %v", err)
	}
	mu.Lock()
	want = `AT+QNWLOCK="common/4g",3,1850,1,1900,2,2000,3`
	if len(executed) == 0 || executed[len(executed)-1] != want {
		t.Fatalf("executed = %v, want %s", executed, want)
	}
	mu.Unlock()
}

// ---------- R7-B24: DNAT 内网地址单播校验 ----------

func TestFirewallFwdRuleRejectsNonUnicastIntIP(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "127.0.0.1", "224.0.0.1", "255.255.255.255", "169.254.1.1"} {
		if _, err := normalizeFirewallFwdRule(firewallFwdRule{ExtPort: 8080, IntIP: ip, IntPort: 80, Proto: "tcp"}); err == nil {
			t.Errorf("IntIP=%s 通过校验:DNAT 指向黑洞/广播地址,保存成功而转发静默失效", ip)
		}
	}
	if _, err := normalizeFirewallFwdRule(firewallFwdRule{ExtPort: 8080, IntIP: "192.168.1.10", IntPort: 80, Proto: "tcp"}); err != nil {
		t.Errorf("合法内网单播地址被拒绝: %v", err)
	}
}

// ---------- R7-B25: mac_bind_set 状态持久化失败如实报错 ----------

func TestMacBindSetReportsStatePersistenceFailure(t *testing.T) {
	ts, client := e2eTestServer(t)
	dir := t.TempDir()
	old := runtimeMacBindStateFile
	runtimeMacBindStateFile = filepath.Join(dir, "mac_bind.conf")
	if err := os.WriteFile(runtimeMacBindStateFile, []byte("{corrupt"), 0600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	mockMacBindMu.Lock()
	oldEntries := append([]macBindLive(nil), mockMacBindEntries...)
	mockMacBindMu.Unlock()
	t.Cleanup(func() {
		runtimeMacBindStateFile = old
		mockMacBindMu.Lock()
		mockMacBindEntries = oldEntries
		mockMacBindMu.Unlock()
	})

	resp := e2eWebSocketCall(t, ts, client, "mbstate", "POST", "/api/network_config_data",
		"action=mac_bind_set&mac=AA:BB:CC:DD:EE:01&ip=192.168.225.77")
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("body 不是 JSON: %v (%v)", err, resp["body"])
	}
	// 旧实现读/写状态失败仅记日志仍返回 ok:true:该绑定不受开机补齐保护,
	// 厂商状态重启丢失后静默消失,而用户看到的是"保存成功"。
	if data["ok"] != false {
		t.Fatalf("状态持久化失败仍报成功: %v", resp["body"])
	}
	if !strings.Contains(fmt.Sprint(data["error"]), "状态持久化失败") {
		t.Fatalf("error = %v, want 含 状态持久化失败", data["error"])
	}
}

// ---------- R7-B26: 失效代数丢弃在途旧读结果 ----------

func TestInvalidateGenerationDiscardsInFlightStaleRead(t *testing.T) {
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 4)}
	old := executeCachedATCommand
	executeCachedATCommand = func(command string, mockMode bool) (string, error) {
		if command == "AT+CSQ" {
			// 模拟:读命令的设备读取已完成、提交尚未发生时,动作命令完成并
			// 失效读缓存(溢出协程与 worker 并发时该时序真实存在)。
			mgr.invalidateReadCache()
			return "AT+CSQ\r\n+CSQ: 20,99\r\nOK", nil
		}
		return command + "\r\nOK", nil
	}
	t.Cleanup(func() { executeCachedATCommand = old })

	mgr.enqueue("AT+CSQ", false)
	mgr.run("AT+CSQ")

	mgr.mu.Lock()
	entry := mgr.entries["AT+CSQ"]
	response, updatedAt, running := entry.response, entry.updatedAt, entry.running
	mgr.mu.Unlock()
	if response != "" || !updatedAt.IsZero() {
		t.Fatalf("动作前的旧读数据被按新鲜提交: response=%q updatedAt=%s", response, updatedAt)
	}
	if running {
		t.Fatalf("running 未复位,后续请求将永久挂起")
	}
}

// ---------- R7-B27: 证书必备 IP 只含稳定地址 ----------

func TestRequiredTLSCertificateIPsOnlyStableAddresses(t *testing.T) {
	for _, ip := range requiredTLSCertificateIPs() {
		if ip.IsLoopback() {
			continue
		}
		if ip.To4() == nil || !ip.IsPrivate() {
			t.Fatalf("动态/公网地址 %s 被纳入证书必备判据:WAN 拨号地址变化会触发非确定性重签", ip)
		}
	}
}
