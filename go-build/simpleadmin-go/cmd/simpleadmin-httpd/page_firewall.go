// page_firewall.go 提供防火墙页的 /api/firewall_data 端点(端口阻止/放行规则管理与链统计)。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultFirewallPortsFile = "/usrdata/simpleadmin/firewall_ports.conf"
	defaultFirewallFwdFile   = "/usrdata/simpleadmin/firewall_fwd.conf"
	firewallMaxPorts         = 64
	firewallMaxChainRules    = 300
	firewallMaxFwdRules      = 32
	firewallChainName        = "SADMIN_FW"
	firewallFwdChainName     = "SADMIN_FWD"
	firewallActionBlock      = "block"
	firewallActionAccept     = "accept"
	// 平台无关的二进制名(native_ttl.go 的同名常量仅 linux 构建可用)。
	firewallIPTablesCommand  = "iptables"
	firewallIP6TablesCommand = "ip6tables"
)

// firewallCommandRunner 执行防火墙管理命令(iptables/ip6tables)并返回合并输出;
// 与 firewallExecutor 注入模式对齐,单测替换为 mock runner,不触碰真实内核规则。
type firewallCommandRunner func(command string, args []string) (string, error)

var (
	runtimeFirewallPortsFile  = defaultFirewallPortsFile
	runtimeFirewallFwdFile    = defaultFirewallFwdFile
	firewallAllowedInterfaces = []string{"bridge0", "eth0", "tailscale0"}
	// runtimeFirewallCommandRunner 默认为平台原生实现(非 Linux 恒报错),单测中替换。
	runtimeFirewallCommandRunner firewallCommandRunner = runFirewallCommandOutput
)

// firewallRule 表示一条防火墙规则:端口 + 动作(阻止或放行)。
type firewallRule struct {
	Port   string `json:"port"`
	Action string `json:"action"`
}

// firewallChainInfo 表示一条 iptables 链的统计信息。
type firewallChainInfo struct {
	Name   string              `json:"name"`
	Policy string              `json:"policy"`
	Pkts   int64               `json:"pkts"`
	Bytes  int64               `json:"bytes"`
	Rules  []firewallRuleEntry `json:"rules"`
}

