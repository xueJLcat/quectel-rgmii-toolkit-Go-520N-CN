package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *simpleAdminServer) handleNetworkData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "", "settings":
		raw := s.fetchPageAT(atKeyNetworkSettings, boolQuery(r, "force", false), true)
		data := parseNetworkSettingsAT(raw)
		// 开机保护期/后台未就绪时返回默认值会误导用户,标记 pending 供前端重试。
		pending := strings.Contains(raw, atCachePendingText)
		data["pending"] = pending
		if !pending && atReadFailed(raw) {
			data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
		}
		writeJSON(w, http.StatusOK, data)
	case "model":
		writeJSON(w, http.StatusOK, map[string]any{"model": deviceModelName, "pending": false})
	case "bands":
		force := boolQuery(r, "force", false)
		wait := boolQuery(r, "wait", true)
		mode := strings.ToUpper(strings.TrimSpace(requestValue(r, "mode")))
		if mode != "" {
			command := networkBandQueryCommand(mode)
			if command == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid mode"})
				return
			}
			raw := s.fetchATCommand(command, force, wait)
			writeJSON(w, http.StatusOK, networkBandsResponse(raw))
			return
		}
		raw := s.fetchPageAT(atKeyNetworkBands, force, wait)
		writeJSON(w, http.StatusOK, networkBandsResponse(raw))
	case "scan":
		mode := requestValue(r, "mode")
		command := scanModeCommand(mode)
		if command == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid scan mode"})
			return
		}
		raw := s.runPageAction(command)
		// 本固件 QSCAN 三模式均不返回结果(已移出接口),扫描仅保留邻区扫描;
		// 响应是 +QNWCFG/+QENG 测量行,没有 +QSCAN 行,固定用专用解析器。
		result := parseNeighbourCellAT(raw)
		if strings.Contains(raw, atCachePendingText) {
			result["pending"] = true
		}
		if atReadFailed(raw) {
			result["ok"] = false
			result["error"] = "扫描失败：AT 无有效应答，请检查模块或稍后重试"
		} else {
			result["ok"] = true
			// 固件(如 RG520NCNAAR02A02M4G_XM)的 QSCAN 会返回 OK 但无任何结果行,
			// 此时"扫描成功但零结果"必须单独标记,前端据此显示固件受限提示,
			// 而不是误导性的"未扫描到小区"。
			if cellScanEmpty(result) {
				result["empty"] = true
			}
		}
		writeJSON(w, http.StatusOK, result)
	case "lock_scanned_cells":
		resp, err := s.lockScannedCells(r)
		if err != nil {
			// 参数校验失败→400;运行时失败(如 SCS 守护锁定探测失败)→200+ok:false,
			// 与 lock_nr_manual/lock_serving_cell 对同类失败的口径保持一致。
			status := http.StatusBadRequest
			if _, isRuntime := err.(*networkActionRuntimeError); isRuntime {
				status = http.StatusOK
			}
			writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "lock_bands":
		mode := strings.ToUpper(strings.TrimSpace(requestValue(r, "mode")))
		values := strings.TrimSpace(requestValue(r, "values"))
		if values == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing values"})
			return
		}
		target := map[string]string{"LTE": "lte_band", "NSA": "nsa_nr5g_band", "SA": "nr5g_band"}[mode]
		if target == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid mode"})
			return
		}
		// 清洗后为空或只剩冒号分隔符说明输入不含任何有效频段号
		// (如 "abc"/"###"/"::")。Quectel 空值语义是恢复全部频段,用户意图
		// 锁频却会静默清空所有频段锁,必须拒绝并返回明确错误。
		bands := safeBandList(values)
		if strings.Trim(bands, ":") == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid band list"})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QNWPREFCFG="%s",%s`, target, bands))
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "reset_bands":
		// 恢复全部频段锁:与 lock_bands 不同,此处空值是设计语义——
		// Quectel 空值即恢复全部频段,正是该动作(前端二次确认"恢复全部")
		// 的目的,因此不做清洗后判空拒绝。
		lte := safeBandList(requestValue(r, "lte"))
		nsa := safeBandList(requestValue(r, "nsa"))
		sa := safeBandList(requestValue(r, "sa"))
		resp := s.runPageAction(fmt.Sprintf(`AT+QNWPREFCFG="lte_band",%s;+QNWPREFCFG= "nsa_nr5g_band",%s;+QNWPREFCFG= "nr5g_band",%s`, lte, nsa, sa))
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "save_settings":
		resp, err := s.saveNetworkSettings(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "lock_lte_manual":
		resp, err := s.lockLTEManual(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "lock_nr_manual":
		pci := stripNonDigits(requestValue(r, "pci"))
		earfcn := stripNonDigits(requestValue(r, "earfcn"))
		scs := stripNonDigits(requestValue(r, "scs"))
		band := stripNonDigits(requestValue(r, "band"))
		if scs == "" {
			scs = inferNRSCSFromBand(band)
		}
		if pci == "" || earfcn == "" || scs == "" || band == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing nr lock parameters"})
			return
		}
		// 本固件 QENG servingcell 的 scs 字段恒为 "-",无法预读;猜错 scs 会把设备
		// 钉死在无法同步的小区造成断网(本次实机事故),即使用户指定单一 SCS,
		// 也必须走带注册校验、失败自动解锁的守护锁定。
		resp, _, ok := s.lockNR5GCellGuarded(pci, earfcn, band, []string{scs})
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "response": resp})
	case "lock_serving_cell":
		// 锁定当前驻留小区:先读服务小区信息确定制式/频点/PCI,
		// NR5G-SA 走守护锁定(防 scs 猜错断网),LTE 直接下发 4g 锁定。
		cell := parseServingCellAT(s.runPageAction(`AT+QENG="servingcell"`))
		switch cell["rat"] {
		case "NR5G-SA":
			candidates := []string{"30", "15"}
			if scs := stripNonDigits(requestValue(r, "scs")); scs != "" {
				candidates = []string{scs}
			}
			resp, scsUsed, ok := s.lockNR5GCellGuarded(cell["pci"], cell["freq"], cell["band"], candidates)
			if !ok {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "response": resp, "locked": "NR5G-SA", "scs": scsUsed, "pci": cell["pci"], "freq": cell["freq"], "band": cell["band"]})
		case "LTE":
			resp := s.runPageAction(fmt.Sprintf(`AT+QNWLOCK="common/4g",1,%s,%s`, cell["freq"], cell["pci"]))
			writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp, "locked": "LTE", "pci": cell["pci"], "freq": cell["freq"]})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "未驻留可锁定的小区"})
		}
	case "unlock_lte":
		resp := s.runPageAction(`AT+QNWLOCK="common/4g",0`)
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "unlock_nr":
		resp := s.runPageAction(`AT+QNWLOCK="common/5g",0`)
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "earfcn_lock_status":
		raw := s.runPageAction(`AT+QNWCFG="nr5g_earfcn_lock";+QNWCFG="lte_earfcn_lock"`)
		nr, lte := parseEarfcnLockAT(raw)
		data := map[string]any{"nr5g_arfcns": nr, "lte_arfcns": lte}
		switch {
		case strings.Contains(raw, atCachePendingText):
			// 开机/模块重启保护期:占位文本不是设备状态证据,标记 pending 供前端
			// 重试;保持 ok:true,避免被前端误判为读取失败。
			data["ok"] = true
			data["pending"] = true
		case !hasEarfcnLockDataLine(raw):
			// 查询类失败绝不返回 ok:true:运行器超时/全设备失败/两条子命令均只回
			// +CME ERROR 时无任何 +QNWCFG: 数据行,若按空列表落 ok:true,前端会把
			// 一次读取失败渲染成确定的"频点锁未启用"。固件即使未启用锁定也会回
			// 数量为 0 的 +QNWCFG: 数据行,故以数据行存在与否判定读取成败;尾部
			// ERROR 的部分成功(一制式有数据行)仍按成功解析。
			data["ok"] = false
			data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
		default:
			data["ok"] = true
		}
		writeJSON(w, http.StatusOK, data)
	case "set_earfcn_lock":
		// 频点锁写入。实机验证:lte_earfcn_lock 写入/查询/清零均可用;
		// nr5g_earfcn_lock 写入在本固件返回 ERROR(查询可用),故失败时提示固件可能不支持。
		rat := strings.ToUpper(strings.TrimSpace(requestValue(r, "rat")))
		var param string
		var limit int
		switch rat {
		case "NR5G":
			param, limit = "nr5g_earfcn_lock", 32
		case "LTE":
			param, limit = "lte_earfcn_lock", 2
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid rat"})
			return
		}
		arfcns := []string{}
		for _, item := range strings.Split(requestValue(r, "arfcns"), ",") {
			if item = strings.TrimSpace(item); item != "" {
				arfcns = append(arfcns, item)
			}
		}
		if len(arfcns) == 0 || len(arfcns) > limit {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid arfcn list"})
			return
		}
		for _, arfcn := range arfcns {
			if stripNonDigits(arfcn) != arfcn {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid arfcn list"})
				return
			}
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QNWCFG="%s",%d,%s`, param, len(arfcns), strings.Join(arfcns, ",")))
		data := map[string]any{"ok": atResponseOK(resp), "response": resp}
		if !atResponseOK(resp) {
			data["error"] = "该固件可能不支持此频点锁"
		}
		writeJSON(w, http.StatusOK, data)
	case "clear_earfcn_lock":
		rat := strings.ToUpper(strings.TrimSpace(requestValue(r, "rat")))
		var param string
		switch rat {
		case "NR5G":
			param = "nr5g_earfcn_lock"
		case "LTE":
			param = "lte_earfcn_lock"
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid rat"})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QNWCFG="%s",0`, param))
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}
func networkBandQueryCommand(mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "LTE":
		return `AT+QNWPREFCFG="lte_band"`
	case "NSA":
		return `AT+QNWPREFCFG="nsa_nr5g_band"`
	case "SA":
		return `AT+QNWPREFCFG="nr5g_band"`
	default:
		return ""
	}
}

