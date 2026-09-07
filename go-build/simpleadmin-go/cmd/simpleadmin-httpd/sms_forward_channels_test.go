package main

// 短信转发通道测试:Webhook 扩展参数(method/headers/timeoutSec/template)的
// 校验/组装/投递,与 Server酱 通道(sct.ftqq.com 公开 API)的端点推导、标题
// 清洗、载荷生成、code!=0 失败判定、重试节奏,以及双通道统一分发/轮询器
// 启停/测试推送接口。网络一律经 httptest 或注入记录器,不产生真实外联。

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Server酱 投递记录器(与 installSMSWebhookRecorder 同模式) ---

type serverChanPostRecord struct {
	url  string
	body []byte
}

func installServerChanRecorder(t *testing.T) <-chan serverChanPostRecord {
	t.Helper()
	ch := make(chan serverChanPostRecord, 16)
	serverChanPost = func(apiURL string, body []byte) error {
		ch <- serverChanPostRecord{url: apiURL, body: body}
		return nil
	}
	return ch
}

func waitForServerChanPosts(t *testing.T, ch <-chan serverChanPostRecord, count int) []serverChanPostRecord {
	t.Helper()
	records := []serverChanPostRecord{}
	timeout := time.After(2 * time.Second)
	for len(records) < count {
		select {
		case rec := <-ch:
			records = append(records, rec)
		case <-timeout:
			t.Fatalf("等待 %d 条 Server酱 推送超时，实际收到 %d 条", count, len(records))
		}
	}
	return records
}

func expectNoServerChanPosts(t *testing.T, ch <-chan serverChanPostRecord) {
	t.Helper()
	select {
	case rec := <-ch:
		t.Fatalf("预期没有推送，却收到: %s", rec.body)
	case <-time.After(100 * time.Millisecond):
	}
}

// --- Webhook 扩展参数:配置校验与持久化 ---

func TestSMSWebhookConfigExtendedFields(t *testing.T) {
	setupSMSWebhookTest(t)

	cfg := smsWebhookConfig{
		Enabled:    true,
		URL:        "http://example.com/hook",
		Method:     "put",
		Headers:    "Authorization: Bearer tok\nX-Tag: cpe",
		TimeoutSec: 25,
		Template:   `{"msg":"{text}","idx":{index}}`,
	}
	if err := writeSMSWebhookConfig(cfg); err != nil {
		t.Fatalf("写入扩展配置失败: %v", err)
	}
	got, err := readSMSWebhookConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if got.Method != "PUT" || got.TimeoutSec != 25 || got.Template != cfg.Template {
		t.Fatalf("扩展字段读回不一致: %+v", got)
	}
	if got.Headers != cfg.Headers {
		t.Fatalf("headers 读回不一致: %q", got.Headers)
	}

	status := currentSMSWebhookStatus()
	if status["method"] != "PUT" || status["timeoutSec"] != 25 || status["template"] != cfg.Template {
		t.Fatalf("状态接口应返回归一化扩展字段: %v", status)
	}

	// 缺省配置:method 归一化 POST、timeout 回落 10。
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入缺省配置失败: %v", err)
	}
	status = currentSMSWebhookStatus()
	if status["method"] != "POST" || status["timeoutSec"] != smsWebhookDefaultTimeoutSec {
		t.Fatalf("缺省应为 POST/10s: %v", status)
	}
}

