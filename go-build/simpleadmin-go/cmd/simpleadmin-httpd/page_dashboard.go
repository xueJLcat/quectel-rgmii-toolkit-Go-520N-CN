package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *simpleAdminServer) handleDashboardData(w http.ResponseWriter, r *http.Request) {
	raw := s.fetchPageAT(atKeyDashboard, boolQuery(r, "force", false), true)
	data := parseDashboardAT(raw)
	// 开机保护期/后台未就绪时返回默认值会误导用户,标记 pending 供前端重试。
	data["pending"] = strings.Contains(raw, atCachePendingText)
	if !data["pending"].(bool) && atReadFailed(raw) {
		data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
	}
	if boolQuery(r, "debug", false) {
		data["raw"] = raw
	}
	data["internetConnection"] = "未连接"
	hasIP := stringValue(data["ipv4"]) != "-" || stringValue(data["ipv6"]) != "-"
	if hasIP {
		if s.cfg.mockMode || dashboardInternetAlive(r.Context()) {
			data["internetConnection"] = "已连接"
		}
	}
	data["uptimeParts"] = parseUptimeParts(nativeOrMockUptime(s.cfg.mockMode))
	for key, value := range systemResourceMetrics(s.cfg.mockMode) {
		data[key] = value
	}
	recordDashboardMetricsIfReady(data)
	data["lastUpdate"] = time.Now().Format("2006/01/02 15:04:05")
	writeJSON(w, http.StatusOK, data)
}

const (
	// 总览页探测的总超时:断网时 nativePing 会逐个拨号 3 个地址
	// (每个 1.5s),不加总超时会阻塞请求约 4.5s,拖垮 2s 轮询。
	dashboardPingTimeout = 1500 * time.Millisecond
	// 探测结果短缓存:轮询命中缓存不再真实探测。
	dashboardPingCacheTTL = 5 * time.Second
)

// dashboardPingProbe 是总览页使用的连通性探测器,声明为 var 便于测试注入;
// 生产路径仍走 nativePing。/api/get_ping 端点不走这里,行为不变。
var dashboardPingProbe = func(ctx context.Context) bool { return nativePing(ctx) }

var dashboardPingCache = struct {
	sync.Mutex
	result     bool
	validUntil time.Time
}{}

// dashboardInternetAlive 返回带总超时与短缓存的连通性探测结果。
// 缓存有效期内直接复用上次结果;过期后在总超时内真实探测一次并回填。
func dashboardInternetAlive(ctx context.Context) bool {
	dashboardPingCache.Lock()
	if time.Now().Before(dashboardPingCache.validUntil) {
		result := dashboardPingCache.result
		dashboardPingCache.Unlock()
		return result
	}
	dashboardPingCache.Unlock()

	pingCtx, cancel := context.WithTimeout(ctx, dashboardPingTimeout)
	defer cancel()
	result := dashboardPingProbe(pingCtx)

	dashboardPingCache.Lock()
	dashboardPingCache.result = result
	dashboardPingCache.validUntil = time.Now().Add(dashboardPingCacheTTL)
	dashboardPingCache.Unlock()
	return result
}

func resetDashboardPingCacheForTest() {
	dashboardPingCache.Lock()
	dashboardPingCache.result = false
	dashboardPingCache.validUntil = time.Time{}
	dashboardPingCache.Unlock()
}