func networkBandsResponse(raw string) map[string]any {
	data := parseNetworkBandsAT(raw)
	if strings.Contains(raw, atCachePendingText) {
		data["pending"] = true
	}
	if atReadErrorText(raw) {
		data["error"] = strings.TrimSpace(raw)
	}
	return data
}

func parseNetworkBandsAT(raw string) map[string]any {
	locked := map[string]string{"lte_band": "", "nsa_nr5g_band": "", "nr5g_band": ""}
	re := regexp.MustCompile(`"([^"]+)"\s*,\s*([0-9:]+)`)
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		locked[m[1]] = m[2]
	}
	return map[string]any{"locked_lte_bands": locked["lte_band"], "locked_nsa_bands": locked["nsa_nr5g_band"], "locked_sa_bands": locked["nr5g_band"]}
}

func atReadErrorText(raw string) bool {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" || strings.Contains(raw, atCachePendingText) {
		return false
	}
	return strings.Contains(text, "timeout waiting") || strings.Contains(text, "failed") || strings.Contains(text, "error")
}

// atReadFailed 判定本次读取是否完全失败:非保护期、原始响应里没有任何
// 以 "+" 开头的模块数据行,且响应为空或呈错误文本特征。用于区分
// "AT 读取失败"(返回 error)与"无卡/未驻网"(仍有部分数据行,不报 error)。
func atReadFailed(raw string) bool {
	if strings.Contains(raw, atCachePendingText) {
		return false
	}
	for _, line := range atLines(raw) {
		if strings.HasPrefix(line, "+") {
			return false
		}
	}
	trimmed := strings.TrimSpace(raw)
	return trimmed == "" || atReadErrorText(raw)
}

