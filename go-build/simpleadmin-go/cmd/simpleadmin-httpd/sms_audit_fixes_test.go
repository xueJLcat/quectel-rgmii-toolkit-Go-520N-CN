// sms_audit_fixes_test.go 覆盖本轮短信域审计修复:
// B3 四字段非空 alpha 头 + 非 DELIVER PDU 必须跳过(不再产生假短信),
//
//	同形态头的合法 DELIVER 保留;
//
// B6 sim_status 需 +CPIN 正向证据才判已插卡,空/错误文本判未插卡;
// B7 PDU 时间戳时区按 15 分钟刻度换算为时:分显示。
package main

import (
	"fmt"
	"strings"
	"testing"
)

// ---------- B3: 四字段非空 alpha 头 ----------

// TestParseSMSListSkipsNonDeliverPDUFourFieldNonEmptyAlpha ME 电话簿对号码
// 存有姓名时,状态报告/已发送条目的列表头是四字段非空 alpha
// `+CMGL: 3,2,<alpha>,<len>`,parts[3] 是 PDU 长度。旧实现因
// `parts[2] != ""` 把长度回退成日期,绕过 `date == ""` 闸门,
// 把非 DELIVER PDU 解码成乱码假短信(还会被 webhook 推送)。必须跳过。
func TestParseSMSListSkipsNonDeliverPDUFourFieldNonEmptyAlpha(t *testing.T) {
	pdu := buildMockSMSDeliverPDU("+8613800138000", "不会显示")
	// 首字节 0x04(DELIVER)改为 0x06(STATUS-REPORT),parseSMSDeliverPDU 拒绝。
	statusReport := pdu[:2] + "06" + pdu[4:]
	for _, alpha := range []string{`"张三"`, `张三`} {
		raw := fmt.Sprintf("+CMGL: 3,2,%s,%d\r\n%s\r\nOK\r\n", alpha, len(statusReport)/2-1, statusReport)
		data := parseSMSListAT(raw, "ME")
		messages, _ := data["messages"].([]map[string]any)
		if len(messages) != 0 {
			t.Fatalf("alpha=%s: messages = %#v, want empty (non-DELIVER with non-empty alpha skipped)", alpha, messages)
		}
	}
}

// TestParseSMSListKeepsDeliverFourFieldNonEmptyAlpha 同形态头下的合法
// DELIVER 不受修复影响:正常解析,日期取 PDU 内时间戳而非头部长度字段。
func TestParseSMSListKeepsDeliverFourFieldNonEmptyAlpha(t *testing.T) {
	deliver := buildMockSMSDeliverPDU("+8610001", "正常短信")
	raw := fmt.Sprintf("+CMGL: 1,0,\"张三\",%d\r\n%s\r\nOK\r\n", len(deliver)/2-1, deliver)
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 (DELIVER with four-field non-empty alpha kept)", messages)
	}
	if got := stringValue(messages[0]["text"]); got != "正常短信" {
		t.Fatalf("text = %q, want 正常短信", got)
	}
	if got := stringValue(messages[0]["sender"]); got != "+8610001" {
		t.Fatalf("sender = %q, want +8610001", got)
	}
	// 日期来自 PDU 时间戳(时区已按 B7 换算为时:分),不是头部长度字段。
	if got := stringValue(messages[0]["date"]); got != "26/08/29,14:30:00+08:00" {
		t.Fatalf("date = %q, want 26/08/29,14:30:00+08:00 from PDU timestamp", got)
	}
}

// TestParseSMSListTextModeFourFieldDateFallbackRequiresDateShape 四字段头
// 回退仅采信日期形状的 parts[3]:日期形状保留,非日期形状(如纯数字)不回退。
func TestParseSMSListTextModeFourFieldDateFallbackRequiresDateShape(t *testing.T) {
	raw := `+CMGL: 1,1,"+8613800138000","26/08/30,10:20:30+32"
Hello
OK`
	data := parseSMSListAT(raw, "ME")
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1", messages)
	}
	if got := stringValue(messages[0]["date"]); got != "26/08/30,10:20:30+32" {
		t.Fatalf("date = %q, want 26/08/30,10:20:30+32 kept via date-shaped fallback", got)
	}
}

// ---------- B6: sim_status 正向证据 ----------

func TestSMSSIMInsertedRequiresPositiveEvidence(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"空响应", "", false},
		{"pending 文本", atCachePendingText, false},
		{"运行器错误文本", "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR", false},
		{"CPIN READY", "AT+QSIMSTAT?;+CPIN?\r\n+QSIMSTAT: 0,1\r\n+CPIN: READY\r\nOK\r\n", true},
		{"CPIN SIM PIN 也算在卡", "+CPIN: SIM PIN", true},
		{"CPIN NOT INSERTED", "+CPIN: NOT INSERTED", false},
		{"SIM NOT INSERTED 标志", "SIM NOT INSERTED", false},
		{"CME ERROR 10", "+CME ERROR: 10", false},
		{"仅 QSIMSTAT 无 CPIN 行不算证据", "+QSIMSTAT: 0,1\r\nOK", false},
	}
	for _, tc := range cases {
		if got := smsSIMInserted(tc.raw); got != tc.want {
			t.Fatalf("%s: smsSIMInserted = %v, want %v (raw=%q)", tc.name, got, tc.want, tc.raw)
		}
	}
}

// ---------- B7: PDU 时间戳时区刻度换算 ----------

func TestDecodePDUTimestampConvertsTimezoneQuarters(t *testing.T) {
	// 前 6 字节固定编码 26/08/29,14:30:00(与项目 mock PDU 一致)。
	stamp := func(tz byte) string {
		return decodePDUTimestamp([]byte{0x62, 0x80, 0x92, 0x41, 0x03, 0x00, tz})
	}
	cases := []struct {
		name string
		tz   byte
		want string
	}{
		// 北京:32 刻度 × 15 分 = 08:00(旧实现误显示 +32)。
		{"北京 32 刻度", 0x23, "26/08/29,14:30:00+08:00"},
		// 负偏移:符号位(0x08)置位的 32 刻度。
		{"负 32 刻度", 0x23 | 0x08, "26/08/29,14:30:00-08:00"},
		// 印度:22 刻度 × 15 分 = 05:30(半小时刻度必须保留分钟)。
		{"印度 22 刻度", 0x22, "26/08/29,14:30:00+05:30"},
		{"负 22 刻度", 0x22 | 0x08, "26/08/29,14:30:00-05:30"},
		{"零刻度", 0x00, "26/08/29,14:30:00+00:00"},
	}
	for _, tc := range cases {
		if got := stamp(tc.tz); got != tc.want {
			t.Fatalf("%s: decodePDUTimestamp = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestBuildMockSMSDeliverPDUDecodesBeijingTimezone mock PDU 的时区字节
// (0x23 = 32 刻度)经列表解析后必须显示 +08:00 而非 +32。
func TestBuildMockSMSDeliverPDUDecodesBeijingTimezone(t *testing.T) {
	pdu, ok := parseSMSDeliverPDU(buildMockSMSDeliverPDU("+8610001", "时区"))
	if !ok {
		t.Fatal("mock PDU 解析失败")
	}
	if !strings.HasSuffix(pdu.Date, "+08:00") {
		t.Fatalf("mock PDU date = %q, want suffix +08:00", pdu.Date)
	}
}
