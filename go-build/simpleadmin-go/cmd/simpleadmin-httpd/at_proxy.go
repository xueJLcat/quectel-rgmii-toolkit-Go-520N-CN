package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// defaultATProxySock 是 AT 代理 Unix Socket 缺省路径,可用 -at-proxy-sock 覆盖。
	defaultATProxySock   = "/usrdata/simpleadmin/at_proxy.sock"
	atProxyMaxCommandLen = 4096   // 单条命令长度上限,防止滥用
	atProxyMaxPayloadLen = 4096   // 交互事务报文上限,短信 PDU/文本均远小于该值
	atProxyMaxTimeoutMS  = 180000 // 超时上限 3 分钟,与前端 API 请求超时对齐
	atProxyDialProbeMS   = 500    // 遗留 socket 探活超时
)

// atProxyWriteTimeout 限制"写响应"本身的时间,防半开客户端无限阻塞写端。
// 注意:它只约束写操作,不能覆盖 exec 的执行时长(最长可达锁等待+命令超时
// 数分钟);写时限必须在 exec 返回后、真正写响应前按当前时刻设置。
// 声明为变量以便测试注入更小的时限。
var atProxyWriteTimeout = 10 * time.Second

type atProxyRequest struct {
	Command string `json:"command"`
	// Payload 非空表示交互事务(如 AT+CMGS/CMGW):服务端先发 command,
	// 等待模块 "> " 提示符后写入 payload 并自动补 Ctrl-Z,再读取终结结果。
	// 协议保持一问一答,客户端无需感知提示符时序。
	Payload   string `json:"payload,omitempty"`
	TimeoutMS int    `json:"timeout_ms"` // 0 或缺省 = 按命令类型自动(atCommandTimeoutMS);payload 模式下忽略
}

type atProxyResponse struct {
	OK       bool   `json:"ok"`
	Response string `json:"response,omitempty"` // 原始 AT transcript,原样透传
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"` // "protocol"(请求格式/连接层)或 "exec"(AT 执行失败)
}

// atProxyExecutor 执行一条请求并返回原始 transcript。
// payload 非空时按交互事务执行(等 "> " 提示符后写入报文体)。
type atProxyExecutor func(command, payload string, timeoutMS int) (string, error)

// probeLiveATProxy 探测 sockPath 上是否有另一实例在监听:Dial 成功即说明
// 对端存活,立即关闭连接返回,不发送任何请求,对端只会看到 EOF,无副作用。
func probeLiveATProxy(sockPath string) bool {
	conn, err := net.DialTimeout("unix", sockPath, time.Duration(atProxyDialProbeMS)*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// startATProxy 在 sockPath 上启动 AT 代理监听,返回 listener 供外部关闭。
// 已存在的 socket 文件先探活:存活则拒绝启动(避免抢占另一实例);
// 不存活则判定为上次进程异常退出的残留(Unix socket 文件不会随进程退出自动清理),删除后继续。
func startATProxy(sockPath string, mock bool) (net.Listener, error) {
	if sockPath == "" {
		return nil, errors.New("AT 代理 socket 路径为空")
	}

	if _, err := os.Stat(sockPath); err == nil {
		if probeLiveATProxy(sockPath) {
			return nil, fmt.Errorf("另一个实例正在监听 %s", sockPath)
		}
		log.Printf("AT 代理发现遗留 socket %s(无存活监听),删除后继续", sockPath)
		if err := os.Remove(sockPath); err != nil {
			return nil, fmt.Errorf("删除遗留 socket %s 失败: %v", sockPath, err)
		}
	}

	// 首次运行时父目录(如 /usrdata/simpleadmin)可能尚不存在。
	if err := os.MkdirAll(filepath.Dir(sockPath), 0755); err != nil {
		return nil, fmt.Errorf("创建 AT 代理 socket 目录失败: %v", err)
	}

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("监听 AT 代理 socket %s 失败: %v", sockPath, err)
	}
	// 权限 0666:本机是单管理员设备,信任边界就是本机——允许任意本地进程
	// (如随附的 Windows 预览版客户端、调试脚本)调用;AT 层另有全局文件锁串行化兜底。
	if err := os.Chmod(sockPath, 0666); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("设置 AT 代理 socket %s 权限失败: %v", sockPath, err)
	}

	exec := func(command, payload string, timeoutMS int) (string, error) {
		return atProxyExecute(command, payload, timeoutMS, mock)
	}
	go func() {
		// 临时错误(EMFILE fd 耗尽等)按 5ms→1s 指数退避,与
		// net/http.Server.Serve 同款处理:不退避时 Accept 失败会立即返回,
		// 循环变成 100% CPU 热自旋并刷爆日志。
		var tempDelay time.Duration
		for {
			conn, err := listener.Accept()
			if err != nil {
				// listener 被外部 Close 是正常退出路径,不当错误处理。
				if errors.Is(err, net.ErrClosed) {
					return
				}
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Temporary() {
					if tempDelay == 0 {
						tempDelay = 5 * time.Millisecond
					} else {
						tempDelay *= 2
					}
					if tempDelay > time.Second {
						tempDelay = time.Second
					}
					log.Printf("AT 代理 accept 临时错误: %v,%s 后重试", err, tempDelay)
					time.Sleep(tempDelay)
					continue
				}
				log.Printf("AT 代理 accept 失败: %v", err)
				continue
			}
			tempDelay = 0
			go handleATProxyConn(conn, exec)
		}
	}()

	log.Printf("AT 代理已监听 %s(mock=%v)", sockPath, mock)
	return listener, nil
}

