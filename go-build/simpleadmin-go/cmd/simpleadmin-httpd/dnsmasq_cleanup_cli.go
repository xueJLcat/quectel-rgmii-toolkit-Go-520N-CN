package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runDnsmasqCleanupCommand 清理本服务引入的全部 dnsmasq 自定义,供卸载脚本调用:
// ① 逐条删除经本服务下发的静态绑定并重载;② 移除厂商配置中的标记块;
// ③ 删除状态文件与包含文件;④ 标记块有变动时重启单元恢复原状。
// 非 Linux 环境无 dnsmasq,打印提示后直接返回。
func runDnsmasqCleanupCommand(args []string) {
	if runtime.GOOS != "linux" {
		fmt.Println("dnsmasq 清理仅支持 Linux 设备环境")
		return
	}

	log.Printf("开始清理 dnsmasq 自定义配置")

	// AT 通道初始化与 at 子命令一致:优先读取设备列表配置文件,为空回退缺省候选。
	runtimeATDevices = collectATDeviceCandidates("", defaultATDevicesFile)
	if len(runtimeATDevices) == 0 {
		runtimeATDevices = defaultATDeviceCandidates()
	}

	deleted := cleanupMacBindBindings()
	markerRemoved, markerRemaining := cleanupDnsmasqMarker()
	cleanupDnsmasqStateFiles(markerRemaining)

	if markerRemoved {
		log.Printf("标记块已移除,重启 %s 使厂商配置恢复原状", dnsmasqUnitName)
		if err := restartDnsmasqUnit(); err != nil {
			log.Printf("警告: 重启 %s 失败: %v", dnsmasqUnitName, err)
		}
	}

	log.Printf("dnsmasq 自定义配置清理完成: 删除静态绑定 %d 条, 标记块移除=%v, 标记块残留=%v", deleted, markerRemoved, markerRemaining)
}

// cleanupMacBindBindings 只删除经本服务下发的静态绑定:取状态文件记录与固件
// 实时列表的交集(按 MAC 比较,忽略大小写),不经手其他途径建立的绑定;删除后
// 重载 dnsmasq 使变更生效。状态文件读取失败或 AT 查询失败仅警告,不中断后续
// 文件清理。返回成功删除条数。
func cleanupMacBindBindings() int {
	owned, err := readMacBindState()
	if err != nil {
		log.Printf("警告: 读取静态绑定状态失败,跳过绑定清理: %v", err)
		return 0
	}
	if len(owned) == 0 {
		log.Printf("无本服务下发的绑定记录,跳过绑定清理")
		return 0
	}
	ownedMACs := make(map[string]bool, len(owned))
	for _, entry := range owned {
		ownedMACs[strings.ToUpper(entry.MAC)] = true
	}

	query := `AT+QMAP="MAC_bind"`
	out, err := runATCommandUntilDone(query, 200, atCommandTimeoutMS(query))
	if err != nil {
		log.Printf("警告: 查询静态绑定失败,跳过绑定清理: %v", err)
		return 0
	}
	if strings.TrimSpace(out) == "" || !atResponseOK(out) {
		log.Printf("警告: 静态绑定查询响应异常,跳过绑定清理: %s", outputSummary(out))
		return 0
	}
	entries := parseMacBindAT(out)
	if len(entries) == 0 {
		log.Printf("固件无静态绑定,无需清理")
		return 0
	}
	deleted, matched := 0, 0
	for _, entry := range entries {
		if !ownedMACs[strings.ToUpper(entry.MAC)] {
			// 不在本服务状态文件中的绑定由其他途径建立,保留不动。
			continue
		}
		matched++
		command := fmt.Sprintf(`AT+QMAP="MAC_bind",%d,"",""`, entry.Index)
		resp, delErr := runATCommandUntilDone(command, 200, atSingleCommandTimeoutMS(command))
		if delErr != nil || !atResponseOK(resp) {
			log.Printf("警告: 删除静态绑定槽位 %d(%s -> %s)失败: %v", entry.Index, entry.MAC, entry.IP, delErr)
			continue
		}
		deleted++
	}
	log.Printf("已删除本服务下发的静态绑定 %d/%d 条(固件共 %d 条)", deleted, matched, len(entries))
	if deleted > 0 {
		if err := reloadDnsmasqHosts(); err != nil {
			log.Printf("警告: 重载 dnsmasq 静态租约失败: %v", err)
		}
	}
	return deleted
}

