package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

func (s *simpleAdminServer) handleDeviceInfoData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	if action == "" || action == "get" {
		force := boolQuery(r, "force", false)
		raw := s.fetchPageAT(atKeyDeviceInfo, force, true)
		data := parseDeviceInfoAT(raw)
		data["modelName"] = deviceModelName
		pending := strings.Contains(raw, atCachePendingText)
		data["pending"] = pending
		// 与其余状态端点一致:非保护期且完全拿不到数据行才算读取失败,标记 error
		// 供前端显示失败态/重试,而不是把失败当成"未就绪"无限静默等待。
		if !pending && atReadFailed(raw) {
			data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	if action == "set_imei" {
		imei := stripNonDigits(requestValue(r, "imei"))
		if len(imei) != 15 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid imei"})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+EGMR=1,7,"%s";+CFUN=1,1`, imei))
		// 改 IMEI 命令带 +CFUN=1,1,模块先重启、后应答,收不到应答/超时是常态;
		// 只有明确终结错误行才算失败,超时无应答按"重启进行中"处理
		// (回归:按 atResponseOK 判定会把运行器超时文本误报为操作失败)。
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseRebootOK(resp), "response": resp})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
}
func parseDeviceInfoAT(raw string) map[string]any {
	lines := atLines(raw)
	data := map[string]any{"manufacturer": "-", "modelName": "-", "firmwareVersion": "-", "simStatus": "未知", "simInserted": false, "imsi": "-", "iccid": "-", "imei": "-", "lanIp": "-", "wwanIpv4": "-", "wwanIpv6": "-", "phoneNumber": "-"}
	plain := []string{}
	simStatusKnown := false
	simInserted := false
	simExplicitAbsent := false
	for _, line := range lines {
		up := strings.ToUpper(line)
		if strings.Contains(up, "SIM NOT INSERTED") || strings.Contains(up, "+CME ERROR: 10") || strings.Contains(up, "+CPIN: NOT INSERTED") {
			simStatusKnown = true
			simInserted = false
			simExplicitAbsent = true
		}
		switch {
		case strings.HasPrefix(up, "+QSIMSTAT:"):
			parts := strings.Split(strings.TrimPrefix(line, "+QSIMSTAT:"), ",")
			if len(parts) > 1 {
				simStatusKnown = true
				simInserted = strings.TrimSpace(parts[1]) == "1"
				if !simInserted {
					simExplicitAbsent = true
				}
			}
		case strings.HasPrefix(up, "+CPIN:"):
			simStatusKnown = true
			// READY 表示已插卡且未锁;SIM PIN / SIM PUK 等状态同样证明 SIM 在位
			// (只是待解锁),不能判为未插卡——旧实现只认 READY,会把 PIN 锁的卡
			// 报成"未插卡"并清空 imsi/iccid 等仍可读的字段。与 smsSIMInserted
			// 口径一致:状态非空且非 NOT INSERTED 即判已插卡。
			cpinState := strings.TrimSpace(strings.TrimPrefix(up, "+CPIN:"))
			simInserted = cpinState != "" && cpinState != "NOT INSERTED"
			if !simInserted {
				simExplicitAbsent = true
			}
		case strings.HasPrefix(up, "+CGSN:"):
			if imei := extractIMEI(line); imei != "" {
				data["imei"] = imei
			}
		case strings.HasPrefix(up, "+ICCID:"):
			if iccid := strings.TrimSpace(strings.TrimPrefix(line, "+ICCID:")); iccid != "" {
				data["iccid"] = iccid
				simStatusKnown = true
				simInserted = true
			}
		case strings.HasPrefix(line, `+QMAP: "LANIP"`):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 3 {
				data["lanIp"] = parts[3]
			}
		case strings.HasPrefix(line, `+QMAP: "WWAN"`) && strings.Contains(line, `"IPV4"`):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 4 {
				data["wwanIpv4"] = parts[4]
			}
		case strings.HasPrefix(line, `+QMAP: "WWAN"`) && strings.Contains(line, `"IPV6"`):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 4 {
				data["wwanIpv6"] = parts[4]
			}
		case strings.HasPrefix(up, "+CNUM:"):
			parts := csvFields(strings.TrimPrefix(line, "+CNUM:"))
			for _, part := range parts {
				if strings.HasPrefix(part, "+") || len(stripNonDigits(part)) >= 5 {
					data["phoneNumber"] = part
					break
				}
			}
		case isPlainATValue(line):
			plain = append(plain, line)
		}
	}
	nonDigits := []string{}
	digits := []string{}
	for _, p := range plain {
		if regexp.MustCompile(`^\d{10,20}$`).MatchString(p) {
			digits = append(digits, p)
		} else {
			nonDigits = append(nonDigits, p)
		}
	}
	if len(nonDigits) > 0 {
		data["manufacturer"] = nonDigits[0]
	}
	if len(nonDigits) > 2 {
		data["modelName"] = nonDigits[1]
		data["firmwareVersion"] = nonDigits[2]
	} else if len(nonDigits) > 1 {
		data["firmwareVersion"] = nonDigits[1]
	}
	if data["imei"] == "-" {
		for _, d := range digits {
			if len(d) >= 14 && len(d) <= 17 {
				data["imei"] = d
				break
			}
		}
	}
	for _, d := range digits {
		if d != data["imei"] && len(d) >= 14 && len(d) <= 16 {
			data["imsi"] = d
			simStatusKnown = true
			simInserted = true
			break
		}
	}
	if simExplicitAbsent || (simStatusKnown && !simInserted) {
		data["simStatus"] = "未插卡"
		data["simInserted"] = false
		data["imsi"] = "-"
		data["iccid"] = "-"
		data["wwanIpv4"] = "-"
		data["wwanIpv6"] = "-"
		data["phoneNumber"] = "未插卡"
		return data
	}
	if simInserted {
		data["simStatus"] = "已插卡"
		data["simInserted"] = true
		if stringValue(data["phoneNumber"]) == "-" {
			data["phoneNumber"] = "无本机号码"
		}
	}
	return data
}
