package main

import "testing"

// 本文件为一批"既有测试未覆盖、但真实可触发"的功能性缺陷修复的回归测试。
// 每个用例对应一处修复,注释说明回归背景,防止后续改动再次引入。

// ---------- decodeMaybeUCS2:纯数字串不再被误判为 UCS2 ----------
//
// 回归背景:旧实现只要"长度为 4 倍数且全为十六进制字符"就按 UCS2 解码,
// 而十进制数字本身也是合法十六进制,导致文本模式的数字内容被误解成乱码:
// 数字正文 "2024" 被解成 U+2024,8 位客服短号被解成两个 CJK 字符。
// 修复:要求至少含一个十六进制字母 A-F 作为 UCS2 的正向证据。
// 说明:形似 UCS2 的纯数字串(如 "0041"='A')按"宁可不解码"处理——
// 短信列表固定 PDU 模式,真正的 UCS2 由 DCS 判定走 decodeUCS2Bytes,
// 这里的启发式只服务文本模式回退,数字串在该语境下几乎必然是字面数字。
func TestDecodeMaybeUCS2RejectsPureDigits(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"数字正文原样保留", "2024", "2024"},
		{"8位数字短号原样保留", "12345678", "12345678"},
		{"形似UCS2的纯数字原样保留", "0041", "0041"},
		{"中文UCS2正常解码", "4E2D6587", "中文"},
		{"含字母的拉丁UCS2正常解码", "00E9", "é"},
		{"非十六进制原样保留", "+8613800138000", "+8613800138000"},
	}
	for _, tc := range cases {
		if got := decodeMaybeUCS2(tc.input); got != tc.want {
			t.Errorf("%s: decodeMaybeUCS2(%q) = %q, want %q", tc.name, tc.input, got, tc.want)
		}
	}
}

// ---------- decodePDUAddress:字母型发送者按 GSM 7bit 解码 ----------
//
// 回归背景:旧实现 toa&0x90==0x90 同时命中国际号码(0x91)与字母型
// 发送者(0xD0,TON=0x5),却一律按 BCD 数字解码并强加 "+" 前缀,
// 字母发送者(如 "MyBank")显示为乱码。修复:按类型号码段(TON)分支,
// 字母型用 GSM 7bit 解出原文且不加 "+",国际号码才加 "+"。
func TestDecodePDUAddressAlphanumericSender(t *testing.T) {
	// "MyBank" 的 GSM 7bit 压缩结果,半八度数=11(=向上取整 6*7/4)。
	raw := []byte{0xCD, 0xBC, 0x30, 0xEC, 0x5E, 0x03}
	if got := decodePDUAddress(11, 0xD0, raw); got != "MyBank" {
		t.Errorf("alphanumeric sender = %q, want MyBank", got)
	}
}

func TestDecodePDUAddressInternationalNumericKeepsPlus(t *testing.T) {
	// 8613800138000 的 BCD 半八度编码(13 位,补 F 成偶数),国际号码
	// (TOA 0x91)需保留 "+"。
	raw := []byte{0x68, 0x31, 0x08, 0x10, 0x83, 0x00, 0xF0}
	got := decodePDUAddress(13, 0x91, raw)
	if got != "+8613800138000" {
		t.Errorf("international numeric = %q, want +8613800138000", got)
	}
}

// ---------- parseDeviceInfoAT:SIM PIN/PUK 判定为已插卡 ----------
//
// 回归背景:旧实现 +CPIN 只认 READY,`+CPIN: SIM PIN`(卡被 PIN 锁但显然
// 在位)被判为未插卡,并清空 imsi/iccid 等仍可读字段。与页面内
// smsSIMInserted 的正确口径矛盾。修复:状态非空且非 NOT INSERTED 即在位。
func TestParseDeviceInfoCPINLockedStillInserted(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantStatus  string
		wantPresent bool
	}{
		{"SIM PIN 锁仍是在位", "+CPIN: SIM PIN\nOK", "已插卡", true},
		{"SIM PUK 锁仍是在位", "+CPIN: SIM PUK\nOK", "已插卡", true},
		{"READY 在位", "+CPIN: READY\nOK", "已插卡", true},
		{"NOT INSERTED 未插卡", "+CPIN: NOT INSERTED\nOK", "未插卡", false},
	}
	for _, tc := range cases {
		data := parseDeviceInfoAT(tc.raw)
		if data["simStatus"] != tc.wantStatus {
			t.Errorf("%s: simStatus = %#v, want %s", tc.name, data["simStatus"], tc.wantStatus)
		}
		if data["simInserted"] != tc.wantPresent {
			t.Errorf("%s: simInserted = %#v, want %v", tc.name, data["simInserted"], tc.wantPresent)
		}
	}
}

// ---------- parseDashboardAT:CGCONTRDP 按地址族落位 ----------
//
// 回归背景:+CGCONTRDP 按地址族逐行返回,IPv6-only PDN(或 IPV4V6 未分到
// v4)时第 4 列承载的是 v6 地址;旧实现无条件写进 ipv4,导致 v6 地址显示
// 在 IPv4 栏。修复:用 net.ParseIP 判定地址族再落入对应字段。
func TestParseDashboardCGCONTRDPAddressFamily(t *testing.T) {
	v6 := "+QSIMSTAT: 0,1\n+CGCONTRDP: 1,0,\"ctnet\",\"240e:45d:820:43e6:a098:a1e9:3b2b:e417\"\nOK"
	data := parseDashboardAT(v6)
	if data["ipv4"] != "-" {
		t.Errorf("IPv6-only: ipv4 = %#v, want - (不应被 v6 地址污染)", data["ipv4"])
	}
	if data["ipv6"] != "240e:45d:820:43e6:a098:a1e9:3b2b:e417" {
		t.Errorf("IPv6-only: ipv6 = %#v, want v6 地址", data["ipv6"])
	}

	v4 := "+QSIMSTAT: 0,1\n+CGCONTRDP: 1,0,\"ctnet\",\"10.47.28.216\"\nOK"
	data = parseDashboardAT(v4)
	if data["ipv4"] != "10.47.28.216" {
		t.Errorf("IPv4: ipv4 = %#v, want 10.47.28.216", data["ipv4"])
	}
	if data["ipv6"] != "-" {
		t.Errorf("IPv4: ipv6 = %#v, want -", data["ipv6"])
	}
}

// ---------- mccToCallingCode:MCC 362 不再映射 +1 ----------
//
// 回归背景:MCC 362(荷属安的列斯)横跨库拉索/博内尔(+599)与圣马丁
// (+1-721)两个编号计划,自动加 +1 对两地都无法路由。修复:移出 "1" 映射,
// 落入"未配置国家/地区代码"分支提示用户用国际格式输入。
func TestMCCToCallingCode362NotMappedToOne(t *testing.T) {
	if got := mccToCallingCode("362"); got != "" {
		t.Errorf("mccToCallingCode(362) = %q, want \"\" (避免拼出不可路由号码)", got)
	}
}
