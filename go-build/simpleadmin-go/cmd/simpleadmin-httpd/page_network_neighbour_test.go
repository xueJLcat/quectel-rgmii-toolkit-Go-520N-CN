package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// neighbourScanRealDeviceRaw 是实机(电信定制 RG520N-CN,驻留 NR5G-SA)抓取的
// 邻区扫描原始响应:固件 AT+QSCAN 不返回任何小区(已知缺陷),但
// +QNWCFG nr5g_meas_info 返回两条 NR 测量行;+QENG="neighbourcell"
// 在 SA 驻留时为空,故响应里没有任何 +QENG 行。
const neighbourScanRealDeviceRaw = "AT+QNWCFG=\"nr5g_meas_info\";+QENG=\"neighbourcell\"\r\n" +
	"+QNWCFG: \"nr5g_meas_info\",1,428910,277,-64,-11\r\n" +
	"+QNWCFG: \"nr5g_meas_info\",1,428910,581,-112,-15\r\n" +
	"\r\n" +
	"OK\r\n"

// ---------- parseNeighbourCellAT ----------

// TestParseNeighbourCellATRealDeviceCapture 用实机抓取原文验证:
// 两条 +QNWCFG 测量行解析为 2 个 NR5G 小区、0 个 LTE 小区,
// 频段由 428910 推导为 "1",字段索引(频点/PCI/RSRP)与实测布局一致。
func TestParseNeighbourCellATRealDeviceCapture(t *testing.T) {
	result := parseNeighbourCellAT(neighbourScanRealDeviceRaw)
	nr, ok := result["nr5g_cells_parsed"].([]map[string]string)
	if !ok {
		t.Fatalf("nr5g_cells_parsed type = %T, want []map[string]string", result["nr5g_cells_parsed"])
	}
	lte, ok := result["lte_cells_parsed"].([]map[string]string)
	if !ok {
		t.Fatalf("lte_cells_parsed type = %T, want []map[string]string", result["lte_cells_parsed"])
	}
	if len(nr) != 2 || len(lte) != 0 {
		t.Fatalf("nr=%d lte=%d, want 2/0 for real-device capture", len(nr), len(lte))
	}
	wants := []map[string]string{
		{"type": "NR5G", "provider": "", "band": "1", "freq": "428910", "pci": "277", "rsrp": "-64"},
		{"type": "NR5G", "provider": "", "band": "1", "freq": "428910", "pci": "581", "rsrp": "-112"},
	}
	for i, want := range wants {
		for k, v := range want {
			if nr[i][k] != v {
				t.Fatalf("nr[%d][%s] = %q, want %q (cell=%v)", i, k, nr[i][k], v, nr[i])
			}
		}
	}
}

// TestParseNeighbourCellATLTELines 覆盖 +QENG 邻区行的两种形态
// (intra/inter):字段取 freq=parts[2]、pci=parts[3]、rsrp=parts[5],
// band 固定 "-"(手册未给 LTE 频段号字段),与 2.0 版解析一致。
func TestParseNeighbourCellATLTELines(t *testing.T) {
	raw := "AT+QNWCFG=\"nr5g_meas_info\";+QENG=\"neighbourcell\"\r\n" +
		"+QENG: \"neighbourcell intra\",\"LTE\",1650,445,-7,-88,-65,0,37,7,16,6,44\r\n" +
		"+QENG: \"neighbourcell inter\",\"LTE\",1850,123,-9,-95,-70,2,40,8,18,7,50\r\n" +
		"OK\r\n"
	result := parseNeighbourCellAT(raw)
	nr, _ := result["nr5g_cells_parsed"].([]map[string]string)
	lte, _ := result["lte_cells_parsed"].([]map[string]string)
	if len(nr) != 0 || len(lte) != 2 {
		t.Fatalf("nr=%d lte=%d, want 0/2", len(nr), len(lte))
	}
	wants := []map[string]string{
		{"type": "LTE", "provider": "", "band": "-", "freq": "1650", "pci": "445", "rsrp": "-88"},
		{"type": "LTE", "provider": "", "band": "-", "freq": "1850", "pci": "123", "rsrp": "-95"},
	}
	for i, want := range wants {
		for k, v := range want {
			if lte[i][k] != v {
				t.Fatalf("lte[%d][%s] = %q, want %q (cell=%v)", i, k, lte[i][k], v, lte[i])
			}
		}
	}
}

// TestParseNeighbourCellATIgnoresGarbage 无关行(命令回显、servingcell 行、
// 残缺/噪声行)一律忽略,不影响有效邻区行的解析。
func TestParseNeighbourCellATIgnoresGarbage(t *testing.T) {
	raw := "AT+QNWCFG=\"nr5g_meas_info\";+QENG=\"neighbourcell\"\r\n" +
		"+QENG: \"servingcell\",\"NOCONN\",\"NR5G\",460,11,627264,301,78,1,1,30,18,-85,-11,23\r\n" +
		"+QNWCFG: \"nr5g_meas_info\",1,428910\r\n" +
		"GARBAGE LINE\r\n" +
		"+QNWCFG: \"nr5g_meas_info\",1,428910,277,-64,-11\r\n" +
		"+QENG: \"neighbourcell\"\r\n" +
		"OK\r\n"
	result := parseNeighbourCellAT(raw)
	nr, _ := result["nr5g_cells_parsed"].([]map[string]string)
	lte, _ := result["lte_cells_parsed"].([]map[string]string)
	if len(nr) != 1 || len(lte) != 0 {
		t.Fatalf("nr=%d lte=%d, want 1/0 (garbage must be ignored)", len(nr), len(lte))
	}
	if nr[0]["freq"] != "428910" || nr[0]["pci"] != "277" || nr[0]["rsrp"] != "-64" {
		t.Fatalf("valid line parsed wrong: %v", nr[0])
	}
}

