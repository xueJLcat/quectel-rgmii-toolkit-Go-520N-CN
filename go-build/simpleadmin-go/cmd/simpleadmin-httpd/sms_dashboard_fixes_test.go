// sms_dashboard_fixes_test.go 覆盖本轮修复:
// 长短信重复投递按 (concatRef, concatSeq) 去重,正文不翻倍;
// 合并元信息取 concatSeq==1(或最小有效 seq)的分片;
// 批量删除逐条执行并聚合成功/失败数;
// 号码含字母显式报错、国内号码剥离一个前导 0;
// 模块未就绪文案与真无 SIM 文案分支;
// pending 时不写入历史采样;
// 总览探测带总超时且命中短缓存不重复真实探测。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------- 重复投递分片去重 ----------

func TestMergeSMSFragmentsDedupesRepeatedDelivery(t *testing.T) {
	// 运营商重复投递:同一条长短信的两组分片同驻留(索引不同)。
	entries := []map[string]any{
		{"sender": "10086", "date": "26/08/30,10:00:00+32", "text": "第一段", "indices": []int{1}, "concatRef": "10086:7:AA", "concatTotal": 2, "concatSeq": 1},
		{"sender": "10086", "date": "26/08/30,10:00:00+32", "text": "第二段", "indices": []int{2}, "concatRef": "10086:7:AA", "concatTotal": 2, "concatSeq": 2},
		{"sender": "10086", "date": "26/08/30,10:05:00+32", "text": "第一段", "indices": []int{3}, "concatRef": "10086:7:AA", "concatTotal": 2, "concatSeq": 1},
		{"sender": "10086", "date": "26/08/30,10:05:00+32", "text": "第二段", "indices": []int{4}, "concatRef": "10086:7:AA", "concatTotal": 2, "concatSeq": 2},
	}
	messages := mergeSMSFragments(entries)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1: %#v", len(messages), messages)
	}
	text := stringValue(messages[0]["text"])
	if text != "第一段第二段" {
		t.Fatalf("text = %q, want 第一段第二段 (duplicated fragments must not double the body)", text)
	}
	if got := fmt.Sprint(messages[0]["indices"]); got != "[1 2]" {
		t.Fatalf("indices = %s, want [1 2] (first-arrived fragments kept)", got)
	}
}

// ---------- 元信息取首片(最小 seq) ----------

func TestBuildSMSFragmentGroupMetaFromFirstSeqFragment(t *testing.T) {
	// 分片乱序入库:存储顺序第一片是 seq=2,元信息必须取 seq=1 分片。
	entries := []map[string]any{
		{"sender": "10001", "date": "26/08/30,10:21:25+32", "text": "尾部", "indices": []int{5}, "concatRef": "10001:9:BB", "concatTotal": 2, "concatSeq": 2},
		{"sender": "10001", "date": "26/08/30,10:21:21+32", "text": "头部", "indices": []int{4}, "concatRef": "10001:9:BB", "concatTotal": 2, "concatSeq": 1},
	}
	messages := mergeSMSFragments(entries)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1: %#v", len(messages), messages)
	}
	if got := stringValue(messages[0]["date"]); got != "26/08/30,10:21:21+32" {
		t.Fatalf("date = %q, want seq=1 fragment date", got)
	}
	if got := stringValue(messages[0]["text"]); got != "头部尾部" {
		t.Fatalf("text = %q, want seq-ordered body 头部尾部", got)
	}
	if got := fmt.Sprint(messages[0]["indices"]); got != "[4 5]" {
		t.Fatalf("indices = %s, want [4 5]", got)
	}

	// 无 seq==1 时回退到最小有效 seq 分片。
	group := []map[string]any{
		{"sender": "10001", "date": "26/08/30,10:21:30+32", "text": "b", "indices": []int{7}, "concatRef": "10001:9:CC", "concatTotal": 2, "concatSeq": 3},
		{"sender": "10001", "date": "26/08/30,10:21:20+32", "text": "a", "indices": []int{6}, "concatRef": "10001:9:CC", "concatTotal": 2, "concatSeq": 2},
	}
	meta := smsFragmentGroupMeta(group)
	if got := stringValue(meta["date"]); got != "26/08/30,10:21:20+32" {
		t.Fatalf("fallback meta date = %q, want minimal-seq fragment", got)
	}
}

// ---------- 批量删除逐条执行 ----------