func parseNetworkSettingsAT(raw string) map[string]any {
	lines := atLines(raw)
	out := map[string]any{"sim": "-", "apn": "-", "cellLockStatus": "未锁定", "prefNetwork": "-", "nrModeControl": "未禁用", "nrModeControlNum": "0", "bands": "-", "pdpType": "-"}
	lock4g, lock5g := "0", "0"
	bandItems := []struct {
		typ, band string
		idx       int
	}{}
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+QUIMSLOT:"):
			out["sim"] = strings.TrimSpace(strings.TrimPrefix(line, "+QUIMSLOT:"))
		case strings.HasPrefix(line, "+CGCONTRDP:"):
			parts := csvFields(strings.TrimPrefix(line, "+CGCONTRDP:"))
			if len(parts) > 2 && parts[2] != "" {
				out["apn"] = parts[2]
			}
		case strings.HasPrefix(line, "+CGDCONT:"):
			parts := csvFields(strings.TrimPrefix(line, "+CGDCONT:"))
			// 只认 cid=1 的上下文;子串 "1," 会误匹配其它上下文
			// (如 sos 上下文的标志位 ",0,0,0,1,")。
			if len(parts) == 0 || parts[0] != "1" {
				continue
			}
			if len(parts) > 1 {
				out["pdpType"] = parts[1]
			}
			if len(parts) > 2 && stringValue(out["apn"]) == "-" {
				out["apn"] = parts[2]
			}
		case strings.HasPrefix(line, `+QNWLOCK: "common/4g"`):
			if m := regexp.MustCompile(`,\s*([0-9]+)`).FindStringSubmatch(line); len(m) > 1 {
				lock4g = m[1]
			}
		case strings.HasPrefix(line, `+QNWLOCK: "common/5g"`):
			if m := regexp.MustCompile(`,\s*([0-9]+)`).FindStringSubmatch(line); len(m) > 1 {
				lock5g = m[1]
			}
		case strings.HasPrefix(line, `+QNWPREFCFG: "mode_pref"`):
			parts := csvFields(strings.TrimPrefix(line, "+QNWPREFCFG:"))
			if len(parts) > 1 {
				out["prefNetwork"] = parts[1]
			}
		case strings.HasPrefix(line, `+QNWPREFCFG: "nr5g_disable_mode"`):
			parts := csvFields(strings.TrimPrefix(line, "+QNWPREFCFG:"))
			if len(parts) > 1 {
				out["nrModeControlNum"] = parts[1]
			}
		case strings.HasPrefix(line, "+QCAINFO:"):
			parts := csvFields(strings.TrimPrefix(line, "+QCAINFO:"))
			if len(parts) > 3 {
				bandItems = append(bandItems, struct {
					typ, band string
					idx       int
				}{strings.ToUpper(parts[0]), parts[3], i})
			}
		}
	}
	if lock4g != "0" && lock5g != "0" {
		out["cellLockStatus"] = "已锁定4G和5G"
	} else if lock4g != "0" {
		out["cellLockStatus"] = "已锁定4G"
	} else if lock5g != "0" {
		out["cellLockStatus"] = "已锁定5G"
	}
	switch out["nrModeControlNum"] {
	case "1":
		out["nrModeControl"] = "禁用SA"
	case "2":
		out["nrModeControl"] = "禁用NSA"
	}
	sort.SliceStable(bandItems, func(i, j int) bool { return bandOrder(bandItems[i].typ) < bandOrder(bandItems[j].typ) })
	bands := []string{}
	for _, item := range bandItems {
		if item.band != "" {
			bands = append(bands, item.band)
		}
	}
	if len(bands) > 0 {
		out["bands"] = strings.Join(bands, " / ")
	}
	return out
}