// recordDashboardMetricsIfReady 仅在数据就绪时记录历史采样:
// 开机保护期/重启保护期 pending 数据全是默认零值,写入趋势图会
// 出现开机/重启后的"零点",必须跳过;AT 读取完全失败(error)时
// 信号/流量字段同样是零值,记入趋势会伪装成真实的信号丢失/流量归零,
// 也必须跳过。
func recordDashboardMetricsIfReady(data map[string]any) {
	if pending, _ := data["pending"].(bool); pending {
		return
	}
	if _, hasError := data["error"]; hasError {
		return
	}
	recordMetricsHistoryFromDashboard(data)
}
func parseDashboardAT(raw string) map[string]any {
	lines := atLines(raw)
	data := map[string]any{
		"sim": "未激活", "temperature": "N/A", "active_sim": "-", "network_provider": "-", "mccmnc": "-", "apn": "-",
		"network_mode": "-", "ipv4": "-", "ipv6": "-", "bands": "-", "bandwidth": "-", "csq": "-", "rssi": "-",
		"cellID": "-", "eNBID": "-", "tac": "-", "rsrqLTE": "-", "rsrqNR": "-", "rsrpLTE": "-", "rsrpNR": "-",
		"sinrLTE": "-", "sinrNR": "-", "prxqrsrp": "-", "drxqrsrp": "-", "rx2qrsrp": "-", "rx3qrsrp": "-",
		"earfcns": "-", "pcc_pci": "-", "scc_pci": "-", "signalAssessment": "-", "nr_rx_bytes": 0, "nr_tx_bytes": 0,
		"nr_rx_human": "-", "nr_tx_human": "-", "nr_dl_speed": "-", "nr_ul_speed": "-", "signalPercentage": 0,
		"rsrqLTEPercentage": 0, "rsrqNRPercentage": 0, "rsrpLTEPercentage": 0, "rsrpNRPercentage": 0, "sinrLTEPercentage": 0, "sinrNRPercentage": 0,
	}

	tempSum, tempCount := 0, 0
	qcaLines := []string{}
	qrsrpLines := []string{}
	simStatusKnown := false
	simInserted := false
	simExplicitAbsent := false
	for _, line := range lines {
		upperLine := strings.ToUpper(line)
		if strings.Contains(upperLine, "SIM NOT INSERTED") || strings.Contains(upperLine, "+CME ERROR: 10") || strings.Contains(upperLine, "+CPIN: NOT INSERTED") {
			simStatusKnown = true
			simInserted = false
		}
		switch {
		case strings.HasPrefix(line, "+QTEMP:"):
			parts := csvFields(strings.TrimPrefix(line, "+QTEMP:"))
			if len(parts) > 1 {
				v, err := strconv.Atoi(strings.Trim(parts[1], `"`))
				if err == nil && v != -273 && v != 0 {
					tempSum += v
					tempCount++
				}
			}
		case strings.HasPrefix(line, "+QSIMSTAT:"):
			parts := strings.Split(strings.TrimPrefix(line, "+QSIMSTAT:"), ",")
			if len(parts) > 1 {
				simStatusKnown = true
				simInserted = strings.TrimSpace(parts[1]) == "1"
				if simInserted {
					data["sim"] = "已激活"
				} else {
					data["sim"] = "未激活"
				}
			}
		case strings.HasPrefix(line, "+CSQ:"):
			csq := strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "+CSQ:"), ",")[0])
			// CSQ=99 表示信号强度不可知,按未知处理。
			if csq == "99" {
				csq = "-"
			}
			data["csq"] = csq
		case strings.HasPrefix(line, "+QUIMSLOT:"):
			data["active_sim"] = strings.TrimSpace(strings.TrimPrefix(line, "+QUIMSLOT:"))
		case strings.HasPrefix(line, "+QSPN:"):
			parts := csvFields(strings.TrimPrefix(line, "+QSPN:"))
			if len(parts) >= 5 {
				rawName := firstNonEmpty(parts[2], parts[0], "-")
				if len(parts) > 3 && strings.TrimSpace(parts[3]) != "0" {
					rawName = decodeMaybeUCS2(rawName)
				}
				data["network_provider"] = rawName
				data["mccmnc"] = parts[4]
			}
		case strings.HasPrefix(line, "+CGCONTRDP:"):
			parts := csvFields(strings.TrimPrefix(line, "+CGCONTRDP:"))
			if len(parts) > 2 && parts[2] != "" {
				data["apn"] = parts[2]
			}
			// +CGCONTRDP 按地址族逐行返回,第 4 列(parts[3])承载的地址本身
			// 可能是 v4 也可能是 v6(IPv6-only PDN 或 IPV4V6 未分到 v4 时该行
			// 只有 v6 地址)。旧实现无条件把 parts[3] 写进 ipv4,v6 地址会显示在
			// v4 栏位;必须先用 net.ParseIP 判定地址族再落入对应字段。
			if len(parts) > 3 && parts[3] != "" {
				if ip := net.ParseIP(parts[3]); ip != nil {
					if ip.To4() != nil {
						if parts[3] != "0.0.0.0" && stringValue(data["ipv4"]) == "-" {
							data["ipv4"] = parts[3]
							data["sim"] = "已激活"
						}
					} else if !isAllZeroV6(parts[3]) && stringValue(data["ipv6"]) == "-" {
						data["ipv6"] = parts[3]
						data["sim"] = "已激活"
					}
				}
			}
			// parts[4] 回退只接受真正的 IPv6(部分固件把 v6 地址放第 5 列);
			// 子网掩码/前缀长度/拼接的 DNS 串不是合法 IP,自然被跳过,
			// 不会再被误写进 ipv6 字段。
			if len(parts) > 4 && parts[4] != "" && !isAllZeroV6(parts[4]) && stringValue(data["ipv6"]) == "-" {
				if ip := net.ParseIP(parts[4]); ip != nil && ip.To4() == nil {
					data["ipv6"] = parts[4]
					data["sim"] = "已激活"
				}
			}
		case strings.HasPrefix(line, "+QMAP:") && strings.Contains(line, `"WWAN"`) && strings.Contains(line, `"IPV4"`):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 4 && parts[4] != "" && parts[4] != "0.0.0.0" {
				data["ipv4"] = parts[4]
				data["sim"] = "已激活"
			}
		case strings.HasPrefix(line, "+QMAP:") && strings.Contains(line, `"WWAN"`) && strings.Contains(line, `"IPV6"`):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 4 && parts[4] != "" && !isAllZeroV6(parts[4]) {
				data["ipv6"] = parts[4]
				data["sim"] = "已激活"
			}
		case strings.HasPrefix(line, "+QENG:"):
			applyQENGDashboard(line, data)
		case strings.HasPrefix(line, "+QCAINFO:"):
			qcaLines = append(qcaLines, line)
		case strings.HasPrefix(line, "+QRSRP:"):
			qrsrpLines = append(qrsrpLines, line)
		case strings.HasPrefix(line, "+QGDNRCNT:"):
			parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "+QGDNRCNT:")), ",")
			if len(parts) >= 2 {
				rx, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
				tx, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				data["nr_rx_bytes"] = rx
				data["nr_tx_bytes"] = tx
				data["nr_rx_human"] = humanBytesGo(float64(rx))
				data["nr_tx_human"] = humanBytesGo(float64(tx))
			}
		}
	}
	if tempCount > 0 {
		data["temperature"] = strconv.Itoa((tempSum + tempCount/2) / tempCount)
	}
	if simExplicitAbsent || (simStatusKnown && !simInserted) {
		applyDashboardSIMAbsent(data)
		return data
	}
	applyQCAInfoDashboard(qcaLines, data)
	applyQRSRPDashboard(qrsrpLines, data)
	bestRsrp := maxInt(percentFromAny(data["rsrpLTEPercentage"]), percentFromAny(data["rsrpNRPercentage"]))
	bestRsrq := maxInt(percentFromAny(data["rsrqLTEPercentage"]), percentFromAny(data["rsrqNRPercentage"]))
	bestSinr := maxInt(percentFromAny(data["sinrLTEPercentage"]), percentFromAny(data["sinrNRPercentage"]))
	if bestRsrp > 0 || bestRsrq > 0 || bestSinr > 0 {
		sig := weightedSignalPercent(bestRsrp, bestRsrq, bestSinr)
		data["signalPercentage"] = sig
		data["signalAssessment"] = signalQualityGo(sig)
	}
	return data
}