// ---------- nrARFCNToBand ----------

// TestNrARFCNToBand 覆盖 38.104 频段推导:区间内命中、重叠区间按优先级
// 首匹配(428910→1 而非 66;538000→7 而非 41;653334→77 而非 78)、
// 边界值、区间外与非法输入。
func TestNrARFCNToBand(t *testing.T) {
	cases := []struct {
		arfcn, want string
	}{
		{"428910", "1"},
		{"627264", "78"},
		{"499200", "41"},
		{"151600", "28"},
		{"185000", "8"},
		{"653334", "77"},
		{"434500", "66"},
		{"537999", "41"},
		{"538000", "7"},
		{"999999999", "-"},
		{"", "-"},
		{"abc", "-"},
	}
	for _, tc := range cases {
		if got := nrARFCNToBand(tc.arfcn); got != tc.want {
			t.Fatalf("nrARFCNToBand(%q) = %q, want %q", tc.arfcn, got, tc.want)
		}
	}
}

// ---------- scanModeCommand ----------

// TestScanModeCommandNeighbourScan 邻区扫描模式必须映射到实测可用的
// 测量信息组合命令(固件 QSCAN 存在不返回小区的已知缺陷)。
func TestScanModeCommandNeighbourScan(t *testing.T) {
	command := scanModeCommand("Neighbour Scan")
	if command == "" {
		t.Fatalf("scanModeCommand(Neighbour Scan) is empty")
	}
	if want := `AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"`; command != want {
		t.Fatalf("scanModeCommand(Neighbour Scan) = %q, want %q", command, want)
	}
}

// ---------- handler 级 ----------

// TestHandleNetworkDataNeighbourScanWithCells 注入实机响应:
// ok=true、解析出 2 个 NR5G 小区、频段推导为 "1",且不带 empty 字段。
func TestHandleNetworkDataNeighbourScanWithCells(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return neighbourScanRealDeviceRaw, nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"scan"}, "mode": {"Neighbour Scan"}})
	if status != http.StatusOK {
		t.Fatalf("neighbour scan status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("neighbour scan ok = %v, want true (error=%v)", data["ok"], data["error"])
	}
	if _, exists := data["empty"]; exists {
		t.Fatalf("neighbour scan response contains empty=%v, want field absent when cells parsed", data["empty"])
	}
	nr, _ := data["nr5g_cells_parsed"].([]any)
	lte, _ := data["lte_cells_parsed"].([]any)
	if len(nr) != 2 || len(lte) != 0 {
		t.Fatalf("nr=%d lte=%d, want 2/0", len(nr), len(lte))
	}
	for i, item := range nr {
		cell, _ := item.(map[string]any)
		if cell["band"] != "1" {
			t.Fatalf("nr[%d] band = %v, want \"1\" (arfcn 428910, cell=%v)", i, cell["band"], cell)
		}
		if cell["freq"] != "428910" {
			t.Fatalf("nr[%d] freq = %v, want 428910 (cell=%v)", i, cell["freq"], cell)
		}
	}
}

// TestHandleNetworkDataNeighbourScanMarksEmptyWhenNoCells 模块只回
// 命令回显 + OK(无任何测量行)时:ok=true 且 empty=true,
// 与 QSCAN 的零结果语义保持一致。
func TestHandleNetworkDataNeighbourScanMarksEmptyWhenNoCells(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"scan"}, "mode": {"Neighbour Scan"}})
	if status != http.StatusOK {
		t.Fatalf("neighbour scan status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("neighbour scan ok = %v, want true (error=%v)", data["ok"], data["error"])
	}
	if data["empty"] != true {
		t.Fatalf("neighbour scan empty = %v, want true for echo+OK response", data["empty"])
	}
	nr, _ := data["nr5g_cells_parsed"].([]any)
	lte, _ := data["lte_cells_parsed"].([]any)
	if len(nr) != 0 || len(lte) != 0 {
		t.Fatalf("nr=%d lte=%d, want 0/0", len(nr), len(lte))
	}
}

// TestMockATResponseNeighbourScan mock 分支对邻区扫描组合命令返回
// 完整响应(回显 + 测量行 + OK),且不被泛化 QENG 分支抢先。
func TestMockATResponseNeighbourScan(t *testing.T) {
	command := scanModeCommand("Neighbour Scan")
	resp := mockATResponse(command)
	if !strings.HasPrefix(resp, command+"\r\n") {
		t.Fatalf("mock response missing command echo: %q", resp)
	}
	if !strings.Contains(resp, `+QNWCFG: "nr5g_meas_info",1,627264,301,-85,-11`) ||
		!strings.Contains(resp, `+QNWCFG: "nr5g_meas_info",1,627264,198,-97,-13`) {
		t.Fatalf("mock response missing QNWCFG measurement lines: %q", resp)
	}
	if !strings.Contains(resp, `+QENG: "neighbourcell intra","LTE",1650,445,-7,-88,-65,0,37,7,16,6,44`) {
		t.Fatalf("mock response missing LTE neighbour line: %q", resp)
	}
	if !strings.HasSuffix(resp, "OK\r\n") {
		t.Fatalf("mock response must end with OK: %q", resp)
	}
}
