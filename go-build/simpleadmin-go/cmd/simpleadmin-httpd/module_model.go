package main

import (
	"net/http"
)

// 本项目仅适配 RG520N-CN,型号硬编码,不做动态识别
// (实机 AT+CGMM 恒返回 RG520N-CN,AT+QGETCAPABILITY 不支持)。
const deviceModelName = "RG520N-CN"

func nativeOrMockUptime(mock bool) string {
	if mock {
		return mockUptimeText()
	}
	return nativeUptimeText()
}

// currentDeviceModel 返回本机适配的模块型号。
func currentDeviceModel() string {
	return deviceModelName
}

// handleModuleModel 保留 /api/module_model 端点(前端 brand/deviceinfo 仍调用),
// 型号直接返回硬编码值,永远不 pending。
func (s *simpleAdminServer) handleModuleModel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"model": deviceModelName, "pending": false})
}
