//go:build linux
// +build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ttlIPTablesTimeout 单条 iptables/ip6tables 命令的执行上限:applySavedTTLAtStartup
// 在 HTTP 监听前同步执行,iptables 罕见挂起(内核模块自动加载/锁等待)时
// 不得无限阻塞整个启动。
const ttlIPTablesTimeout = 10 * time.Second

// runTTLIPTables 执行 TTL 相关的 iptables/ip6tables 命令:统一附加 -w
// (等待 xtables 锁,厂商 QCMAP/防火墙脚本与本工具的防火墙页会并发操作
// iptables,不带 -w 时锁竞争立即失败)与 context 超时。
func runTTLIPTables(command string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ttlIPTablesTimeout)
	defer cancel()
	full := append([]string{"-w"}, args...)
	return exec.CommandContext(ctx, command, full...).CombinedOutput()
}

const (
	iptablesCommand  = "iptables"
	ip6tablesCommand = "ip6tables"
)

type ttlRuleSpec struct {
	command string
	target  string
	setFlag string
}

var ttlRuleSpecs = []ttlRuleSpec{
	{command: iptablesCommand, target: "TTL", setFlag: "--ttl-set"},
	{command: ip6tablesCommand, target: "HL", setFlag: "--hl-set"},
}

func runTTLCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: simpleadmin-httpd ttl {apply|off|status}")
		os.Exit(1)
	}

	switch args[0] {
	case "apply", "start", "restart":
		value, err := readTTLValue()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		// 应用失败必须以非零退出码结束:init/卸载/编排脚本按退出码判定
		// 成败,恒 0 会把"规则没有生效"当成功继续后续步骤(HTTP 路径
		// 已如实上报,CLI 路径对齐同一语义)。
		lines, applied := applyNativeTTL(value, false)
		printLines(lines)
		if !applied {
			os.Exit(1)
		}
	case "off", "stop":
		lines, ok := setNativeTTLWithStatus(0)
		printLines(lines)
		if !ok {
			os.Exit(1)
		}
	case "status":
		value, enabled := currentTTLValue()
		fmt.Printf("enabled=%t ttl=%d\n", enabled, value)
	default:
		fmt.Println("Usage: simpleadmin-httpd ttl {apply|off|status}")
		os.Exit(1)
	}
}

func printLines(lines []string) {
	for _, line := range lines {
		fmt.Println(line)
	}
}

func applySavedTTLAtStartup() {
	value, err := readTTLValue()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := writeTTLValue(0); err != nil {
				log.Printf("初始化 TTL 配置失败: %v", err)
			}
		} else {
			log.Printf("读取 TTL 配置失败,保留现有文件: %v", err)
		}
		return
	}
	if value <= 0 {
		return
	}
	lines, _ := applyNativeTTL(value, false)
	for _, line := range lines {
		log.Printf("TTL: %s", line)
	}
}

func setNativeTTL(value int) []string {
	logs, _ := setNativeTTLWithStatus(value)
	return logs
}

// nativeTTLMu 保护"快照旧值→删旧规则→加新规则→持久化"的整体互斥:
// WS 网关对每帧独立协程分发、directAPI 模式天然并发,两个并发 set_ttl
// 交错会在 mangle 链同时残留两条托管规则(生效值由插入顺序决定)、
// 状态文件与内核实际生效值背离、并发 -D 竞争让本应成功的请求误报失败。
// 同类多步事务(firewallMu/macBindMu/dnsUpstreamMu/cellLockMu)均有互斥,
// TTL 此前遗漏。CLI 子命令是独立进程,跨进程竞争由 iptables -w 兜底。
var nativeTTLMu sync.Mutex

// setNativeTTLWithStatus 与 setNativeTTL 相同,但额外返回规则是否成功应用
// 并持久化,供 HTTP 接口如实上报成败(旧路径恒返回 200,前端无法感知
// 应用失败)。
func setNativeTTLWithStatus(value int) ([]string, bool) {
	nativeTTLMu.Lock()
	defer nativeTTLMu.Unlock()
	logs, applied := applyNativeTTL(value, true)
	if !applied {
		return logs, false
	}
	if err := writeTTLValue(value); err != nil {
		logs = append(logs, "failed to write ttlvalue: "+err.Error())
		return logs, false
	}
	return logs, true
}

func applyNativeTTL(value int, persistLog bool) ([]string, bool) {
	logs := []string{"Applying TTL rules with Go native handler"}
	// 删除前先快照内核中正在生效的托管规则值:删除成功而添加失败时
	// (xtables 锁竞争/ip6tables 缺失等)按快照回滚,否则用户原本正常的
	// TTL 改写被一次失败的"改值"静默摧毁——内核无规则、状态文件仍是旧值、
	// UI 显示"已启用",三方分裂且开机自愈不会清理。
	oldValue := currentManagedTTLValue()
	removeLogs, removedAll := removeNativeTTLRules()
	logs = append(logs, removeLogs...)
	if !removedAll {
		// 清理失败时内核可能残留托管规则:禁用路径若持久化 0 会出现
		// "状态未激活但规则仍在"的不一致;按"失败不持久化"语义返回失败。
		if value <= 0 {
			logs = append(logs, "TTL disable failed: managed rules not fully removed, ttlvalue not persisted")
		} else {
			logs = append(logs, "TTL rules not applied, ttlvalue not persisted")
		}
		return logs, false
	}

	if value <= 0 {
		logs = append(logs, "TTL disabled")
		return logs, true
	}

	logs = append(logs, fmt.Sprintf("Enabling TTL with value: %d", value))
	addLogs, applied := addNativeTTLRules(value)
	logs = append(logs, addLogs...)

	if !applied {
		// v4/v6 部分成功时先清掉本次已插入的规则,保证"失败 = 内核无本次
		// 新规则"的确定语义(否则 v4 生效而状态上报"未启用")。
		cleanLogs, _ := removeNativeTTLRules()
		logs = append(logs, cleanLogs...)
		// 再按快照值回滚此前正常生效的旧规则(尽力而为,失败明确告警)。
		if oldValue > 0 {
			restoreLogs, restored := addNativeTTLRules(oldValue)
			logs = append(logs, restoreLogs...)
			if restored {
				logs = append(logs, fmt.Sprintf("rolled back to previous TTL value: %d", oldValue))
			} else {
				logs = append(logs, "WARNING: rollback to previous TTL value failed, managed rules may be missing")
			}
		}
		logs = append(logs, "TTL rules not applied, ttlvalue not persisted")
		return logs, false
	}
	if persistLog {
		logs = append(logs, "TTL value saved")
	}
	return logs, true
}