// firewallRuleEntry 表示链内一条规则及其计数。
type firewallRuleEntry struct {
	Num         int64  `json:"num"`
	Pkts        int64  `json:"pkts"`
	Bytes       int64  `json:"bytes"`
	Target      string `json:"target"`
	Proto       string `json:"proto"`
	In          string `json:"in"`
	Out         string `json:"out"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Extra       string `json:"extra"`
}

// firewallFwdRule 表示一条 DNAT 端口转发规则:外部端口 → 内网 IPv4:端口。
type firewallFwdRule struct {
	ExtPort int    `json:"extPort"`
	IntIP   string `json:"intIP"`
	IntPort int    `json:"intPort"`
	Proto   string `json:"proto"`
	Enabled bool   `json:"enabled"`
}

func isFirewallPortToken(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func isValidFirewallPort(port string) bool {
	value, err := strconv.Atoi(port)
	return err == nil && isFirewallPortToken(port) && value >= 1 && value <= 65535
}

// parseFirewallPorts 解析逗号分隔的端口列表:去重保序,空输入返回空列表(合法,表示清空)。
func parseFirewallPorts(raw string) ([]string, error) {
	ports := []string{}
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !isValidFirewallPort(item) {
			return nil, fmt.Errorf("无效的防火墙端口: %s", item)
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		ports = append(ports, item)
	}
	if len(ports) > firewallMaxPorts {
		return nil, fmt.Errorf("防火墙端口数量 %d 超过上限 %d", len(ports), firewallMaxPorts)
	}
	return ports, nil
}

// parseFirewallRuleLines 逐行解析规则文本:新格式 "<action> <port>",旧格式纯端口行视为
// block;容错跳过空行与非法行,同端口同动作去重保序。
func parseFirewallRuleLines(raw string) ([]firewallRule, error) {
	rules := []firewallRule{}
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		var rule firewallRule
		switch len(fields) {
		case 1:
			rule = firewallRule{Port: fields[0], Action: firewallActionBlock}
		case 2:
			rule = firewallRule{Port: fields[1], Action: fields[0]}
		default:
			continue
		}
		if rule.Action != firewallActionBlock && rule.Action != firewallActionAccept {
			continue
		}
		if !isValidFirewallPort(rule.Port) {
			continue
		}
		key := rule.Action + " " + rule.Port
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, rule)
	}
	return rules, nil
}

// validateFirewallRules 严格校验规则列表:动作必须为 block/accept、端口必须为 1-65535
// 纯数字、同一端口不能同时阻止与放行、总数不超过上限;同端口同动作去重保序。
func validateFirewallRules(rules []firewallRule) ([]firewallRule, error) {
	cleaned := []firewallRule{}
	seen := map[string]bool{}
	for _, rule := range rules {
		rule.Action = strings.TrimSpace(rule.Action)
		rule.Port = strings.TrimSpace(rule.Port)
		if rule.Action != firewallActionBlock && rule.Action != firewallActionAccept {
			return nil, fmt.Errorf("无效的防火墙动作: %s", rule.Action)
		}
		if !isValidFirewallPort(rule.Port) {
			return nil, fmt.Errorf("无效的防火墙端口: %s", rule.Port)
		}
		key := rule.Action + " " + rule.Port
		if seen[key] {
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, rule)
	}
	for _, rule := range cleaned {
		conflict := firewallActionAccept
		if rule.Action == firewallActionAccept {
			conflict = firewallActionBlock
		}
		if seen[conflict+" "+rule.Port] {
			return nil, fmt.Errorf("端口冲突: %s 同时被阻止和放行", rule.Port)
		}
	}
	if len(cleaned) > firewallMaxPorts {
		return nil, fmt.Errorf("防火墙规则数量 %d 超过上限 %d", len(cleaned), firewallMaxPorts)
	}
	return cleaned, nil
}

// readFirewallRulesFile 读取规则配置,容错跳过非法行;文件不存在视为空列表。
func readFirewallRulesFile(path string) ([]firewallRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []firewallRule{}, nil
		}
		return nil, err
	}
	return parseFirewallRuleLines(string(data))
}

// writeFirewallRulesFile 以临时文件+rename 方式原子写入规则配置(新格式 "<action> <port>")。
func writeFirewallRulesFile(path string, rules []firewallRule) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".firewall_ports.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	for _, rule := range rules {
		if _, err := fmt.Fprintf(tmp, "%s %s\n", rule.Action, rule.Port); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	// rename 前先落盘(与 dnsmasq_config.atomicWriteFile 一致):设备掉电常见,
	// 避免 rename 后元数据已更新而数据仍在页缓存,掉电留下空/截断的规则文件,
	// 开机恢复时用户保存的端口规则静默丢失。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// parseIPTablesCount 解析 iptables 计数,容错 K/M/G 后缀(-x 模式下为纯数字)。
func parseIPTablesCount(value string) int64 {
	multiplier := int64(1)
	if value != "" {
		switch value[len(value)-1] {
		case 'K', 'k':
			multiplier = 1024
			value = value[:len(value)-1]
		case 'M', 'm':
			multiplier = 1024 * 1024
			value = value[:len(value)-1]
		case 'G', 'g':
			multiplier = 1024 * 1024 * 1024
			value = value[:len(value)-1]
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return n * multiplier
}

// parseIPTablesChainDump 解析 `iptables -vnL -x --line-numbers` 输出:链头行
// (policy 或 references 形式)、列头行跳过、规则行前 10 列 + 其余拼接为 Extra,
// 每链规则上限 firewallMaxChainRules 条。
func parseIPTablesChainDump(raw string) []firewallChainInfo {
	chains := []firewallChainInfo{}
	var current *firewallChainInfo
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Chain" && len(fields) >= 2 {
			info := firewallChainInfo{Name: fields[1], Rules: []firewallRuleEntry{}}
			if len(fields) >= 7 && fields[2] == "(policy" {
				info.Policy = fields[3]
				info.Pkts = parseIPTablesCount(fields[4])
				info.Bytes = parseIPTablesCount(fields[6])
			}
			chains = append(chains, info)
			current = &chains[len(chains)-1]
			continue
		}
		if current == nil || fields[0] == "num" || len(fields) < 10 {
			continue
		}
		if len(current.Rules) >= firewallMaxChainRules {
			continue
		}
		current.Rules = append(current.Rules, firewallRuleEntry{
			Num:         parseIPTablesCount(fields[0]),
			Pkts:        parseIPTablesCount(fields[1]),
			Bytes:       parseIPTablesCount(fields[2]),
			Target:      fields[3],
			Proto:       fields[4],
			In:          fields[6],
			Out:         fields[7],
			Source:      fields[8],
			Destination: fields[9],
			Extra:       strings.Join(fields[10:], " "),
		})
	}
	return chains
}

// mockChainsForRules 在 mock 模式下合成与真实 -vnL 结构一致的链统计:
// INPUT/FORWARD 为空链, SADMIN_FW 按放行在前、阻止在后的顺序生成 0 计数条目。
func mockChainsForRules(rules []firewallRule) []firewallChainInfo {
	entries := []firewallRuleEntry{}
	num := int64(0)
	addEntry := func(target, in, port string) {
		num++
		entries = append(entries, firewallRuleEntry{
			Num:         num,
			Target:      target,
			Proto:       "tcp",
			In:          in,
			Out:         "*",
			Source:      "0.0.0.0/0",
			Destination: "0.0.0.0/0",
			Extra:       "tcp dpt:" + port,
		})
	}
	for _, rule := range rules {
		if rule.Action == firewallActionAccept {
			addEntry("ACCEPT", "*", rule.Port)
		}
	}
	for _, rule := range rules {
		if rule.Action != firewallActionBlock {
			continue
		}
		for _, iface := range firewallAllowedInterfaces {
			addEntry("ACCEPT", iface, rule.Port)
		}
		addEntry("DROP", "*", rule.Port)
	}
	return []firewallChainInfo{
		{Name: "INPUT", Policy: "ACCEPT", Rules: []firewallRuleEntry{}},
		{Name: "FORWARD", Policy: "ACCEPT", Rules: []firewallRuleEntry{}},
		{Name: firewallChainName, Policy: "DROP", Rules: entries},
	}
}

// firewallJumpInstalledFromChains 从链统计中判断 INPUT 是否挂载了指向防火墙链的跳转。
func firewallJumpInstalledFromChains(chains []firewallChainInfo) bool {
	for _, chain := range chains {
		if chain.Name != "INPUT" {
			continue
		}
		for _, entry := range chain.Rules {
			if entry.Target == firewallChainName {
				return true
			}
		}
	}
	return false
}

// firewallIPTablesCommands 生成规则命令序列:先清空链;放行规则整体在前(每端口单条
// ACCEPT 优先命中),阻止规则在后(每端口先允许接口 ACCEPT 再对其余接口 DROP),仅 IPv4。
// 纯函数,新规则应用与失败后按旧规则重放恢复共用同一生成逻辑。
func firewallIPTablesCommands(rules []firewallRule) [][]string {
	cmds := [][]string{{"-F", firewallChainName}}
	for _, rule := range rules {
		if rule.Action != firewallActionAccept {
			continue
		}
		cmds = append(cmds, []string{"-A", firewallChainName, "-p", "tcp", "--dport", rule.Port, "-j", "ACCEPT"})
	}
	for _, rule := range rules {
		if rule.Action != firewallActionBlock {
			continue
		}
		for _, iface := range firewallAllowedInterfaces {
			cmds = append(cmds, []string{"-A", firewallChainName, "-i", iface, "-p", "tcp", "--dport", rule.Port, "-j", "ACCEPT"})
		}
		cmds = append(cmds, []string{"-A", firewallChainName, "-p", "tcp", "--dport", rule.Port, "-j", "DROP"})
	}
	return cmds
}

// firewallExecutor 执行单条命令参数序列;注入执行桩便于测试模拟部分失败与恢复流程。
type firewallExecutor func(args []string) error

// applyFirewallRulesTransactional 以给定执行器完成"应用新规则 + 失败按旧规则重放"的事务:
// 逐条执行新规则命令序列,任一条失败即停止应用,立即重放旧规则命令序列(首条 flush
// 保证从干净状态重建),事务语义为新规则全成或恢复旧规则,不允许停留在内核只剩半套新
// 规则、配置文件仍是旧规则的残缺/背离状态。恢复阶段本身也可能失败(如 xtables 锁持续
// 被占),此时逐条告警并明确提示链可能残缺,但原错误照常返回,由调用方决定不落盘。
func applyFirewallRulesTransactional(executor firewallExecutor, rules, oldRules []firewallRule) error {
	for _, args := range firewallIPTablesCommands(rules) {
		if err := executor(args); err != nil {
			restoreFirewallRules(executor, err, oldRules)
			return err
		}
	}
	return nil
}

// restoreFirewallRules 在应用失败后尽力按旧规则命令序列重建链,并记录恢复结果;
// 恢复失败说明链仍可能处于残缺态,必须明确告警,不吞错。
func restoreFirewallRules(executor firewallExecutor, applyErr error, oldRules []firewallRule) {
	failed := 0
	for _, args := range firewallIPTablesCommands(oldRules) {
		if err := executor(args); err != nil {
			failed++
			log.Printf("防火墙恢复旧规则命令失败: iptables %s: %v", strings.Join(args, " "), err)
		}
	}
	if failed > 0 {
		log.Printf("警告: 防火墙规则应用失败且旧规则恢复不完整(%d 条命令失败), 链 %s 可能处于残缺状态, 原始错误: %v",
			failed, firewallChainName, applyErr)
		return
	}
	log.Printf("防火墙规则应用失败, 已按旧规则重建链 %s, 原始错误: %v", firewallChainName, applyErr)
}

// normalizeFirewallFwdRule 校验并归一化单条转发规则:端口 1-65535、内网地址必须为
// 合法 IPv4、协议仅限 tcp/udp(大小写与首尾空白归一化)。
func normalizeFirewallFwdRule(rule firewallFwdRule) (firewallFwdRule, error) {
	rule.Proto = strings.ToLower(strings.TrimSpace(rule.Proto))
	rule.IntIP = strings.TrimSpace(rule.IntIP)
	if rule.ExtPort < 1 || rule.ExtPort > 65535 {
		return rule, fmt.Errorf("无效的外部端口: %d", rule.ExtPort)
	}
	if rule.IntPort < 1 || rule.IntPort > 65535 {
		return rule, fmt.Errorf("无效的内部端口: %d", rule.IntPort)
	}
	if ip := net.ParseIP(rule.IntIP); ip == nil || ip.To4() == nil {
		return rule, fmt.Errorf("无效的内网 IPv4 地址: %s", rule.IntIP)
	}
	if rule.Proto != "tcp" && rule.Proto != "udp" {
		return rule, fmt.Errorf("无效的转发协议: %s", rule.Proto)
	}
	return rule, nil
}

// parseFirewallFwdRules 解析 fwd_save 的 rules 参数(JSON 数组字符串)并严格校验:
// 数量不超过 firewallMaxFwdRules、逐条通过 normalizeFirewallFwdRule、
// (外部端口, 协议) 唯一;任一不满足即整体拒绝。
func parseFirewallFwdRules(raw string) ([]firewallFwdRule, error) {
	var rules []firewallFwdRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("无效的转发规则 JSON: %v", err)
	}
	if len(rules) > firewallMaxFwdRules {
		return nil, fmt.Errorf("转发规则数量 %d 超过上限 %d", len(rules), firewallMaxFwdRules)
	}
	cleaned := []firewallFwdRule{}
	seen := map[string]bool{}
	for i, rule := range rules {
		clean, err := normalizeFirewallFwdRule(rule)
		if err != nil {
			return nil, fmt.Errorf("规则 %d: %v", i+1, err)
		}
		key := clean.Proto + " " + strconv.Itoa(clean.ExtPort)
		if seen[key] {
			return nil, fmt.Errorf("规则 %d: 重复的转发规则: %s 端口 %d", i+1, clean.Proto, clean.ExtPort)
		}
		seen[key] = true
		cleaned = append(cleaned, clean)
	}
	return cleaned, nil
}

// parseFirewallFwdRuleLines 逐行解析转发规则文本("<外部端口> <协议> <内网IP> <内部端口> <1|0>"),
// 容错跳过空行与非法行,(外部端口, 协议) 去重保序。
func parseFirewallFwdRuleLines(raw string) []firewallFwdRule {
	rules := []firewallFwdRule{}
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || (fields[4] != "0" && fields[4] != "1") {
			continue
		}
		extPort, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		intPort, err := strconv.Atoi(fields[3])
		if err != nil {
			continue
		}
		clean, err := normalizeFirewallFwdRule(firewallFwdRule{
			ExtPort: extPort, Proto: fields[1], IntIP: fields[2], IntPort: intPort, Enabled: fields[4] == "1",
		})
		if err != nil {
			continue
		}
		key := clean.Proto + " " + strconv.Itoa(clean.ExtPort)
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, clean)
	}
	return rules
}

// readFirewallFwdRulesFile 读取转发规则配置,容错跳过非法行;文件不存在视为空列表。
func readFirewallFwdRulesFile(path string) ([]firewallFwdRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []firewallFwdRule{}, nil
		}
		return nil, err
	}
	return parseFirewallFwdRuleLines(string(data)), nil
}

// writeFirewallFwdRulesFile 以临时文件+落盘+rename 约定原子写入转发规则配置
// (复用 atomicWriteFile);enabled=false 的规则以 0 持久化,不写入链但重启后保留。
func writeFirewallFwdRulesFile(path string, rules []firewallFwdRule) error {
	var sb strings.Builder
	for _, rule := range rules {
		enabled := "0"
		if rule.Enabled {
			enabled = "1"
		}
		fmt.Fprintf(&sb, "%d %s %s %d %s\n", rule.ExtPort, rule.Proto, rule.IntIP, rule.IntPort, enabled)
	}
	return atomicWriteFile(path, []byte(sb.String()), 0644)
}

// firewallNatDumpChains 经注入点执行 iptables -t nat -vnL -x --line-numbers,
// 复用 parseIPTablesChainDump 解析 nat 表链统计。
func firewallNatDumpChains() ([]firewallChainInfo, error) {
	out, err := runtimeFirewallCommandRunner(firewallIPTablesCommand, []string{"-t", "nat", "-vnL", "-x", "--line-numbers"})
	if err != nil {
		return nil, err
	}
	return parseIPTablesChainDump(out), nil
}

// firewallFwdStateFromChains 从 nat 表链统计判断 SADMIN_FWD 链是否存在、
// PREROUTING 是否已挂载指向它的跳转。
func firewallFwdStateFromChains(chains []firewallChainInfo) (chainExists, jumpInstalled bool) {
	for _, chain := range chains {
		switch chain.Name {
		case firewallFwdChainName:
			chainExists = true
		case "PREROUTING":
			for _, entry := range chain.Rules {
				if entry.Target == firewallFwdChainName {
					jumpInstalled = true
				}
			}
		}
	}
	return chainExists, jumpInstalled
}

// firewallFwdIPTablesCommands 生成 nat 表命令序列:链/跳转缺失则先安装,随后 flush 链,
// 再为 enabled 规则逐条重建 DNAT(enabled=false 仅持久化不写入链)。
// 纯函数,新规则应用与失败后按旧规则重放恢复共用同一生成逻辑。
func firewallFwdIPTablesCommands(chainExists, jumpInstalled bool, rules []firewallFwdRule) [][]string {
	cmds := [][]string{}
	if !chainExists {
		cmds = append(cmds, []string{"-t", "nat", "-N", firewallFwdChainName})
	}
	if !jumpInstalled {
		cmds = append(cmds, []string{"-t", "nat", "-I", "PREROUTING", "1", "-j", firewallFwdChainName})
	}
	cmds = append(cmds, []string{"-t", "nat", "-F", firewallFwdChainName})
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		cmds = append(cmds, []string{"-t", "nat", "-A", firewallFwdChainName,
			"-p", rule.Proto, "--dport", strconv.Itoa(rule.ExtPort),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", rule.IntIP, rule.IntPort)})
	}
	return cmds
}

// applyFirewallFwdRulesTransactional 以给定执行器完成"安装跳转 + 应用新规则 + 失败按
// 旧规则重放"的事务,语义与 applyFirewallRulesTransactional 对齐:任一条失败立即停止,
// 按旧规则重放恢复;恢复本身失败逐条告警,原错误照常返回,不吞错。
func applyFirewallFwdRulesTransactional(executor firewallExecutor, chainExists, jumpInstalled bool, rules, oldRules []firewallFwdRule) error {
	for _, args := range firewallFwdIPTablesCommands(chainExists, jumpInstalled, rules) {
		if err := executor(args); err != nil {
			restoreFirewallFwdRules(executor, err, oldRules)
			return err
		}
	}
	return nil
}

// restoreFirewallFwdRules 在应用失败后尽力按旧规则命令序列重建转发链(此时链与跳转
// 视为已就位);恢复不完整说明链仍可能残缺,必须明确告警,不吞错。
func restoreFirewallFwdRules(executor firewallExecutor, applyErr error, oldRules []firewallFwdRule) {
	failed := 0
	for _, args := range firewallFwdIPTablesCommands(true, true, oldRules) {
		if err := executor(args); err != nil {
			failed++
			log.Printf("防火墙转发恢复旧规则命令失败: iptables %s: %v", strings.Join(args, " "), err)
		}
	}
	if failed > 0 {
		log.Printf("警告: 防火墙转发规则应用失败且旧规则恢复不完整(%d 条命令失败), 链 %s 可能处于残缺状态, 原始错误: %v",
			failed, firewallFwdChainName, applyErr)
		return
	}
	log.Printf("防火墙转发规则应用失败, 已按旧规则重建链 %s, 原始错误: %v", firewallFwdChainName, applyErr)
}

// applyFirewallFwdRules 以事务语义重建 nat 表 SADMIN_FWD 链:先转储 nat 表核对链与
// PREROUTING 跳转状态(缺失则随命令序列一并安装),再 flush+重建 DNAT 规则,任一步
// 失败按 oldRules 重放恢复。转储失败直接返回错误,绝不把未知状态伪装成未安装。
func applyFirewallFwdRules(rules, oldRules []firewallFwdRule) error {
	chains, err := firewallNatDumpChains()
	if err != nil {
		return err
	}
	chainExists, jumpInstalled := firewallFwdStateFromChains(chains)
	executor := func(args []string) error {
		_, err := runtimeFirewallCommandRunner(firewallIPTablesCommand, args)
		return err
	}
	return applyFirewallFwdRulesTransactional(executor, chainExists, jumpInstalled, rules, oldRules)
}

// applySavedFirewallFwdAtStartup 启动时恢复已保存的 DNAT 转发规则,失败仅告警不阻断
// 启动;无已知旧规则可回退时传 nil:失败把链重建为空,规则仍在配置文件,下次启动重试。
func applySavedFirewallFwdAtStartup() {
	rules, err := readFirewallFwdRulesFile(runtimeFirewallFwdFile)
	if err != nil {
		log.Printf("读取防火墙转发配置失败: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	if err := applyFirewallFwdRules(rules, nil); err != nil {
		log.Printf("启动应用防火墙转发规则失败: %v", err)
	}
}

func (s *simpleAdminServer) handleFirewallData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "", "status":
		rules, err := readFirewallRulesFile(runtimeFirewallPortsFile)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		acceptCount := 0
		for _, rule := range rules {
			if rule.Action == firewallActionAccept {
				acceptCount++
			}
		}
		ruleCount := acceptCount + (len(rules)-acceptCount)*4
		var chains []firewallChainInfo
		jumpInstalled := false
		if s.cfg.mockMode {
			jumpInstalled = len(rules) > 0
			chains = mockChainsForRules(rules)
		} else {
			dumped, err := firewallDumpChains()
			if err != nil {
				log.Printf("读取防火墙链统计失败: %v", err)
			} else {
				chains = dumped
				jumpInstalled = firewallJumpInstalledFromChains(dumped)
			}
		}
		if chains == nil {
			chains = []firewallChainInfo{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"rules":         rules,
			"ruleCount":     ruleCount,
			"jumpInstalled": jumpInstalled,
			"chains":        chains,
		})
	case "status6":
		// IPv6 链只读展示:经注入点执行 ip6tables,失败如实上报(200 ok:false),
		// 绝不把查询失败伪装成空结果。
		out, err := runtimeFirewallCommandRunner(firewallIP6TablesCommand, []string{"-vnL", "-x", "--line-numbers"})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chains": parseIPTablesChainDump(out)})
	case "fwd_list":
		forwarded, err := readFirewallFwdRulesFile(runtimeFirewallFwdFile)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jumpInstalled := false
		if s.cfg.mockMode {
			// mock 模式不触内核:与 status 相同思路合成"有启用规则即已安装跳转"。
			for _, rule := range forwarded {
				if rule.Enabled {
					jumpInstalled = true
					break
				}
			}
		} else {
			chains, err := firewallNatDumpChains()
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			_, jumpInstalled = firewallFwdStateFromChains(chains)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "forwarded": forwarded, "jumpInstalled": jumpInstalled})
	case "fwd_save":
		rules, err := parseFirewallFwdRules(requestValue(r, "rules"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if !s.cfg.mockMode {
			// 先读出当前落盘的旧规则:应用中途失败时按旧规则重放恢复 nat 链,
			// 只有应用成功才落盘,保证内核与配置文件不背离。
			oldRules, err := readFirewallFwdRulesFile(runtimeFirewallFwdFile)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if err := applyFirewallFwdRules(rules, oldRules); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		if err := writeFirewallFwdRulesFile(runtimeFirewallFwdFile, rules); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
	case "save":
		blockPorts, err := parseFirewallPorts(requestValue(r, "block_ports"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		acceptPorts, err := parseFirewallPorts(requestValue(r, "accept_ports"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		assembled := []firewallRule{}
		for _, port := range acceptPorts {
			assembled = append(assembled, firewallRule{Port: port, Action: firewallActionAccept})
		}
		for _, port := range blockPorts {
			assembled = append(assembled, firewallRule{Port: port, Action: firewallActionBlock})
		}
		rules, err := validateFirewallRules(assembled)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if !s.cfg.mockMode {
			// 先读出当前落盘的旧规则:应用中途失败时按旧规则重放恢复内核链,
			// 只有应用成功才落盘,保证内核与配置文件不背离。
			oldRules, err := readFirewallRulesFile(runtimeFirewallPortsFile)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if err := applyFirewallRules(rules, oldRules); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		if err := writeFirewallRulesFile(runtimeFirewallPortsFile, rules); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}