func TestSMSWebhookConfigRejectsInvalidExtendedFields(t *testing.T) {
	setupSMSWebhookTest(t)

	cases := []struct {
		name string
		cfg  smsWebhookConfig
	}{
		{"非法 method", smsWebhookConfig{URL: "http://a/b", Method: "DELETE"}},
		{"timeout 超上限", smsWebhookConfig{URL: "http://a/b", TimeoutSec: 121}},
		{"timeout 为负", smsWebhookConfig{URL: "http://a/b", TimeoutSec: -1}},
		{"请求头缺冒号", smsWebhookConfig{URL: "http://a/b", Headers: "badline"}},
		{"请求头名为空", smsWebhookConfig{URL: "http://a/b", Headers: ": value"}},
		{"请求头名含空格", smsWebhookConfig{URL: "http://a/b", Headers: "Bad Name: value"}},
		{"模板非 JSON", smsWebhookConfig{URL: "http://a/b", Template: "{not json}"}},
	}
	for _, tc := range cases {
		if err := writeSMSWebhookConfig(tc.cfg); err == nil {
			t.Fatalf("%s 应被拒绝", tc.name)
		}
	}
}

// --- Webhook 载荷模板渲染 ---

func TestRenderSMSWebhookBody(t *testing.T) {
	payload := map[string]any{
		"sender":  "+8613800000000",
		"date":    "26/09/01,10:00:00+32",
		"text":    "带\"引号\"和\n换行的内容",
		"index":   7,
		"storage": "ME",
	}

	// 空模板 = 内置缺省五字段 JSON。
	defaultBody := renderSMSWebhookBody("", payload)
	var parsed map[string]any
	if err := json.Unmarshal(defaultBody, &parsed); err != nil {
		t.Fatalf("缺省载荷不是合法 JSON: %v", err)
	}
	if parsed["sender"] != "+8613800000000" || int(parsed["index"].(float64)) != 7 || parsed["storage"] != "ME" {
		t.Fatalf("缺省载荷字段不符: %s", defaultBody)
	}

	// 模板占位符:字符串值 JSON 转义(引号/换行不破坏结构),{index} 为裸数字。
	tmpl := `{"from":"{sender}","msg":"{text}","idx":{index},"st":"{storage}","at":"{date}"}`
	rendered := renderSMSWebhookBody(tmpl, payload)
	if !json.Valid(rendered) {
		t.Fatalf("模板渲染结果不是合法 JSON: %s", rendered)
	}
	var got map[string]any
	if err := json.Unmarshal(rendered, &got); err != nil {
		t.Fatalf("解析渲染结果失败: %v", err)
	}
	if got["msg"] != payload["text"] || got["from"] != payload["sender"] || got["at"] != payload["date"] || got["st"] != "ME" {
		t.Fatalf("模板字段替换不符: %s", rendered)
	}
	if int(got["idx"].(float64)) != 7 {
		t.Fatalf("index 占位符应为数字 7: %s", rendered)
	}
}

func TestValidateSMSWebhookTemplateRejectsBrokenJSON(t *testing.T) {
	if err := validateSMSWebhookTemplate(`{"a":"{text}"`); err == nil {
		t.Fatal("缺右括号应被拒绝")
	}
	// 样例文本含引号/反斜杠/换行:未走转义的实现会在此暴露。
	if err := validateSMSWebhookTemplate(`{"a":"{text}","b":{index}}`); err != nil {
		t.Fatalf("合法模板被拒绝: %v", err)
	}
	if err := validateSMSWebhookTemplate("   "); err != nil {
		t.Fatalf("空模板应放行(回落缺省载荷): %v", err)
	}
}

// --- Webhook 请求组装 ---

func TestBuildSMSWebhookRequestGETUsesQuery(t *testing.T) {
	cfg := smsWebhookConfig{URL: "http://example.com/hook?tag=cpe", Method: "GET", Template: `{"x":"{text}"}`}
	payload := map[string]any{"sender": "+861", "date": "d", "text": "hello world", "index": 3, "storage": "SM"}
	rq, err := buildSMSWebhookRequest(cfg, payload)
	if err != nil {
		t.Fatalf("组装 GET 请求失败: %v", err)
	}
	if rq.Method != "GET" || len(rq.Body) != 0 {
		t.Fatalf("GET 不应带请求体: %+v", rq)
	}
	parsed, err := url.Parse(rq.URL)
	if err != nil {
		t.Fatalf("GET URL 不合法: %v", err)
	}
	query := parsed.Query()
	if query.Get("tag") != "cpe" {
		t.Fatalf("原有查询参数应保留: %s", rq.URL)
	}
	if query.Get("text") != "hello world" || query.Get("sender") != "+861" || query.Get("index") != "3" || query.Get("storage") != "SM" {
		t.Fatalf("查询参数不完整: %s", rq.URL)
	}
}

