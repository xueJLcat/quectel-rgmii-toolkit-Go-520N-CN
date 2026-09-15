package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type smsWebhookPostRecord struct {
	url     string
	body    []byte
	method  string
	headers map[string]string
}

func setupSMSWebhookTest(t *testing.T) {
	t.Helper()
	oldTTLFile := runtimeTTLValueFile
	oldPost := smsWebhookPost
	oldServerChanPost := serverChanPost
	oldHighWater := smsWebhookHighWater
	oldInitialized := smsWebhookInitialized
	oldFreedSlots := smsWebhookFreedSlots
	t.Cleanup(func() {
		stopSMSWebhookPoller()
		runtimeTTLValueFile = oldTTLFile
		smsWebhookPost = oldPost
		serverChanPost = oldServerChanPost
		smsWebhookHighWater = oldHighWater
		smsWebhookInitialized = oldInitialized
		smsWebhookFreedSlots = oldFreedSlots
	})
	runtimeTTLValueFile = filepath.Join(t.TempDir(), "ttlvalue")
	smsWebhookPost = defaultSMSWebhookPost
	serverChanPost = defaultServerChanPost
	smsWebhookHighWater = map[string]int{}
	smsWebhookInitialized = map[string]bool{}
	smsWebhookFreedSlots = map[string]map[int]bool{}
}

func installSMSWebhookRecorder(t *testing.T) <-chan smsWebhookPostRecord {
	t.Helper()
	ch := make(chan smsWebhookPostRecord, 16)
	smsWebhookPost = func(rq smsWebhookRequest) error {
		ch <- smsWebhookPostRecord{url: rq.URL, body: rq.Body, method: rq.Method, headers: rq.Headers}
		return nil
	}
	return ch
}

func waitForSMSWebhookPosts(t *testing.T, ch <-chan smsWebhookPostRecord, count int) []smsWebhookPostRecord {
	t.Helper()
	records := []smsWebhookPostRecord{}
	timeout := time.After(2 * time.Second)
	for len(records) < count {
		select {
		case rec := <-ch:
			records = append(records, rec)
		case <-timeout:
			t.Fatalf("等待 %d 条 webhook 通知超时，实际收到 %d 条", count, len(records))
		}
	}
	return records
}

func expectNoSMSWebhookPosts(t *testing.T, ch <-chan smsWebhookPostRecord) {
	t.Helper()
	select {
	case rec := <-ch:
		t.Fatalf("预期没有通知，却收到: %s", rec.body)
	case <-time.After(100 * time.Millisecond):
	}
}

func smsWebhookTestMessage(sender, date, text string, indices ...int) map[string]any {
	return map[string]any{"sender": sender, "date": date, "text": text, "indices": indices}
}

func TestSMSWebhookConfigReadWrite(t *testing.T) {
	setupSMSWebhookTest(t)

	cfg, err := readSMSWebhookConfig()
	if err != nil {
		t.Fatalf("读取缺省配置失败: %v", err)
	}
	if cfg.Enabled || cfg.URL != "" {
		t.Fatalf("缺省配置应为关闭且空 URL，实际: %+v", cfg)
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://192.168.1.2:8080/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	got, err := readSMSWebhookConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !got.Enabled || got.URL != "http://192.168.1.2:8080/hook" {
		t.Fatalf("配置读回不一致: %+v", got)
	}

	expectedPath := filepath.Join(filepath.Dir(runtimeTTLValueFile), "sms_webhook.conf")
	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("配置文件权限应为 0600，实际: %v", info.Mode().Perm())
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "ftp://example.com"}); err == nil {
		t.Fatal("非法 URL 应被拒绝")
	}
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "example.com/hook"}); err == nil {
		t.Fatal("缺少 http/https 前缀的 URL 应被拒绝")
	}
}

func TestNotifyNewSMSFirstCallOnlyInitializes(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "hello", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "world", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	status := currentSMSWebhookStatus()
	if status["lastNotifiedIndex"] != 2 {
		t.Fatalf("首次调用后水位应为 2，实际: %v", status["lastNotifiedIndex"])
	}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "hello", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "world", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)
}