// cellScanEmpty 判定扫描结果解析出的小区总数是否为零。
// 固件(如 RG520NCNAAR02A02M4G_XM)的 QSCAN 会返回 OK 但无任何结果行,
// 前端据此显示固件受限提示而非误导性的"未扫描到小区"。
// 字段类型断言失败时按 0 个小区处理。
func cellScanEmpty(result map[string]any) bool {
	nr, _ := result["nr5g_cells_parsed"].([]map[string]string)
	lte, _ := result["lte_cells_parsed"].([]map[string]string)
	return len(nr)+len(lte) == 0
}

// parseNeighbourCellAT 解析邻区扫描(模式 "Neighbour Scan")的原始响应,
// 返回键为 nr5g_cells_parsed/lte_cells_parsed 的 map。
// NR 邻区来自 `+QNWCFG: "nr5g_meas_info",<序号>,<NR-ARFCN>,<PCI>,<RSRP>,<RSRQ>`;
// LTE 邻区来自 `+QENG: "neighbourcell intra/inter","LTE",<earfcn>,<PCID>,<RSRQ>,<RSRP>,...`
// (3GPP/厂商手册 13 字段格式,本机驻留 NR5G-SA 时为空)。
// 实机验证于电信定制 RG520N-CN;固件 AT+QSCAN 存在不返回小区的已知缺陷。
func parseNeighbourCellAT(raw string) map[string]any {
	result := map[string]any{"nr5g_cells_parsed": []map[string]string{}, "lte_cells_parsed": []map[string]string{}}
	nr := []map[string]string{}
	lte := []map[string]string{}
	for _, line := range atLines(raw) {
		switch {
		case strings.Contains(line, `+QNWCFG: "nr5g_meas_info"`):
			// 行首为 `+QNWCFG: "nr5g_meas_info",...`,去掉前缀后按逗号切分:
			// parts[0]="nr5g_meas_info",1=序号,2=NR-ARFCN,3=PCI,4=RSRP,5=RSRQ。
			parts := csvFields(strings.TrimPrefix(line, "+QNWCFG:"))
			if len(parts) < 6 || parts[0] != "nr5g_meas_info" {
				continue
			}
			// meas_info 不含频段号,用 NR-ARFCN→band 表推导供锁小区参数使用。
			arfcn := parts[2]
			nr = append(nr, map[string]string{"type": "NR5G", "provider": "", "band": nrARFCNToBand(arfcn), "freq": arfcn, "pci": parts[3], "rsrp": parts[4]})
		case strings.Contains(line, `"neighbourcell intra","LTE"`), strings.Contains(line, `"neighbourcell inter","LTE"`):
			// 去掉 `+QENG:` 前缀后:
			// parts[0]="neighbourcell intra/inter",1="LTE",2=earfcn,3=PCID,4=RSRQ,5=RSRP。
			parts := csvFields(strings.TrimPrefix(line, "+QENG:"))
			if len(parts) < 6 {
				continue
			}
			lte = append(lte, map[string]string{"type": "LTE", "provider": "", "band": "-", "freq": parts[2], "pci": parts[3], "rsrp": parts[5]})
		}
	}
	result["nr5g_cells_parsed"] = nr
	result["lte_cells_parsed"] = lte
	return result
}

