package main

import (
	"fmt"
	"net/http"
	"strings"
)

// signalDetailATCommand 信号详情页专用组合命令:四天线 RSRP + 服务小区信息
// + 载波聚合(主/辅载波)。该命令未登记进周期刷新集合
// (见 isPeriodicRefreshSuppressedATCommand),仅当信号详情页请求时才拉取,
// 关闭页面后不再产生任何相关 AT 流量。
func signalDetailATCommand() string {
	return `AT+QRSRP;+QENG="servingcell";+QCAINFO`
}

// signalAntennaIDs 四根接收天线的标识与显示名,顺序与 +QRSRP 应答字段一一对应。
var signalAntennaIDs = []struct{ id, label string }{
	{"PRX", "天线1 (PRX)"},
	{"DRX", "天线2 (DRX)"},
	{"RX2", "天线3 (RX2)"},
	{"RX3", "天线4 (RX3)"},
}

func (s *simpleAdminServer) handleSignalData(w http.ResponseWriter, r *http.Request) {
	raw := s.fetchPageAT(atKeySignalDetail, boolQuery(r, "force", false), true)
	data := parseSignalDetailAT(raw)
	// 与其余状态端点契约一致:非保护期且完全拿不到数据行才算读取失败,
	// 标记 error 供前端显示失败态/重试——AT 故障不得静默渲染成
	// "四根 - 天线的无信号空态"(失败伪装成空结果)。
	if pending, _ := data["pending"].(bool); !pending && atReadFailed(raw) {
		data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
	}
	writeJSON(w, http.StatusOK, data)
}

// parseSignalDetailAT 解析信号详情专用命令应答为结构化数据:
//
//	{"ok": 是否取到真实数据, "pending": 开机保护期/执行中,
//	 "rat": QRSRP 报告的制式, "antennas": [{id,label,rsrp,percent}x4],
//	 "carriers": [{role,band,arfcn,bandwidth,pci}...], "cell": {服务小区摘要}}
//
// 空应答、运行器错误文本解析为 ok:false 的四根 "-" 天线行与空载波列表,
// 前端按缺省态展示。
func parseSignalDetailAT(raw string) map[string]any {
	antennas := make([]map[string]any, 0, len(signalAntennaIDs))
	for _, antenna := range signalAntennaIDs {
		antennas = append(antennas, map[string]any{"id": antenna.id, "label": antenna.label, "rsrp": "-", "percent": 0})
	}
	data := map[string]any{
		"ok":       false,
		"pending":  strings.Contains(raw, atCachePendingText),
		"rat":      "",
		"antennas": antennas,
		"carriers": []map[string]any{},
		"cell":     signalCellSummary(nil),
	}
	if data["pending"].(bool) {
		return data
	}

	// 预置 "-" 缺省与仪表盘一致:appendDashboardList 依赖键已存在,
	// 否则缺失键会被 stringValue(nil) 污染成 "<nil> / 值"。
	cell := map[string]any{"earfcns": "-"}
	qrsrpLines := []string{}
	qcaLines := []string{}
	hasData := false
	for _, rawLine := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, "+QRSRP:"):
			qrsrpLines = append(qrsrpLines, line)
			hasData = true
		case strings.HasPrefix(line, "+QENG:"):
			applyQENGDashboard(line, cell)
			hasData = true
		case strings.HasPrefix(line, "+QCAINFO:"):
			qcaLines = append(qcaLines, line)
			hasData = true
		}
	}
	if !hasData {
		return data
	}
	data["ok"] = true
	applySignalAntennas(qrsrpLines, antennas, data)
	carriers := parseSignalCarriers(qcaLines)
	data["carriers"] = carriers
	// QCAINFO 的 SCC PCI 聚合进 cell 摘要(复用仪表盘 " + " 连接口径):
	// 摘要键列表与前端都为 scc_pci 预留了行,旧实现没把 QCAINFO 数据源
	// 接进来,该行恒 "-" 被前端隐藏,载波聚合时 SCC PCI 只在 carriers 可见。
	sccPCIs := []string{}
	for _, carrier := range carriers {
		if role, _ := carrier["role"].(string); role != "SCC" {
			continue
		}
		if pci, _ := carrier["pci"].(string); pci != "" && pci != "-" {
			sccPCIs = append(sccPCIs, pci)
		}
	}
	if len(sccPCIs) > 0 {
		cell["scc_pci"] = strings.Join(sccPCIs, " + ")
	}
	data["cell"] = signalCellSummary(cell)
	return data
}