func applyDashboardSIMAbsent(data map[string]any) {
	reset := map[string]any{
		"sim": "未激活", "active_sim": "-", "network_provider": "-", "mccmnc": "-", "apn": "-",
		"network_mode": "未插卡", "ipv4": "-", "ipv6": "-", "bands": "-", "bandwidth": "-", "csq": "-", "rssi": "-",
		"cellID": "-", "eNBID": "-", "tac": "-", "rsrqLTE": "-", "rsrqNR": "-", "rsrpLTE": "-", "rsrpNR": "-",
		"sinrLTE": "-", "sinrNR": "-", "prxqrsrp": "-", "drxqrsrp": "-", "rx2qrsrp": "-", "rx3qrsrp": "-",
		"earfcns": "-", "pcc_pci": "-", "scc_pci": "-", "signalAssessment": "未知", "nr_rx_bytes": int64(0), "nr_tx_bytes": int64(0),
		"nr_rx_human": "-", "nr_tx_human": "-", "nr_dl_speed": "-", "nr_ul_speed": "-", "signalPercentage": 0,
		"rsrqLTEPercentage": 0, "rsrqNRPercentage": 0, "rsrpLTEPercentage": 0, "rsrpNRPercentage": 0, "sinrLTEPercentage": 0, "sinrNRPercentage": 0,
	}
	for key, value := range reset {
		data[key] = value
	}
}
func applyQENGDashboard(line string, data map[string]any) {
	p := csvFields(strings.TrimPrefix(line, "+QENG:"))
	if len(p) == 0 {
		return
	}
	kind := strings.ToUpper(p[0])
	if kind == "SERVINGCELL" {
		applyServingCellDashboard(p, data)
		return
	}
	switch kind {
	case "LTE":
		data["network_mode"] = "LTE"
		applyLTEDashboard(p, data)
	case "NR5G-NSA":
		data["network_mode"] = "NR5G-NSA"
		applyNRNSADashboard(p, data)
	case "NR5G-SA":
		data["network_mode"] = "NR5G-SA"
		applyNRSAStandaloneDashboard(p, data)
	}
}