func TestBuildSMSWebhookRequestPOSTHeadersAndTimeout(t *testing.T) {
	cfg := smsWebhookConfig{
		URL:        "http://example.com/hook",
		Method:     "PUT",
		Headers:    "Authorization: Bearer tok\n\nX-Tag: cpe\n",
		TimeoutSec: 25,
	}
	payload := map[string]any{"sender": "s", "date": "d", "text": "t", "index": 1, "storage": "ME"}
	rq, err := buildSMSWebhookRequest(cfg, payload)
	if err != nil {
		t.Fatalf("组装请求失败: %v", err)
	}
	if rq.Method != "PUT" || rq.Timeout != 25*time.Second {
		t.Fatalf("method/timeout 不符: %+v", rq)
	}
	if rq.Headers["Authorization"] != "Bearer tok" || rq.Headers["X-Tag"] != "cpe" || len(rq.Headers) != 2 {
		t.Fatalf("请求头解析不符: %v", rq.Headers)
	}
	if !json.Valid(rq.Body) {
		t.Fatalf("请求体应为缺省 JSON 载荷: %s", rq.Body)
	}
}

func TestDefaultSMSWebhookPostSendsMethodHeadersBody(t *testing.T) {
	var gotMethod, gotAuth, gotContentType string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	rq := smsWebhookRequest{
		Method:  "PUT",
		URL:     ts.URL,
		Headers: map[string]string{"Authorization": "Bearer tok"},
		Body:    []byte(`{"idx":1}`),
		Timeout: 5 * time.Second,
	}
	if err := defaultSMSWebhookPost(rq); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if gotMethod != "PUT" || gotAuth != "Bearer tok" || gotContentType != "application/json" || string(gotBody) != `{"idx":1}` {
		t.Fatalf("服务端收到 method=%s auth=%q ct=%q body=%s", gotMethod, gotAuth, gotContentType, gotBody)
	}

	// 用户显式 Content-Type 覆盖缺省值。
	rq.Headers["Content-Type"] = "text/plain"
	if err := defaultSMSWebhookPost(rq); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if gotContentType != "text/plain" {
		t.Fatalf("显式 Content-Type 应生效,实际: %q", gotContentType)
	}
}

// --- Server酱 端点推导与消息构造 ---

func TestServerChanSendURL(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"SCT123456Tabcdef", "https://sctapi.ftqq.com/SCT123456Tabcdef.send"},
		{"sctp123tXXXXX", "https://123.push.ft07.com/send/sctp123tXXXXX.send"},
	}
	for _, tc := range cases {
		got, err := serverChanSendURL(tc.key)
		if err != nil {
			t.Fatalf("%s 推导失败: %v", tc.key, err)
		}
		if got != tc.want {
			t.Fatalf("%s → %s, want %s", tc.key, got, tc.want)
		}
	}

	bad := []string{"", "   ", "sctpABtXXX", "sctp123", "has space", "SCT/evil", "SCT?a=b", "SCT\n123"}
	for _, key := range bad {
		if _, err := serverChanSendURL(key); err == nil {
			t.Fatalf("非法 SendKey %q 应被拒绝", key)
		}
	}
}

func TestSanitizeServerChanTitle(t *testing.T) {
	if got := sanitizeServerChanTitle("  标题\r\n换行\t "); got != "标题 换行" {
		t.Fatalf("换行应替换为空格并 trim,实际: %q", got)
	}
	long := strings.Repeat("长", 40)
	if got := sanitizeServerChanTitle(long); len([]rune(got)) != 32 {
		t.Fatalf("应按 rune 截断到 32,实际 %d", len([]rune(got)))
	}
}