// nrARFCNToBand 由下行 NR-ARFCN(NREF)推导频段。数据源:3GPP 38.104
// Table 5.4.2.3-1 各频段下行频点的 NR-ARFCN 范围(闭区间,实测值)。
// 多个频段区间存在重叠,按本表给定的优先级顺序取首匹配
// (如 428910 同时落在 1 与 66 区间,优先判 1)。
// 推导结果仅供锁小区命令的 band 参数使用,不代表精确频段归属。
// 非法/空输入或不在任何区间时返回 "-"。
func nrARFCNToBand(arfcn string) string {
	n, err := strconv.Atoi(strings.TrimSpace(arfcn))
	if err != nil {
		return "-"
	}
	ranges := []struct {
		band     string
		min, max int
	}{
		{"1", 422000, 434000},
		{"66", 422000, 440000},
		{"2", 386000, 398000},
		{"25", 386000, 399000},
		{"3", 361000, 376000},
		{"39", 376000, 384000},
		{"5", 173800, 178800},
		{"26", 171800, 178800},
		{"8", 185000, 192000},
		{"12", 145800, 149200},
		{"28", 151600, 160600},
		{"20", 158200, 164200},
		{"71", 123400, 130400},
		{"70", 399000, 404000},
		{"34", 402000, 405000},
		{"38", 514000, 524000},
		{"40", 460000, 480000},
		{"41", 499200, 537999},
		{"7", 524000, 538000},
		{"75", 286400, 303400},
		{"76", 285400, 286400},
		{"78", 620000, 653333},
		{"77", 620000, 680000},
		{"79", 693334, 733333},
	}
	for _, r := range ranges {
		if n >= r.min && n <= r.max {
			return r.band
		}
	}
	return "-"
}

// scanModeCommand 返回扫描模式对应的 AT 命令。本固件(电信定制 RG520N-CN)
// AT+QSCAN 三模式均不返回任何小区(已知缺陷,已从接口移除),
// 仅保留邻区扫描,改走实测可用的测量信息读取:
// +QNWCFG nr5g_meas_info 提供 NR 邻区,+QENG="neighbourcell" 提供 LTE 邻区。
// 未知模式返回 "",由调用方走 400 路径。
func scanModeCommand(mode string) string {
	switch mode {
	case "Neighbour Scan":
		return `AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"`
	default:
		return ""
	}
}

// parseServingCellAT 解析 AT+QENG="servingcell" 原始响应中的
// `+QENG: "servingcell"` 行。字段索引取自实机抓取的驻网行布局:
// TrimPrefix "+QENG:" 并 csvFields 后,
// parts[0]="servingcell"、parts[2]=制式、parts[7]=PCI、
// NR5G-SA 的 parts[9]=频点(NREF)/parts[10]=频段、LTE 的 parts[8]=EARFCN。
// scs 字段(parts[16])在本机固件恒为 "-"(不可用),仅当其为纯数字时返回。
// 未驻网/残缺行只返回制式;无任何可用行时返回 {rat:"UNKNOWN"}。
func parseServingCellAT(raw string) map[string]string {
	for _, line := range atLines(raw) {
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+QENG:"))
		if len(parts) == 0 || parts[0] != "servingcell" {
			continue
		}
		rat := "UNKNOWN"
		if len(parts) > 2 && parts[2] != "" {
			rat = parts[2]
		}
		switch rat {
		case "NR5G-SA":
			if len(parts) >= 11 {
				cell := map[string]string{"rat": rat, "pci": parts[7], "freq": parts[9], "band": parts[10], "scs": ""}
				if len(parts) > 16 && parts[16] != "" && stripNonDigits(parts[16]) == parts[16] {
					cell["scs"] = parts[16]
				}
				return cell
			}
		case "LTE":
			if len(parts) >= 9 {
				return map[string]string{"rat": rat, "pci": parts[7], "freq": parts[8]}
			}
		}
		return map[string]string{"rat": rat}
	}
	return map[string]string{"rat": "UNKNOWN"}
}

// c5gregRegistered 判定 AT+C5GREG? 响应是否已注册网络:
// 行形如 `+C5GREG: 0,<stat>`,stat 为 1(本地)或 5(漫游)视为已注册。
func c5gregRegistered(resp string) bool {
	for _, line := range atLines(resp) {
		if !strings.HasPrefix(line, "+C5GREG:") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+C5GREG:"))
		if len(parts) >= 2 && (parts[1] == "1" || parts[1] == "5") {
			return true
		}
	}
	return false
}

