package main

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

// 文本模式短信列表罐头应答：基线 1 条与新增 1 条两种形态。
const smsWebhookPollRawBase = "+CMGL: 1,1,\"+8613800000001\",,\"26/08/29,10:00:00+32\"\nhello\nOK\n"

const smsWebhookPollRawWithNew = "+CMGL: 1,1,\"+8613800000001\",,\"26/08/29,10:00:00+32\"\nhello\n" +
	"+CMGL: 2,1,\"+8613800000002\",,\"26/08/29,10:01:00+32\"\nworld\nOK\n"

// withSMSWebhookPollFetch 把后台轮询数据源替换为按调用顺序返回的罐头 ME 应答
// （超出数量后重复最后一条），并统计调用次数。罐头应答按页面同源的双存储形态
// 包装：ME 有效性取应答自身的 atResponseOK，SM 固定无效（罐头数据不含 SM）。
func withSMSWebhookPollFetch(t *testing.T, responses ...string) *int32 {
	t.Helper()
	var calls int32
	old := smsWebhookPollFetchDual
	smsWebhookPollFetchDual = func(s *simpleAdminServer) (map[string]any, map[string]bool) {
		n := int(atomic.AddInt32(&calls, 1)) - 1
		raw := responses[len(responses)-1]
		if n < len(responses) {
			raw = responses[n]
		}
		return parseSMSListAT(raw, "ME"), map[string]bool{"ME": atResponseOK(raw), "SM": false}
	}
	t.Cleanup(func() { smsWebhookPollFetchDual = old })
	return &calls
}

// TestSMSWebhookPollTickSkipsWhenInactive 未启用或未配置 URL 时轮询直接跳过，
// 不产生任何 AT 流量。
func TestSMSWebhookPollTickSkipsWhenInactive(t *testing.T) {
	setupSMSWebhookTest(t)
	calls := withSMSWebhookPollFetch(t, smsWebhookPollRawBase)

	smsWebhookPollTick(nil)
	if *calls != 0 {
		t.Fatalf("无配置时不应拉取短信列表, calls = %d", *calls)
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: false, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	smsWebhookPollTick(nil)
	if *calls != 0 {
		t.Fatalf("未启用时不应拉取短信列表, calls = %d", *calls)
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: ""}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	smsWebhookPollTick(nil)
	if *calls != 0 {
		t.Fatalf("URL 为空时不应拉取短信列表, calls = %d", *calls)
	}
}

// TestSMSWebhookPollTickEstablishesBaselineThenNotifies 后台轮询独立于前端
// 建立基线并检测新到达：首轮只建水位不通知，出现更高索引的消息时投递
// 一次通知，随后同一列表不再重复。
func TestSMSWebhookPollTickEstablishesBaselineThenNotifies(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)
	calls := withSMSWebhookPollFetch(t, smsWebhookPollRawBase, smsWebhookPollRawWithNew)

	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != 1 {
		t.Fatalf("首轮应建立基线水位 1，实际: %v", status["lastNotifiedIndex"])
	}

	smsWebhookPollTick(nil)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 2 || payload["sender"] != "+8613800000002" || payload["text"] != "world" {
		t.Fatalf("通知内容不符合新消息: %s", records[0].body)
	}

	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
	if *calls != 3 {
		t.Fatalf("启用后每轮都应拉取, calls = %d, want 3", *calls)
	}
}

// TestSMSWebhookPollTickPendingKeepsBaseline 开机保护期 pending 应答解析为
// 空列表，不得消耗初始化、不得移动水位；保护期结束后新消息照常通知。
func TestSMSWebhookPollTickPendingKeepsBaseline(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)
	withSMSWebhookPollFetch(t, atCachePendingText, atCachePendingText, smsWebhookPollRawWithNew)

	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookInitialized["ME"] {
		t.Fatalf("pending 首扫不得消耗初始化: initialized=%v", smsWebhookInitialized["ME"])
	}

	smsWebhookInitialized["ME"] = true
	smsWebhookHighWater["ME"] = 1
	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookHighWater["ME"] != 1 {
		t.Fatalf("已建基线后 pending 不得移动水位, highWater = %d", smsWebhookHighWater["ME"])
	}

	smsWebhookPollTick(nil)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 2 {
		t.Fatalf("保护期结束后应通知索引 2 的新消息，实际: %s", records[0].body)
	}
}

