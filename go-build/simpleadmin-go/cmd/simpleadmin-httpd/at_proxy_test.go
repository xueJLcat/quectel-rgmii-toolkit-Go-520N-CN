package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 AT 代理(at_proxy.go)与 at-client(at_proxy_client.go)。
// 所有 socket 一律放在 t.TempDir() 下,不触碰 /usrdata 与真实串口设备;
// 所有 listener 都通过 t.Cleanup 关闭,保证 accept 循环随 Close 退出,不泄漏 goroutine。

// atPipeExchange 用 net.Pipe 模拟一次客户端连接:写入单行请求、在独立
// goroutine 中运行 handleATProxyConn、读回单行响应并解析。
// 请求总以 '\n' 结尾,服务端读完整行后才会写响应,因此写读不会互相阻塞;
// 响应读回即代表 executor 已执行完毕,连接关闭后处理 goroutine 随之退出。
func atPipeExchange(t *testing.T, requestLine string, exec atProxyExecutor) atProxyResponse {
	t.Helper()
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleATProxyConn(server, exec)
	}()
	if _, err := client.Write([]byte(requestLine + "\n")); err != nil {
		// 协议错误路径下服务端可能已提前写响应并关闭,写失败不影响断言。
		t.Logf("写入请求失败: %v", err)
	}
	line, err := bufio.NewReader(client).ReadString('\n')
	_ = client.Close()
	<-done
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	var resp atProxyResponse
	if err := json.Unmarshal([]byte(strings.TrimRight(line, "\r\n")), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %q, %v", line, err)
	}
	return resp
}

// mustNotExec 返回一个"绝不应被调用"的 executor:协议校验失败的请求
// 必须在进入执行阶段前被拦截。它运行在 handleATProxyConn 的 goroutine 中,
// 只能用 t.Error 而非 t.Fatal(后者只能在测试主 goroutine 调用)。
func mustNotExec(t *testing.T) atProxyExecutor {
	t.Helper()
	return func(command, payload string, timeoutMS int) (string, error) {
		t.Errorf("executor 不应被调用: command=%q payload=%q timeout_ms=%d", command, payload, timeoutMS)
		return "", nil
	}
}

// listenThenLeaveSocketFile 监听后立即关闭,但保留 socket 文件,
// 模拟进程异常退出留下的残留文件。Go 的 UnixListener 在 Linux 上
// Close 默认会删除文件,需 SetUnlinkOnClose(false) 才能留下。
func listenThenLeaveSocketFile(t *testing.T, sockPath string) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("准备遗留 socket 失败: %v", err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	ln.Close()
	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("遗留 socket 文件应存在: %v", err)
	}
}

// startStubATProxy 用 net.Listen 手写一个最小 AT 代理:每个连接先读完
// 一行请求,再写回固定的单行响应。不经过真实执行链,用于在不起串口/
// mock 全局状态的前提下验证 atClientRun 的退出码分支。
// accept 循环在 t.Cleanup 关闭 listener 时退出。
func startStubATProxy(t *testing.T, sockPath, responseLine string) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("启动存根 AT 代理失败: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener 被 Close,正常退出
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = bufio.NewReader(c).ReadString('\n') // 先收完请求,保持一问一答顺序
				_, _ = c.Write([]byte(responseLine + "\n"))
			}(conn)
		}
	}()
}

