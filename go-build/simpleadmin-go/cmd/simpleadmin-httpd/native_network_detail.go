//go:build linux
// +build linux

// native_network_detail.go 网络详情页系统层采集器(仅 Linux 模块构建):
// 接口状态读 /sys/class/net,局域网设备由租约文件与 /proc/net/arp 合并,
// 解析逻辑见 network_detail.go。
package main

import (
	"os"
	"strconv"
	"strings"
)

func readSysFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func int64FromFile(path string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(readSysFile(path)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// collectInterfaceStats 采集各接口状态:跳过 lo 与"down 且零流量"的未激活接口
// (如备用 PDN rmnet_data1~5);速率由 updateIfaceRateSample 差分得出。
func collectInterfaceStats() []map[string]any {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return []map[string]any{}
	}
	stats := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}
		base := "/sys/class/net/" + name
		mac := strings.TrimSpace(readSysFile(base + "/address"))
		mtu, _ := strconv.Atoi(strings.TrimSpace(readSysFile(base + "/mtu")))
		state := strings.TrimSpace(readSysFile(base + "/operstate"))
		rx := int64FromFile(base + "/statistics/rx_bytes")
		tx := int64FromFile(base + "/statistics/tx_bytes")
		if state == "down" && rx == 0 && tx == 0 {
			continue
		}
		rxRate, txRate := updateIfaceRateSample(name, rx, tx, ifaceNow())
		stats = append(stats, map[string]any{
			"name":    name,
			"mac":     mac,
			"state":   state,
			"mtu":     mtu,
			"rxBytes": rx,
			"txBytes": tx,
			"rxRate":  rxRate,
			"txRate":  txRate,
		})
	}
	sortInterfaceStats(stats)
	return stats
}

// collectLanClients 汇总局域网设备:租约文件 + ARP 表合并。
func collectLanClients() []map[string]any {
	now := ifaceNow()
	var leases []lanClient
	if path := findDnsmasqLeasePath(); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			leases = parseDnsmasqLeases(string(data), now)
		}
	}
	var arps []lanClient
	if data, err := os.ReadFile("/proc/net/arp"); err == nil {
		arps = parseArpTable(string(data))
	}
	merged := mergeLanClients(leases, arps, networkDetailMaxClients)
	out := make([]map[string]any, 0, len(merged))
	for _, c := range merged {
		out = append(out, map[string]any{
			"mac":          c.MAC,
			"ip":           c.IP,
			"hostname":     c.Hostname,
			"leaseSeconds": c.LeaseSeconds,
		})
	}
	return out
}
