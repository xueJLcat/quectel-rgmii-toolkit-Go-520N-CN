// page_at_data.go 提供 AT 命令页的 /api/at_data 端点。
package main

import (
	"net/http"
	"strings"
)

func (s *simpleAdminServer) handleATData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "manual_at":
		// AT 终端是用户手动输入场景，保留显式 AT 调试能力；普通页面状态/动作均不再提交完整 AT。
		command := sanitizeATCommand(requestValue(r, "command"))
		if command == "" {
			command = "ATI"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "response": s.runPageAction(command)})
	case "reset_at":
		resp := s.runPageAction("AT&F")
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}