func TestATProxyStartValidation(t *testing.T) {
	t.Run("空路径报错", func(t *testing.T) {
		ln, err := startATProxy("", true)
		if err == nil {
			_ = ln.Close()
			t.Fatal("空 socket 路径未报错")
		}
		if !strings.Contains(err.Error(), "socket 路径为空") {
			t.Fatalf("错误信息 = %q, want 包含 %q", err.Error(), "socket 路径为空")
		}
	})

	t.Run("已有存活监听拒绝启动", func(t *testing.T) {
		// 先用普通 listener 占住路径,模拟另一实例已在运行;
		// startATProxy 必须探活成功并拒绝,否则会抢走对方的连接。
		sock := filepath.Join(t.TempDir(), "at.sock")
		occupied, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("准备占位监听失败: %v", err)
		}
		t.Cleanup(func() { occupied.Close() })
		ln, err := startATProxy(sock, true)
		if err == nil {
			_ = ln.Close()
			t.Fatal("存在存活监听时未拒绝启动")
		}
		if !strings.Contains(err.Error(), "另一个实例正在监听") {
			t.Fatalf("错误信息 = %q, want 包含 %q", err.Error(), "另一个实例正在监听")
		}
	})

	t.Run("遗留 socket 文件删除后正常启动", func(t *testing.T) {
		// listen 后关闭但保留文件,模拟上次进程异常退出留下的残留文件;
		// startATProxy 应探活失败、删除残留后正常启动。
		sock := filepath.Join(t.TempDir(), "at.sock")
		listenThenLeaveSocketFile(t, sock)
		ln, err := startATProxy(sock, true)
		if err != nil {
			t.Fatalf("遗留 socket 上启动失败: %v", err)
		}
		t.Cleanup(func() { ln.Close() })
		if !probeLiveATProxy(sock) {
			t.Fatal("清理遗留文件后应可正常连接")
		}
	})

	t.Run("启动成功文件存在且可连接", func(t *testing.T) {
		// 路径放在多层尚不存在的子目录下,顺带验证 MkdirAll 补建父目录。
		sock := filepath.Join(t.TempDir(), "sub", "dir", "at.sock")
		ln, err := startATProxy(sock, true)
		if err != nil {
			t.Fatalf("启动失败: %v", err)
		}
		t.Cleanup(func() { ln.Close() })
		if _, err := os.Stat(sock); err != nil {
			t.Fatalf("启动后 socket 文件不存在: %v", err)
		}
		if !probeLiveATProxy(sock) {
			t.Fatal("启动后探活应成功")
		}
	})
}

func TestATProxyHandleConnProtocolErrors(t *testing.T) {
	// 协议错误必须拦截在 executor 之前,且统一回 code=protocol:
	// 客户端据此归入退出码 2(请求/连接层错误),与 AT 业务失败(1)区分。
	longCommand := strings.Repeat("A", atProxyMaxCommandLen+1)
	longPayload := strings.Repeat("B", atProxyMaxPayloadLen+1)
	tests := []struct {
		name      string
		request   string // 单行请求(不含结尾换行,由 atPipeExchange 补 '\n')
		wantError string // 错误信息应包含的子串
	}{
		{"空行", "", "请求为空行"},
		{"纯空白行", " \t ", "请求为空行"},
		{"非法 JSON", "{not-json", "JSON 解析失败"},
		{"command 为空", `{"command":"","timeout_ms":0}`, "AT 命令为空"},
		// \r\n 会被 sanitizeATCommand 剔除,剔除后为空同样按空命令处理。
		{"command 仅换行转义", `{"command":"\r\n","timeout_ms":0}`, "AT 命令为空"},
		{"command 超长", `{"command":"` + longCommand + `","timeout_ms":0}`, "AT 命令超长"},
		{"timeout_ms 为负", `{"command":"ATI","timeout_ms":-1}`, "timeout_ms 不能为负数"},
		{"payload 超长", `{"command":"ATI","payload":"` + longPayload + `"}`, "payload 超长"},
		// ESC 会让模块提前退出输入态,必须在执行前拦截(\u001b = ESC)。
		{"payload 含 ESC", `{"command":"ATI","payload":"abc\u001Bdef"}`, "不允许包含 ESC"},
		// Ctrl-Z 是报文结束符,由服务端自动补发;内嵌 Ctrl-Z 会截断正文
		// 且其后字节被模块当作新 AT 命令执行(\u001a = Ctrl-Z)。
		{"payload 含 Ctrl-Z", `{"command":"ATI","payload":"abc\u001Adef"}`, "不允许包含 Ctrl-Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := atPipeExchange(t, tt.request, mustNotExec(t))
			if resp.OK {
				t.Fatalf("协议错误请求返回 ok=true: %+v", resp)
			}
			if resp.Code != "protocol" {
				t.Fatalf("code = %q, want protocol", resp.Code)
			}
			if !strings.Contains(resp.Error, tt.wantError) {
				t.Fatalf("error = %q, want 包含 %q", resp.Error, tt.wantError)
			}
		})
	}
}

