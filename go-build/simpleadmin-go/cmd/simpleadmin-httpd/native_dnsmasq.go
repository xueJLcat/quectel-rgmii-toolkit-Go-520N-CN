//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	dnsmasqCommand   = "dnsmasq"
	systemctlCommand = "systemctl"
	killallCommand   = "killall"
)

// dnsUpstreamMu 保护 applyDNSUpstream"取旧内容快照→写包含文件→注入标记→校验→
// 重启,失败回滚"的整体互斥,防止并发请求交错:该函数可被页面保存处理器与开机
// 自愈协程并发触发,也可被两个并发保存触发;交错时一方失败回滚(写回自己入口读
// 到的旧内容)会覆盖另一方刚写入的新包含文件。与 macBindMu 同一意图:锁加在
// applyDNSUpstream 自身内部,一次性覆盖所有调用点(页面保存与开机自愈),
// 调用方无需各自加锁,避免遗漏。锁内包含回滚;锁外无状态操作。持锁横跨外部
// 命令(语法校验与重启可能数秒)是必要的:事务必须整体成功或整体回滚,
// 半途放开锁会让快照与写入结果不再一一对应。
// 并发语义说明:applyDNSUpstream 的文件路径为常量、外部命令为直接 exec,
// 无可注入桩,无法在单测中真实并发调用,串行性由本锁保证。
var dnsUpstreamMu sync.Mutex

// runDnsmasqCommand 执行 dnsmasq 自身命令(如 --test 语法校验),15 秒超时,
// 返回合并输出与带命令上下文的错误。
func runDnsmasqCommand(args ...string) (string, error) {
	return runDnsmasqExecCommand(dnsmasqCommand, args...)
}

// runSystemctlCommand 执行 systemctl 服务管理命令,15 秒超时,
// 返回合并输出与带命令上下文的错误。
func runSystemctlCommand(args ...string) (string, error) {
	return runDnsmasqExecCommand(systemctlCommand, args...)
}

// runDnsmasqExecCommand 是 dnsmasq 相关外部命令的底层封装:统一 15 秒超时,
// 失败时错误里附带命令与输出摘要,便于排障。
func runDnsmasqExecCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("%s %s 执行失败: %v: %s", name, strings.Join(args, " "), err, text)
	}
	return text, nil
}

// reloadDnsmasqHosts 让 dnsmasq 重读 dhcp-hostsfile(静态租约):厂商单元的
// ExecReload 即 killall -HUP dnsmasq,SIGHUP 不会重读配置链,因此安全且不中断上游
// DNS 设置;systemctl reload 失败时按厂商语义回退到直接发 HUP。
func reloadDnsmasqHosts() error {
	if _, err := runSystemctlCommand("reload", dnsmasqUnitName); err != nil {
		log.Printf("systemctl reload %s 失败,回退到 killall -HUP: %v", dnsmasqUnitName, err)
		if _, fbErr := runDnsmasqExecCommand(killallCommand, "-HUP", dnsmasqCommand); fbErr != nil {
			return fmt.Errorf("重载 dnsmasq 静态租约失败: %v(回退亦失败: %v)", err, fbErr)
		}
	}
	return nil
}

// restartDnsmasqUnit 重启 dnsmasq 单元:配置链(conf-file/server 等)变更
// 只有重启进程才能生效,SIGHUP 不会重读。
func restartDnsmasqUnit() error {
	if _, err := runSystemctlCommand("restart", dnsmasqUnitName); err != nil {
		log.Printf("重启 %s 失败: %v", dnsmasqUnitName, err)
		return err
	}
	return nil
}

// testDnsmasqConfig 用 dnsmasq --test 对当前配置链做语法校验:优先校验单元实际
// 加载的 bridge0 运行时配置;该文件位于 tmpfs 可能缺失,此时退化为校验厂商主配置。
// 非零退出时返回含输出摘要的错误。
func testDnsmasqConfig() error {
	confPath := dnsmasqBridgeConfPath
	if _, err := os.Stat(dnsmasqBridgeConfPath); err != nil {
		confPath = dnsmasqMainConfPath
	}
	if _, err := runDnsmasqCommand("--test", "--conf-file="+confPath); err != nil {
		log.Printf("dnsmasq 配置语法校验失败(%s): %v", confPath, err)
		return err
	}
	return nil
}

// ensureDNSIncludeFile 写入上游 DNS 包含文件:必须先于标记块注入,
// 否则厂商配置里的 conf-file 指向缺失文件会导致 dnsmasq 重启失败。
func ensureDNSIncludeFile(enabled bool, servers []string) error {
	if err := os.MkdirAll(dnsmasqIncludeDir, 0755); err != nil {
		return err
	}
	content := renderDNSIncludeContent(enabled, servers)
	if err := atomicWriteFile(dnsmasqIncludePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入上游 DNS 包含文件 %s 失败: %v", dnsmasqIncludePath, err)
	}
	return nil
}

