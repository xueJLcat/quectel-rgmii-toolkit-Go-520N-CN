package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Webhook 高水位回绕 ---

// TestSMSWebhookHighWaterResetsOnWraparound 高水位 50、当前列表最大索引 3：
// 判定存储被清空/索引复用，高水位重置为 最大索引-1=2，索引 3 的消息
// 仍补发一次通知（新语义：删除后新消息复用旧索引时宁可重复一次，不可漏报；
// 旧语义为直接重置到最大索引且不补发，会吞掉复用索引的真正新消息）；
// 此后索引 4 的新消息正常触发通知。
func TestSMSWebhookHighWaterResetsOnWraparound(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	smsWebhookInitialized = map[string]bool{"ME": true}
	smsWebhookHighWater = map[string]int{"ME": 50}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/30,09:00:00+32", "删除后残留", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/30,09:01:00+32", "删除后残留", 3),
	}, true)
	// 回绕时仅当前最大索引补发一次，更低的旧索引（1）不补发。
	records := waitForSMSWebhookPosts(t, ch, 1)
	var firstPayload map[string]any
	if err := json.Unmarshal(records[0].body, &firstPayload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(firstPayload["index"].(float64)) != 3 {
		t.Fatalf("回绕时应对当前最大索引 3 补发一次，实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookHighWater["ME"] != 3 {
		t.Fatalf("高水位应落在当前最大索引 3，实际: %d", smsWebhookHighWater["ME"])
	}
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != 3 {
		t.Fatalf("lastNotifiedIndex = %v, want 3", status["lastNotifiedIndex"])
	}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/30,09:00:00+32", "删除后残留", 1),
		smsWebhookTestMessage("+8613800000002", "26/08/30,09:01:00+32", "删除后残留", 3),
		smsWebhookTestMessage("+8613800000003", "26/08/30,09:02:00+32", "新短信", 4),
	}, true)
	records = waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 4 {
		t.Fatalf("应只通知索引 4 的新消息，实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}

// TestSMSWebhookHighWaterKeptOnEmptyList 列表为空时保持高水位不变。
func TestSMSWebhookHighWaterKeptOnEmptyList(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	smsWebhookInitialized = map[string]bool{"ME": true}
	smsWebhookHighWater = map[string]int{"ME": 50}

	notifyNewSMSByWebhook(nil, true)
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookHighWater["ME"] != 50 {
		t.Fatalf("空列表应保持高水位 50，实际: %d", smsWebhookHighWater["ME"])
	}
}

// --- Webhook 投递重试 ---

func withShortSMSWebhookRetryDelays(t *testing.T) {
	t.Helper()
	old := smsWebhookRetryDelays
	smsWebhookRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { smsWebhookRetryDelays = old })
}

// TestSMSWebhookDeliveryGivesUpAfterRetries 持续失败时共尝试 3 次（1+2 重试）后放弃。
func TestSMSWebhookDeliveryGivesUpAfterRetries(t *testing.T) {
	setupSMSWebhookTest(t)
	withShortSMSWebhookRetryDelays(t)

	attempts := 0
	smsWebhookPost = func(rq smsWebhookRequest) error {
		attempts++
		return errors.New("connection refused")
	}

	err := deliverSMSWebhook(smsWebhookConfig{URL: "http://example.com/hook"}, map[string]any{"index": 1})
	if err == nil {
		t.Fatal("全部失败时应返回错误")
	}
	if attempts != 3 {
		t.Fatalf("尝试次数 = %d, want 3（首次 + 2 次重试）", attempts)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("错误信息应包含尝试次数，实际: %v", err)
	}
}

// TestSMSWebhookDeliverySucceedsOnRetry 第二次尝试成功则不再重试。
func TestSMSWebhookDeliverySucceedsOnRetry(t *testing.T) {
	setupSMSWebhookTest(t)
	withShortSMSWebhookRetryDelays(t)

	attempts := 0
	smsWebhookPost = func(rq smsWebhookRequest) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary failure")
		}
		return nil
	}

	if err := deliverSMSWebhook(smsWebhookConfig{URL: "http://example.com/hook"}, map[string]any{"index": 1}); err != nil {
		t.Fatalf("重试后成功不应返回错误: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("尝试次数 = %d, want 2", attempts)
	}
}

// TestSMSWebhookDeliveryRetriesOverHTTP 用本地 HTTP 测试服务器验证
// 非 2xx 视为失败并触发重试，第三次请求成功后放弃前共尝试 3 次。
func TestSMSWebhookDeliveryRetriesOverHTTP(t *testing.T) {
	setupSMSWebhookTest(t)
	withShortSMSWebhookRetryDelays(t)

	var received int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&received, 1) < 3 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	if err := deliverSMSWebhook(smsWebhookConfig{URL: ts.URL}, map[string]any{"index": 1}); err != nil {
		t.Fatalf("第三次请求成功，不应返回错误: %v", err)
	}
	if got := atomic.LoadInt32(&received); got != 3 {
		t.Fatalf("服务器收到 %d 次请求, want 3", got)
	}
}