func TestATProxyHandleConnExecAndTimeout(t *testing.T) {
	t.Run("executor 报错回 exec", func(t *testing.T) {
		// executor 返回 error 时:ok=false、code=exec,错误原文透传,
		// 客户端据此归入退出码 1(AT 业务失败)。
		exec := func(string, string, int) (string, error) { return "", errors.New("串口打开失败") }
		resp := atPipeExchange(t, `{"command":"ATI","timeout_ms":1000}`, exec)
		if resp.OK || resp.Code != "exec" {
			t.Fatalf("响应 = %+v, want ok=false code=exec", resp)
		}
		if !strings.Contains(resp.Error, "串口打开失败") {
			t.Fatalf("error = %q, want 透传 executor 错误", resp.Error)
		}
	})

	t.Run("成功返回原始 transcript", func(t *testing.T) {
		exec := func(string, string, int) (string, error) { return "ATI\r\nOK\r\n", nil }
		resp := atPipeExchange(t, `{"command":"ATI","timeout_ms":1000}`, exec)
		if !resp.OK || resp.Code != "" {
			t.Fatalf("响应 = %+v, want ok=true 无 code", resp)
		}
		if resp.Response != "ATI\r\nOK\r\n" {
			t.Fatalf("response = %q, want 原样透传", resp.Response)
		}
	})

	t.Run("慢命令结果仍能写回客户端", func(t *testing.T) {
		// 写时限只约束"写"本身,不能覆盖 exec 时长:exec 远超写时限时
		// (扫网 180 秒、排队等锁),若在读请求后就固定写时限,慢命令写回时
		// 时限早已过期,结果会写丢。时限必须在 exec 返回后按当前时刻重置。
		prev := atProxyWriteTimeout
		atProxyWriteTimeout = 100 * time.Millisecond
		t.Cleanup(func() { atProxyWriteTimeout = prev })

		exec := func(string, string, int) (string, error) {
			time.Sleep(300 * time.Millisecond) // 远超注入的 100ms 写时限
			return "AT+QSCAN=3,1\r\nOK\r\n", nil
		}
		resp := atPipeExchange(t, `{"command":"AT+QSCAN=3,1","timeout_ms":180000}`, exec)
		if !resp.OK {
			t.Fatalf("慢命令结果未写回客户端: %+v", resp)
		}
		if !strings.Contains(resp.Response, "OK") {
			t.Fatalf("response = %q, want 原样透传", resp.Response)
		}
	})

	// 捕获 executor 入参,验证超时推导/透传/截断都发生在进入执行阶段之前。
	type capturedExec struct {
		command   string
		payload   string
		timeoutMS int
	}
	tests := []struct {
		name          string
		request       atProxyRequest
		wantCommand   string
		wantPayload   string
		wantTimeoutMS int
	}{
		{
			// ATI 属普通命令,atSingleCommandTimeoutMS 默认档 1000 毫秒。
			name:          "timeout_ms 为 0 按命令类型推导",
			request:       atProxyRequest{Command: "ATI"},
			wantCommand:   "ATI",
			wantTimeoutMS: 1000,
		},
		{
			name:          "显式超时原样透传",
			request:       atProxyRequest{Command: "ATI", TimeoutMS: 5000},
			wantCommand:   "ATI",
			wantTimeoutMS: 5000,
		},
		{
			// 超过 atProxyMaxTimeoutMS 必须截断为上限,防止无限超时挂起连接。
			name:          "超上限截断为 atProxyMaxTimeoutMS",
			request:       atProxyRequest{Command: "ATI", TimeoutMS: atProxyMaxTimeoutMS + 1},
			wantCommand:   "ATI",
			wantTimeoutMS: atProxyMaxTimeoutMS,
		},
		{
			// executor 收到的应是清理后的命令。
			name:          "命令两端空白被清理后传给 executor",
			request:       atProxyRequest{Command: "  ATI  ", TimeoutMS: 2000},
			wantCommand:   "ATI",
			wantTimeoutMS: 2000,
		},
		{
			// payload 原样透传给 executor(交互事务报文体不做清理,
			// 换行经 JSON 转义还原后保留;Ctrl-Z/ESC 属协议错误,见
			// TestATProxyHandleConnProtocolErrors)。
			name:          "payload 原样透传给 executor",
			request:       atProxyRequest{Command: `AT+CMGS="+10086"`, Payload: "hello\r\nworld", TimeoutMS: 1000},
			wantCommand:   `AT+CMGS="+10086"`,
			wantPayload:   "hello\r\nworld",
			wantTimeoutMS: 1000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captured := make(chan capturedExec, 1)
			exec := func(command, payload string, timeoutMS int) (string, error) {
				captured <- capturedExec{command, payload, timeoutMS}
				return "OK\r\n", nil
			}
			payload, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("构造请求失败: %v", err)
			}
			resp := atPipeExchange(t, string(payload), exec)
			if !resp.OK {
				t.Fatalf("响应 = %+v, want ok=true", resp)
			}
			got := <-captured
			if got.command != tt.wantCommand || got.timeoutMS != tt.wantTimeoutMS {
				t.Fatalf("executor 入参 = (%q, %d), want (%q, %d)",
					got.command, got.timeoutMS, tt.wantCommand, tt.wantTimeoutMS)
			}
			if got.payload != tt.wantPayload {
				t.Fatalf("executor payload = %q, want %q", got.payload, tt.wantPayload)
			}
		})
	}
}

