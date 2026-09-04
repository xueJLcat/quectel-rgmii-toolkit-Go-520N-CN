package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// at-client 退出码约定(定稿):
//   - 0: 成功,原始 transcript 已输出 stdout;
//   - 1: 业务/AT 执行失败(代理报 exec 错,或 transcript 非 OK 收尾);
//   - 2: 代理不可连接、请求/响应格式错误、用法错误。
//
// 与 flag.ExitOnError 的默认退出码 2 保持一致,便于脚本统一判断。
const (
	atClientExitOK         = 0 // 成功,原始 transcript 已输出 stdout
	atClientExitATFailed   = 1 // 业务/AT 执行失败(代理报 exec 错,或 transcript 非 OK 收尾)
	atClientExitProxyError = 2 // 代理不可连接、请求/响应格式错误、用法错误
)

// at-client 侧的超时与响应上限:
// 交换超时需覆盖服务端最坏情况:全局锁等待 200 秒 + 命令执行 180 秒;
// 响应上限防止异常输出撑爆内存(声明为变量便于测试注入更小的上限)。
const (
	atClientDialTimeout     = 5 * time.Second
	atClientExchangeTimeout = 400 * time.Second
)

var atClientMaxResponseBytes int64 = 16 << 20 // 16MB

// runATClientCommand 实现 simpleadmin-httpd at-client 子命令:
// 经 Unix Socket 把 AT 命令交给主进程的 AT 代理执行,
// 自身不直接访问串口设备,避免与主进程争抢 /dev/smd11。
func runATClientCommand(args []string) {
	fs := flag.NewFlagSet("at-client", flag.ExitOnError)
	sock := fs.String("sock", defaultATProxySock, "AT 代理 Unix Socket 路径")
	timeoutMS := fs.Int("timeout-ms", 0, "AT 命令超时(毫秒);0 = 服务端按命令类型自动决定")
	payload := fs.String("payload", "", "交互事务报文体:等待模块 \"> \" 提示符后发送并自动补 Ctrl-Z(CMGS/CMGW 发短信等场景)")
	_ = fs.Parse(args)

	command := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if command == "" {
		fmt.Fprintln(os.Stderr, "usage: simpleadmin-httpd at-client [--sock "+defaultATProxySock+"] [--timeout-ms 1000] [--payload 报文体] ATI")
		fmt.Fprintln(os.Stderr, "注意: 只经由 Unix Socket 连接服务端,不直接访问串口设备")
		os.Exit(atClientExitProxyError)
	}
	os.Exit(atClientRun(*sock, command, *payload, *timeoutMS))
}

// atClientRun 是 at-client 的可测试核心:发送请求、解析响应并返回退出码,
// 不直接调用 os.Exit。payload 非空时按交互事务执行(如发短信),
// 由服务端等待 "> " 提示符后转发报文体。
func atClientRun(sockPath, command, payload string, timeoutMS int) int {
	req := atProxyRequest{Command: command, Payload: payload, TimeoutMS: timeoutMS}
	// 请求必须保持单行:payload 中的换行经 JSON 转义为 \n,不影响分帧。
	payloadJSON, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 AT 代理失败: %v\n", err)
		return atClientExitProxyError
	}
	raw, err := atClientExchange(sockPath, payloadJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 AT 代理失败: %v\n", err)
		return atClientExitProxyError
	}

	var resp atProxyResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "AT 代理响应异常: %v\n", err)
		return atClientExitProxyError
	}
	if resp.Code == "protocol" {
		fmt.Fprintf(os.Stderr, "请求错误: %s\n", resp.Error)
		return atClientExitProxyError
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "AT 执行失败: %s\n", resp.Error)
		return atClientExitATFailed
	}

	// 原始 transcript 原样写 stdout,不追加/不删改任何字符,
	// 由调用方自行解析。
	fmt.Print(resp.Response)
	if !atResponseOK(resp.Response) {
		fmt.Fprintln(os.Stderr, "AT 响应非 OK")
		return atClientExitATFailed
	}
	return atClientExitOK
}

// atClientExchange 经 Unix Socket 发送单行 JSON 请求并读取单行 JSON 响应。
// 连接被拒、超时等错误原样返回,由上层统一归入退出码 2。
func atClientExchange(sockPath string, payload []byte) ([]byte, error) {
	conn, err := net.DialTimeout("unix", sockPath, atClientDialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(atClientExchangeTimeout)); err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(io.LimitReader(conn, atClientMaxResponseBytes))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		// 未读到换行即到 EOF:响应达到上限被截断(超大转录)。明确报错,
		// 避免把截断误诊为连接/代理故障,也防止把半截 JSON 当成功输出。
		if err == io.EOF && int64(len(line)) >= atClientMaxResponseBytes {
			return nil, fmt.Errorf("AT 响应超过上限 %d 字节,已被截断", atClientMaxResponseBytes)
		}
		return nil, err
	}
	return line, nil
}
