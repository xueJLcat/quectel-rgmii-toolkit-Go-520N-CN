package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- 短信分段按 UTF-16 码元 ----------

func TestSplitSMSUTF16UnitsSingleSegment(t *testing.T) {
	// 68 个汉字 = 68 码元 ≤ 70,应单段(旧按 rune 切 67 会误拆两段)。
	msg := strings.Repeat("测", 68)
	segments := splitSMSUTF16Units(msg, 70, 67)
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if len(segments[0]) != 68 {
		t.Fatalf("segment units = %d, want 68", len(segments[0]))
	}
}

func TestSplitSMSUTF16UnitsEmojiOverflow(t *testing.T) {
	// 35 个 emoji = 70 码元,恰好单段上限。
	msg := strings.Repeat("\U0001F600", 35)
	segments := splitSMSUTF16Units(msg, 70, 67)
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if len(segments[0]) != 70 {
		t.Fatalf("segment units = %d, want 70", len(segments[0]))
	}

	// 36 个 emoji = 72 码元 > 70,需分段且每段 ≤ 67 码元(134 字节 + 6 字节 UDH)。
	msg = strings.Repeat("\U0001F600", 36)
	segments = splitSMSUTF16Units(msg, 70, 67)
	if len(segments) < 2 {
		t.Fatalf("segments = %d, want >= 2", len(segments))
	}
	total := 0
	for i, seg := range segments {
		if len(seg) > 67 {
			t.Fatalf("segment %d units = %d, want <= 67", i, len(seg))
		}
		// 段尾不得是孤立高代理项。
		last := seg[len(seg)-1]
		if last >= 0xD800 && last <= 0xDBFF {
			t.Fatalf("segment %d ends with lone high surrogate %04X", i, last)
		}
		total += len(seg)
	}
	if total != 72 {
		t.Fatalf("total units = %d, want 72", total)
	}
}

// 厂商文本模式分段:66 汉字 + 3 emoji = 72 码元 > 70,需分两段,
// 每段 ≤70 码元(140 字节),十六进制长度 = 码元数*4,代理对不拆段。
func TestSplitSMSVendorSegmentsEmojiOverflow(t *testing.T) {
	msg := strings.Repeat("测", 66) + strings.Repeat("\U0001F600", 3)
	segments := splitSMSVendorSegments(msg)
	if len(segments) < 2 {
		t.Fatalf("segments = %d, want >= 2", len(segments))
	}
	totalUnits := 0
	for i, seg := range segments {
		if len(seg)%4 != 0 {
			t.Fatalf("segment %d hex length = %d, want multiple of 4", i, len(seg))
		}
		units := len(seg) / 4
		if units > smsVendorSegmentUnits {
			t.Fatalf("segment %d units = %d, want <= %d", i, units, smsVendorSegmentUnits)
		}
		if units == 0 {
			t.Fatalf("segment %d is empty", i)
		}
		// 段尾不得是孤立高代理项(十六进制形态为 D800-DBFF 开头)。
		last := seg[len(seg)-4:]
		if len(last) == 4 && last[0] == 'D' && (last[1] == '8' || last[1] == '9' || last[1] == 'A' || last[1] == 'B') {
			t.Fatalf("segment %d ends with lone high surrogate %s", i, last)
		}
		totalUnits += units
	}
	if totalUnits != 72 {
		t.Fatalf("total units = %d, want 72", totalUnits)
	}
}

// ≤70 码元的正文必须单段发送(厂商三元组 1,1)。
func TestSplitSMSVendorSegmentsSingle(t *testing.T) {
	segments := splitSMSVendorSegments(strings.Repeat("测", 70))
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if want := encodeUCS2(strings.Repeat("测", 70)); segments[0] != want {
		t.Fatalf("segment hex mismatch: got len %d, want len %d", len(segments[0]), len(want))
	}
	if got := splitSMSVendorSegments(""); len(got) != 0 {
		t.Fatalf("empty message segments = %d, want 0", len(got))
	}
}

// ---------- SCS 按频段推断 ----------

func TestInferNRSCSFromBand(t *testing.T) {
	checks := map[string]string{
		"1":  "15",
		"28": "15",
		"8":  "15",
		"78": "30",
		"41": "30",
		"":   "30",
		"99": "30",
	}
	for band, want := range checks {
		if got := inferNRSCSFromBand(band); got != want {
			t.Fatalf("inferNRSCSFromBand(%q) = %q, want %q", band, got, want)
		}
	}
}

