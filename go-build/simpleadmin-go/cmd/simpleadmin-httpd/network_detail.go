// network_detail.go 网络详情页的平台无关部分:采样状态、纯解析函数
// (租约/ARP/合并/排序)与租约文件路径发现;系统层采集器在
// native_network_detail*.go 中按平台实现。
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// networkDetailMaxClients 设备列表条数上限,防御异常大文件。
	networkDetailMaxClients = 64
	// networkDetailMinSampleInterval 两次采样最小间隔,低于该间隔沿用上次速率,
	// 避免毫秒级重复请求产生巨大瞬时速率。
	networkDetailMinSampleInterval = 500 * time.Millisecond
)

// ifaceSample 单个接口的上次采样,用于差分计算当前速率。
type ifaceSample struct {
	rxBytes int64
	txBytes int64
	rxRate  int64
	txRate  int64
	at      time.Time
}

var (
	ifaceRateMu   sync.Mutex
	ifaceRateLast = map[string]ifaceSample{}
)

// ifaceNow 采样时钟,声明为 var 便于测试替换。
var ifaceNow = time.Now

func resetIfaceRateSamplesForTest() {
	ifaceRateMu.Lock()
	ifaceRateLast = map[string]ifaceSample{}
	ifaceRateMu.Unlock()
}