// handleATProxyConn 处理单个连接:读一行 JSON 请求、执行 AT、写回一行 JSON 结果,
// 一问一答后立即关闭,协议保持最简。
func handleATProxyConn(conn net.Conn, exec atProxyExecutor) {
	defer conn.Close()

	// 10 秒读超时防只连接不发数据的挂起;LimitReader 限单请求 64KB,防内存滥用。
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		// 读时限设置失败也要写回协议错误:此时尚未设置任何写时限,
		// 先补一个写时限兜底,防止半开对端堵死接收窗口时 Write 无限阻塞。
		_ = conn.SetWriteDeadline(time.Now().Add(atProxyWriteTimeout))
		writeATProxyError(conn, "protocol", fmt.Sprintf("设置读超时失败: %v", err))
		return
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 64*1024)).ReadString('\n')
	// 读请求已完成:清除请求阶段的读时限。协议错误的写回发生在 exec 之前,
	// 先按当前时刻设一个写时限兜底;执行结果的写回在 exec 返回后另行重置
	// 写时限(见该处注释)——exec 可能耗时数分钟,提前设置的绝对时限会在
	// 慢命令场景先于写操作过期,导致结果写不回客户端。
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Now().Add(atProxyWriteTimeout))
	if err != nil && err != io.EOF {
		writeATProxyError(conn, "protocol", fmt.Sprintf("读取请求失败: %v", err))
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		writeATProxyError(conn, "protocol", "请求为空行")
		return
	}

	var req atProxyRequest
	if jsonErr := json.Unmarshal([]byte(line), &req); jsonErr != nil {
		// 超长请求会被 LimitReader 截断,同样在此表现为 JSON 解析失败。
		writeATProxyError(conn, "protocol", fmt.Sprintf("请求 JSON 解析失败: %v", jsonErr))
		return
	}

	command := sanitizeATCommand(req.Command)
	if command == "" {
		writeATProxyError(conn, "protocol", "AT 命令为空")
		return
	}
	if len(command) > atProxyMaxCommandLen {
		writeATProxyError(conn, "protocol", fmt.Sprintf("AT 命令超长(上限 %d 字节)", atProxyMaxCommandLen))
		return
	}

	payload := req.Payload
	if payload != "" {
		if len(payload) > atProxyMaxPayloadLen {
			writeATProxyError(conn, "protocol", fmt.Sprintf("payload 超长(上限 %d 字节)", atProxyMaxPayloadLen))
			return
		}
		// ESC 会让模块提前退出输入态、吞掉报文体,属请求错误而非执行失败。
		if strings.Contains(payload, "\x1B") {
			writeATProxyError(conn, "protocol", "payload 不允许包含 ESC(0x1B)")
			return
		}
		// Ctrl-Z 是报文结束符,由服务端在报文体末尾自动补发(见 AT_PROXY.md);
		// 内嵌的 Ctrl-Z 会让模块提前结束输入态、截断正文,其后字节在命令态
		// 被当作新的 AT 命令执行,破坏事务边界,与 ESC 同规格拦截。
		if strings.Contains(payload, "\x1A") {
			writeATProxyError(conn, "protocol", "payload 不允许包含 Ctrl-Z(0x1A),结束符由服务端自动补发")
			return
		}
	}

	timeout := req.TimeoutMS
	switch {
	case timeout < 0:
		writeATProxyError(conn, "protocol", "timeout_ms 不能为负数")
		return
	case timeout == 0:
		// 未指定超时:按命令类型自动取值,与服务端其余执行路径策略一致。
		// 自动值同样必须封顶:组合命令按子命令累加预算(每段 QSCAN 计 180s,
		// 4096 字节可拼出数百段),不封顶则一条请求可把全局 AT 锁占住数小时,
		// 期间所有 AT 功能集体失败(违反 AT_PROXY.md 的 180s 上限约定)。
		timeout = atCommandTimeoutMS(command)
		if timeout > atProxyMaxTimeoutMS {
			timeout = atProxyMaxTimeoutMS
		}
	case timeout > atProxyMaxTimeoutMS:
		// 截断为上限,防止客户端传入无限超时把连接永久挂起。
		timeout = atProxyMaxTimeoutMS
	}

	out, execErr := exec(command, payload, timeout)
	// exec 可能耗时远超写时限(扫网 180 秒、排队等锁最长 200 秒),读请求后
	// 预设的绝对时限此时往往已过期。写响应的时刻才真正需要防卡死保护,
	// 故在真正写回前按当前时刻重置写时限;限制的是"写"本身,慢命令的
	// 成功/失败结果都能正常写回客户端。
	_ = conn.SetWriteDeadline(time.Now().Add(atProxyWriteTimeout))
	if execErr != nil {
		writeATProxyError(conn, "exec", execErr.Error())
		return
	}
	writeATProxyResponse(conn, atProxyResponse{OK: true, Response: out})
}