func TestRunSMSDeleteIndicesExecutesPerCommand(t *testing.T) {
	commands := []string{}
	run := func(command string) string {
		commands = append(commands, command)
		if command == `AT+CPMS="ME","ME","ME";+CMGD=2` {
			return "+CME ERROR: 10"
		}
		return command + "\r\nOK\r\n"
	}

	result := runSMSDeleteIndices([]string{"1", "2", "3"}, run, "ME")
	// 每条索引独立一条完整命令(前置 +CPMS 锁定目标存储);中间失败不中止后续删除。
	wantCommands := []string{
		`AT+CPMS="ME","ME","ME";+CMGD=1`,
		`AT+CPMS="ME","ME","ME";+CMGD=2`,
		`AT+CPMS="ME","ME","ME";+CMGD=3`,
	}
	if fmt.Sprint(commands) != fmt.Sprint(wantCommands) {
		t.Fatalf("commands = %#v, want %#v (one full command per index, no combined command)", commands, wantCommands)
	}
	if result["ok"] != false {
		t.Fatalf("ok = %v, want false for partial failure", result["ok"])
	}
	errText := stringValue(result["error"])
	if !strings.Contains(errText, "部分删除失败") || !strings.Contains(errText, "已删 2/3") {
		t.Fatalf("error = %q, want contain 部分删除失败 and 已删 2/3", errText)
	}
	if fmt.Sprint(result["deleted"]) != "2" || fmt.Sprint(result["total"]) != "3" {
		t.Fatalf("deleted/total = %v/%v, want 2/3", result["deleted"], result["total"])
	}
	if !strings.Contains(stringValue(result["response"]), "+CME ERROR: 10") {
		t.Fatalf("response = %q, want joined raw responses", result["response"])
	}

	// 全部成功:ok=true 且无 error 字段。
	commands = nil
	allOK := runSMSDeleteIndices([]string{"4", "5"}, func(command string) string { return command + "\r\nOK\r\n" }, "ME")
	if allOK["ok"] != true {
		t.Fatalf("all-success ok = %v, want true", allOK["ok"])
	}
	if _, hasErr := allOK["error"]; hasErr {
		t.Fatalf("all-success must not carry error field: %#v", allOK)
	}
	if fmt.Sprint(allOK["deleted"]) != "2" || fmt.Sprint(allOK["total"]) != "2" {
		t.Fatalf("all-success deleted/total = %v/%v, want 2/2", allOK["deleted"], allOK["total"])
	}

	// 全部失败同样聚合。
	allFail := runSMSDeleteIndices([]string{"6"}, func(command string) string { return "ERROR" }, "ME")
	if allFail["ok"] != false || !strings.Contains(stringValue(allFail["error"]), "已删 0/1") {
		t.Fatalf("all-fail = %#v, want ok=false error 已删 0/1", allFail)
	}

	// SM 存储:命令前置 +CPMS 切到 SM。
	smCommands := []string{}
	runSMSDeleteIndices([]string{"7"}, func(command string) string {
		smCommands = append(smCommands, command)
		return command + "\r\nOK\r\n"
	}, "SM")
	if fmt.Sprint(smCommands) != fmt.Sprint([]string{`AT+CPMS="SM","SM","SM";+CMGD=7`}) {
		t.Fatalf("SM commands = %#v, want SM-scoped CPMS prefix", smCommands)
	}
}

func TestMockModeDeleteIndicesEndpointReportsPerIndexResult(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "di1", "POST", "/api/sms_data", "action=delete_indices&indices=1,2")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("delete_indices status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("delete_indices body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("delete_indices ok = %v, want true in mock mode (error=%v)", data["ok"], data["error"])
	}
	if fmt.Sprint(data["deleted"]) != "2" || fmt.Sprint(data["total"]) != "2" {
		t.Fatalf("delete_indices deleted/total = %v/%v, want 2/2", data["deleted"], data["total"])
	}
}

// ---------- 号码归一化边界 ----------

func TestNormalizeSMSNumberRejectsLetters(t *testing.T) {
	cimi := "AT+CIMI\r\n460111234567890\r\nOK\r\n"
	for _, in := range []string{"+1-800-FLOWERS", "1800FLOWERS", "138abcd8000"} {
		_, err := normalizeSMSNumber(in, cimi)
		if err == nil {
			t.Fatalf("normalizeSMSNumber(%q) error = nil, want explicit letter error", in)
		}
		if !strings.Contains(err.Error(), "字母") {
			t.Fatalf("normalizeSMSNumber(%q) error = %q, want mention letters", in, err.Error())
		}
	}
}

func TestNormalizeSMSNumberStripsOneLeadingZero(t *testing.T) {
	cimi := "AT+CIMI\r\n460111234567890\r\nOK\r\n"
	checks := []struct {
		in   string
		want string
	}{
		{"01012345678", "+861012345678"},   // 本地拨号前导 0 剥离
		{"013800138000", "+8613800138000"}, // 长途前导 0 剥离
		{"13800138000", "+8613800138000"},  // 无前导 0 行为不变
		{"+8613800138000", "+8613800138000"},
		{"008613800138000", "+8613800138000"}, // IDD 前缀分支不变
		{"10001", "10001"},                    // 短号不变
	}
	for _, check := range checks {
		got, err := normalizeSMSNumber(check.in, cimi)
		if err != nil {
			t.Fatalf("normalizeSMSNumber(%q) error: %v", check.in, err)
		}
		if got != check.want {
			t.Fatalf("normalizeSMSNumber(%q) = %q, want %q", check.in, got, check.want)
		}
	}
}