// NR5G 守护锁定的等待时长。运行期值:锁定后等 8 秒让固件重新同步并
// 注册目标小区再校验;失败解锁后等 3 秒给固件留出退出失步状态的时间。
// 抽为包级变量仅为测试可调小,避免单测真实等待 8 秒。
var (
	nr5gLockSettleDelay = 8 * time.Second
	nr5gLockVerifyDelay = 3 * time.Second
)

// cellLockMu 把守护锁定的"下发锁定 → 等待注册 → 校验 → 失败解锁 → 下一
// 候选"多步事务整体互斥:AT 全局文件锁只保证单条命令原子,并发锁定请求
// (WS 网关每条消息独立协程分发,前端重试/多管理员同时操作都会触发)会在
// 8 秒校验窗口内交错——A 校验读到的注册状态可能是 B 刚下发的锁造成的
// (误报成功),A 失败解锁会把 B 刚设的锁一并解掉(误解锁)。与
// macBindMu/dnsUpstreamMu 的事务互斥设计对齐。
var cellLockMu sync.Mutex

// lockNR5GCellGuarded 按候选 SCS 逐个尝试锁定 NR5G 小区,锁定成功后
// 必须校验注册状态,未注册立即自动解锁——防止 scs 不匹配把设备钉死在
// 无法同步的小区导致断网(本次实机事故:band 1 小区实际 30kHz,
// 猜 15 即断网;且本机 QENG servingcell 的 scs 字段恒为 "-",无法预读,
// 因此所有 NR5G 小区锁都必须带此保护)。整个候选序列持 cellLockMu 串行,
// 等待窗口内其他锁定请求不得插入(见 cellLockMu 注释)。
// 成功返回 (锁定响应, 所用 scs, true);全部候选失败返回 ("", "", false)。
func (s *simpleAdminServer) lockNR5GCellGuarded(pci, arfcn, band string, scsCandidates []string) (resp string, scsUsed string, ok bool) {
	cellLockMu.Lock()
	defer cellLockMu.Unlock()
	for _, scs := range scsCandidates {
		lockResp := s.runPageAction(fmt.Sprintf(`AT+QNWLOCK="common/5g",%s,%s,%s,%s`, pci, arfcn, scs, band))
		if !atResponseOK(lockResp) {
			continue
		}
		// 固件接受锁定后需要时间重新同步并注册目标小区。
		time.Sleep(nr5gLockSettleDelay)
		if c5gregRegistered(s.runPageAction("AT+C5GREG?")) {
			return lockResp, scs, true
		}
		// 未注册:大概率 scs 不匹配,设备正钉在无法同步的小区上,
		// 必须立即解锁恢复搜网。固件锁失效后解锁命令可能报
		// +CME ERROR: 904,2 秒后重试一次。
		unlockResp := s.runPageAction(`AT+QNWLOCK="common/5g",0`)
		if strings.Contains(unlockResp, "CME ERROR") {
			time.Sleep(2 * time.Second)
			s.runPageAction(`AT+QNWLOCK="common/5g",0`)
		}
		// 给固件留出退出失步状态的时间,再尝试下一个候选。
		time.Sleep(nr5gLockVerifyDelay)
	}
	return "", "", false
}

// parseEarfcnLockAT 解析频点锁查询响应:
// `+QNWCFG: "nr5g_earfcn_lock",<数量>[,<频点>...]` 与
// `+QNWCFG: "lte_earfcn_lock",...`(TrimPrefix "+QNWCFG:" 后 csvFields,
// parts[0] 判类型、parts[1] 为锁定数量、其余为频点)。
// 实机:启用返回 `+QNWCFG: "lte_earfcn_lock",1,1650`,
// 清零后返回 `+QNWCFG: "lte_earfcn_lock",0`。数量 0 返回空切片。
func parseEarfcnLockAT(raw string) (nr []string, lte []string) {
	nr = []string{}
	lte = []string{}
	for _, line := range atLines(raw) {
		if !strings.HasPrefix(line, "+QNWCFG:") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+QNWCFG:"))
		if len(parts) < 2 || (parts[0] != "nr5g_earfcn_lock" && parts[0] != "lte_earfcn_lock") {
			continue
		}
		count, err := strconv.Atoi(parts[1])
		if err != nil || count <= 0 {
			continue
		}
		arfcns := parts[2:]
		if len(arfcns) > count {
			arfcns = arfcns[:count]
		}
		if parts[0] == "nr5g_earfcn_lock" {
			nr = append(nr, arfcns...)
		} else {
			lte = append(lte, arfcns...)
		}
	}
	return nr, lte
}