// addNativeTTLRules 为 IPv4(TTL)与 IPv6(HL)各插入一条改值规则,
// 返回日志与是否全部成功。
func addNativeTTLRules(value int) ([]string, bool) {
	var logs []string
	ok := true
	for _, spec := range ttlRuleSpecs {
		args := []string{"-t", "mangle", "-I", "POSTROUTING", "-o", "rmnet+", "-j", spec.target, spec.setFlag, strconv.Itoa(value)}
		if out, err := runTTLIPTables(spec.command, args); err != nil {
			logs = append(logs, fmt.Sprintf("%s add failed: %v: %s", spec.command, err, strings.TrimSpace(string(out))))
			ok = false
			continue
		}
		logs = append(logs, fmt.Sprintf("%s rule added", spec.command))
	}
	return logs, ok
}

// currentManagedTTLValue 从内核 -S 列举中读取当前托管 IPv4 TTL 规则的值
// (无托管规则或列举失败返回 0),作为"删后加失败"时的回滚目标。
func currentManagedTTLValue() int {
	out, err := runTTLIPTables(iptablesCommand, []string{"-t", "mangle", "-S", "POSTROUTING"})
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if _, matched := managedTTLDeleteArgs(line, "TTL", "--ttl-set"); !matched {
			continue
		}
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "--ttl-set" {
				if v, err := strconv.Atoi(fields[i+1]); err == nil {
					return v
				}
			}
		}
	}
	return 0
}

// removeNativeTTLRules 清理内核中托管的 TTL/HL 规则,返回日志与是否全部成功。
// 任一列表/删除命令失败都视为清理失败(内核可能残留规则),
// 由调用方据此决定是否持久化新状态。
func removeNativeTTLRules() ([]string, bool) {
	var logs []string
	ok := true
	for _, spec := range ttlRuleSpecs {
		out, err := runTTLIPTables(spec.command, []string{"-t", "mangle", "-S", "POSTROUTING"})
		if err != nil {
			logs = append(logs, fmt.Sprintf("%s list failed: %v: %s", spec.command, err, strings.TrimSpace(string(out))))
			ok = false
			continue
		}

		removed := 0
		for _, line := range strings.Split(string(out), "\n") {
			deleteArgs, matched := managedTTLDeleteArgs(line, spec.target, spec.setFlag)
			if !matched {
				continue
			}
			cmdArgs := append([]string{"-t", "mangle"}, deleteArgs...)
			if delOut, err := runTTLIPTables(spec.command, cmdArgs); err != nil {
				logs = append(logs, fmt.Sprintf("%s delete failed: %v: %s", spec.command, err, strings.TrimSpace(string(delOut))))
				ok = false
				continue
			}
			removed++
		}
		logs = append(logs, fmt.Sprintf("%s removed %d managed TTL rule(s)", spec.command, removed))
	}
	return logs, ok
}

func managedTTLDeleteArgs(ruleLine, target, setFlag string) ([]string, bool) {
	fields := strings.Fields(ruleLine)
	if len(fields) < 8 || fields[0] != "-A" || fields[1] != "POSTROUTING" {
		return nil, false
	}
	if !hasTokenPair(fields, "-o", "rmnet+") || !hasTokenPair(fields, "-j", target) || !hasToken(fields, setFlag) {
		return nil, false
	}
	args := append([]string{"-D", "POSTROUTING"}, fields[2:]...)
	return args, true
}

func hasTokenPair(fields []string, key, value string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func hasToken(fields []string, token string) bool {
	for _, field := range fields {
		if field == token {
			return true
		}
	}
	return false
}

func readTTLValue() (int, error) {
	for _, path := range []string{runtimeTTLValueFile, legacyTTLValueFile} {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
			continue
		}
		value, err := strconv.Atoi(stripNonDigits(string(data)))
		if err != nil || value < 0 || value > 255 {
			return 0, fmt.Errorf("invalid ttlvalue in %s", path)
		}
		return value, nil
	}
	return 0, os.ErrNotExist
}

// writeTTLValue 原子写入 TTL 状态文件:该文件驱动开机自愈重新应用规则,
// 直接覆写遇掉电可能留下截断内容,重启后按非法值拒绝恢复规则,
// 用户保存过的 TTL 设置静默失效(与其他状态文件一致,见 §原子写约定)。
func writeTTLValue(value int) error {
	if value < 0 || value > 255 {
		return fmt.Errorf("invalid TTL value: %d", value)
	}
	return atomicWriteFile(runtimeTTLValueFile, []byte(strconv.Itoa(value)+"\n"), 0644)
}