func applyServingCellDashboard(p []string, data map[string]any) {
	if len(p) < 3 {
		return
	}
	rat := strings.ToUpper(p[2])
	duplex := ""
	if len(p) > 3 && p[3] != "" {
		duplex = " " + p[3]
	}
	switch rat {
	case "NR5G-SA":
		data["network_mode"] = "NR5G-SA" + duplex
		applyNRSACombinedDashboard(p, data)
	case "LTE":
		data["network_mode"] = "LTE" + duplex
		applyLTEServingCellDashboard(p, data)
	}
}

func applyLTEDashboard(p []string, data map[string]any) {
	if len(p) > 4 {
		setCIDFields(p[4], data)
	}
	if len(p) > 5 && p[5] != "" {
		data["pcc_pci"] = p[5]
	}
	if len(p) > 6 && p[6] != "" {
		appendDashboardList(data, "earfcns", p[6])
	}
	if len(p) > 10 && p[10] != "" {
		data["tac"] = hexWithDecimal(p[10])
	}
	if len(p) > 11 {
		data["rsrpLTE"] = p[11]
		data["rsrpLTEPercentage"] = calcRSRP(toInt(p[11]))
	}
	if len(p) > 12 {
		data["rsrqLTE"] = p[12]
		data["rsrqLTEPercentage"] = calcRSRQ(toInt(p[12]))
	}
	if len(p) > 13 {
		data["rssi"] = p[13]
	}
	if len(p) > 14 {
		data["sinrLTE"] = p[14]
		data["sinrLTEPercentage"] = calcLTESINRPercent(toInt(p[14]))
	}
}