func TestBuildServerChanSMSMessage(t *testing.T) {
	title, desp := buildServerChanSMSMessage(map[string]any{
		"sender": "+8613800000000", "date": "26/09/01,10:00:00+32",
		"text": "验证码 1234", "index": 3, "storage": "ME",
	})
	if title != "新短信：+8613800000000" {
		t.Fatalf("标题不符: %q", title)
	}
	for _, want := range []string{"**发件人**：+8613800000000", "**时间**：26/09/01,10:00:00+32", "**内容**：验证码 1234", "存储 ME · 索引 3"} {
		if !strings.Contains(desp, want) {
			t.Fatalf("正文缺少 %q: %s", want, desp)
		}
	}
	if strings.Contains(desp, "\n\n\n") {
		t.Fatalf("Markdown 段落应为单空行分隔: %q", desp)
	}

	// 发件人/内容缺省的兜底文案。
	title, desp = buildServerChanSMSMessage(map[string]any{"index": 1, "storage": "SM"})
	if title != "新短信：未知发件人" || !strings.Contains(desp, "（无内容）") {
		t.Fatalf("兜底不符: title=%q desp=%s", title, desp)
	}
}

// --- Server酱 配置持久化 ---

func TestSMSServerChanConfigReadWrite(t *testing.T) {
	setupSMSWebhookTest(t)

	cfg, err := readSMSServerChanConfig()
	if err != nil {
		t.Fatalf("读取缺省配置失败: %v", err)
	}
	if cfg.Enabled || cfg.SendKey != "" {
		t.Fatalf("缺省配置应为关闭且空 SendKey: %+v", cfg)
	}

	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: " SCT123Tabc "}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	got, err := readSMSServerChanConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !got.Enabled || got.SendKey != "SCT123Tabc" {
		t.Fatalf("配置读回不一致(SendKey 应 trim): %+v", got)
	}

	info, err := os.Stat(filepath.Join(filepath.Dir(runtimeTTLValueFile), "sms_serverchan.conf"))
	if err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("配置文件权限应为 0600,实际: %v", info.Mode().Perm())
	}

	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "bad key with space"}); err == nil {
		t.Fatal("非法 SendKey 应被拒绝")
	}
}

// --- Server酱 应答判定与重试 ---

func TestDefaultServerChanPostResponseCodes(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"成功", http.StatusOK, `{"code":0,"message":"","data":{"pushid":"1"}}`, ""},
		{"额度用尽", http.StatusOK, `{"code":43001,"message":"额度用完"}`, "code=43001"},
		{"错误信息透传", http.StatusOK, `{"code":40001,"message":"错误的sendkey"}`, "错误的sendkey"},
		{"非 JSON 应答", http.StatusOK, `gateway said ok`, "无法解析"},
		{"HTTP 500", http.StatusInternalServerError, `boom`, "HTTP 500"},
	}
	for _, tc := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		err := defaultServerChanPost(ts.URL, []byte(`{"title":"t"}`))
		ts.Close()
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: 不应失败: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: 错误应含 %q,实际: %v", tc.name, tc.wantErr, err)
		}
	}
}

func TestDefaultServerChanPostErrorHidesSendKey(t *testing.T) {
	key := "SCTSECRETKEY123"
	apiURL, err := serverChanSendURL(key)
	if err != nil {
		t.Fatalf("推导端点失败: %v", err)
	}
	// 127.0.0.1:1 立即拒连;错误文本不得携带含 SendKey 的完整 URL。
	err = defaultServerChanPost(strings.Replace(apiURL, "https://sctapi.ftqq.com/", "http://127.0.0.1:1/", 1), []byte(`{"title":"t"}`))
	if err == nil {
		t.Fatal("应返回连接错误")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("错误文本泄露 SendKey: %v", err)
	}
}

