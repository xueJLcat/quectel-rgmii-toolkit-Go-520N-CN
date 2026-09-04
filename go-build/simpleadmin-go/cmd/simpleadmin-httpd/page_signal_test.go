package main

import (
	"fmt"
	"testing"
	"time"
)

// 实机抓取的 NR5G-SA 应答(四天线值已改造为可区分的百分比映射)。
const signalDetailRawNRSA = "AT+QRSRP;+QENG=\"servingcell\";+QCAINFO\r\n" +
	"+QRSRP: -75,-95,-110,-32768,NR5G\r\n" +
	"+QENG: \"servingcell\",\"NOCONN\",\"NR5G-SA\",\"FDD\",460,11,2426AB483,277,242400,428910,1,6,-65,-11,25,0,-\r\n" +
	"+QCAINFO: \"PCC\",428910,6,\"NR5G BAND 1\",277\r\n" +
	"OK\r\n"

func signalDetailAntennas(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()
	antennas, ok := data["antennas"].([]map[string]any)
	if !ok || len(antennas) != 4 {
		t.Fatalf("antennas = %#v, want 4 rows", data["antennas"])
	}
	return antennas
}

func TestParseSignalDetailNR5GSA(t *testing.T) {
	data := parseSignalDetailAT(signalDetailRawNRSA)
	if data["ok"] != true || data["pending"] != false {
		t.Fatalf("ok/pending = %v/%v, want true/false", data["ok"], data["pending"])
	}
	if data["rat"] != "NR5G" {
		t.Fatalf("rat = %v, want NR5G", data["rat"])
	}

	antennas := signalDetailAntennas(t, data)
	wantRSRP := []string{"-75", "-95", "-110", "-"}
	wantPercent := []int{100, 62, 25, 0}
	for i, antenna := range antennas {
		if antenna["rsrp"] != wantRSRP[i] {
			t.Fatalf("antenna[%d].rsrp = %v, want %s", i, antenna["rsrp"], wantRSRP[i])
		}
		if toInt(fmt.Sprint(antenna["percent"])) != wantPercent[i] {
			t.Fatalf("antenna[%d].percent = %v, want %d", i, antenna["percent"], wantPercent[i])
		}
	}
	if antennas[0]["label"] != "天线1 (PRX)" || antennas[3]["id"] != "RX3" {
		t.Fatalf("antenna labels/ids wrong: %#v", antennas)
	}

	cell, _ := data["cell"].(map[string]any)
	if cell == nil {
		t.Fatal("cell summary missing")
	}
	if cell["network_mode"] != "NR5G-SA FDD" {
		t.Fatalf("cell.network_mode = %v, want NR5G-SA FDD", cell["network_mode"])
	}
	if cell["pcc_pci"] != "277" || cell["rsrpNR"] != "-65" || cell["rsrqNR"] != "-11" || cell["sinrNR"] != "25" {
		t.Fatalf("cell fields wrong: %#v", cell)
	}
	if cell["earfcns"] != "428910" {
		t.Fatalf("cell.earfcns = %v, want 428910", cell["earfcns"])
	}

	carriers, _ := data["carriers"].([]map[string]any)
	if len(carriers) != 1 {
		t.Fatalf("carriers = %#v, want 1 PCC", data["carriers"])
	}
	pcc := carriers[0]
	if pcc["role"] != "PCC" || pcc["band"] != "N1" || pcc["arfcn"] != "428910" || pcc["bandwidth"] != "40MHz" || pcc["pci"] != "277" {
		t.Fatalf("PCC carrier wrong: %#v", pcc)
	}
}

// TestParseSignalDetailCarriersAggregation 载波聚合生效时 PCC+SCC 全部列出,
// LTE/NR 载波的频段简写、带宽换算与 PCI 取位各自正确。
func TestParseSignalDetailCarriersAggregation(t *testing.T) {
	raw := "+QCAINFO: \"PCC\",1650,3,\"LTE BAND 3\",100,101\r\n" +
		"+QCAINFO: \"SCC\",6480,6,\"NR5G BAND 1\",277,278\r\n" +
		"OK\r\n"
	data := parseSignalDetailAT(raw)
	carriers, _ := data["carriers"].([]map[string]any)
	if len(carriers) != 2 {
		t.Fatalf("carriers = %#v, want 2 (PCC+SCC)", data["carriers"])
	}
	pcc, scc := carriers[0], carriers[1]
	if pcc["role"] != "PCC" || pcc["band"] != "B3" || pcc["bandwidth"] != "10MHz" || pcc["pci"] != "101" {
		t.Fatalf("LTE PCC wrong: %#v", pcc)
	}
	if scc["role"] != "SCC" || scc["band"] != "N1" || scc["bandwidth"] != "40MHz" || scc["pci"] != "278" {
		t.Fatalf("NR SCC wrong: %#v", scc)
	}
}