// ensureDnsmasqMarker 确保厂商配置文件末尾存在指向包含文件的完整标记块:该文件
// 出厂后不被 QCMAP 重写,可安全追加;首次改动前备份原文以便恢复。读取失败直接
// 返回错误,绝不凭空创建厂商配置文件。
func ensureDnsmasqMarker() error {
	data, err := os.ReadFile(dnsmasqMainConfPath)
	if err != nil {
		return fmt.Errorf("读取厂商 dnsmasq 配置 %s 失败: %v", dnsmasqMainConfPath, err)
	}
	content := string(data)
	if dnsMarkerBlockComplete(content) {
		return nil
	}
	// 保留厂商文件原有权限,写回时维持一致。
	perm := os.FileMode(0644)
	if info, statErr := os.Stat(dnsmasqMainConfPath); statErr == nil {
		perm = info.Mode().Perm()
	}
	if _, statErr := os.Stat(dnsmasqConfBackupPath); statErr != nil {
		if err := atomicWriteFile(dnsmasqConfBackupPath, data, perm); err != nil {
			return fmt.Errorf("备份厂商 dnsmasq 配置到 %s 失败: %v", dnsmasqConfBackupPath, err)
		}
	}
	if strings.Contains(content, dnsmasqMarkerBegin) {
		// 起始标记已存在但块不完整:视为损坏,先移除残留再重新追加修复。
		// 只有起始标记的残留可能是上次写入中断,直接跳过会让功能静默失效。
		content = removeDNSMarkerBlock(content)
		if strings.Contains(content, dnsmasqMarkerBegin) {
			// 只剩起始标记的残留会被 removeDNSMarkerBlock 保守保留,
			// 从起始标记行截断到文件末尾(块总在文件末尾追加,见 truncateDNSMarkerTail)。
			content = truncateDNSMarkerTail(content)
		}
	}
	updated := appendDNSMarkerBlock(content)
	if err := atomicWriteFile(dnsmasqMainConfPath, []byte(updated), perm); err != nil {
		return fmt.Errorf("写入厂商 dnsmasq 配置 %s 失败: %v", dnsmasqMainConfPath, err)
	}
	return nil
}

// applyDNSUpstream 按固定顺序应用上游 DNS 设置:① 先写包含文件,避免 dnsmasq
// 因缺失的 conf-file 启动失败;② 注入标记块;③ 语法校验,失败则不重启;
// ④ 重启单元使配置链生效。
// 回滚语义:写新内容前先取旧内容快照(优先包含文件磁盘实际内容;不存在或不可读
// 时按保存状态渲染,状态读取失败按禁用态渲染)。注入标记后任何一步(校验或重启,
// 以及标记注入本身)失败都把包含文件回滚为旧内容,保证失败后"磁盘 == 运行 ==
// 状态文件"三者仍是旧值、保持一致(状态文件由调用方在本函数成功后才写入);
// 否则磁盘已是新值而运行中仍是旧配置,任何外部 dnsmasq 重启都会加载被拒绝的
// 配置。回滚本身失败仅告警,原错误照常返回。
// 并发:整体由 dnsUpstreamMu 串行化,函数内加锁,一次性覆盖页面保存与开机
// 自愈两个调用点。
func applyDNSUpstream(enabled bool, servers []string) error {
	// 整个事务(含回滚)互斥,见 dnsUpstreamMu 注释:并发交错时一方回滚会
	// 覆盖另一方刚写入的新配置。
	dnsUpstreamMu.Lock()
	defer dnsUpstreamMu.Unlock()

	var oldContent string
	if data, err := os.ReadFile(dnsmasqIncludePath); err == nil {
		oldContent = string(data)
	} else {
		state, stateErr := readDNSUpstreamState()
		if stateErr != nil {
			state = dnsUpstreamState{}
		}
		oldContent = renderDNSIncludeContent(state.Enabled, state.Servers)
	}
	rollback := func() {
		if err := atomicWriteFile(dnsmasqIncludePath, []byte(oldContent), 0644); err != nil {
			log.Printf("警告: 回滚上游 DNS 包含文件 %s 失败: %v", dnsmasqIncludePath, err)
		}
	}

	if err := ensureDNSIncludeFile(enabled, servers); err != nil {
		return err
	}
	if err := ensureDnsmasqMarker(); err != nil {
		rollback()
		return err
	}
	if err := testDnsmasqConfig(); err != nil {
		rollback()
		return fmt.Errorf("配置校验未通过,已跳过重启: %v", err)
	}
	if err := restartDnsmasqUnit(); err != nil {
		rollback()
		return err
	}
	return nil
}

// dnsUpstreamAppliedConsistent 判断设备现状与保存的上游 DNS 状态是否已一致:
// 厂商配置含完整标记块(见 dnsMarkerBlockComplete 的完整性定义),且包含文件的
// 磁盘内容与按该状态渲染的结果完全一致。两者同时满足说明配置已生效,无需重启。
func dnsUpstreamAppliedConsistent(state dnsUpstreamState) bool {
	data, err := os.ReadFile(dnsmasqMainConfPath)
	if err != nil || !dnsMarkerBlockComplete(string(data)) {
		return false
	}
	include, err := os.ReadFile(dnsmasqIncludePath)
	if err != nil {
		return false
	}
	return string(include) == renderDNSIncludeContent(state.Enabled, state.Servers)
}

// reconcileDNSUpstreamAtStartup 开机自愈:固件重置可能抹掉厂商配置里的标记块,
// 按已保存状态重新对齐;未启用时不做任何事,失败仅告警不阻断启动。
func reconcileDNSUpstreamAtStartup() {
	state, err := readDNSUpstreamState()
	if err != nil {
		log.Printf("读取上游 DNS 状态失败,跳过开机自愈: %v", err)
		return
	}
	if !state.Enabled {
		return
	}
	if dnsUpstreamAppliedConsistent(state) {
		// 已启用且设备现状与状态一致:直接返回,避免每次服务重启都重启
		// dnsmasq 单元造成一次 DNS 瞬断。
		log.Printf("上游 DNS 配置与保存状态一致,跳过重启 %s", dnsmasqUnitName)
		return
	}
	if err := applyDNSUpstream(state.Enabled, state.Servers); err != nil {
		log.Printf("开机自愈上游 DNS 设置失败: %v", err)
	}
}
