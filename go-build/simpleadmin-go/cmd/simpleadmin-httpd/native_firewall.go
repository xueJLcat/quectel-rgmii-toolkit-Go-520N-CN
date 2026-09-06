//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// runFirewallCommand 执行单条 iptables 命令,10 秒超时,失败时返回带命令上下文的错误。
func runFirewallCommand(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, iptablesCommand, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s 执行失败: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runFirewallCommandOutput 执行单条 iptables/ip6tables 命令并返回合并输出,
// 10 秒超时,失败时返回带命令上下文(含输出摘要)的错误。
func runFirewallCommandOutput(command string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s 执行失败: %v: %s", command, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ensureFirewallChain 确保 SADMIN_FW 链存在且已挂到 INPUT 链首位。
func ensureFirewallChain() error {
	if out, err := exec.Command(iptablesCommand, "-N", firewallChainName).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if !strings.Contains(msg, "Chain already exists") {
			return fmt.Errorf("创建防火墙链 %s 失败: %v: %s", firewallChainName, err, msg)
		}
	}
	if err := exec.Command(iptablesCommand, "-C", "INPUT", "-j", firewallChainName).Run(); err != nil {
		return runFirewallCommand([]string{"-I", "INPUT", "1", "-j", firewallChainName})
	}
	return nil
}

// applyFirewallRules 以事务语义重建 SADMIN_FW 链规则:新规则命令序列全部成功,或任一
// 步失败时按 oldRules(调用方传入的当前落盘旧规则)重放恢复链,不允许停留在半套新规则
// 的残缺态;恢复结果记日志,原错误照常返回。空规则列表即清空(仅保留跳转);
// oldRules 为 nil 表示调用方无已知旧规则,失败时链被重建为空。
func applyFirewallRules(rules, oldRules []firewallRule) error {
	if err := ensureFirewallChain(); err != nil {
		return err
	}
	return applyFirewallRulesTransactional(runFirewallCommand, rules, oldRules)
}

// firewallDumpChains 执行 iptables -vnL -x --line-numbers(10 秒超时)并解析全部链的计数与规则。
func firewallDumpChains() ([]firewallChainInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, iptablesCommand, "-vnL", "-x", "--line-numbers").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("iptables -vnL 执行失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return parseIPTablesChainDump(string(out)), nil
}

// applySavedFirewallAtStartup 启动时恢复已保存的防火墙规则与 DNAT 转发规则,
// 失败仅告警不阻断启动。
func applySavedFirewallAtStartup() {
	rules, err := readFirewallRulesFile(runtimeFirewallPortsFile)
	if err != nil {
		log.Printf("读取防火墙配置失败: %v", err)
	} else if len(rules) > 0 {
		// 启动时无已知旧规则可回退,传 nil:应用失败时把链重建为空,
		// 避免停留在残缺态;规则仍在配置文件,下次启动会重试。
		if err := applyFirewallRules(rules, nil); err != nil {
			log.Printf("启动应用防火墙规则失败: %v", err)
		}
	}
	applySavedFirewallFwdAtStartup()
}