// parseSignalCarriers 解析 +QCAINFO 各载波行:role 为 PCC(主载波)/
// SCC(辅载波),字段复用仪表盘的频段简写、带宽换算与 PCI 取位逻辑;
// 载波数 >1 即载波聚合生效。无应答时返回空列表(未驻留/不支持)。
func parseSignalCarriers(lines []string) []map[string]any {
	carriers := []map[string]any{}
	for _, line := range lines {
		p := csvFields(strings.TrimPrefix(line, "+QCAINFO:"))
		if len(p) < 4 {
			continue
		}
		role := strings.ToUpper(strings.TrimSpace(p[0]))
		bandRaw := strings.ToUpper(p[3])
		isNR := strings.Contains(bandRaw, "NR")
		carriers = append(carriers, map[string]any{
			"role":      role,
			"band":      shortBandName(p[3]),
			"arfcn":     strings.TrimSpace(p[1]),
			"bandwidth": bwMHz(p[2], isNR),
			"pci":       qcaPCI(p, role, isNR),
		})
	}
	return carriers
}

// applySignalAntennas 填充每根天线的 RSRP。EN-DC 双连接时模块返回 LTE 与
// NR5G 两行,按总览页口径拼为 "LTE值/NR值" 展示;单制式时单值展示。
// percent 取 NR(缺则 LTE)值按 RSRP 百分比映射,"-" 占位计 0。
func applySignalAntennas(lines []string, antennas []map[string]any, data map[string]any) {
	var lte, nr [4]string
	hasLTE, hasNR := false, false
	for _, line := range lines {
		parts := csvFields(strings.TrimPrefix(line, "+QRSRP:"))
		if len(parts) < 4 {
			continue
		}
		values := [4]string{
			normalizeQRSRPValue(parts[0]), normalizeQRSRPValue(parts[1]),
			normalizeQRSRPValue(parts[2]), normalizeQRSRPValue(parts[3]),
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.Contains(upper, "LTE"):
			lte, hasLTE = values, true
		case strings.Contains(upper, "NR5G"):
			nr, hasNR = values, true
		default:
			// 无 RAT 后缀的旧式应答按 LTE 槽位收纳,仅在尚无任何数据时生效。
			if !hasLTE && !hasNR {
				lte, hasLTE = values, true
			}
		}
	}
	if !hasLTE && !hasNR {
		return
	}
	// 制式在循环后整体判定,不随行序"最后写入者胜":EN-DC 双行并存时制式
	// 恒为 NR5G(与 percent 取 NR 值的口径一致),固件行序颠倒(NR5G 行在前)
	// 时旧实现会误标 LTE;无后缀旧式应答按 LTE 收纳后此处同样得到 LTE,
	// 不再遗留空串。
	if hasNR {
		data["rat"] = "NR5G"
	} else {
		data["rat"] = "LTE"
	}
	for i := range antennas {
		value, primary := "-", "-"
		switch {
		case hasLTE && hasNR:
			value = lte[i] + "/" + nr[i]
			primary = firstValidQRSRP(nr[i], lte[i])
		case hasNR:
			value, primary = nr[i], nr[i]
		default:
			value, primary = lte[i], lte[i]
		}
		antennas[i]["rsrp"] = value
		antennas[i]["percent"] = calcRSRP(toInt(primary))
	}
}

func firstValidQRSRP(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" && trimmed != "-" {
			return value
		}
	}
	return "-"
}

// signalCellSummary 从复用仪表盘 QENG 解析得到的键集中摘出服务区字段,
// 缺失键统一补 "-",保证前端表格行恒定。
func signalCellSummary(cell map[string]any) map[string]any {
	summary := map[string]any{}
	for _, key := range []string{
		"network_mode", "pcc_pci", "scc_pci", "tac", "earfcns", "cellID", "eNBID",
		"rsrpLTE", "rsrqLTE", "sinrLTE", "rsrpNR", "rsrqNR", "sinrNR", "rssi",
	} {
		if value, ok := cell[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			summary[key] = value
		} else {
			summary[key] = "-"
		}
	}
	return summary
}