func applyLTEServingCellDashboard(p []string, data map[string]any) {
	if len(p) > 6 {
		setCIDFields(p[6], data)
	}
	if len(p) > 7 && p[7] != "" {
		data["pcc_pci"] = p[7]
	}
	// servingcell 格式中 p[8] 才是 earfcn;p[9] 是频段号。
	if len(p) > 8 && p[8] != "" {
		appendDashboardList(data, "earfcns", p[8])
	}
	if len(p) > 12 && p[12] != "" {
		data["tac"] = hexWithDecimal(p[12])
	}
	if len(p) > 13 {
		data["rsrpLTE"] = p[13]
		data["rsrpLTEPercentage"] = calcRSRP(toInt(p[13]))
	}
	if len(p) > 14 {
		data["rsrqLTE"] = p[14]
		data["rsrqLTEPercentage"] = calcRSRQ(toInt(p[14]))
	}
	if len(p) > 15 {
		data["rssi"] = p[15]
	}
	if len(p) > 16 {
		data["sinrLTE"] = p[16]
		data["sinrLTEPercentage"] = calcLTESINRPercent(toInt(p[16]))
	}
}

func applyNRNSADashboard(p []string, data map[string]any) {
	if len(p) > 3 && p[3] != "" {
		data["pcc_pci"] = p[3]
	}
	if len(p) > 4 {
		data["rsrpNR"] = p[4]
		data["rsrpNRPercentage"] = calcRSRP(toInt(p[4]))
	}
	if len(p) > 5 {
		data["sinrNR"] = p[5]
		data["sinrNRPercentage"] = calcSINR(toInt(p[5]))
	}
	if len(p) > 6 {
		data["rsrqNR"] = p[6]
		data["rsrqNRPercentage"] = calcRSRQ(toInt(p[6]))
	}
	if len(p) > 7 && p[7] != "" {
		appendDashboardList(data, "earfcns", p[7])
	}
}

func applyNRSAStandaloneDashboard(p []string, data map[string]any) {
	if len(p) > 3 && p[3] != "" {
		data["pcc_pci"] = p[3]
	}
	if len(p) > 4 {
		data["rsrpNR"] = p[4]
		data["rsrpNRPercentage"] = calcRSRP(toInt(p[4]))
	}
	if len(p) > 5 {
		data["sinrNR"] = p[5]
		data["sinrNRPercentage"] = calcSINR(toInt(p[5]))
	}
	if len(p) > 6 {
		data["rsrqNR"] = p[6]
		data["rsrqNRPercentage"] = calcRSRQ(toInt(p[6]))
	}
	if len(p) > 7 && p[7] != "" {
		appendDashboardList(data, "earfcns", p[7])
	}
}

func applyNRSACombinedDashboard(p []string, data map[string]any) {
	if len(p) > 6 {
		setCIDFields(p[6], data)
	}
	if len(p) > 7 && p[7] != "" {
		data["pcc_pci"] = p[7]
	}
	if len(p) > 8 && p[8] != "" {
		data["tac"] = hexWithDecimal(p[8])
	}
	if len(p) > 9 && p[9] != "" {
		appendDashboardList(data, "earfcns", p[9])
	}
	if len(p) > 12 {
		data["rsrpNR"] = p[12]
		data["rsrpNRPercentage"] = calcRSRP(toInt(p[12]))
	}
	if len(p) > 13 {
		data["rsrqNR"] = p[13]
		data["rsrqNRPercentage"] = calcRSRQ(toInt(p[13]))
	}
	if len(p) > 14 {
		data["sinrNR"] = p[14]
		data["sinrNRPercentage"] = calcSINR(toInt(p[14]))
	}
}

func setCIDFields(cid string, data map[string]any) {
	cid = strings.TrimSpace(cid)
	if cid == "" || cid == "-" {
		return
	}
	if len(cid) > 2 {
		short := cid[len(cid)-2:]
		data["eNBID"] = cid[:len(cid)-2]
		data["cellID"] = fmt.Sprintf("%s(%d), %s(%d)", short, hexToInt(short), cid, hexToInt(cid))
		return
	}
	data["cellID"] = cid
}

func hexWithDecimal(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "-" {
		return "-"
	}
	return fmt.Sprintf("%s (%d)", v, hexToInt(v))
}