// hasEarfcnLockDataLine 判定原始应答是否含至少一条 earfcn_lock 的 +QNWCFG:
// 数据行(数量 0 的禁用态行也算)。固件查询成功必回数据行,完全读取失败
// (超时/全设备失败/双子命令均 +CME ERROR)则没有;不能用 atReadFailed 判定——
// +CME ERROR: 行同样以 "+" 开头会被误判为"非失败"。
func hasEarfcnLockDataLine(raw string) bool {
	for _, line := range atLines(raw) {
		if !strings.HasPrefix(line, "+QNWCFG:") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+QNWCFG:"))
		if len(parts) >= 1 && (parts[0] == "nr5g_earfcn_lock" || parts[0] == "lte_earfcn_lock") {
			return true
		}
	}
	return false
}

// inferNRSCSFromBand 在扫描结果未提供 SCS 时按频段推断子载波间隔。
// FDD 低频段普遍为 15kHz,中高频 TDD 为 30kHz;未知频段回退 30。
// 写死 30 会导致 15kHz 小区(如 n1/n28)锁频参数不匹配而失败。
func inferNRSCSFromBand(band string) string {
	switch strings.TrimSpace(band) {
	case "1", "3", "5", "8", "12", "13", "14", "18", "20", "26", "28", "66", "71", "85", "89", "94":
		return "15"
	default:
		return "30"
	}
}

// networkActionRuntimeError 表示网络锁定动作执行期的运行时失败(区别于参数
// 校验失败),供 handler 以 200+ok:false 上报(契约:运行时失败不用 400)。
type networkActionRuntimeError struct{ msg string }

func (e *networkActionRuntimeError) Error() string { return e.msg }

func (s *simpleAdminServer) lockScannedCells(r *http.Request) (string, error) {
	mode := requestValue(r, "mode")
	if mode == "NR5G Only" {
		pci := stripNonDigits(requestValue(r, "pci"))
		earfcn := stripNonDigits(requestValue(r, "earfcn"))
		scs := stripNonDigits(requestValue(r, "scs"))
		band := stripNonDigits(requestValue(r, "band"))
		if pci == "" || earfcn == "" || band == "" {
			return "", fmt.Errorf("missing nr cell parameters")
		}
		// scs 无法预读(本机 QENG servingcell 的 scs 恒为 "-"),猜错会断网:
		// 请求带 scs 时单候选,否则按 30/15 顺序探测,全程带注册校验与自动解锁。
		candidates := []string{"30", "15"}
		if scs != "" {
			candidates = []string{scs}
		}
		resp, _, ok := s.lockNR5GCellGuarded(pci, earfcn, band, candidates)
		if !ok {
			return "", &networkActionRuntimeError{"SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试"}
		}
		return resp, nil
	}
	if mode == "LTE Only" {
		// 每个参数值都按逗号无条件展开:多值与分隔串混用(earfcn=1,2&
		// earfcn=3)时旧实现不拆分 "1,2",被 stripNonDigits 洗成 "12" 后
		// 静默锁定错误频点——错值落在合法 EARFCN 范围内时最危险,设备可能
		// 钉死在无小区频点上断网。
		earfcns := splitRequestListValues(requestValues(r, "earfcn"), ",")
		pcis := splitRequestListValues(requestValues(r, "pci"), ",")
		if len(earfcns) == 0 || len(earfcns) != len(pcis) || len(earfcns) > 10 {
			return "", fmt.Errorf("invalid lte cell parameters")
		}
		parts := []string{strconv.Itoa(len(earfcns))}
		for i := range earfcns {
			earfcn := stripNonDigits(earfcns[i])
			pci := stripNonDigits(pcis[i])
			// 拆分后的空段必须拒绝:旧实现直接拼接,向固件下发
			// `...,2,1850,1,,2` 形态的畸形命令(NR5G 路径与 lockLTEManual
			// 均有判空,LTE Alone 此前遗漏)。
			if earfcn == "" || pci == "" {
				return "", fmt.Errorf("invalid lte cell parameters")
			}
			parts = append(parts, earfcn, pci)
		}
		return s.runPageAction(`AT+QNWLOCK="common/4g",` + strings.Join(parts, ",")), nil
	}
	return "", fmt.Errorf("invalid scan mode")
}

func (s *simpleAdminServer) saveNetworkSettings(r *http.Request) (string, error) {
	commands, err := networkSettingsCommands(requestValue(r, "pdpType"), requestValue(r, "apn"), requestValue(r, "modePref"), requestValue(r, "nrDisableMode"))
	if err != nil {
		return "", err
	}
	if len(commands) == 0 {
		return "", fmt.Errorf("no changes")
	}
	return s.runPageAction("AT" + strings.Join(commands, ";")), nil
}