func TestDeliverSMSServerChanRetries(t *testing.T) {
	setupSMSWebhookTest(t)
	withShortSMSWebhookRetryDelays(t)

	attempts := 0
	serverChanPost = func(apiURL string, body []byte) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}
	if err := deliverSMSServerChan("SCT123Tabc", "标题", "正文"); err != nil {
		t.Fatalf("第三次成功不应返回错误: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("尝试次数 = %d, want 3", attempts)
	}

	attempts = 0
	serverChanPost = func(apiURL string, body []byte) error {
		attempts++
		return errors.New("temporary failure")
	}
	err := deliverSMSServerChan("SCT123Tabc", "标题", "正文")
	if err == nil || attempts != 3 || !strings.Contains(err.Error(), "3") {
		t.Fatalf("全部失败应含尝试次数: err=%v attempts=%d", err, attempts)
	}

	// 非法 key 不进入重试循环。
	attempts = 0
	if err := deliverSMSServerChan("bad key", "t", "d"); err == nil {
		t.Fatal("非法 SendKey 应返回错误")
	}
	if attempts != 0 {
		t.Fatalf("非法 SendKey 不应发起请求, attempts=%d", attempts)
	}
}

// --- 双通道统一分发与轮询器 ---

func TestNotifyNewSMSDispatchesBothChannels(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入 webhook 配置失败: %v", err)
	}
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入 serverchan 配置失败: %v", err)
	}
	webhookCh := installSMSWebhookRecorder(t)
	serverChanCh := installServerChanRecorder(t)

	// 首扫建基线,两通道都不推送。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/09/01,10:00:00+32", "存量", 1),
	}, true)
	expectNoSMSWebhookPosts(t, webhookCh)
	expectNoServerChanPosts(t, serverChanCh)

	// 新到达:两通道各收到一次,内容一致。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/09/01,10:00:00+32", "存量", 1),
		smsWebhookTestMessage("+8613800000002", "26/09/01,10:01:00+32", "验证码 8888", 2),
	}, true)

	records := waitForSMSWebhookPosts(t, webhookCh, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("webhook 载荷不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 2 || payload["text"] != "验证码 8888" {
		t.Fatalf("webhook 载荷不符: %s", records[0].body)
	}

	scRecords := waitForServerChanPosts(t, serverChanCh, 1)
	if scRecords[0].url != "https://sctapi.ftqq.com/SCT123Tabc.send" {
		t.Fatalf("Server酱 端点不符: %s", scRecords[0].url)
	}
	var scBody map[string]string
	if err := json.Unmarshal(scRecords[0].body, &scBody); err != nil {
		t.Fatalf("Server酱 载荷不是合法 JSON: %v", err)
	}
	if scBody["title"] != "新短信：+8613800000002" || !strings.Contains(scBody["desp"], "验证码 8888") {
		t.Fatalf("Server酱 载荷不符: %s", scRecords[0].body)
	}
	expectNoSMSWebhookPosts(t, webhookCh)
	expectNoServerChanPosts(t, serverChanCh)
}

func TestNotifyNewSMSServerChanOnly(t *testing.T) {
	setupSMSWebhookTest(t)
	// webhook 未配置,仅 Server酱 启用:高水位照常推进,只推 Server酱。
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	webhookCh := installSMSWebhookRecorder(t)
	serverChanCh := installServerChanRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/09/01,10:00:00+32", "存量", 1),
	}, true)
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/09/01,10:00:00+32", "存量", 1),
		smsWebhookTestMessage("+8613800000002", "26/09/01,10:01:00+32", "新到达", 2),
	}, true)

	waitForServerChanPosts(t, serverChanCh, 1)
	expectNoSMSWebhookPosts(t, webhookCh)
}