func TestATProxyProbeLive(t *testing.T) {
	t.Run("存活监听返回 true", func(t *testing.T) {
		sock := filepath.Join(t.TempDir(), "at.sock")
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("监听失败: %v", err)
		}
		t.Cleanup(func() { ln.Close() })
		if !probeLiveATProxy(sock) {
			t.Fatal("存活监听应探测为 true")
		}
	})

	t.Run("文件不存在返回 false", func(t *testing.T) {
		if probeLiveATProxy(filepath.Join(t.TempDir(), "no.sock")) {
			t.Fatal("不存在的路径应探测为 false")
		}
	})

	t.Run("文件存在但无监听返回 false", func(t *testing.T) {
		sock := filepath.Join(t.TempDir(), "at.sock")
		listenThenLeaveSocketFile(t, sock) // 只剩文件、无人监听(拨号会被拒绝)
		if probeLiveATProxy(sock) {
			t.Fatal("遗留文件应探测为 false")
		}
	})
}

func TestATProxyEndToEndMock(t *testing.T) {
	// 真实 startATProxy(mock=true)走内置 mockATResponse,不触碰串口;
	// 通过直接 socket 请求与 atClientRun 两条路径验证完整链路。
	sock := filepath.Join(t.TempDir(), "at.sock")
	ln, err := startATProxy(sock, true)
	if err != nil {
		t.Fatalf("启动 mock AT 代理失败: %v", err)
	}
	closed := false
	closeListener := func() {
		if !closed {
			closed = true
			_ = ln.Close()
		}
	}
	t.Cleanup(closeListener)

	t.Run("直接 socket 请求 ATI 得到 mock transcript", func(t *testing.T) {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatalf("连接代理失败: %v", err)
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("设置超时失败: %v", err)
		}
		if _, err := conn.Write([]byte(`{"command":"ATI","timeout_ms":0}` + "\n")); err != nil {
			t.Fatalf("写请求失败: %v", err)
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("读响应失败: %v", err)
		}
		var resp atProxyResponse
		if err := json.Unmarshal([]byte(strings.TrimRight(line, "\r\n")), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %q, %v", line, err)
		}
		if !resp.OK || resp.Code != "" {
			t.Fatalf("响应 = %+v, want ok=true 无 code", resp)
		}
		// mock_at_responses.go 中 ATI 的输出形态为
		// "ATI\r\nQuectel\r\nRG520N-CN\r\nRevision: ...\r\nOK\r\n"。
		if !strings.Contains(resp.Response, "Quectel") || !strings.Contains(resp.Response, mockModuleModel) {
			t.Fatalf("response 缺少预期 mock 内容: %q", resp.Response)
		}
		if !atResponseOK(resp.Response) {
			t.Fatalf("response 应以 OK 收尾: %q", resp.Response)
		}
	})

	// atClientRun 用 fmt.Print 把 transcript 直接写到真实 stdout,
	// 这里不捕获标准输出,只断言退出码与不 panic。
	t.Run("atClientRun 成功返回 0", func(t *testing.T) {
		if code := atClientRun(sock, "ATI", "", 0); code != atClientExitOK {
			t.Fatalf("退出码 = %d, want %d", code, atClientExitOK)
		}
	})

	t.Run("atClientRun transcript 非 OK 收尾返回 1", func(t *testing.T) {
		// 裸查询 AT+QMAP="DHCPV4DNS" 在 mock 下以 ERROR 收尾
		// (见 mockSettingsStatusResponse):代理层成功但 AT 业务失败 → 退出码 1。
		if code := atClientRun(sock, `AT+QMAP="DHCPV4DNS"`, "", 0); code != atClientExitATFailed {
			t.Fatalf("退出码 = %d, want %d", code, atClientExitATFailed)
		}
	})

	t.Run("payload 交互事务 mock 返回 0", func(t *testing.T) {
		// payload 非空走交互事务路径;mock 返回 "> \r\n+CMGS: 1\r\nOK\r\n",
		// 含 OK 且无 ERROR → 退出码 0。
		code := atClientRun(sock, `AT+CMGS="+10086"`, "hello", 0)
		if code != atClientExitOK {
			t.Fatalf("退出码 = %d, want %d", code, atClientExitOK)
		}
	})

	closeListener()
	// listener.Close 后 accept 循环应以 net.ErrClosed 退出;
	// 轮询确认端口已释放,避免 goroutine/连接泄漏。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && probeLiveATProxy(sock) {
		time.Sleep(10 * time.Millisecond)
	}
	if probeLiveATProxy(sock) {
		t.Fatal("关闭 listener 后代理仍存活")
	}
}