func TestParseSignalDetailNoCarriers(t *testing.T) {
	data := parseSignalDetailAT("+QRSRP: -75,-80,-81,-79,NR5G\r\nOK\r\n")
	carriers, _ := data["carriers"].([]map[string]any)
	if carriers == nil || len(carriers) != 0 {
		t.Fatalf("carriers = %#v, want empty non-nil", data["carriers"])
	}
}

func TestParseSignalDetailPending(t *testing.T) {
	data := parseSignalDetailAT(atCachePendingText)
	if data["pending"] != true || data["ok"] != false {
		t.Fatalf("pending/ok = %v/%v, want true/false", data["pending"], data["ok"])
	}
	for i, antenna := range signalDetailAntennas(t, data) {
		if antenna["rsrp"] != "-" {
			t.Fatalf("pending 时 antenna[%d].rsrp = %v, want -", i, antenna["rsrp"])
		}
	}
}

func TestParseSignalDetailEmptyOrErrorText(t *testing.T) {
	for _, raw := range []string{"", "AT command runner error: timeout"} {
		data := parseSignalDetailAT(raw)
		if data["ok"] != false || data["pending"] != false {
			t.Fatalf("raw=%q ok/pending = %v/%v, want false/false", raw, data["ok"], data["pending"])
		}
		antennas := signalDetailAntennas(t, data)
		for i, antenna := range antennas {
			if antenna["rsrp"] != "-" || toInt(fmt.Sprint(antenna["percent"])) != 0 {
				t.Fatalf("raw=%q antenna[%d] = %#v, want -/0", raw, i, antenna)
			}
		}
	}
}

func TestParseSignalDetailLTEOnlyAndENCD(t *testing.T) {
	lte := parseSignalDetailAT("+QRSRP: -85,-88,-90,-92,LTE\r\nOK\r\n")
	if lte["rat"] != "LTE" {
		t.Fatalf("lte-only rat = %v, want LTE", lte["rat"])
	}
	antennas := signalDetailAntennas(t, lte)
	if antennas[0]["rsrp"] != "-85" || antennas[3]["rsrp"] != "-92" {
		t.Fatalf("lte-only antenna values wrong: %#v", antennas)
	}

	// EN-DC:LTE 与 NR5G 两行并存,展示值拼接,百分比取 NR。
	endc := parseSignalDetailAT("+QRSRP: -85,-86,-86,-83,LTE\r\n+QRSRP: -100,-101,-102,-103,NR5G\r\nOK\r\n")
	if endc["rat"] != "NR5G" {
		t.Fatalf("en-dc rat = %v, want NR5G", endc["rat"])
	}
	antennas = signalDetailAntennas(t, endc)
	if antennas[0]["rsrp"] != "-85/-100" {
		t.Fatalf("en-dc antenna[0].rsrp = %v, want -85/-100", antennas[0]["rsrp"])
	}
	if toInt(fmt.Sprint(antennas[0]["percent"])) != calcRSRP(-100) {
		t.Fatalf("en-dc antenna[0].percent = %v, want from NR value -100", antennas[0]["percent"])
	}
}

// TestSignalDetailCommandCachePolicy 信号详情命令:5 秒缓存、不参与周期刷新
// (只有页面请求才产生 AT 流量);仪表盘组合命令虽含 +QENG/+QRSRP,
// 不得被误判为信号详情命令而遭抑制。
func TestSignalDetailCommandCachePolicy(t *testing.T) {
	if !isSignalDetailCommand(signalDetailATCommand()) {
		t.Fatal("信号详情命令应被识别")
	}
	dashboard := pageATCommand(atKeyDashboard)
	if isSignalDetailCommand(dashboard) {
		t.Fatal("仪表盘组合命令不得被误判为信号详情命令")
	}
	if !isPeriodicRefreshSuppressedATCommand(signalDetailATCommand()) {
		t.Fatal("信号详情命令应排除在周期刷新之外")
	}
	if isPeriodicRefreshSuppressedATCommand(dashboard) {
		t.Fatal("仪表盘命令必须保持周期刷新")
	}
	if got := maxAgeForATCacheCommand(signalDetailATCommand()); got != 5*time.Second {
		t.Fatalf("信号详情缓存时长 = %v, want 5s", got)
	}
}