// apnMaxLength 是 APN 长度上限(Quectel 模块 CGDCONT 允许的 APN 长度)。
const apnMaxLength = 100

// networkSettingsCommands 拼装保存网络设置的写命令序列。
// APN 为空时跳过 CGDCONT 重写,保留模块当前 APN(前端仅改 PDP 类型时提交空 APN);
// APN 非空时必须是合法字符集([A-Za-z0-9._-])且不超过 apnMaxLength:非法输入
// 明确拒绝而不是静默剥离——旧实现把全非法字符(如 "###")清洗成空 APN 后照样
// 下发 +CGDCONT=1,"IPV4V6",""(空 APN 写入固件,PDP 激活异常),含空格/中文的
// APN 被无声改写,用户无从得知;
// 其余参数(mode_pref/nr5g_disable_mode)照常处理。
func networkSettingsCommands(pdpType, apn, modePref, nrDisableMode string) ([]string, error) {
	commands := []string{}
	pdp := strings.ToUpper(strings.TrimSpace(pdpType))
	apn = strings.TrimSpace(apn)
	if apn != "" {
		cleaned := sanitizeAPN(apn)
		if cleaned != apn || cleaned == "" || len(cleaned) > apnMaxLength {
			return nil, fmt.Errorf("invalid apn")
		}
		if pdp == "" {
			pdp = "IPV4V6"
		}
		if !map[string]bool{"IP": true, "IPV6": true, "IPV4V6": true}[pdp] {
			return nil, fmt.Errorf("invalid pdp type")
		}
		commands = append(commands, fmt.Sprintf(`+CGDCONT=1,"%s","%s"`, pdp, cleaned))
	}
	mode := strings.TrimSpace(modePref)
	if mode != "" {
		commands = append(commands, fmt.Sprintf(`+QNWPREFCFG="mode_pref",%s`, sanitizeModePref(mode)))
	}
	nrMode := stripNonDigits(nrDisableMode)
	if nrMode != "" {
		commands = append(commands, fmt.Sprintf(`+QNWPREFCFG="nr5g_disable_mode",%s`, nrMode))
	}
	return commands, nil
}

func (s *simpleAdminServer) lockLTEManual(r *http.Request) (string, error) {
	cellNum := stripNonDigits(requestValue(r, "cellNum"))
	// 每个参数值都按分号无条件展开:多值与分隔符混用时旧实现不拆分,
	// "1850,1;1900" 的尾段被 stripNonDigits 洗成 (1850, 11900),
	// 静默锁定错误小区。
	values := splitRequestListValues(requestValues(r, "pairs"), ";")
	pairs := []string{}
	for _, p := range values {
		fields := strings.Split(p, ",")
		if len(fields) != 2 {
			continue
		}
		earfcn := stripNonDigits(fields[0])
		pci := stripNonDigits(fields[1])
		if earfcn != "" && pci != "" {
			pairs = append(pairs, earfcn, pci)
		}
	}
	if cellNum == "" {
		cellNum = strconv.Itoa(len(pairs) / 2)
	}
	if len(pairs) == 0 {
		return "", fmt.Errorf("missing lte lock pairs")
	}
	// 有效参数对数必须与 cellNum 一致,否则生成参数数量不匹配的 AT+QNWLOCK。
	if want, err := strconv.Atoi(cellNum); err != nil || want*2 != len(pairs) {
		return "", fmt.Errorf("invalid cell parameters")
	}
	return s.runPageAction(fmt.Sprintf(`AT+QNWLOCK="common/4g",%s,%s`, cellNum, strings.Join(pairs, ","))), nil
}
func safeBandList(v string) string { return regexp.MustCompile(`[^0-9:]`).ReplaceAllString(v, "") }
func sanitizeModePref(v string) string {
	return regexp.MustCompile(`[^A-Za-z0-9_+:]`).ReplaceAllString(v, "")
}
func bandOrder(typ string) int {
	if typ == "PCC" {
		return 0
	}
	if typ == "SCC" {
		return 1
	}
	return 2
}

func networkName(mcc, mnc string) string {
	names := map[string]string{"46000": "中国移动", "46001": "中国联通", "46003": "中国电信", "46009": "中国联通", "46011": "中国电信", "46015": "中国广电", "46020": "中国铁通"}
	if v := names[mcc+mnc]; v != "" {
		return v
	}
	return strings.TrimSpace(mcc + " " + mnc)
}
