package main

import (
	"bufio"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------- R3-B3: 裸 +CMGL 命令也获得短信列表额外预算 ----------

func TestStandaloneSMSListCommandGetsExtraBudget(t *testing.T) {
	// 手动 AT 终端 / at-client 直接执行裸 +CMGL=4 时,旧实现只给 1 秒预算,
	// 收件箱稍多即假超时并把半截输出当数据。
	if got := atCommandTimeoutMS("AT+CMGL=4"); got < 1000+atSMSListExtraTimeoutMS {
		t.Fatalf("atCommandTimeoutMS(AT+CMGL=4) = %d, want >= %d", got, 1000+atSMSListExtraTimeoutMS)
	}
	if got := atCommandTimeoutMS(`AT+CMGL="ALL"`); got < 1000+atSMSListExtraTimeoutMS {
		t.Fatalf(`atCommandTimeoutMS(AT+CMGL="ALL") = %d, want >= %d`, got, 1000+atSMSListExtraTimeoutMS)
	}
	// 组合命令总预算保持原值(子命令各自计入额外预算,不再二次叠加)。
	smsList := smsListATCommand()
	if got := atCommandTimeoutMS(smsList); got != 21000 {
		t.Fatalf("atCommandTimeoutMS(sms list) = %d, want 21000", got)
	}
	// 普通命令不受影响。
	if got := atSingleCommandTimeoutMS("ATI"); got != 1000 {
		t.Fatalf("atSingleCommandTimeoutMS(ATI) = %d, want 1000", got)
	}
}

// ---------- R3-B4: 等待预算覆盖运行器重试最坏耗时 ----------

func TestATCacheWaitTimeoutCoversRunnerRetry(t *testing.T) {
	// QMBNCFG 单命令超时 8s;运行器超时后重试一次,最坏 2×8s+重试间隔,
	// Fetch 的等待预算必须覆盖,否则重试进行中就被判超时返回旧数据。
	command := "AT+QMBNCFG"
	timeout := atCommandTimeoutMS(command)
	wait := atCacheWaitTimeout(command)
	if want := time.Duration(2*timeout)*time.Millisecond + 250*time.Millisecond; wait < want {
		t.Fatalf("atCacheWaitTimeout = %s, want >= %s(覆盖重试最坏耗时)", wait, want)
	}
}

// ---------- R3-B2: 控制帧 125 字节上限 ----------

func TestConsoleControlFrameSizeCapped(t *testing.T) {
	ws, client := newPipeConsoleConn(t)
	payload := strings.Repeat("x", 200)
	go func() { _, _ = client.Write(wsMaskedFrame(true, 0x9, []byte(payload))) }()

	_, _, _, err := ws.readClientFrame()
	if err == nil || !strings.Contains(err.Error(), "control frame too large") {
		t.Fatalf("超大控制帧应被拒绝,实际: %v", err)
	}
}

func TestAPIWebSocketControlFrameSizeCapped(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = server.Close() })
	ws := &apiWebSocketConn{conn: server, br: bufio.NewReader(server)}

	payload := strings.Repeat("x", 200)
	go func() { _, _ = client.Write(wsMaskedFrame(true, 0x9, []byte(payload))) }()

	_, _, _, err := ws.readClientFrame()
	if err == nil || !strings.Contains(err.Error(), "control frame too large") {
		t.Fatalf("超大控制帧应被拒绝,实际: %v", err)
	}
}

// ---------- R3-B5: UTF-16 分段退化参数不死循环 ----------

func TestSplitSMSUTF16UnitsDegenerateLimitTerminates(t *testing.T) {
	// multiLimit==1 且段首为高代理项时,回退逻辑曾产生空段死循环。
	// "🙂AB" 的 UTF-16 码元为 [D83D DE42 0041 0042],段首即高代理项。
	message := "\U0001F642AB"
	done := make(chan [][]uint16, 1)
	go func() { done <- splitSMSUTF16Units(message, 1, 1) }()
	select {
	case segments := <-done:
		total := 0
		for _, seg := range segments {
			if len(seg) == 0 {
				t.Fatalf("分段结果含空段: %#v", segments)
			}
			total += len(seg)
		}
		if total != 4 {
			t.Fatalf("分段总码元数 = %d, want 4", total)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("splitSMSUTF16Units 退化参数下死循环")
	}
}

// ---------- R3-B1: 截止时刻竞态复判(结构断言) ----------

func TestSMDReaderDeadlineBranchRechecksBuffer(t *testing.T) {
	data, err := os.ReadFile("smd_reader.go")
	if err != nil {
		t.Fatalf("读取源码: %v", err)
	}
	src := string(data)
	// readUntilTokensWithEcho 的 deadline 分支是文件中第二处(排空函数还有一处)。
	idx := strings.LastIndex(src, "case <-deadline.C:")
	if idx < 0 {
		t.Fatalf("未找到 deadline 分支")
	}
	branch := src[idx:]
	if end := strings.Index(branch, "\n\t\t}"); end > 0 {
		branch = branch[:end]
	}
	if !strings.Contains(branch, "r.snapshot()") || !strings.Contains(branch, "containsAnyToken") {
		t.Fatalf("deadline 分支未在截止瞬间重取缓冲并复判终结符,迟到完整响应会被误报超时")
	}
}
