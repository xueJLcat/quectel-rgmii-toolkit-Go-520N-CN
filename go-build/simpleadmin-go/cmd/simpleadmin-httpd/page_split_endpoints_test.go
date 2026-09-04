// page_split_endpoints_test.go 覆盖 /api/settings_data 拆分后的三个新端点
// (/api/at_data、/api/network_config_data、/api/system_data):
// 非法 action 必须一律 400;系统状态端点独立返回 IMEI;网络设置状态端点
// 的响应不再包含 IMEI 字段;AT 端点的恢复出厂设置动作可正常执行且响应
// 回显 AT&F;manual_at 缺少 command 时回退执行 ATI;各端点对非法参数
// (IMEI 过短、超过 255 的 IP 段、未知模式、非法协议族)统一以 400 和
// 对应错误文案拒绝;禁用 IP 透传返回重启通知字段;mock 模式下启用
// DNS V4 代理(写命令)必须成功。
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSplitEndpointsRejectUnsupportedActions(t *testing.T) {
	ts, client := e2eTestServer(t)
	for _, path := range []string{"/api/at_data", "/api/network_config_data", "/api/system_data"} {
		resp := e2eWebSocketCall(t, ts, client, "bogus:"+path, "POST", path, "action=bogus")
		if status := fmt.Sprint(resp["status"]); status != "400" {
			t.Fatalf("%s action=bogus status = %v, want 400 (error=%v)", path, resp["status"], resp["error"])
		}
	}
}

func TestSystemDataStatusReturnsMockIMEI(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "sys1", "POST", "/api/system_data", "")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("system_data status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("system_data body not json: %v", err)
	}
	if got := fmt.Sprint(data["imei"]); got != mockIMEI {
		t.Fatalf("system_data imei = %q, want %q", got, mockIMEI)
	}
	if data["pending"] != false {
		t.Fatalf("system_data pending = %v, want false", data["pending"])
	}
}

func TestNetworkConfigDataStatusHasNoIMEI(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "nc1", "POST", "/api/network_config_data", "")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("network_config_data status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("network_config_data body not json: %v", err)
	}
	if _, exists := data["imei"]; exists {
		t.Fatalf("network_config_data must not contain imei key: %#v", data)
	}
	if got := fmt.Sprint(data["currentUsbNetMode"]); got != "RMNET" {
		t.Fatalf("network_config_data currentUsbNetMode = %q, want RMNET", got)
	}
}

// 回归:真实固件 `AT+QMAP="DHCPV4DNS"` 查询恒返回 ERROR,状态组合命令因此以
// ERROR 收尾。这种"尾部 ERROR 但数据行完整"的部分成功绝不能被标记为读取失败,
// 否则前端会无限重试、页面永久卡在"状态获取中"(历史回归)。同时 DNS V4 必须
// 以 dnsV4QueryFailed=true 标记"未知",而不是回退成误导性的启用/禁用。
func TestNetworkConfigDataStatusToleratesTrailingError(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "nc-trail", "POST", "/api/network_config_data", "action=status")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("network_config_data status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("network_config_data body not json: %v", err)
	}
	if _, exists := data["error"]; exists {
		t.Fatalf("尾部 ERROR 的组合响应不得标记读取失败: %#v", data)
	}
	if data["pending"] != false {
		t.Fatalf("pending = %v, want false", data["pending"])
	}
	if data["dnsV4QueryFailed"] != true {
		t.Fatalf("dnsV4QueryFailed = %v, want true (DHCPV4DNS 查询不被固件支持)", data["dnsV4QueryFailed"])
	}
	if data["DNSV6ProxyStatus"] != false {
		t.Fatalf("DNSV6ProxyStatus = %v, want false (mock/固件为 disable)", data["DNSV6ProxyStatus"])
	}
}