// ---------- 短号不加国家码 ----------

func TestNormalizeSMSNumberShortCode(t *testing.T) {
	cimi := "AT+CIMI\r\n460115177700001\r\nOK\r\n"
	checks := []struct {
		in   string
		want string
	}{
		{"10001", "10001"},
		{"10086", "10086"},
		{"95588", "95588"},
		{"110", "110"},
		{"13800138000", "+8613800138000"},
		{"+8613800138000", "+8613800138000"},
		{"008613800138000", "+8613800138000"},
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

// ---------- LTE servingcell EARFCN 取 p[8] ----------

func TestParseDashboardLTEServingCellEarfcnIndex(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","LTE","FDD",460,01,5A29C0B,465,1650,3,5,5,DE10,-69,-5,-65,21,51`
	data := parseDashboardAT(raw)
	earfcns := stringValue(data["earfcns"])
	if !strings.Contains(earfcns, "1650") {
		t.Fatalf("earfcns = %q, want contain 1650 (p[8])", earfcns)
	}
	if strings.Contains(earfcns, " 3") || earfcns == "3" {
		t.Fatalf("earfcns = %q, must not use band indicator (p[9])", earfcns)
	}
}

// ---------- LTE SINR 2X-20 换算 ----------

func TestCalcLTESINRPercentConversion(t *testing.T) {
	// 原始 15 → 实际 10 dB → 50%。
	if got := calcLTESINRPercent(15); got != 50 {
		t.Fatalf("calcLTESINRPercent(15) = %d, want 50", got)
	}
	// 原始 10 → 实际 0 dB → 0%。
	if got := calcLTESINRPercent(10); got != 0 {
		t.Fatalf("calcLTESINRPercent(10) = %d, want 0", got)
	}
	// 原始 25 → 实际 30 dB → 100%。
	if got := calcLTESINRPercent(25); got != 100 {
		t.Fatalf("calcLTESINRPercent(25) = %d, want 100", got)
	}
}

// ---------- 信号字段 "-" 不得算成 100% ----------

func TestParseDashboardInvalidSignalFieldsYieldZeroPercent(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","NR5G-SA","FDD",460,11,24211A484,649,242000,428910,1,6,-,-,25,0,-`
	data := parseDashboardAT(raw)
	if got := percentFromAny(data["rsrpNRPercentage"]); got != 0 {
		t.Fatalf("rsrpNRPercentage = %d, want 0 for '-'", got)
	}
	if got := percentFromAny(data["rsrqNRPercentage"]); got != 0 {
		t.Fatalf("rsrqNRPercentage = %d, want 0 for '-'", got)
	}
	if stringValue(data["signalAssessment"]) == "优秀" {
		t.Fatalf("signalAssessment = %v, must not be 优秀 for '-'", data["signalAssessment"])
	}
}

// ---------- CGDCONT 精确匹配 cid=1 ----------

func TestParseNetworkSettingsCGDCONTExactCID(t *testing.T) {
	raw := `+CGDCONT: 1,"IPV4V6","ctnet","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,0,,,,,,,,,"",,,,0
+CGDCONT: 4,"IP","sos","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,1,,,,,,,,,"",,,,0`
	data := parseNetworkSettingsAT(raw)
	if got := stringValue(data["pdpType"]); got != "IPV4V6" {
		t.Fatalf("pdpType = %q, want IPV4V6 (cid=1), not overridden by sos context", got)
	}
	if got := stringValue(data["apn"]); got != "ctnet" {
		t.Fatalf("apn = %q, want ctnet", got)
	}
}

// ---------- 非 DELIVER 短信不回退乱码 ----------

func TestParseSMSListSkipsNonDeliverPDU(t *testing.T) {
	// PDU 模式(头部无日期),正文是一个无法解析为 DELIVER 的十六进制串
	// (模拟已发送/状态报告)。不得回退解码成乱码假短信。
	raw := `+CMGL: 3,2,47
0102030405
OK`
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 0 {
		t.Fatalf("messages = %#v, want empty (non-DELIVER skipped)", messages)
	}
}

func TestParseSMSListSkipsNonDeliverPDUFourFieldEmptyAlpha(t *testing.T) {
	// 本机固件与项目 mock 上非 DELIVER 条目(状态报告/已发送)的真实列表头形态:
	// 四字段空 alpha `+CMGL: 3,2,,47`,parts[3] 是 PDU 长度。
	// csvFields 保留空字段后该头切出 4 段,长度字段绝不得被回退成日期,
	// 否则绕过 `date == ""` 闸门,非 DELIVER PDU 被解码成乱码假短信。
	pdu := buildMockSMSDeliverPDU("+8613800138000", "不会显示")
	// 首字节 0x04(DELIVER)改为 0x06(STATUS-REPORT),parseSMSDeliverPDU 拒绝。
	statusReport := pdu[:2] + "06" + pdu[4:]
	raw := fmt.Sprintf("+CMGL: 3,2,,%d\r\n%s\r\nOK\r\n", len(statusReport)/2-1, statusReport)
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 0 {
		t.Fatalf("messages = %#v, want empty (non-DELIVER with four-field empty alpha header skipped)", messages)
	}

	// 同形态头部下的 DELIVER 条目仍必须正常解析(修复不得误伤收件)。
	deliver := buildMockSMSDeliverPDU("+8610001", "正常短信")
	raw = fmt.Sprintf("+CMGL: 1,0,,%d\r\n%s\r\nOK\r\n", len(deliver)/2-1, deliver)
	data = parseSMSListAT(raw, "ME")
	messages, _ = data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 (DELIVER with four-field empty alpha header kept)", messages)
	}
	if got := stringValue(messages[0]["text"]); got != "正常短信" {
		t.Fatalf("text = %q, want 正常短信", got)
	}
}

func TestParseSMSListTextModeFourFieldHeaderKeepsDate(t *testing.T) {
	// 文本模式四字段头(idx,stat,sender,date,无 alpha)是 parts[3] 回退的
	// 合法场景:发送者非空时日期必须保留,修复不得过度拦截。
	raw := `+CMGL: 1,1,"+8613800138000","26/08/30,10:20:30+32"
Hello
OK`
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1", messages)
	}
	if got := stringValue(messages[0]["date"]); got != "26/08/30,10:20:30+32" {
		t.Fatalf("date = %q, want 26/08/30,10:20:30+32 from parts[3] fallback", got)
	}
	if got := stringValue(messages[0]["text"]); got != "Hello" {
		t.Fatalf("text = %q, want Hello", got)
	}
}

