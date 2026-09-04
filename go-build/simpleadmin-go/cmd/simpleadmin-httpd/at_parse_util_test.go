// at_parse_util_test.go 覆盖 AT 解析工具的审计修复:
// csvFields 保留未加引号的空字段(旧正则会丢弃导致字段索引左移);
// atResponseOK 按行判定终结(子串 Contains("OK") 会被回显中的用户数据欺骗);
// hasATTerminalErrorLine/atResponseRebootOK:重启类命令只有出现明确终结
// 错误行才算失败,运行器超时无应答按"重启进行中"处理(修复 set_imei/reboot
// 被 atResponseOK 新语义误判失败的回归)。
package main

import (
	"reflect"
	"testing"
)

func TestCSVFields(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		// 空字段语义(本次修复点):未加引号的空字段必须保留,索引不左移。
		{"未加引号空字段保留", "1,,2", []string{"1", "", "2"}},
		{"连续多个空字段", "a,,,b", []string{"a", "", "", "b"}},
		{"首尾空字段", ",a,", []string{"", "a", ""}},
		{"成对引号的空字段", `a,"",b`, []string{"a", "", "b"}},
		{"空输入得单个空字段", "", []string{""}},
		// 引号语义:引号内逗号不是分隔点。
		{"引号内逗号不切分", ` "a,b",c`, []string{"a,b", "c"}},
		{"引号字段与普通字段混排", `"x","y,z",w`, []string{"x", "y,z", "w"}},
		// 既有非空响应回归:与旧实现逐字段一致。
		{"QMAP LANIP", ` "LANIP",192.168.225.20,192.168.225.170,192.168.225.1`, []string{"LANIP", "192.168.225.20", "192.168.225.170", "192.168.225.1"}},
		{"QMAP MAC_bind", ` "MAC_bind",1,"52:54:00:12:34:56","192.168.225.50"`, []string{"MAC_bind", "1", "52:54:00:12:34:56", "192.168.225.50"}},
		{"QMAP MPDN_rule", ` "MPDN_rule",0,0,0,0,0`, []string{"MPDN_rule", "0", "0", "0", "0", "0"}},
		{"CGCONTRDP 前四列", ` 1,0,"ctnet","100.104.47.236","36.14.4.64"`, []string{"1", "0", "ctnet", "100.104.47.236", "36.14.4.64"}},
		{"QSPN UCS2", ` "00430048004E002D00430054","00430054","4E2D56FD75354FE1",1,"46011"`, []string{"00430048004E002D00430054", "00430054", "4E2D56FD75354FE1", "1", "46011"}},
		{"QTEMP", `"modem-lte-sub6-pa1","40"`, []string{"modem-lte-sub6-pa1", "40"}},
		{"CMGL 引号时间戳为单字段", ` 1,1,"+8613800138000",,"26/08/30,10:20:30+32"`, []string{"1", "1", "+8613800138000", "", "26/08/30,10:20:30+32"}},
		{"CNUM 空号码", ` ,"",255`, []string{"", "", "255"}},
		{"QRSRP 负数", ` -85,-86,-86,-83,NR5G`, []string{"-85", "-86", "-86", "-83", "NR5G"}},
		{"两端空白被修剪", ` a , "b" `, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := csvFields(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("csvFields(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestATResponseOK(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		// 标准终结行。
		{"标准 OK 收尾", "AT+CGMI\r\nQuectel\r\nOK\r\n", true},
		{"小写 ok 大小写不敏感", "AT\r\nok\r\n", true},
		{"ERROR 收尾", "AT+CFUN?\r\nERROR\r\n", false},
		{"+CME ERROR 收尾", "AT+CFUN?\r\n+CME ERROR: 10\r\n", false},
		{"+CMS ERROR 收尾", "AT+CMGS=1\r\n+CMS ERROR: 500\r\n", false},
		// 回显/数据含 "OK" 子串不得误判为成功(本次修复点)。
		{"回显与数据含 OK 子串但终结为 ERROR", "AT+CMGS=\"10086OK\"\r\n> hello OK\r\nERROR\r\n", false},
		{"数据行含 OK 子串且无终结行", "+QMAP: \"OK_DATA\",1\r\n", false},
		// ERROR 立即失败,其后即便出现 OK 也不翻案。
		{"ERROR 之后的 OK 不改失败判定", "AT\r\nERROR\r\nOK\r\n", false},
		// 无终结行/空文本。
		{"无终结行", "ATI\r\nQuectel\r\n", false},
		{"空文本", "", false},
		// 保护期与运行器错误文本(含 "OK/ERROR" 字样)均不得判为成功。
		{"保护期 pending 文本", atCachePendingText, false},
		{"运行器错误文本", "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR", false},
		// 常见 mock transcript 形态。
		{"mock ATI transcript", "ATI\r\nQuectel\r\nRG520N-CN\r\nRevision: RG520NCNAAR02A02M4G_XM\r\nOK\r\n", true},
		{"交互事务 transcript", "> \r\n+CMGS: 1\r\nOK\r\n", true},
		{"组合查询以 ERROR 收尾", "AT+QMAP=\"DHCPV4DNS\"\r\n\r\nERROR\r\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atResponseOK(tt.text); got != tt.want {
				t.Fatalf("atResponseOK(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestHasATTerminalErrorLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"标准 OK 收尾无错误行", "AT+CGMI\r\nQuectel\r\nOK\r\n", false},
		{"ERROR 收尾", "AT+CFUN?\r\nERROR\r\n", true},
		{"小写 error 大小写不敏感", "AT\r\n error \r\n", true},
		{"+CME ERROR 收尾", "AT+CFUN?\r\n+CME ERROR: 10\r\n", true},
		{"+CMS ERROR 收尾", "AT+CMGS=1\r\n+CMS ERROR: 500\r\n", true},
		{"仅错误行无回显", "+CME ERROR: 10", true},
		{"数据行包含 ERROR 子串不是终结错误行", "+QMAP: \"ERROR_DATA\",1\r\nOK\r\n", false},
		// 运行器超时文本含 "OK/ERROR" 字样,但只是普通行,不是模块终结错误行。
		{"运行器超时错误文本", "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR", false},
		{"保护期 pending 文本", atCachePendingText, false},
		{"交互事务 transcript", "> \r\n+CMGS: 1\r\nOK\r\n", false},
		{"空文本", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasATTerminalErrorLine(tt.text); got != tt.want {
				t.Fatalf("hasATTerminalErrorLine(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestATResponseRebootOK(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"标准 OK 收尾", "AT+CFUN=1,1\r\nOK\r\n", true},
		{"ERROR 收尾判失败", "AT+CFUN=1,1\r\nERROR\r\n", false},
		{"+CME ERROR 收尾判失败", "AT+EGMR=1,7,\"012345678901234\";+CFUN=1,1\r\n+CME ERROR: 10\r\n", false},
		{"+CMS ERROR 收尾判失败", "AT+CFUN=1,1\r\n+CMS ERROR: 500\r\n", false},
		{"ERROR 之后的 OK 不改失败判定", "AT+CFUN=1,1\r\nERROR\r\nOK\r\n", false},
		// 重启类命令模块先重启、后应答:超时无应答是常态,按"重启进行中"处理。
		{"运行器超时错误文本按重启进行中", "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR", true},
		{"运行器动作不重试错误文本按重启进行中", "all AT device candidates failed (action not retried): /dev/smd11: read failed: timeout waiting for OK/ERROR", true},
		{"无终结行按重启进行中", "AT+CFUN=1,1\r\n", true},
		{"空文本按重启进行中", "", true},
		{"保护期 pending 文本无错误行", atCachePendingText, true},
		// 交互事务形态同样适用(无终结错误行即通过)。
		{"mock CMGS transcript", "> \r\n+CMGS: 1\r\nOK\r\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atResponseRebootOK(tt.text); got != tt.want {
				t.Fatalf("atResponseRebootOK(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}

	// 两种判定的关键分界:运行器超时文本对 rebootOK 为 true、对 atResponseOK
	// 为 false;标准成功/失败文本两者结论一致。
	runnerTimeout := "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR"
	if !atResponseRebootOK(runnerTimeout) || atResponseOK(runnerTimeout) {
		t.Fatalf("runner timeout text: rebootOK = %v, atResponseOK = %v, want true/false",
			atResponseRebootOK(runnerTimeout), atResponseOK(runnerTimeout))
	}
	cmgs := "> \r\n+CMGS: 1\r\nOK\r\n"
	if !atResponseRebootOK(cmgs) || !atResponseOK(cmgs) {
		t.Fatalf("CMGS transcript: rebootOK = %v, atResponseOK = %v, want true/true",
			atResponseRebootOK(cmgs), atResponseOK(cmgs))
	}
}
