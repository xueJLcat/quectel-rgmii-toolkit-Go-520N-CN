// system_monitor.go 提供系统监控页的 /api/system_monitor 端点:
// CPU/内存占用卡片数据 + 进程 Top20(按 CPU 或内存排序)。
// 进程 CPU 占用按两次采样 /proc/[pid]/stat 的时钟节拍增量计算(类 htop)。
package main

import (
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	sortProcessByCPU   = "cpu"
	sortProcessByMem   = "mem"
	systemMonitorTopN  = 20
	systemMonitorDelay = 300 * time.Millisecond
)

// processInfo 进程 Top 列表条目。
type processInfo struct {
	PID        int     `json:"pid"`
	User       string  `json:"user"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	CPUPercent float64 `json:"cpuPercent"`
	MemPercent float64 `json:"memPercent"`
	RSSHuman   string  `json:"rssHuman"`
}

// processSample /proc/[pid]/stat 单次采样。
type processSample struct {
	name       string
	state      string
	totalTicks uint64
	rssPages   int64
}

func (s *simpleAdminServer) handleSystemMonitor(w http.ResponseWriter, r *http.Request) {
	sortBy := strings.TrimSpace(requestValue(r, "sort"))
	if sortBy != sortProcessByMem {
		sortBy = sortProcessByCPU
	}
	writeJSON(w, http.StatusOK, systemMonitorData(s.cfg.mockMode, sortBy))
}

func systemMonitorData(mock bool, sortBy string) map[string]any {
	if mock {
		return mockSystemMonitorData(sortBy)
	}
	ramPercent, ramUsedKiB, ramTotalKiB := currentRAMUsage()
	processes, processCount := topProcesses(sortBy, systemMonitorTopN)
	return map[string]any{
		"cpuUsagePercent": currentCPUUsagePercent(),
		"ramUsagePercent": ramPercent,
		"ramUsedHuman":    humanBytesFromKiB(ramUsedKiB),
		"ramTotalHuman":   humanBytesFromKiB(ramTotalKiB),
		"loadAverage":     readLoadAverage(),
		"uptime":          nativeUptimeText(),
		"processCount":    processCount,
		"processes":       processes,
	}
}

// readLoadAverage 读取 /proc/loadavg 的三个负载值,失败返回 "-"。
func readLoadAverage() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "-"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "-"
	}
	return strings.Join(fields[:3], " ")
}

// parseProcStatLine 解析 /proc/[pid]/stat 一行:进程名取首个 '(' 与末个 ')'
// 之间的内容(容忍名字内含空格/括号);其后字段 state 为第 3 字段,
// utime/stime 为第 14/15 字段, rss(页数)为第 24 字段。
func parseProcStatLine(data string) (processSample, bool) {
	open := strings.IndexByte(data, '(')
	closed := strings.LastIndexByte(data, ')')
	if open < 0 || closed <= open {
		return processSample{}, false
	}
	rest := strings.Fields(data[closed+1:])
	// rest[0]=state(字段3);字段 N 对应 rest[N-3]:utime=rest[11]、
	// stime=rest[12]、rss=rest[21]。
	if len(rest) < 22 {
		return processSample{}, false
	}
	utime, err1 := strconv.ParseUint(rest[11], 10, 64)
	stime, err2 := strconv.ParseUint(rest[12], 10, 64)
	rss, err3 := strconv.ParseInt(rest[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || rss < 0 {
		return processSample{}, false
	}
	return processSample{
		name:       data[open+1 : closed],
		state:      rest[0],
		totalTicks: utime + stime,
		rssPages:   rss,
	}, true
}

// sampleProcesses 采样 /proc 下全部进程的时钟节拍与内存页数;
// 采样瞬间已退出的进程跳过。
func sampleProcesses() map[int]processSample {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	samples := make(map[int]processSample, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		if sample, ok := parseProcStatLine(string(data)); ok {
			samples[pid] = sample
		}
	}
	return samples
}

// parsePasswdUsers 解析 /etc/passwd 内容得到 uid→用户名映射(同 uid 取首条)。
func parsePasswdUsers(content string) map[int]string {
	users := map[int]string{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		if _, exists := users[uid]; !exists {
			users[uid] = fields[0]
		}
	}
	return users
}

// procRealUID 从 /proc/[pid]/status 读取进程真实 UID。
func procRealUID(pid int) (int, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, false
		}
		return uid, true
	}
	return 0, false
}

// sortProcessList 按 sortBy(cpu/mem)降序排列,次键互为对方的降序,
// 再按 PID 升序保证结果稳定。
func sortProcessList(list []processInfo, sortBy string) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if sortBy == sortProcessByMem {
			if a.MemPercent != b.MemPercent {
				return a.MemPercent > b.MemPercent
			}
			if a.CPUPercent != b.CPUPercent {
				return a.CPUPercent > b.CPUPercent
			}
			return a.PID < b.PID
		}
		if a.CPUPercent != b.CPUPercent {
			return a.CPUPercent > b.CPUPercent
		}
		if a.MemPercent != b.MemPercent {
			return a.MemPercent > b.MemPercent
		}
		return a.PID < b.PID
	})
}

// osClockTicksPerSecond 返回 /proc 进程时钟节拍频率(USER_HZ)。Linux 用户态
// ABI 恒为 100(与内核 CONFIG_HZ 无关),进程 CPU 占用按此换算。
func osClockTicksPerSecond() float64 {
	return 100
}

func roundOneDecimal(value float64) float64 {
	return math.Round(value*10) / 10
}

// topProcesses 两次采样计算进程 CPU 占用,返回排序后的前 limit 个进程
// 与采样到的进程总数。
func topProcesses(sortBy string, limit int) ([]processInfo, int) {
	first := sampleProcesses()
	start := time.Now()
	time.Sleep(systemMonitorDelay)
	second := sampleProcesses()
	elapsed := time.Since(start).Seconds()
	hz := osClockTicksPerSecond()

	_, _, ramTotalKiB := currentRAMUsage()
	pageKiB := int64(os.Getpagesize()) / 1024

	users := map[int]string{}
	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		users = parsePasswdUsers(string(data))
	}

	infos := make([]processInfo, 0, len(second))
	for pid, sample := range second {
		info := processInfo{PID: pid, Name: sample.name, State: sample.state, User: "-"}
		if prev, ok := first[pid]; ok && elapsed > 0 && hz > 0 && sample.totalTicks >= prev.totalTicks {
			delta := sample.totalTicks - prev.totalTicks
			info.CPUPercent = roundOneDecimal(100 * float64(delta) / (elapsed * hz))
		}
		rssKiB := sample.rssPages * pageKiB
		if rssKiB <= 0 {
			info.RSSHuman = "0 KB"
		} else {
			info.RSSHuman = humanBytesFromKiB(rssKiB)
		}
		if ramTotalKiB > 0 {
			info.MemPercent = roundOneDecimal(float64(rssKiB) * 100 / float64(ramTotalKiB))
		}
		if uid, ok := procRealUID(pid); ok {
			if name, known := users[uid]; known {
				info.User = name
			} else {
				info.User = strconv.Itoa(uid)
			}
		}
		infos = append(infos, info)
	}

	sortProcessList(infos, sortBy)
	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}
	return infos, len(second)
}

// mockSystemMonitorData mock 模式返回固定的系统监控数据(不读 /proc)。
func mockSystemMonitorData(sortBy string) map[string]any {
	processes := []processInfo{
		{PID: 1421, User: "root", Name: "QCMAP_Client", State: "S", CPUPercent: 9.4, MemPercent: 4.2, RSSHuman: "86.0 MB"},
		{PID: 812, User: "root", Name: "quectel-qmi", State: "S", CPUPercent: 6.1, MemPercent: 2.1, RSSHuman: "43.0 MB"},
		{PID: 1988, User: "root", Name: "simpleadmin-htt", State: "S", CPUPercent: 4.7, MemPercent: 3.5, RSSHuman: "71.7 MB"},
		{PID: 655, User: "root", Name: "netmgrd", State: "S", CPUPercent: 3.2, MemPercent: 2.8, RSSHuman: "57.3 MB"},
		{PID: 1750, User: "nobody", Name: "dnsmasq", State: "S", CPUPercent: 1.8, MemPercent: 1.1, RSSHuman: "22.5 MB"},
		{PID: 1, User: "root", Name: "systemd", State: "S", CPUPercent: 0.6, MemPercent: 1.9, RSSHuman: "38.9 MB"},
		{PID: 2104, User: "root", Name: "smd_reader", State: "S", CPUPercent: 0.5, MemPercent: 0.6, RSSHuman: "12.3 MB"},
		{PID: 933, User: "root", Name: "modem_mgr", State: "S", CPUPercent: 0.4, MemPercent: 1.6, RSSHuman: "32.8 MB"},
		{PID: 305, User: "root", Name: "kworker/0:1", State: "I", CPUPercent: 0.3, MemPercent: 0, RSSHuman: "0 KB"},
		{PID: 478, User: "root", Name: "usbnetd", State: "S", CPUPercent: 0.3, MemPercent: 0.8, RSSHuman: "16.4 MB"},
		{PID: 1201, User: "root", Name: "ntp-sync", State: "S", CPUPercent: 0.2, MemPercent: 0.4, RSSHuman: "8.2 MB"},
		{PID: 589, User: "root", Name: "dropbear", State: "S", CPUPercent: 0.1, MemPercent: 0.5, RSSHuman: "10.2 MB"},
		{PID: 2210, User: "root", Name: "at_proxy", State: "S", CPUPercent: 0.1, MemPercent: 0.3, RSSHuman: "6.1 MB"},
		{PID: 2, User: "root", Name: "kthreadd", State: "S", CPUPercent: 0, MemPercent: 0, RSSHuman: "0 KB"},
		{PID: 311, User: "root", Name: "ksoftirqd/0", State: "S", CPUPercent: 0, MemPercent: 0, RSSHuman: "0 KB"},
		{PID: 700, User: "root", Name: "logd", State: "S", CPUPercent: 0, MemPercent: 0.7, RSSHuman: "14.3 MB"},
		{PID: 866, User: "root", Name: "thermald", State: "S", CPUPercent: 0, MemPercent: 0.5, RSSHuman: "10.2 MB"},
		{PID: 1033, User: "root", Name: "diagd", State: "S", CPUPercent: 0, MemPercent: 0.9, RSSHuman: "18.4 MB"},
		{PID: 1544, User: "root", Name: "bridged", State: "S", CPUPercent: 0, MemPercent: 0.6, RSSHuman: "12.3 MB"},
		{PID: 1677, User: "root", Name: "firewalld", State: "S", CPUPercent: 0, MemPercent: 0.4, RSSHuman: "8.2 MB"},
		{PID: 2305, User: "root", Name: "udhcpc", State: "S", CPUPercent: 0, MemPercent: 0.2, RSSHuman: "4.1 MB"},
		{PID: 2418, User: "root", Name: "sleep", State: "S", CPUPercent: 0, MemPercent: 0.1, RSSHuman: "2.0 MB"},
	}
	// processCount 与真实路径同义:采样到的进程总数(截断前)。
	processCount := len(processes)
	sortProcessList(processes, sortBy)
	if len(processes) > systemMonitorTopN {
		processes = processes[:systemMonitorTopN]
	}
	return map[string]any{
		"cpuUsagePercent": 36,
		"ramUsagePercent": 58,
		"ramUsedHuman":    "1.2 GB",
		"ramTotalHuman":   "2.0 GB",
		"loadAverage":     "0.42 0.35 0.28",
		"uptime":          "up 3 day, 4 hour, 12 min",
		"processCount":    processCount,
		"processes":       processes,
	}
}