func TestNotifyNewSMSBothChannelsDisabled(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: false, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	webhookCh := installSMSWebhookRecorder(t)
	serverChanCh := installServerChanRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/09/01,10:00:00+32", "msg", 1),
	}, true)
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000002", "26/09/01,10:01:00+32", "msg2", 2),
	}, true)
	expectNoSMSWebhookPosts(t, webhookCh)
	expectNoServerChanPosts(t, serverChanCh)

	// 全通道关闭时不推进水位:重新启用后存量不误推、增量正常。
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != -1 {
		t.Fatalf("关闭状态不应推进水位: %v", status)
	}
}

func TestSyncSMSWebhookPollerServerChanChannel(t *testing.T) {
	setupSMSWebhookTest(t)
	oldWait := smsWebhookPollWait
	smsWebhookPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		return false
	}
	t.Cleanup(func() { smsWebhookPollWait = oldWait })

	// 仅 Server酱 启用也应启动轮询器。
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if !smsWebhookPollerRunning() {
		t.Fatal("Server酱 启用时应启动轮询器")
	}

	// 关闭 Server酱 且 webhook 未启用 → 停止。
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: false, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("全部通道关闭时应停止轮询器")
	}

	// SendKey 清空的启用态视为未配置,不启动。
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: ""}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("SendKey 为空不应启动轮询器")
	}
}

func TestSMSWebhookPollTickServerChanOnly(t *testing.T) {
	setupSMSWebhookTest(t)
	calls := withSMSWebhookPollFetch(t, smsWebhookPollRawBase)
	serverChanCh := installServerChanRecorder(t)

	// Server酱 单通道启用:轮询照常拉取并建基线。
	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	smsWebhookPollTick(nil)
	if *calls != 1 {
		t.Fatalf("Server酱 启用时应拉取短信列表, calls = %d", *calls)
	}
	expectNoServerChanPosts(t, serverChanCh)
}

// --- 测试推送接口 ---

func newTestSMSForwardRequest(body string) *http.Request {
	req := httptest.NewRequest("POST", "/api/test_sms_forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandleTestSMSForwardWebhook(t *testing.T) {
	setupSMSWebhookTest(t)
	s := &simpleAdminServer{}

	// 未配置地址:业务失败以 200 + ok:false 返回。
	rec := httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=webhook"))
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if rec.Code != http.StatusOK || result["ok"] != false || !strings.Contains(result["error"].(string), "Webhook 地址") {
		t.Fatalf("未配置时应提示填写地址: %d %v", rec.Code, result)
	}

	// 已配置:单次投递(不进入重试),样例载荷五字段齐全。
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: false, URL: "http://example.com/hook", Method: "GET"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)
	rec = httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=webhook"))
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("测试推送应成功: %v", result)
	}
	records := waitForSMSWebhookPosts(t, ch, 1)
	if records[0].method != "GET" || !strings.Contains(records[0].url, "sender=%2B8613800000000") {
		t.Fatalf("测试推送未按配置组装: %+v", records[0])
	}
	expectNoSMSWebhookPosts(t, ch)
}

