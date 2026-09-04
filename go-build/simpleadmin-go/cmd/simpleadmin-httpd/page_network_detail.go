// page_network_detail.go 提供网络详情页 /api/network_detail 端点:
// WAN/LAN 地址(复用现有 AT 解析)、接口状态(系统层采集)与局域网设备
// (dnsmasq 租约 + ARP 合并,见 native_network_detail*.go)。
package main

import (
	"net/http"
	"strings"
)

func (s *simpleAdminServer) handleNetworkDetail(w http.ResponseWriter, r *http.Request) {
	force := boolQuery(r, "force", false)

	// WAN 地址复用总览页的 AT 解析(共享缓存,成本极低);
	// LAN 网关复用网络设置页的 LANIP 记录解析。
	dashRaw := s.fetchPageAT(atKeyDashboard, force, true)
	dash := parseDashboardAT(dashRaw)
	cfgRaw := s.fetchPageAT(atKeyNetworkConfigStatus, force, true)
	cfg := parseNetworkConfigStatusAT(cfgRaw)

	// 开机保护期/后台未就绪时任一数据源 pending 即整体 pending,前端按重试处理。
	pending := strings.Contains(dashRaw, atCachePendingText) || strings.Contains(cfgRaw, atCachePendingText)

	lanGateway := stringValue(cfg["lanGwIp"])
	if lanGateway == "" {
		lanGateway = "-"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending":    pending,
		"wanIPv4":    stringValue(dash["ipv4"]),
		"wanIPv6":    stringValue(dash["ipv6"]),
		"lanGateway": lanGateway,
		"interfaces": collectInterfaceStats(),
		"clients":    collectLanClients(),
	})
}