func TestATClientRunExitCodes(t *testing.T) {
	// 退出码语义(见 at_proxy_client.go 头部约定):
	//   0 = 代理成功且 transcript 以 OK 收尾;
	//   1 = AT 业务失败(exec 错,或 transcript 含 ERROR/无 OK);
	//   2 = 代理不可连、请求/响应格式错误等连接与协议层问题。
	// 用写死响应的存根服务端隔离真实执行链,逐一覆盖三条分支。
	line := func(resp atProxyResponse) string {
		t.Helper()
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("构造存根响应失败: %v", err)
		}
		return string(data)
	}

	tests := []struct {
		name     string
		response string // 存根回写的单行响应;仅 noListen 用例为空
		noListen bool   // 路径从未存在
		deadFile bool   // socket 文件存在但无监听(连接被拒)
		want     int
	}{
		{
			name:     "成功且 transcript 以 OK 收尾返回 0",
			response: line(atProxyResponse{OK: true, Response: "ATI\r\nOK\r\n"}),
			want:     atClientExitOK,
		},
		{
			// 边界:代理层 ok=true,但 transcript 含 ERROR(atResponseOK 为
			// false)仍属 AT 业务失败 → 1,而不是连接/协议层的 2。
			name:     "transcript 含 ERROR 收尾返回 1",
			response: line(atProxyResponse{OK: true, Response: "AT+CFUN?\r\n+CME ERROR: 10\r\n"}),
			want:     atClientExitATFailed,
		},
		{
			// 边界:transcript 既无 OK 也无 ERROR 同样不满足 atResponseOK → 1。
			name:     "transcript 无 OK 返回 1",
			response: line(atProxyResponse{OK: true, Response: "READY\r\n"}),
			want:     atClientExitATFailed,
		},
		{
			// code=protocol 表示请求/连接层错误,与用法错误同级 → 2。
			name:     "服务端报 protocol 返回 2",
			response: line(atProxyResponse{OK: false, Error: "请求为空行", Code: "protocol"}),
			want:     atClientExitProxyError,
		},
		{
			name:     "服务端报 exec 返回 1",
			response: line(atProxyResponse{OK: false, Error: "串口打开失败", Code: "exec"}),
			want:     atClientExitATFailed,
		},
		{
			// 响应格式损坏按代理层错误处理 → 2。
			name:     "响应非 JSON 返回 2",
			response: "garbage-not-json",
			want:     atClientExitProxyError,
		},
		{
			name:     "socket 不存在返回 2",
			noListen: true,
			want:     atClientExitProxyError,
		},
		{
			// 残留文件无人监听时拨号被拒,同样归入代理不可连 → 2。
			name:     "socket 文件存在但无监听返回 2",
			deadFile: true,
			want:     atClientExitProxyError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sock := filepath.Join(t.TempDir(), "at.sock")
			switch {
			case tt.noListen:
				// 路径保持不存在
			case tt.deadFile:
				listenThenLeaveSocketFile(t, sock)
			default:
				startStubATProxy(t, sock, tt.response)
			}
			// atClientRun 用 fmt.Print/Fprintln 直写真实 stdout/stderr,
			// 不便捕获;此处只断言退出码与不 panic。
			if got := atClientRun(sock, "ATI", "", 0); got != tt.want {
				t.Fatalf("atClientRun 退出码 = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------- mock MAC_bind 有状态行为(查-增-查-改-删-查) ----------

// resetMockMacBind 把包级 mock 绑定表复位到初态并返回复原函数,
// 保证测试不泄漏状态给其它用例。
func resetMockMacBind(t *testing.T) {
	t.Helper()
	mockMacBindMu.Lock()
	prev := mockMacBindEntries
	mockMacBindEntries = []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}}
	mockMacBindMu.Unlock()
	t.Cleanup(func() {
		mockMacBindMu.Lock()
		mockMacBindEntries = prev
		mockMacBindMu.Unlock()
	})
}

func TestMockMacBindStatefulSequence(t *testing.T) {
	resetMockMacBind(t)

	query := func() (string, []macBindLive) {
		t.Helper()
		resp := mockATResponse(`AT+QMAP="MAC_bind"`)
		if !atResponseOK(resp) {
			t.Fatalf("MAC_bind 查询响应应以 OK 收尾: %q", resp)
		}
		return resp, parseMacBindAT(resp)
	}

	// 初查:固定一条示例绑定,回显首行为原命令,渲染可被 parseMacBindAT 解析。
	resp, live := query()
	if !strings.HasPrefix(resp, "AT+QMAP=\"MAC_bind\"\r\n") {
		t.Fatalf("查询响应首行应为原命令回显: %q", resp)
	}
	want := []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}}
	if !reflect.DeepEqual(live, want) {
		t.Fatalf("初始查询 = %#v, want %#v", live, want)
	}

	// 增:新槽位写入后再查应多一条。
	if !atResponseOK(mockATResponse(`AT+QMAP="MAC_bind",2,"AA:BB:CC:DD:EE:99","192.168.225.77"`)) {
		t.Fatal("MAC_bind 写命令应以 OK 收尾")
	}
	_, live = query()
	want = append(want, macBindLive{Index: 2, MAC: "AA:BB:CC:DD:EE:99", IP: "192.168.225.77"})
	if !reflect.DeepEqual(live, want) {
		t.Fatalf("新增后查询 = %#v, want %#v", live, want)
	}

	// 改:同槽位覆盖,条数不变、IP 更新。
	if !atResponseOK(mockATResponse(`AT+QMAP="MAC_bind",2,"AA:BB:CC:DD:EE:99","192.168.225.78"`)) {
		t.Fatal("MAC_bind 覆盖写命令应以 OK 收尾")
	}
	_, live = query()
	if len(live) != 2 || live[1].IP != "192.168.225.78" {
		t.Fatalf("覆盖写后查询 = %#v, want 槽位 2 的 IP 更新为 192.168.225.78", live)
	}

	// 删:先删新增条目回到初态,再删初始条目得到空表(查询只回 OK)。
	if !atResponseOK(mockATResponse(`AT+QMAP="MAC_bind",2,"",""`)) {
		t.Fatal("MAC_bind 删除命令应以 OK 收尾")
	}
	_, live = query()
	if len(live) != 1 || live[0].Index != 1 {
		t.Fatalf("删除槽位 2 后查询 = %#v, want 只剩初始一条", live)
	}
	if !atResponseOK(mockATResponse(`AT+QMAP="MAC_bind",1,"",""`)) {
		t.Fatal("MAC_bind 删除命令应以 OK 收尾")
	}
	resp, live = query()
	if len(live) != 0 {
		t.Fatalf("清空后查询 = %#v, want 空", live)
	}
	if !strings.HasSuffix(strings.TrimRight(resp, "\r\n"), "OK") || strings.Contains(resp, "+QMAP:") {
		t.Fatalf("空表查询应只回命令回显 + OK: %q", resp)
	}
}

