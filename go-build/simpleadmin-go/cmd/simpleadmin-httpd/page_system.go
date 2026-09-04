// page_system.go 提供系统设置页的 /api/system_data 端点。
package main

import (
	"fmt"
	"net/http"
	"strings"
)

// systemPowerAction 执行设备整机重启/关机,可在测试中替换为假实现。
var systemPowerAction = nativeDevicePower

func (s *simpleAdminServer) handleSystemData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "", "status":
		raw := s.fetchPageAT(atKeySystemStatus, boolQuery(r, "force", false), true)
		data := parseSystemStatusAT(raw)
		// 开机保护期/后台未就绪时返回默认值会误导用户,标记 pending 供前端重试。
		pending := strings.Contains(raw, atCachePendingText)
		data["pending"] = pending
		// 与其余状态端点一致:非保护期且完全拿不到数据行才算读取失败,标记 error
		// 供前端显示失败态;否则 AT 通道持续故障时设置页会永久静默显示旧 IMEI。
		if !pending && atReadFailed(raw) {
			data["error"] = "AT 数据读取失败，请检查模块或稍后重试"
		}
		writeJSON(w, http.StatusOK, data)
	case "set_imei":
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
	case "reboot":
		resp := s.runPageAction("AT+CFUN=1,1")
		// 重启类命令判定同 set_imei,见 atResponseRebootOK 注释。
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseRebootOK(resp), "response": resp})
	case "reboot_device", "poweroff_device":
		// 整机重启/关机:执行系统 reboot / poweroff 命令。设备随即开始停机,
		// 命令本身不等待完成;只有命令无法启动才判定失败。
		if s.cfg.mockMode {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mock": true})
			return
		}
		powerAction := powerActionReboot
		if action == "poweroff_device" {
			powerAction = powerActionPoweroff
		}
		if err := systemPowerAction(powerAction); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}
func parseSystemStatusAT(raw string) map[string]any {
	lines := atLines(raw)
	out := map[string]any{"imei": "-"}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "+CGSN:"):
			if imei := extractIMEI(line); imei != "" {
				out["imei"] = imei
			}
		case isPlainATValue(line):
			if out["imei"] == "-" {
				if imei := extractIMEI(line); imei != "" {
					out["imei"] = imei
				}
			}
		}
	}
	return out
}
