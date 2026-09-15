//go:build !linux
// +build !linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// nativeTTLMu 与 Linux 实现同一声明(每个平台各编译一份):保护
// setNativeTTLWithStatus"应用→持久化"事务的整体互斥。
var nativeTTLMu sync.Mutex

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

func applySavedTTLAtStartup() {}

func setNativeTTL(value int) []string {
	logs, _ := setNativeTTLWithStatus(value)
	return logs
}

// setNativeTTLWithStatus 与 setNativeTTL 相同,但额外返回规则是否成功应用
// 并持久化,供 HTTP 接口如实上报成败。
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
	logs := []string{"Windows local test mode: TTL rules are simulated only"}
	if value <= 0 {
		logs = append(logs, "TTL disabled")
		return logs, true
	}
	logs = append(logs, fmt.Sprintf("TTL mock enabled with value: %d", value))
	if persistLog {
		logs = append(logs, "TTL value saved")
	}
	return logs, true
}

func removeNativeTTLRules() ([]string, bool) {
	return []string{"Windows local test mode: no kernel TTL rules to remove"}, true
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
	data, err := os.ReadFile(runtimeTTLValueFile)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(stripNonDigits(string(data)))
	if err != nil || value < 0 || value > 255 {
		return 0, fmt.Errorf("invalid ttlvalue in %s", runtimeTTLValueFile)
	}
	return value, nil
}

// writeTTLValue 原子写入 TTL 状态文件(与 linux 实现同一约定):
// 直接覆写遇掉电可能留下截断内容,重启后按非法值拒绝恢复规则。
func writeTTLValue(value int) error {
	if value < 0 || value > 255 {
		return fmt.Errorf("invalid TTL value: %d", value)
	}
	return atomicWriteFile(runtimeTTLValueFile, []byte(strconv.Itoa(value)+"\n"), 0644)
}
