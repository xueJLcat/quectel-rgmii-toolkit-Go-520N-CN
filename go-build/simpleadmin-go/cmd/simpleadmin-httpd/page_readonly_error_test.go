package main

import (
	"strings"
	"testing"
)

// 真实固件(RG520N-CN)上 `AT+QMAP="DHCPV4DNS"` 恒返回 ERROR,导致网络设置页
// 组合查询以 ERROR 收尾。只读组合命令的解析必须容忍这种尾部 ERROR。
func TestParseNetworkConfigStatusToleratesTrailingError(t *testing.T) {
	raw := `AT+QMAP="MPDN_RULE";+QMAP="DHCPV6DNS";+QCFG="usbnet";+QMAP="DMZ";+QMAP="DHCPV4DNS"
+QMAP: "MPDN_rule",0,0,0,0,0
+QMAP: "MPDN_rule",1,0,0,0,0
+QMAP: "MPDN_rule",2,0,0,0,0
+QMAP: "MPDN_rule",3,0,0,0,0

+QMAP: "DHCPV6DNS","disable"

+QCFG: "usbnet",0

+QMAP: "DMZ",0,4
+QMAP: "DMZ",0,6

ERROR
`
	data := parseNetworkConfigStatusAT(raw)
	if data["ipPassStatus"] != false {
		t.Fatalf("ipPassStatus = %v, want false", data["ipPassStatus"])
	}
	if data["DNSV6ProxyStatus"] != false {
		t.Fatalf("DNSV6ProxyStatus = %v, want false (DHCPV6DNS disable)", data["DNSV6ProxyStatus"])
	}
	if data["DNSV4ProxyStatus"] != false {
		t.Fatalf("DNSV4ProxyStatus = %v, want false (DHCPV4DNS unsupported)", data["DNSV4ProxyStatus"])
	}
	if got := stringValue(data["currentUsbNetMode"]); got != "RMNET" {
		t.Fatalf("currentUsbNetMode = %q, want RMNET", got)
	}
	if got := stringValue(data["dmzMode"]); got != "0" {
		t.Fatalf("dmzMode = %q, want 0", got)
	}
}

// 组合命令中某个子命令失败时,模块会中止后续子命令并以 ERROR 收尾;
// 首页解析必须正常使用已收到的字段,缺失字段保持默认值。
func TestParseDashboardToleratesAbortedCompoundResponse(t *testing.T) {
	raw := `AT+QSIMSTAT?;+CSQ;+QTEMP;+QUIMSLOT?;+QSPN;+QMAP="WWAN";+QENG="servingcell";+QCAINFO;+QGDNRCNT?;+QGDCNT?;+CGCONTRDP=1;+QRSRP
+QSIMSTAT: 0,1

+CSQ: 99,99

+QUIMSLOT: 1

+QMAP: "WWAN",1,1,"IPV4","100.104.47.236"

ERROR
`
	data := parseDashboardAT(raw)
	if got := stringValue(data["sim"]); got != "已激活" {
		t.Fatalf("sim = %q, want 已激活", got)
	}
	if got := stringValue(data["csq"]); got != "-" {
		t.Fatalf("csq = %q, want - (99 means unknown)", got)
	}
	if got := stringValue(data["ipv4"]); got != "100.104.47.236" {
		t.Fatalf("ipv4 = %q, want 100.104.47.236", got)
	}
	// 中止后的字段保持缺省占位,不得残留旧值语义。
	if got := stringValue(data["network_mode"]); got != "-" {
		t.Fatalf("network_mode = %q, want -", got)
	}
	if got := stringValue(data["apn"]); got != "-" {
		t.Fatalf("apn = %q, want -", got)
	}
}

// 组合命令响应中出现的 ERROR 行本身不得被当作有效数据行。
func TestParseIgnoresBareErrorLines(t *testing.T) {
	raw := `+QUIMSLOT: 1

+QNWPREFCFG: "mode_pref",AUTO

ERROR
`
	data := parseNetworkSettingsAT(raw)
	if got := stringValue(data["sim"]); got != "1" {
		t.Fatalf("sim = %q, want 1", got)
	}
	if got := stringValue(data["prefNetwork"]); got != "AUTO" {
		t.Fatalf("prefNetwork = %q, want AUTO", got)
	}
	if strings.Contains(strings.ToLower(stringValue(data["apn"])), "error") {
		t.Fatalf("apn must not contain error text: %q", data["apn"])
	}
}
