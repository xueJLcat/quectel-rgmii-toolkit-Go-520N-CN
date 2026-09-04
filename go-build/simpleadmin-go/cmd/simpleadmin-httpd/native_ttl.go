//go:build linux
// +build linux

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

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
		lines, _ := applyNativeTTL(value, false)
		printLines(lines)
	case "off", "stop":
		printLines(setNativeTTL(0))
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

// setNativeTTLWithStatus 与 setNativeTTL 相同,但额外返回规则是否成功应用
// 并持久化,供 HTTP 接口如实上报成败(旧路径恒返回 200,前端无法感知
// 应用失败)。
func setNativeTTLWithStatus(value int) ([]string, bool) {
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
	applied := true
	for _, spec := range ttlRuleSpecs {
		args := []string{"-t", "mangle", "-I", "POSTROUTING", "-o", "rmnet+", "-j", spec.target, spec.setFlag, strconv.Itoa(value)}
		if out, err := exec.Command(spec.command, args...).CombinedOutput(); err != nil {
			logs = append(logs, fmt.Sprintf("%s add failed: %v: %s", spec.command, err, strings.TrimSpace(string(out))))
			applied = false
			continue
		}
		logs = append(logs, fmt.Sprintf("%s rule added", spec.command))
	}

	if !applied {
		logs = append(logs, "TTL rules not applied, ttlvalue not persisted")
		return logs, false
	}
	if persistLog {
		logs = append(logs, "TTL value saved")
	}
	return logs, true
}

// removeNativeTTLRules 清理内核中托管的 TTL/HL 规则,返回日志与是否全部成功。
// 任一列表/删除命令失败都视为清理失败(内核可能残留规则),
// 由调用方据此决定是否持久化新状态。
func removeNativeTTLRules() ([]string, bool) {
	var logs []string
	ok := true
	for _, spec := range ttlRuleSpecs {
		out, err := exec.Command(spec.command, "-t", "mangle", "-S", "POSTROUTING").CombinedOutput()
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
			if delOut, err := exec.Command(spec.command, cmdArgs...).CombinedOutput(); err != nil {
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