func hexToInt(v string) int64 {
	n, err := strconv.ParseInt(v, 16, 64)
	if err != nil {
		return 0
	}
	return n
}

func appendDashboardList(data map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return
	}
	cur := stringValue(data[key])
	if cur == "" || cur == "-" {
		data[key] = value
		return
	}
	items := strings.Split(cur, " / ")
	for _, item := range items {
		if item == value {
			return
		}
	}
	data[key] = cur + " / " + value
}

func applyQCAInfoDashboard(lines []string, data map[string]any) {
	bands, bws, earfcns, scc := []string{}, []string{}, []string{}, []string{}
	for _, line := range lines {
		p := csvFields(strings.TrimPrefix(line, "+QCAINFO:"))
		if len(p) < 4 {
			continue
		}
		typ := strings.ToUpper(p[0])
		bandRaw := strings.ToUpper(p[3])
		isNR := strings.Contains(bandRaw, "NR")
		if p[1] != "" {
			earfcns = append(earfcns, p[1])
		}
		band := shortBandName(p[3])
		if band != "" {
			bands = append(bands, band)
		}
		bw := bwMHz(p[2], isNR)
		if bw != "" && bw != "-" {
			bws = append(bws, bw)
		}
		pci := qcaPCI(p, typ, isNR)
		if typ == "PCC" && pci != "" && pci != "-" {
			data["pcc_pci"] = pci
		}
		if typ == "SCC" && pci != "" && pci != "-" {
			scc = append(scc, pci)
		}
	}
	if len(bands) > 0 {
		data["bands"] = strings.Join(bands, " / ")
	}
	if len(bws) > 0 {
		data["bandwidth"] = strings.Join(bws, " / ")
	}
	if len(earfcns) > 0 {
		data["earfcns"] = strings.Join(earfcns, " / ")
	}
	if len(scc) > 0 {
		data["scc_pci"] = strings.Join(scc, " + ")
	}
}

func qcaPCI(p []string, typ string, isNR bool) string {
	if len(p) < 5 {
		return "-"
	}
	if isNR {
		if strings.EqualFold(typ, "SCC") && len(p) > 5 {
			return p[5]
		}
		return p[4]
	}
	if len(p) > 5 {
		return p[5]
	}
	return p[4]
}

func applyQRSRPDashboard(lines []string, data map[string]any) {
	type vals struct{ prx, drx, rx2, rx3 string }
	lte, nr := vals{"-", "-", "-", "-"}, vals{"-", "-", "-", "-"}
	hasLTE, hasNR := false, false
	for _, line := range lines {
		p := csvFields(strings.TrimPrefix(line, "+QRSRP:"))
		if len(p) < 4 {
			continue
		}
		v := vals{normalizeQRSRPValue(p[0]), normalizeQRSRPValue(p[1]), normalizeQRSRPValue(p[2]), normalizeQRSRPValue(p[3])}
		if strings.Contains(line, "LTE") {
			lte = v
			hasLTE = true
		} else if strings.Contains(line, "NR5G") {
			nr = v
			hasNR = true
		}
	}
	if hasLTE && hasNR {
		data["prxqrsrp"] = lte.prx + "/" + nr.prx
		data["drxqrsrp"] = lte.drx + "/" + nr.drx
		data["rx2qrsrp"] = lte.rx2 + "/" + nr.rx2
		data["rx3qrsrp"] = lte.rx3 + "/" + nr.rx3
	} else if hasLTE {
		data["prxqrsrp"] = lte.prx
		data["drxqrsrp"] = lte.drx
		data["rx2qrsrp"] = lte.rx2
		data["rx3qrsrp"] = lte.rx3
	} else if hasNR {
		data["prxqrsrp"] = nr.prx
		data["drxqrsrp"] = nr.drx
		data["rx2qrsrp"] = nr.rx2
		data["rx3qrsrp"] = nr.rx3
	}
}