// --- 定时重启补跑与持久化 ---

// TestSchedulerCatchUpAfterMissedWindow 已过设定时刻且今天未执行 → 补跑触发
// 并写入持久化状态文件；同日再次轮询不再触发。
func TestSchedulerCatchUpAfterMissedWindow(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	// 设定时刻前的一次轮询，留下"进程在运行"的检查记录。
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 3, 59, 30, 0, time.Local)) {
		t.Fatalf("未到设定时刻不应触发")
	}

	// 错过 04:00 窗口后于 04:10 轮询：补跑触发。
	if !schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 10, 0, 0, time.Local)) {
		t.Fatalf("已过设定时刻且今天未执行，应补跑触发")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}

	if _, err := os.Stat(schedulerStatePath()); err != nil {
		t.Fatalf("状态文件未写入: %v", err)
	}
	if st := readSchedulerState(); st.LastRunDate != "2026-01-02" {
		t.Fatalf("状态文件 lastRunDate = %q, want 2026-01-02", st.LastRunDate)
	}

	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 10, 30, 0, time.Local)) {
		t.Fatalf("同日再次轮询不应重复触发")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}
}

// TestSchedulerPersistedStateSurvivesRestart 状态文件存在时，跨"重启"
// （清空内存去重状态、重新轮询）当日不重复触发。
func TestSchedulerPersistedStateSurvivesRestart(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	schedulerCheckOnce(nil, time.Date(2026, 1, 2, 3, 59, 30, 0, time.Local))
	if !schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 10, 0, 0, time.Local)) {
		t.Fatalf("补跑应触发")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}

	// 模拟进程重启：内存去重状态丢失，状态文件仍在。
	schedulerMu.Lock()
	schedulerLastRunDate = ""
	schedulerMu.Unlock()

	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 11, 0, 0, time.Local)) {
		t.Fatalf("重启后凭持久化状态不应重复触发")
	}
	if *count != 1 {
		t.Fatalf("重启动作次数 = %d, want 1", *count)
	}
	if got := currentSchedulerStatus()["lastRebootDate"]; got != "2026-01-02" {
		t.Fatalf("lastRebootDate = %v, want 从状态文件恢复的 2026-01-02", got)
	}
}

// TestSchedulerNoCatchUpWithoutPriorCheck 无历史检查记录（如首次启动即已过
// 设定时刻）时不补跑，避免误重启。
func TestSchedulerNoCatchUpWithoutPriorCheck(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 5, 30, 0, 0, time.Local)) {
		t.Fatalf("缺少设定时刻前的检查记录时不应补跑")
	}
	if *count != 0 {
		t.Fatalf("重启动作次数 = %d, want 0", *count)
	}
}

// --- 看门狗 ---

// TestWatchdogReEnableResetsFailureCount 已累积失败后禁用再启用，失败计数清零重来。
func TestWatchdogReEnableResetsFailureCount(t *testing.T) {
	_, actionCalls, online := setupWatchdogTest(t)
	*online = false
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 5, CooldownMinutes: 30})

	watchdogTick(nil)
	watchdogTick(nil)
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 2 {
		t.Fatalf("consecutiveFailures = %v, want 2", status["consecutiveFailures"])
	}

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: false, FailThreshold: 5, CooldownMinutes: 30})
	watchdogTick(nil)

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 5, CooldownMinutes: 30})
	watchdogTick(nil)

	if *actionCalls != 0 {
		t.Fatalf("action calls = %d, want 0（重新启用后计数应清零重来）", *actionCalls)
	}
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 1 {
		t.Fatalf("重新启用后 consecutiveFailures = %v, want 1（清零后新累计 1 次）", status["consecutiveFailures"])
	}
}

// TestWatchdogLoopSleepsBeforeFirstCheck 循环先等待一个周期再执行首轮探测，
// 避免启动时 WAN 未就绪即探测。
func TestWatchdogLoopSleepsBeforeFirstCheck(t *testing.T) {
	checkCalls, _, _ := setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 5, CooldownMinutes: 30})

	events := make(chan string, 4)
	oldWait := watchdogPollWait
	watchdogPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		events <- "wait"
		<-stop // 首轮等待被 stop 唤醒后退出循环，避免测试中无限轮询
		return false
	}
	t.Cleanup(func() { watchdogPollWait = oldWait })

	startWatchdogPoller(nil)
	if !watchdogPollerRunning() {
		t.Fatalf("poller not running after start")
	}
	t.Cleanup(stopWatchdogPoller)

	select {
	case ev := <-events:
		if ev != "wait" {
			t.Fatalf("首个循环事件 = %q, want wait", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("等待循环首次等待超时")
	}

	time.Sleep(50 * time.Millisecond)
	if *checkCalls != 0 {
		t.Fatalf("首轮等待前不应执行探测, checkCalls = %d", *checkCalls)
	}
}
