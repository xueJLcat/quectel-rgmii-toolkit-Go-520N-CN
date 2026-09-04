package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func atLines(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || s == "OK" {
			continue
		}
		// 后台未就绪提示不是设备数据,不能参与解析,
		// 否则会被当作厂商/型号/固件等普通值显示。
		if s == atCachePendingText {
			continue
		}
		// 模块/串口异常时运行器错误文本会被写入缓存并参与解析,
		// 这类行不是模块返回的数据,必须整体过滤。
		if isRunnerErrorLine(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// atRunnerErrorMarkers 是运行器/串口层错误文案的特征串,逐条取自真实错误产生处:
// at_runner.go 的候选设备失败/超时/读写失败与短信事务错误,
// native_serial.go 的 AT 锁文件与串口会话错误,
// smd_reader.go 的 SMD 读取器错误。
// 特征串足够具体,不会命中模块返回的合法数据行;
// 以 "+" 开头的行是模块数据/协议行(如 +CME ERROR),不视为运行器错误。
var atRunnerErrorMarkers = []string{
	"AT device candidates failed",
	"no AT device candidates configured",
	"SMS AT transaction failed",
	"timeout waiting for ",
	"read failed:",
	"write failed:",
	"write CMGS failed:",
	"write SMS body failed:",
	"read SMS prompt failed:",
	"open AT lock file ",
	"lock AT lock file ",
	"open /dev/",
	"SMD reader helper did not ",
	"SMD shell stdin writer ",
	"AT session write timed out",
	"AT lock held by another process",
	"still locked after",
}

func isRunnerErrorLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "+") {
		return false
	}
	for _, marker := range atRunnerErrorMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func isQMAPRecord(line, name string) bool {
	if !strings.HasPrefix(strings.TrimSpace(line), "+QMAP:") {
		return false
	}
	parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
	return len(parts) > 0 && strings.EqualFold(parts[0], name)
}

// csvFields 按逗号切分一行 AT 应答为字段列表:逐字符扫描并跟踪引号状态,
// 引号内的逗号不是分隔点(如 `"a,b"`);每段 trim 空白后剥掉成对外层引号。
// 未加引号的空字段一律保留——旧正则 `"[^"]*"|[^,]+` 实现会把 `1,,2` 解析成
// ["1","2"],造成字段索引左移(如 +CMGL 头部缺失的号码/时间字段)。
func csvFields(value string) []string {
	fields := []string{}
	var current strings.Builder
	inQuotes := false
	for _, r := range value {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == ',' && !inQuotes:
			fields = append(fields, trimCSVField(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	fields = append(fields, trimCSVField(current.String()))
	return fields
}

func trimCSVField(segment string) string {
	field := strings.TrimSpace(segment)
	if len(field) >= 2 && strings.HasPrefix(field, `"`) && strings.HasSuffix(field, `"`) {
		field = field[1 : len(field)-1]
	}
	return field
}

func isPlainATValue(s string) bool {
	up := strings.ToUpper(strings.TrimSpace(s))
	return s != "" && up != "OK" && up != "ERROR" && !strings.HasPrefix(up, "AT+") && !strings.HasPrefix(s, "+")
}

func extractIMEI(s string) string {
	if m := regexp.MustCompile(`(\d{14,17})`).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

// hasATTerminalErrorLine 逐行判定 transcript 中是否出现明确的终结错误行:
// 每行 trim + upper 后,行等于 "ERROR" 或以 "+CME ERROR"/"+CMS ERROR" 开头。
// 纯函数,供 atResponseOK 与 atResponseRebootOK 复用,保证两处错误行口径一致。
// 运行器/串口层错误文案(如超时提示)不是模块终结错误行,不会命中。
func hasATTerminalErrorLine(text string) bool {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.ToUpper(strings.TrimSpace(rawLine))
		if line == "ERROR" || strings.HasPrefix(line, "+CME ERROR") || strings.HasPrefix(line, "+CMS ERROR") {
			return true
		}
	}
	return false
}

// atResponseOK 按行判定 transcript 是否以 OK 收尾:有独立 OK 行且没有明确
// 终结错误行。逐行扫描而不是子串 Contains("OK"):回显里的用户数据(如短信
// 正文、号码中含 "OK" 字样)不会被误判为成功终结。行等于 OK(大小写不敏感)
// 记为成功;出现终结错误行判失败;扫完未见到 OK 同样判失败。
// 注意:重启类命令不适用本函数,见 atResponseRebootOK。
func atResponseOK(text string) bool {
	sawOK := false
	for _, rawLine := range strings.Split(text, "\n") {
		if strings.ToUpper(strings.TrimSpace(rawLine)) == "OK" {
			sawOK = true
			break
		}
	}
	return sawOK && !hasATTerminalErrorLine(text)
}

// atResponseRebootOK 判定重启类命令(AT+CFUN=1,1、AT+EGMR=...;+CFUN=1,1)
// 的执行结果:只有出现明确终结错误行才算失败。
//
// 回归背景:atResponseOK 改为按行判定后,重启类命令"模块先重启、后应答",
// 收不到应答/超时是常态,运行器的超时错误文本(无独立 OK 行)进入
// atResponseOK 返回 false,导致 set_imei/reboot 误报"操作失败"。
// 重启类命令的判定因此独立:超时无应答按"重启进行中"处理,与
// page_network_config.go 的 ipPassthroughDisableOutcome 对 CFUN
// "只执行、不判定"的策略一致。
func atResponseRebootOK(text string) bool {
	return !hasATTerminalErrorLine(text)
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func isAllZeroV6(v string) bool {
	return regexp.MustCompile(`^0(\.0){15}$`).MatchString(v) || v == "0:0:0:0:0:0:0:0"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func percentFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}
func toInt(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func decodeMaybeUCS2(value string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(value), " ", "")
	if clean == "" || len(clean)%4 != 0 || !regexp.MustCompile(`^[0-9A-Fa-f]+$`).MatchString(clean) {
		return value
	}
	// UCS2 判定需要正向证据:至少含一个十六进制字母 A-F。纯数字同样是合法
	// "十六进制",旧实现会把长度为 4 倍数的纯数字串(8 位客服短号、"2024"
	// 这类数字正文/发送者)误解成 UCS2,产生 CJK/Unicode 乱码。真实的
	// 中文、字母等 UCS2 编码必然含 A-F 字母,据此可安全排除纯数字误判。
	if !regexp.MustCompile(`[A-Fa-f]`).MatchString(clean) {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(clean); i += 4 {
		n, err := strconv.ParseInt(clean[i:i+4], 16, 32)
		if err != nil {
			return value
		}
		b.WriteRune(rune(n))
	}
	return b.String()
}