// 回归:AT 通道完全读不到数据(运行器错误/空响应)时才判定为读取失败;
// "尾部 ERROR 的部分成功"与"保护期提示"都不得误判为失败。
func TestNetworkConfigStatusReadFailedPredicate(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"尾部 ERROR 但有数据行不算失败", "+QMAP: \"DMZ\",0,4\r\nERROR\r\n", false},
		{"正常完整响应不算失败", "+QMAP: \"MPDN_rule\",0,0,0,0,0\r\n+QCFG: \"usbnet\",0\r\nOK\r\n", false},
		{"仅 QCFG 数据行不算失败", "+QCFG: \"usbnet\",0\r\nOK\r\n", false},
		{"运行器错误文本判为失败", "all AT device candidates failed after retry", true},
		{"空响应判为失败", "", true},
		{"保护期提示不算失败(由 pending 处理)", atCachePendingText, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := networkConfigStatusReadFailed(tt.raw); got != tt.want {
				t.Fatalf("networkConfigStatusReadFailed(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestATDataResetAtExecutesFactoryReset(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "rst1", "POST", "/api/at_data", "action=reset_at")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("at_data reset_at status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("at_data reset_at body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("at_data reset_at ok = %v, want true", data["ok"])
	}
	if got := fmt.Sprint(data["response"]); !strings.Contains(got, "AT&F") {
		t.Fatalf("at_data reset_at response = %q, want contain AT&F", got)
	}
}

func TestATDataManualAtFallsBackToATI(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "ati1", "POST", "/api/at_data", "action=manual_at")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("at_data manual_at status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("at_data manual_at body not json: %v", err)
	}
	if got := fmt.Sprint(data["response"]); !strings.Contains(got, "Quectel") {
		t.Fatalf("at_data manual_at response = %q, want contain Quectel (fallback ATI)", got)
	}
}

func TestSplitEndpointsRejectInvalidParams(t *testing.T) {
	ts, client := e2eTestServer(t)
	cases := []struct {
		name    string
		path    string
		body    string
		wantErr string
	}{
		{"set_imei too short", "/api/system_data", "action=set_imei&imei=123", "invalid imei"},
		{"dmz ip octet over 255", "/api/network_config_data", "action=dmz&enabled=1&ip=300.1.1.1", "invalid dmz ip"},
		{"usbnet unknown mode", "/api/network_config_data", "action=usbnet&mode=FOO", "invalid usbnet mode"},
		{"dns invalid family", "/api/network_config_data", "action=dns_proxy&family=5&enabled=1", "invalid dns family"},
		{"passthrough unknown mode", "/api/network_config_data", "action=ip_passthrough&enabled=1&mode=FOO", "invalid passthrough mode"},
		{"lan ip octet over 255", "/api/network_config_data", "action=lanip&start=1.2.3.4&end=1.2.3.400&gateway=1.2.3.1", "invalid lan ip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := e2eWebSocketCall(t, ts, client, "bad:"+tc.name, "POST", tc.path, tc.body)
			if status := fmt.Sprint(resp["status"]); status != "400" {
				t.Fatalf("%s %s status = %v, want 400 (body=%v)", tc.path, tc.body, resp["status"], resp["body"])
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
				t.Fatalf("%s body not json: %v", tc.path, err)
			}
			if data["ok"] != false {
				t.Fatalf("%s ok = %v, want false", tc.path, data["ok"])
			}
			if got := fmt.Sprint(data["error"]); got != tc.wantErr {
				t.Fatalf("%s error = %q, want %q", tc.path, got, tc.wantErr)
			}
		})
	}
}

func TestNetworkConfigDisableIPPassthroughReturnsRebootNotice(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "ipt1", "POST", "/api/network_config_data", "action=ip_passthrough&enabled=0")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("ip_passthrough disable status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("ip_passthrough disable body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("ip_passthrough disable ok = %v, want true", data["ok"])
	}
	if data["reboot"] != true {
		t.Fatalf("ip_passthrough disable reboot = %v, want true", data["reboot"])
	}
	if got := fmt.Sprint(data["rebootCountdownSeconds"]); got != "40" {
		t.Fatalf("ip_passthrough disable rebootCountdownSeconds = %v, want 40", data["rebootCountdownSeconds"])
	}
}

func TestNetworkConfigDNSV4EnableSucceedsInMock(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "dns4", "POST", "/api/network_config_data", "action=dns_proxy&family=4&enabled=1")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("dns_proxy v4 enable status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("dns_proxy v4 enable body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("dns_proxy v4 enable ok = %v, want true (mock write must answer OK), response=%v", data["ok"], data["response"])
	}
}