// TestSMSWebhookPollTickValidEmptyInboxNotifiesFirstArrival 轮询读到真实空
// 收件箱（仅 OK 应答）时以高水位 -1 建立基线，首条到达的短信即被通知；
// 与 TestNotifyNewSMSValidEmptyScanInitializesBaseline 同源语义的端到端验证。
func TestSMSWebhookPollTickValidEmptyInboxNotifiesFirstArrival(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)
	withSMSWebhookPollFetch(t, "OK\n", smsWebhookPollRawBase)

	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
	if !smsWebhookInitialized["ME"] || smsWebhookHighWater["ME"] != -1 {
		t.Fatalf("真实空收件箱应建立基线: initialized=%v highWater=%d", smsWebhookInitialized["ME"], smsWebhookHighWater["ME"])
	}

	smsWebhookPollTick(nil)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 1 {
		t.Fatalf("空收件箱首条到达短信应被通知，实际: %s", records[0].body)
	}

	smsWebhookPollTick(nil)
	expectNoSMSWebhookPosts(t, ch)
}

// TestSMSWebhookPollerLoopSleepsFirst 循环先等待一个间隔再首次轮询，
// 与看门狗节奏一致，避免开机即拉取与保护期竞争。
func TestSMSWebhookPollerLoopSleepsFirst(t *testing.T) {
	setupSMSWebhookTest(t)

	events := make(chan string, 4)
	oldWait := smsWebhookPollWait
	smsWebhookPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		events <- "sleep"
		<-stop
		return false
	}
	t.Cleanup(func() { smsWebhookPollWait = oldWait })

	var fetchCalls int32
	oldFetch := smsWebhookPollFetchDual
	smsWebhookPollFetchDual = func(s *simpleAdminServer) (map[string]any, map[string]bool) {
		atomic.AddInt32(&fetchCalls, 1)
		return map[string]any{}, map[string]bool{}
	}
	t.Cleanup(func() { smsWebhookPollFetchDual = oldFetch })

	startSMSWebhookPoller(nil)

	select {
	case ev := <-events:
		if ev != "sleep" {
			t.Fatalf("首个循环事件 = %q, want sleep", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("等待循环首次等待超时")
	}

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&fetchCalls) != 0 {
		t.Fatalf("首轮等待前不应拉取短信列表, fetchCalls = %d", fetchCalls)
	}
}

// TestSyncSMSWebhookPollerFollowsConfig 轮询器按配置启停：未启用或 URL
// 为空时不启动循环；启用且 URL 非空时启动（重复同步幂等）；关闭后立即停止。
func TestSyncSMSWebhookPollerFollowsConfig(t *testing.T) {
	setupSMSWebhookTest(t)
	oldWait := smsWebhookPollWait
	smsWebhookPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		return false
	}
	t.Cleanup(func() { smsWebhookPollWait = oldWait })

	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("无配置时不应启动轮询器")
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: false, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("关闭状态不应启动轮询器")
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: ""}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("URL 为空不应启动轮询器")
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if !smsWebhookPollerRunning() {
		t.Fatal("启用且 URL 非空时应启动轮询器")
	}
	syncSMSWebhookPoller(nil)
	if !smsWebhookPollerRunning() {
		t.Fatal("重复同步应保持运行（幂等）")
	}

	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: false, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("关闭后应停止轮询器")
	}
	syncSMSWebhookPoller(nil)
	if smsWebhookPollerRunning() {
		t.Fatal("重复同步应保持停止（幂等）")
	}
}

// TestSMSWebhookPollerStopsDuringSleep 等待间隔期间收到停止信号时循环立即
// 退出，不会完成当前等待再执行一次轮询。
func TestSMSWebhookPollerStopsDuringSleep(t *testing.T) {
	setupSMSWebhookTest(t)
	exited := make(chan struct{})
	oldWait := smsWebhookPollWait
	smsWebhookPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		select {
		case <-exited:
		default:
			close(exited)
		}
		return false
	}
	t.Cleanup(func() { smsWebhookPollWait = oldWait })

	startSMSWebhookPoller(nil)
	if !smsWebhookPollerRunning() {
		t.Fatal("启动后轮询器应在运行")
	}
	stopSMSWebhookPoller()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("停止信号后循环未退出")
	}
	if smsWebhookPollerRunning() {
		t.Fatal("停止后运行状态应为否")
	}
}
