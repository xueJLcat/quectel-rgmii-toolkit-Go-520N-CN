package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------- ME+SM 双存储列表合并 ----------

// 文本模式短信罐头应答(带日期头,走文本解析路径)。
const smsDualRawME = "+CMGL: 1,1,\"+8613800138001\",\"26/08/29,10:00:00+32\"\n4F60\nOK\n"
const smsDualRawSM = "+CMGL: 2,1,\"+8613800138002\",\"26/08/29,10:01:00+32\"\n597D\nOK\n"

// TestParseSMSListATStorageTag 解析结果按存储标记,缺省归一为 ME。
func TestParseSMSListATStorageTag(t *testing.T) {
	me := parseSMSListAT(smsDualRawME, "ME")
	meMsgs, _ := me["messages"].([]map[string]any)
	if len(meMsgs) != 1 || meMsgs[0]["storage"] != "ME" {
		t.Fatalf("ME 解析应带 storage=ME: %#v", me["messages"])
	}
	sm := parseSMSListAT(smsDualRawSM, "SM")
	smMsgs, _ := sm["messages"].([]map[string]any)
	if len(smMsgs) != 1 || smMsgs[0]["storage"] != "SM" {
		t.Fatalf("SM 解析应带 storage=SM: %#v", sm["messages"])
	}
	// 非法存储名归一回 ME。
	other := parseSMSListAT(smsDualRawME, "XX")
	otherMsgs, _ := other["messages"].([]map[string]any)
	if len(otherMsgs) != 1 || otherMsgs[0]["storage"] != "ME" {
		t.Fatalf("非法存储名应归一为 ME: %#v", other["messages"])
	}
}

// TestFetchSMSListDualStorageMergesAndSurvivesSMFailure 双存储合并:
// ME 成功 + SM 成功 → 两者合并;SM 失败(错误文本) → 只保留 ME 结果。
func TestFetchSMSListDualStorageMergesAndSurvivesSMFailure(t *testing.T) {
	me := parseSMSListAT(smsDualRawME, "ME")
	sm := parseSMSListAT(smsDualRawSM, "SM")
	merged, smHas := mergeSMSListDual(me, sm)
	msgs, _ := merged["messages"].([]map[string]any)
	if len(msgs) != 2 || !smHas {
		t.Fatalf("双存储合并应有 2 条: %#v", merged["messages"])
	}
	if msgs[0]["storage"] != "ME" || msgs[1]["storage"] != "SM" {
		t.Fatalf("合并顺序应为 ME 在前 SM 在后: %#v", msgs)
	}

	// SM 读取失败(仅错误文本,无 +CMGL) → 只保留 ME。
	smFail := parseSMSListAT("all AT device candidates failed", "SM")
	mergedOnlyME, smHasFail := mergeSMSListDual(parseSMSListAT(smsDualRawME, "ME"), smFail)
	msgsOnly, _ := mergedOnlyME["messages"].([]map[string]any)
	if len(msgsOnly) != 1 || msgsOnly[0]["storage"] != "ME" || smHasFail {
		t.Fatalf("SM 失败时应只保留 ME: %#v", mergedOnlyME["messages"])
	}
}

// ---------- 存储感知删除令牌 ----------

// TestParseSMSDeleteToken 解析 "ME:1"/"SM:2"/裸索引(兼容旧前端按 ME)。
func TestParseSMSDeleteToken(t *testing.T) {
	checks := []struct {
		in          string
		wantStorage string
		wantIndex   string
	}{
		{"ME:1", "ME", "1"},
		{"SM:2", "SM", "2"},
		{"sm:3", "SM", "3"},
		{"me:4", "ME", "4"},
		{"5", "ME", "5"},
		{" 6 ", "ME", "6"},
		{"7abc8", "ME", "78"},
		{"", "ME", ""},
	}
	for _, c := range checks {
		gotStorage, gotIndex := parseSMSDeleteToken(c.in)
		if gotStorage != c.wantStorage || gotIndex != c.wantIndex {
			t.Fatalf("parseSMSDeleteToken(%q) = (%q,%q), want (%q,%q)",
				c.in, gotStorage, gotIndex, c.wantStorage, c.wantIndex)
		}
	}
}