func TestHandleTestSMSForwardServerChan(t *testing.T) {
	setupSMSWebhookTest(t)
	s := &simpleAdminServer{}

	rec := httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=serverchan"))
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if result["ok"] != false || !strings.Contains(result["error"].(string), "SendKey") {
		t.Fatalf("未配置时应提示填写 SendKey: %v", result)
	}

	if err := writeSMSServerChanConfig(smsServerChanConfig{Enabled: true, SendKey: "SCT123Tabc"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installServerChanRecorder(t)
	rec = httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=serverchan"))
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("测试推送应成功: %v", result)
	}
	records := waitForServerChanPosts(t, ch, 1)
	if records[0].url != "https://sctapi.ftqq.com/SCT123Tabc.send" {
		t.Fatalf("测试推送端点不符: %s", records[0].url)
	}
	var body map[string]string
	if err := json.Unmarshal(records[0].body, &body); err != nil {
		t.Fatalf("测试推送载荷不是 JSON: %v", err)
	}
	if body["title"] != "SimpleAdmin 测试推送" || !strings.Contains(body["desp"], "测试消息") {
		t.Fatalf("测试推送载荷不符: %s", records[0].body)
	}

	// 投递失败(如 code!=0):错误文本透传给页面,便于展示 Server酱 原因。
	serverChanPost = func(apiURL string, body []byte) error {
		return errors.New("Server酱 返回 code=43001: 额度用完")
	}
	rec = httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=serverchan"))
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if result["ok"] != false || !strings.Contains(result["error"].(string), "额度用完") {
		t.Fatalf("投递失败错误应透传: %v", result)
	}
}

func TestHandleTestSMSForwardUnknownChannel(t *testing.T) {
	setupSMSWebhookTest(t)
	s := &simpleAdminServer{}
	rec := httptest.NewRecorder()
	s.handleTestSMSForward(rec, newTestSMSForwardRequest("channel=email"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知通道应返回 400,实际: %d", rec.Code)
	}
}

// --- set 接口扩展参数 ---

func TestHandleSetSMSWebhookExtendedParams(t *testing.T) {
	setupSMSWebhookTest(t)
	s := &simpleAdminServer{}

	setReq := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/set_sms_webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.handleSetSMSWebhook(rec, req)
		return rec
	}

	rec := setReq("enabled=1&url=http%3A%2F%2Fexample.com%2Fhook&method=put&headers=Authorization%3A%20Bearer%20tok&timeoutSec=30&template=%7B%22m%22%3A%22%7Btext%7D%22%7D")
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应成功: %d %s", rec.Code, rec.Body.String())
	}
	cfg, err := readSMSWebhookConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !cfg.Enabled || cfg.Method != "PUT" || cfg.TimeoutSec != 30 || cfg.Headers != "Authorization: Bearer tok" || cfg.Template != `{"m":"{text}"}` {
		t.Fatalf("扩展参数未落盘: %+v", cfg)
	}

	// 空值清除:headers/template/timeoutSec 提交空串回落缺省。
	rec = setReq("headers=&template=&timeoutSec=&method=post")
	if rec.Code != http.StatusOK {
		t.Fatalf("清除应成功: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ = readSMSWebhookConfig()
	if cfg.Headers != "" || cfg.Template != "" || cfg.TimeoutSec != 0 {
		t.Fatalf("空值应清除扩展字段: %+v", cfg)
	}

	// 非法参数 400。
	if rec := setReq("method=DELETE"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 method 应 400: %d", rec.Code)
	}
	if rec := setReq("timeoutSec=abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非整数 timeoutSec 应 400: %d", rec.Code)
	}
	if rec := setReq("template=%7Bbad%7D"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法模板应 400: %d", rec.Code)
	}
}

func TestHandleSetSMSServerChan(t *testing.T) {
	setupSMSWebhookTest(t)
	s := &simpleAdminServer{}

	setReq := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/set_sms_serverchan", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.handleSetSMSServerChan(rec, req)
		return rec
	}

	if rec := setReq("enabled=1&sendKey=SCT123Tabc"); rec.Code != http.StatusOK {
		t.Fatalf("保存应成功: %d %s", rec.Code, rec.Body.String())
	}
	cfg, _ := readSMSServerChanConfig()
	if !cfg.Enabled || cfg.SendKey != "SCT123Tabc" {
		t.Fatalf("配置未落盘: %+v", cfg)
	}

	// 非法 key 400;空值清除。
	if rec := setReq("sendKey=bad%20key"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 SendKey 应 400: %d", rec.Code)
	}
	if rec := setReq("enabled=0&sendKey="); rec.Code != http.StatusOK {
		t.Fatalf("清除应成功: %d", rec.Code)
	}
	cfg, _ = readSMSServerChanConfig()
	if cfg.Enabled || cfg.SendKey != "" {
		t.Fatalf("空值应清除: %+v", cfg)
	}
}