// ---------- mock 设置类写命令只回 OK ----------

func TestMockSettingsWriteCommandAnswersOnlyOK(t *testing.T) {
	// 真实固件对写命令只回 OK;mock 写响应混入查询态状态行会误导调用方。
	writes := []string{
		`AT+QMAP="DHCPV4DNS","enable"`,
		`AT+QMAP="MPDN_RULE",0`,
		`AT+QMAP="MPDN_RULE",0,1,0,1,1,"FF:FF:FF:FF:FF:FF"`,
		`AT+QMAP="DMZ",0`,
		`AT+QMAP="DMZ",1,4,192.168.225.99`,
		`AT+QCFG="usbnet",1`,
	}
	for _, command := range writes {
		resp := mockATResponse(command)
		if !atResponseOK(resp) {
			t.Fatalf("写命令 %q 响应应以 OK 收尾: %q", command, resp)
		}
		if strings.Contains(resp, "+QMAP:") || strings.Contains(resp, "+QCFG:") {
			t.Fatalf("写命令 %q 响应不应附带状态行: %q", command, resp)
		}
	}

	// 裸查询与分号组合读命令行为不变:仍返回状态行。
	composite := `AT+QMAP="MPDN_RULE";+QMAP="DHCPV6DNS";+QCFG="usbnet";+QMAP="DMZ";+QMAP="DHCPV4DNS"`
	resp := mockATResponse(composite)
	for _, wantLine := range []string{`+QMAP: "MPDN_rule",3,0,0,0,0`, `+QMAP: "DHCPV6DNS","disable"`, `+QMAP: "DMZ",0,4`} {
		if !strings.Contains(resp, wantLine) {
			t.Fatalf("组合读命令响应缺少状态行 %q: %q", wantLine, resp)
		}
	}
	// 裸 DHCPV4DNS 查询在该固件上不支持,以 ERROR 收尾(at-client 退出码 1 的来源)。
	if atResponseOK(resp) {
		t.Fatalf("组合读命令应以 ERROR 收尾: %q", resp)
	}
}