// ---------- 保护期文案 ----------

func TestNormalizeSMSNumberPendingWording(t *testing.T) {
	// 开机保护期占位文本/空应答/运行器错误 → 模块未就绪。
	for _, raw := range []string{
		atCachePendingText,
		"",
		"all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR",
	} {
		_, err := normalizeSMSNumber("13800138000", raw)
		if err == nil || err.Error() != "模块未就绪，请稍后重试" {
			t.Fatalf("normalizeSMSNumber with not-ready raw %q error = %v, want 模块未就绪，请稍后重试", raw, err)
		}
	}

	// 真无 SIM(+CME ERROR: 10 / SIM NOT INSERTED)保留原文案。
	for _, raw := range []string{
		"AT+CIMI\r\n+CME ERROR: 10\r\n",
		"AT+CIMI\r\nSIM NOT INSERTED\r\n",
	} {
		_, err := normalizeSMSNumber("13800138000", raw)
		if err == nil || !strings.Contains(err.Error(), "AT+CIMI 未返回有效 IMSI") {
			t.Fatalf("normalizeSMSNumber with no-SIM raw %q error = %v, want original IMSI wording", raw, err)
		}
	}
}

// ---------- pending 时不记录采样 ----------

func TestDashboardPendingSkipsMetricsHistory(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	pendingData := map[string]any{
		"pending": true, "cpuUsagePercent": 0, "ramUsagePercent": 0,
		"signalPercentage": 0, "nr_rx_bytes": int64(0), "nr_tx_bytes": int64(0),
	}
	recordDashboardMetricsIfReady(pendingData)
	if points := snapshotMetricsHistory(); len(points) != 0 {
		t.Fatalf("pending recorded %d points, want 0 (boot/reboot zeros must not enter history)", len(points))
	}

	readyData := map[string]any{
		"pending": false, "cpuUsagePercent": 36, "ramUsagePercent": 58,
		"signalPercentage": 86, "rsrpLTE": "-", "rsrpNR": "-86",
		"nr_rx_bytes": int64(1000), "nr_tx_bytes": int64(500),
	}
	recordDashboardMetricsIfReady(readyData)
	points := snapshotMetricsHistory()
	if len(points) != 1 {
		t.Fatalf("ready recorded %d points, want 1", len(points))
	}
	if points[0].CPUPercent != 36 || points[0].SignalPercent != 86 {
		t.Fatalf("point = %+v, want cpu=36 sig=86", points[0])
	}
}

// ---------- 探测缓存与超时 ----------

func TestDashboardInternetAliveCachesProbeResult(t *testing.T) {
	resetDashboardPingCacheForTest()
	defer resetDashboardPingCacheForTest()
	oldProbe := dashboardPingProbe
	defer func() { dashboardPingProbe = oldProbe }()

	calls := 0
	var seenDeadline time.Duration
	dashboardPingProbe = func(ctx context.Context) bool {
		calls++
		if deadline, ok := ctx.Deadline(); ok {
			seenDeadline = time.Until(deadline)
		}
		return true
	}

	if !dashboardInternetAlive(context.Background()) {
		t.Fatalf("first probe result = false, want true")
	}
	if !dashboardInternetAlive(context.Background()) {
		t.Fatalf("cached probe result = false, want true")
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1 (second call must hit cache, no real probe)", calls)
	}
	// 探测必须带 ≤1.5s 总超时,避免断网阻塞约 4.5s。
	if seenDeadline <= 0 || seenDeadline > dashboardPingTimeout {
		t.Fatalf("probe ctx deadline remaining = %v, want (0, %v]", seenDeadline, dashboardPingTimeout)
	}
	if dashboardPingTimeout > 1500*time.Millisecond {
		t.Fatalf("dashboardPingTimeout = %v, want <= 1.5s", dashboardPingTimeout)
	}

	// 缓存过期后再次真实探测。
	dashboardPingCache.Lock()
	dashboardPingCache.validUntil = time.Now().Add(-time.Second)
	dashboardPingCache.Unlock()
	if !dashboardInternetAlive(context.Background()) {
		t.Fatalf("post-expiry probe result = false, want true")
	}
	if calls != 2 {
		t.Fatalf("probe calls after expiry = %d, want 2", calls)
	}
}

func TestDashboardInternetAliveCachesNegativeResult(t *testing.T) {
	resetDashboardPingCacheForTest()
	defer resetDashboardPingCacheForTest()
	oldProbe := dashboardPingProbe
	defer func() { dashboardPingProbe = oldProbe }()

	calls := 0
	dashboardPingProbe = func(ctx context.Context) bool {
		calls++
		return false
	}
	if dashboardInternetAlive(context.Background()) {
		t.Fatalf("probe result = true, want false")
	}
	if dashboardInternetAlive(context.Background()) {
		t.Fatalf("cached negative result = true, want false")
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1 (negative result must also be cached)", calls)
	}
}