// TestNotifyNewSMSEmptyFirstScanDoesNotInitialize 首扫无效空列表(开机保护期
// 返回 pending → 解析为空, scanValid=false)不得消耗初始化:高水位保持 -1、
// initialized 不置位;待二扫拿到真实数据才建立基线——存量消息不算“新到达”、
// 不通知,此后仅增量消息触发通知。回归背景:旧实现无条件置位 initialized,
// 重启后保护期内打开短信页 → 首扫以空列表结束、基线未建立,
// 下次成功读取时全部存量消息(索引 > -1)被逐条重推。
func TestNotifyNewSMSEmptyFirstScanDoesNotInitialize(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{}, false)
	expectNoSMSWebhookPosts(t, ch)
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != -1 {
		t.Fatalf("无效空首扫后应保持未初始化(lastNotifiedIndex=-1)，实际: %v", status["lastNotifiedIndex"])
	}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "存量1", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "存量2", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != 2 {
		t.Fatalf("真实数据首扫后基线水位应为 2，实际: %v", status["lastNotifiedIndex"])
	}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "存量1", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "存量2", 2),
		smsWebhookTestMessage("+8613800000003", "26/08/29,10:02:00+32", "新到达", 3),
	}, true)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 3 {
		t.Fatalf("应只通知增量索引 3，存量不重推，实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}

// TestNotifyNewSMSValidEmptyScanInitializesBaseline 收件箱确为空（有效扫描，
// 应答含 OK 无错误）时以高水位 -1 建立基线，空收件箱设备到达的第一条短信
// 判为新到达并通知。回归背景:旧语义只在首个非空扫描建基线，空收件箱设备
// 的第一条到达短信会被基线消耗而永不通知（必漏报）。
func TestNotifyNewSMSValidEmptyScanInitializesBaseline(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{}, true)
	expectNoSMSWebhookPosts(t, ch)
	if !smsWebhookInitialized["ME"] || smsWebhookHighWater["ME"] != -1 {
		t.Fatalf("有效空扫描应建立基线: initialized=%v highWater=%d", smsWebhookInitialized["ME"], smsWebhookHighWater["ME"])
	}
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != -1 {
		t.Fatalf("基线水位应为 -1，实际: %v", status["lastNotifiedIndex"])
	}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/31,10:00:00+32", "第一条新短信", 1),
	}, true)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 1 {
		t.Fatalf("空收件箱到达的第一条短信应被通知，实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}

func TestNotifyNewSMSOnlyNewEntries(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "old1", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "old2", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "old1", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "old2", 2),
		smsWebhookTestMessage("+8613800000003", "26/08/29,10:02:00+32", "new3", 3),
		smsWebhookTestMessage("+8613800000004", "26/08/29,10:03:00+32", "new4", 4, 5),
	}, true)
	records := waitForSMSWebhookPosts(t, ch, 2)

	gotIndexes := map[int]bool{}
	for _, rec := range records {
		if rec.url != "http://example.com/hook" {
			t.Fatalf("通知 URL 不正确: %s", rec.url)
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.body, &payload); err != nil {
			t.Fatalf("通知内容不是合法 JSON: %v", err)
		}
		gotIndexes[int(payload["index"].(float64))] = true
		if payload["sender"] == "" || payload["text"] == "" {
			t.Fatalf("通知内容缺少字段: %s", rec.body)
		}
	}
	if !gotIndexes[3] || !gotIndexes[5] {
		t.Fatalf("应只通知新条目索引 3 和 5，实际: %v", gotIndexes)
	}
	expectNoSMSWebhookPosts(t, ch)
}

func TestNotifyNewSMSSkipsIndexesNotAboveWatermark(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "msg", 3),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "msg", 1, 2, 3),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	status := currentSMSWebhookStatus()
	if status["lastNotifiedIndex"] != 3 {
		t.Fatalf("水位应保持为 3，实际: %v", status["lastNotifiedIndex"])
	}
}

func TestNotifyNewSMSDisabled(t *testing.T) {
	setupSMSWebhookTest(t)
	ch := installSMSWebhookRecorder(t)

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/29,10:00:00+32", "msg", 1),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: false, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000002", "26/08/29,10:01:00+32", "msg2", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)

	status := currentSMSWebhookStatus()
	if status["enabled"] != false {
		t.Fatalf("状态应显示未启用: %v", status)
	}
}