// cleanupDnsmasqMarker 移除厂商配置文件末尾的上游 DNS 标记块,写回时保留原文件
// 权限;文件不存在或无标记时静默跳过。返回值:是否实际移除了标记块;标记块是否
// 仍被认为残留在厂商配置中(读写失败无法确认时为 true,调用方据此保留包含目录,
// 否则厂商配置仍指向它,删除后 dnsmasq 下次重启会因缺失 conf-file 目标而失败)。
func cleanupDnsmasqMarker() (removed bool, markerRemaining bool) {
	data, err := os.ReadFile(dnsmasqMainConfPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("警告: 读取厂商 dnsmasq 配置 %s 失败: %v", dnsmasqMainConfPath, err)
			// 读取失败无法确认标记块是否存在,保守视为仍残留。
			return false, true
		}
		log.Printf("厂商 dnsmasq 配置 %s 不存在,跳过标记块清理", dnsmasqMainConfPath)
		return false, false
	}
	if !strings.Contains(string(data), dnsmasqMarkerBegin) {
		log.Printf("厂商 dnsmasq 配置无标记块,无需清理")
		return false, false
	}
	// 保留厂商文件原有权限,写回时维持一致(同 ensureDnsmasqMarker)。
	perm := os.FileMode(0644)
	if info, statErr := os.Stat(dnsmasqMainConfPath); statErr == nil {
		perm = info.Mode().Perm()
	}
	cleaned := removeDNSMarkerBlock(string(data))
	if strings.Contains(cleaned, dnsmasqMarkerBegin) {
		// 只有起始标记的残留被 removeDNSMarkerBlock 保守保留,从起始标记行
		// 截断到文件末尾(块总由本服务追加在末尾,见 truncateDNSMarkerTail)。
		cleaned = truncateDNSMarkerTail(cleaned)
	}
	if cleaned == string(data) {
		log.Printf("警告: 厂商 dnsmasq 配置 %s 中的标记块未能移除", dnsmasqMainConfPath)
		return false, true
	}
	if err := atomicWriteFile(dnsmasqMainConfPath, []byte(cleaned), perm); err != nil {
		log.Printf("警告: 写回厂商 dnsmasq 配置 %s 失败: %v", dnsmasqMainConfPath, err)
		return false, true
	}
	log.Printf("已移除厂商 dnsmasq 配置中的标记块")
	return true, false
}

// cleanupDnsmasqStateFiles 删除上游 DNS 与静态租约状态文件、备份目录及包含目录;
// 路径不存在时忽略。标记块移除失败时保留包含目录,其余照常删除。
func cleanupDnsmasqStateFiles(markerRemaining bool) {
	for _, path := range []string{runtimeDNSUpstreamStateFile, runtimeMacBindStateFile} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("警告: 删除状态文件 %s 失败: %v", path, err)
		}
	}
	dirs := []string{filepath.Dir(dnsmasqConfBackupPath)}
	if markerRemaining {
		// 厂商配置仍指向包含文件,删除包含目录会导致 dnsmasq 下次重启失败。
		log.Printf("警告: 标记块未能移除,保留包含目录 %s", dnsmasqIncludeDir)
	} else {
		dirs = append(dirs, dnsmasqIncludeDir)
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("警告: 删除目录 %s 失败: %v", dir, err)
		}
	}
	log.Printf("状态文件与包含目录清理完成")
}