// ---------- 目标模块锚定 ----------

// 项目目标模块为移远 RG520N-CN(实机验证,型号硬编码不做动态识别);
// mock 原型与型号接口必须返回该型号,前端据此(及 band_map 默认值)
// 只显示该模块支持的频段。
func TestTargetModuleIsRG520NCN(t *testing.T) {
	if deviceModelName != "RG520N-CN" {
		t.Fatalf("deviceModelName = %q, want RG520N-CN", deviceModelName)
	}
	if mockModuleModel != "RG520N-CN" {
		t.Fatalf("mockModuleModel = %q, want RG520N-CN", mockModuleModel)
	}
	if got := currentDeviceModel(); got != "RG520N-CN" {
		t.Fatalf("currentDeviceModel = %q, want RG520N-CN", got)
	}
}

// /api/module_model 端点保留,直接返回硬编码型号且永不 pending。
func TestHandleModuleModelReturnsHardcodedRG520NCN(t *testing.T) {
	s := &simpleAdminServer{}
	w := httptest.NewRecorder()
	s.handleModuleModel(w, httptest.NewRequest(http.MethodGet, "/api/module_model", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if got := fmt.Sprint(data["model"]); got != "RG520N-CN" {
		t.Fatalf("model = %q, want RG520N-CN", got)
	}
	if data["pending"] != false {
		t.Fatalf("pending = %v, want false", data["pending"])
	}
}

// ---------- QRSRP 占位值过滤 ----------

func TestNormalizeQRSRPValue(t *testing.T) {
	checks := map[string]string{
		"-85":    "-85",
		"-44":    "-44",
		"-32768": "-",
	}
	for in, want := range checks {
		if got := normalizeQRSRPValue(in); got != want {
			t.Fatalf("normalizeQRSRPValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------- pending 文本不参与解析 ----------

func TestParseDeviceInfoIgnoresPendingText(t *testing.T) {
	raw := atCachePendingText
	data := parseDeviceInfoAT(raw)
	if got := stringValue(data["manufacturer"]); got != "-" {
		t.Fatalf("manufacturer = %q, want - (pending text must not be parsed)", got)
	}
	if got := stringValue(data["firmwareVersion"]); got != "-" {
		t.Fatalf("firmwareVersion = %q, want -", got)
	}
}