// TestDeleteIndicesGroupsByStorage 不同存储的索引分别下发对应存储的删除命令。
func TestDeleteIndicesGroupsByStorage(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	commands := []string{}
	origRun := smsDeleteRunForTest
	smsDeleteRunForTest = func(s *simpleAdminServer, command string) string {
		commands = append(commands, command)
		return command + "\r\nOK\r\n"
	}
	t.Cleanup(func() { smsDeleteRunForTest = origRun })

	result := srv.deleteSMSIndicesByToken([]string{"ME:1", "SM:2", "ME:3", "4"})
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("ok = %v, want true, error=%v", result["ok"], result["error"])
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		`AT+CPMS="ME","ME","ME";+CMGD=1`,
		`AT+CPMS="ME","ME","ME";+CMGD=3`,
		`AT+CPMS="ME","ME","ME";+CMGD=4`,
		`AT+CPMS="SM","SM","SM";+CMGD=2`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少删除命令 %q, 实际:\n%s", want, joined)
		}
	}
	if fmt := jsonNumber(result["deleted"]); fmt != 4 {
		t.Fatalf("deleted = %v, want 4", result["deleted"])
	}
}

func jsonNumber(v any) int {
	b, _ := json.Marshal(v)
	var n int
	_ = json.Unmarshal(b, &n)
	return n
}

// ---------- 逐存储 webhook 高水位 ----------

// TestSMSWebhookPerStorageHighWater ME 与 SM 高水位独立,互不漏报。
func TestSMSWebhookPerStorageHighWater(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	msg := func(storage, sender string, idx int) map[string]any {
		m := smsWebhookTestMessage(sender, "26/08/30,09:00:00+32", "x", idx)
		m["storage"] = storage
		return m
	}

	// 首扫建立两个存储的基线。
	notifyNewSMSByWebhook([]map[string]any{
		msg("ME", "+861", 5),
		msg("SM", "+862", 2),
	}, true)
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookHighWater["ME"] != 5 || smsWebhookHighWater["SM"] != 2 {
		t.Fatalf("基线应分别为 ME=5/SM=2, 实际 ME=%d SM=%d",
			smsWebhookHighWater["ME"], smsWebhookHighWater["SM"])
	}

	// SM 新增索引 3(低于 ME 高水位 5)必须被通知——单一高水位会漏报它。
	notifyNewSMSByWebhook([]map[string]any{
		msg("ME", "+861", 5),
		msg("SM", "+862", 2),
		msg("SM", "+863", 3),
	}, true)
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if jsonNumber(payload["index"]) != 3 || payload["storage"] != "SM" {
		t.Fatalf("应通知 SM 存储索引 3 的新消息, 实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}

// TestSMSWebhookMEFailedSMValidDoesNotFakeMEBaseline ME 读取失败而 SM 有效时,
// 不得把 ME 当作已扫描并建立空基线(高水位 -1);否则随后首次真正的 ME 有效
// 扫描会把整个存量收件箱全部当成"新到达"逐条误推。
func TestSMSWebhookMEFailedSMValidDoesNotFakeMEBaseline(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	msg := func(storage, sender string, idx int) map[string]any {
		m := smsWebhookTestMessage(sender, "26/08/30,09:00:00+32", "x", idx)
		m["storage"] = storage
		return m
	}

	// ME 读取失败(无消息、有效性假),SM 有效且有 1 条:仅 SM 推进高水位,
	// ME 绝不能被初始化。
	notifyNewSMSByWebhookStorages([]map[string]any{
		msg("SM", "+862", 2),
	}, map[string]bool{"ME": false, "SM": true})
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookInitialized["ME"] {
		t.Fatalf("ME 读取失败不得建立假基线: initialized=%v highWater=%d",
			smsWebhookInitialized["ME"], smsWebhookHighWater["ME"])
	}

	// 首次 ME 有效扫描带着存量消息到达:应整体作为基线消耗(不逐条误推)。
	// 若上一步已建立假基线 -1,这两条存量消息(索引均 > -1)会被全部误推。
	notifyNewSMSByWebhookStorages([]map[string]any{
		msg("ME", "+861", 1),
		msg("ME", "+861", 3),
	}, map[string]bool{"ME": true, "SM": false})
	expectNoSMSWebhookPosts(t, ch)
	if smsWebhookHighWater["ME"] != 3 {
		t.Fatalf("首次 ME 有效扫描高水位应为 3, 实际: %d", smsWebhookHighWater["ME"])
	}

	// 此后 ME 真正新到达(索引 4)照常通知一次。
	notifyNewSMSByWebhookStorages([]map[string]any{
		msg("ME", "+861", 3),
		msg("ME", "+869", 4),
	}, map[string]bool{"ME": true, "SM": false})
	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if jsonNumber(payload["index"]) != 4 || payload["storage"] != "ME" {
		t.Fatalf("应通知 ME 索引 4 的新消息, 实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)
}