func isMACAddress(s string) bool {
	if len(s) != 17 {
		return false
	}
	for i := 0; i < 17; i++ {
		c := s[i]
		if i%3 == 2 {
			if c != ':' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// clampRate 把字节差值换算为每秒速率;计数回绕/接口重建导致的负差值按 0 处理。
func clampRate(delta int64, dt float64) int64 {
	if delta < 0 || dt <= 0 {
		return 0
	}
	return int64(float64(delta) / dt)
}

// updateIfaceRateSample 记录本次采样并返回当前速率:距上次采样不足最小间隔时
// 沿用上次速率,首次采样速率为 0。
func updateIfaceRateSample(name string, rx, tx int64, now time.Time) (int64, int64) {
	ifaceRateMu.Lock()
	defer ifaceRateMu.Unlock()
	prev, ok := ifaceRateLast[name]
	sample := ifaceSample{rxBytes: rx, txBytes: tx, at: now}
	if ok {
		if dt := now.Sub(prev.at); dt >= networkDetailMinSampleInterval {
			sample.rxRate = clampRate(rx-prev.rxBytes, dt.Seconds())
			sample.txRate = clampRate(tx-prev.txBytes, dt.Seconds())
		} else {
			sample.rxRate = prev.rxRate
			sample.txRate = prev.txRate
		}
	}
	ifaceRateLast[name] = sample
	return sample.rxRate, sample.txRate
}

// interfaceDisplayPriority 常用接口固定靠前:WAN、LAN 桥、物理网口,其余按名称。
func interfaceDisplayPriority(name string) int {
	switch name {
	case "rmnet_data0":
		return 0
	case "bridge0":
		return 1
	case "eth0":
		return 2
	default:
		return 3
	}
}

func sortInterfaceStats(stats []map[string]any) {
	sort.SliceStable(stats, func(i, j int) bool {
		ni, _ := stats[i]["name"].(string)
		nj, _ := stats[j]["name"].(string)
		pi, pj := interfaceDisplayPriority(ni), interfaceDisplayPriority(nj)
		if pi != pj {
			return pi < pj
		}
		return ni < nj
	})
}

// lanClient 局域网设备条目;LeaseSeconds 为剩余租约秒数,-1 表示来自 ARP(无租约)。
type lanClient struct {
	MAC          string
	IP           string
	Hostname     string
	LeaseSeconds int64
}

var (
	// dnsmasqLeaseConfPaths 扫描顺序即配置加载链:桥接入口 → 厂商主配置。
	dnsmasqLeaseConfPaths = []string{dnsmasqBridgeConfPath, dnsmasqMainConfPath}
	// dnsmasqLeaseCandidates 配置中未显式指定租约文件时的常见缺省路径。
	dnsmasqLeaseCandidates = []string{
		"/var/run/dnsmasq.leases",
		"/var/run/data/dnsmasq.leases",
		"/var/lib/misc/dnsmasq.leases",
		"/tmp/dnsmasq.leases",
	}
)

// dnsmasqLeasePathFromFile 从单个 conf 内容中提取 dhcp-leasefile 路径,
// 注释行忽略;未找到返回空串。
func dnsmasqLeasePathFromFile(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		value, ok := strings.CutPrefix(line, "dhcp-leasefile=")
		if !ok {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// findDnsmasqLeasePath 发现租约文件路径:先扫描配置链与包含目录中的
// dhcp-leasefile 指令,再回退常见缺省路径;均未命中返回空串。
func findDnsmasqLeasePath() string {
	paths := append([]string{}, dnsmasqLeaseConfPaths...)
	if entries, err := os.ReadDir(dnsmasqIncludeDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				paths = append(paths, filepath.Join(dnsmasqIncludeDir, entry.Name()))
			}
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if value := dnsmasqLeasePathFromFile(string(data)); value != "" {
			return value
		}
	}
	for _, candidate := range dnsmasqLeaseCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// parseDnsmasqLeases 解析 dnsmasq 租约文件内容,行格式:
// `<到期时间戳> <MAC> <IP> <主机名> <客户端标识>`。
// 跳过:字段不足、时间戳非法、已到期、标识非 MAC 的 IPv6(DUID)行;
// 主机名 `*` 归一为空串。
func parseDnsmasqLeases(content string, now time.Time) []lanClient {
	clients := []lanClient{}
	for _, raw := range strings.Split(content, "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 4 {
			continue
		}
		expiry, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		remaining := expiry - now.Unix()
		if remaining <= 0 {
			continue
		}
		mac := strings.ToUpper(fields[1])
		if !isMACAddress(mac) {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		clients = append(clients, lanClient{MAC: mac, IP: fields[2], Hostname: hostname, LeaseSeconds: remaining})
	}
	return clients
}

// lanArpDevices 局域网侧接口:ARP 条目仅保留这些设备上的邻居,
// 排除 WAN 口的运营商侧条目。
var lanArpDevices = map[string]bool{"bridge0": true, "eth0": true}

// parseArpTable 解析 /proc/net/arp(首行表头,六列:IP 类型 标志 MAC 掩码 设备):
// 跳过标志为 0(未完成解析)、MAC 全零、非 LAN 接口与残缺行。
func parseArpTable(content string) []lanClient {
	clients := []lanClient{}
	for i, raw := range strings.Split(content, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) < 6 {
			continue
		}
		if fields[2] == "0x0" {
			continue
		}
		mac := strings.ToUpper(fields[3])
		if !isMACAddress(mac) || mac == "00:00:00:00:00:00" {
			continue
		}
		if !lanArpDevices[fields[5]] {
			continue
		}
		clients = append(clients, lanClient{MAC: mac, IP: fields[0], LeaseSeconds: -1})
	}
	return clients
}

func parseIPv4Octets(ip string) []int {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return nil
	}
	octets := make([]int, 4)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return nil
		}
		octets[i] = n
	}
	return octets
}

// compareIPv4 IPv4 按数值逐段比较(保证 .9 排在 .10 前);非 IPv4 回退字符串比较。
func compareIPv4(a, b string) bool {
	pa, pb := parseIPv4Octets(a), parseIPv4Octets(b)
	if pa != nil && pb != nil {
		for i := range pa {
			if pa[i] != pb[i] {
				return pa[i] < pb[i]
			}
		}
		return false
	}
	if pa != nil {
		return true
	}
	if pb != nil {
		return false
	}
	return a < b
}

// mergeLanClients 租约优先合并 ARP 条目:与租约 IP 或 MAC 重复的 ARP 条目丢弃,
// 其余按静态设备(租约 -1)追加;按 IP 排序并截断到 limit。
func mergeLanClients(leases, arps []lanClient, limit int) []lanClient {
	out := append([]lanClient{}, leases...)
	knownIP := make(map[string]bool, len(leases))
	knownMAC := make(map[string]bool, len(leases))
	for _, c := range leases {
		knownIP[c.IP] = true
		knownMAC[c.MAC] = true
	}
	for _, c := range arps {
		if knownIP[c.IP] || knownMAC[c.MAC] {
			continue
		}
		out = append(out, c)
		knownIP[c.IP] = true
		knownMAC[c.MAC] = true
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareIPv4(out[i].IP, out[j].IP)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