// writeATProxyResponse 写单行 JSON 响应(以 '\n' 结尾),客户端按行读取。
func writeATProxyResponse(conn net.Conn, resp atProxyResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// 固定结构序列化理论上不会失败;兜底降级为手写 protocol 错误。
		data = []byte(`{"ok":false,"error":"响应序列化失败","code":"protocol"}`)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		log.Printf("AT 代理写响应失败: %v", err)
	}
}

// writeATProxyError 写 protocol/exec 错误的单行 JSON 响应。
func writeATProxyError(conn net.Conn, code, message string) {
	writeATProxyResponse(conn, atProxyResponse{OK: false, Error: message, Code: code})
}

// atProxyExecute 是代理的 AT 执行入口:
//   - payload 非空 → 交互事务(CMGS/CMGW 形态),复用短信页生产验证过的
//     runSMSTransaction:等 "> " 提示符、写报文体 + Ctrl-Z、失败自动 ESC 中止;
//     事务内含提示符 10 秒与终结 60 秒预算,timeoutMS 不参与;
//   - 否则按普通命令走既有执行链。
//
// mock 模式两条路径都返回预制响应;真实模式与 Web 界面、缓存任务共享
// 同一把全局文件锁 + 设备锁,串行化 /dev/smd11 访问,杜绝多进程抢占。
func atProxyExecute(command, payload string, timeoutMS int, mock bool) (string, error) {
	if payload != "" {
		if mock {
			return mockATProxyTransactionResponse(), nil
		}
		return runSMSTransaction(command, payload)
	}
	if mock {
		return mockATResponse(command), nil
	}
	return runATCommandUntilDone(command, 200, timeoutMS)
}