// normalizeQRSRPValue 过滤天线占位值:模块对未使用/无效天线返回
// -32768 等占位,原样显示会误导用户。-44 是 3GPP RSRP 有效上限,不视为占位。
func normalizeQRSRPValue(value string) string {
	trimmed := strings.TrimSpace(value)
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return trimmed
	}
	if v <= -32000 {
		return "-"
	}
	return trimmed
}
func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
func calcRSRP(v int) int {
	// RSRP 物理范围约 -140..-44 dBm;字段为 "-" 时 toInt 得 0,
	// 属于无效值,不能映射成 100%。
	if v <= -120 || v > -44 {
		return 0
	}
	return clamp((v + 120) * 100 / 40)
}
func calcRSRQ(v int) int {
	// RSRQ 物理范围约 -20..-3 dB;"-" 解析出的 0 视为无效。
	if v <= -20 || v > -3 {
		return 0
	}
	return clamp((v + 20) * 100 / 12)
}
func calcSINR(v int) int {
	if v <= 0 {
		return 0
	}
	return clamp(v * 100 / 20)
}

// calcLTESINRPercent 处理 LTE QENG 的 SINR 原始值:手册口径为
// 实际 dB = 原始值*2 - 20(原始 0-25 对应 -20..30 dB),再映射百分比。
func calcLTESINRPercent(raw int) int {
	return calcSINR(raw*2 - 20)
}
func weightedSignalPercent(rsrpPct, rsrqPct, sinrPct int) int {
	return clamp((rsrpPct*50 + rsrqPct*25 + sinrPct*25 + 50) / 100)
}
func signalQualityGo(p int) string {
	if p >= 80 {
		return "优秀"
	}
	if p >= 60 {
		return "良好"
	}
	if p >= 40 {
		return "一般"
	}
	if p >= 0 {
		return "差"
	}
	return "无信号"
}
func shortBandName(s string) string {
	text := strings.ToUpper(strings.TrimSpace(s))
	if m := regexp.MustCompile(`NR5G\s+BAND\s+(\d+)`).FindStringSubmatch(text); len(m) > 1 {
		return "N" + m[1]
	}
	if m := regexp.MustCompile(`NR\s+BAND\s+(\d+)`).FindStringSubmatch(text); len(m) > 1 {
		return "N" + m[1]
	}
	if m := regexp.MustCompile(`LTE\s+BAND\s+(\d+)`).FindStringSubmatch(text); len(m) > 1 {
		return "B" + m[1]
	}
	return s
}

func bwMHz(code string, nr bool) string {
	n := toInt(code)
	if nr {
		if n >= 0 && n <= 5 {
			return fmt.Sprintf("%dMHz", (n+1)*5)
		}
		if n >= 6 && n <= 12 {
			return fmt.Sprintf("%dMHz", (n-2)*10)
		}
		return "-"
	}
	m := map[int]float64{0: 1.4, 1: 3, 2: 5, 3: 10, 4: 15, 5: 20, 6: 1.4, 15: 3, 25: 5, 50: 10, 75: 15, 100: 20}
	if v, ok := m[n]; ok {
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", v), "0"), ".") + "MHz"
	}
	return "-"
}
func humanBytesGo(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for bytes >= 1024 && i < len(units)-1 {
		bytes /= 1024
		i++
	}
	if i > 0 && bytes < 10 {
		return fmt.Sprintf("%.1f %s", bytes, units[i])
	}
	return fmt.Sprintf("%.0f %s", bytes, units[i])
}
func parseUptimeParts(text string) map[string]int {
	days, hours, minutes := 0, 0, 0
	if m := regexp.MustCompile(`(\d+)\s+day`).FindStringSubmatch(text); len(m) > 1 {
		days = toInt(m[1])
	}
	if m := regexp.MustCompile(`(\d+)\s+hour`).FindStringSubmatch(text); len(m) > 1 {
		hours = toInt(m[1])
	}
	if m := regexp.MustCompile(`(\d+)\s+min`).FindStringSubmatch(text); len(m) > 1 {
		minutes = toInt(m[1])
	}
	return map[string]int{"days": days, "hours": hours, "minutes": minutes}
}
