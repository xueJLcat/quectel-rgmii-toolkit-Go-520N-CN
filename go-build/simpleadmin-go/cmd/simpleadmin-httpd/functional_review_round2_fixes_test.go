package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- R2-B1: 控制台 WS 空首分片 ----------

// wsMaskedFrame 构造客户端到服务端的带掩码帧(支持 7/16 位长度)。
func wsMaskedFrame(fin bool, opcode byte, payload []byte) []byte {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	header := []byte{b0}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n <= 0xffff:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127,
			byte(0), byte(0), byte(0), byte(0),
			byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	mask := [4]byte{1, 2, 3, 4}
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	return append(append(header, mask[:]...), masked...)
}

func newPipeConsoleConn(t *testing.T) (*nativeWSConn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = server.Close() })
	return &nativeWSConn{conn: server, br: bufio.NewReader(server), rows: 24, cols: 80}, client
}

// TestConsoleReadClientMessageEmptyFirstFragment RFC 6455 允许空的首分片;
// 旧实现以 buf==nil 判定分片状态,空首分片(空载荷不改变 nil)使后续延续帧
// 全被当作孤立帧丢弃,整条消息静默丢失。
func TestConsoleReadClientMessageEmptyFirstFragment(t *testing.T) {
	ws, client := newPipeConsoleConn(t)

	var frames []byte
	frames = append(frames, wsMaskedFrame(false, 0x2, nil)...)           // 空首分片
	frames = append(frames, wsMaskedFrame(false, 0x0, []byte("hel"))...) // 延续
	frames = append(frames, wsMaskedFrame(true, 0x0, []byte("lo"))...)   // 终结
	go func() { _, _ = client.Write(frames) }()

	opcode, payload, err := ws.readClientMessage()
	if err != nil {
		t.Fatalf("readClientMessage: %v", err)
	}
	if opcode != 0x2 || string(payload) != "hello" {
		t.Fatalf("消息重组结果 = (%#x, %q), want (0x2, \"hello\")", opcode, payload)
	}
}

// TestConsoleReadClientMessageNormalFragmentation 常规分片与单帧行为不受影响。
func TestConsoleReadClientMessageNormalFragmentation(t *testing.T) {
	ws, client := newPipeConsoleConn(t)

	var frames []byte
	frames = append(frames, wsMaskedFrame(true, 0x2, []byte("one"))...)
	frames = append(frames, wsMaskedFrame(false, 0x2, []byte("ab"))...)
	frames = append(frames, wsMaskedFrame(true, 0x0, []byte("cd"))...)
	go func() { _, _ = client.Write(frames) }()

	opcode, payload, err := ws.readClientMessage()
	if err != nil || opcode != 0x2 || string(payload) != "one" {
		t.Fatalf("单帧消息 = (%#x, %q, %v), want (0x2, \"one\", nil)", opcode, payload, err)
	}
	opcode, payload, err = ws.readClientMessage()
	if err != nil || opcode != 0x2 || string(payload) != "abcd" {
		t.Fatalf("分片消息 = (%#x, %q, %v), want (0x2, \"abcd\", nil)", opcode, payload, err)
	}
}

// ---------- R2-B2: 看门狗探测预算随目标数缩放 ----------

func TestWatchdogProbeBudgetScalesWithTargets(t *testing.T) {
	if got, want := watchdogProbeBudget(1), dialTargetTimeout+500*time.Millisecond; got != want {
		t.Fatalf("budget(1) = %s, want %s", got, want)
	}
	if got, want := watchdogProbeBudget(4), 4*dialTargetTimeout+500*time.Millisecond; got != want {
		t.Fatalf("budget(4) = %s, want %s", got, want)
	}
	// 预算必须覆盖最大目标数的串行拨号最坏耗时,否则末尾目标被掐死。
	if budget := watchdogProbeBudget(watchdogMaxTargets); budget < time.Duration(watchdogMaxTargets)*dialTargetTimeout {
		t.Fatalf("budget(%d) = %s 不足以覆盖串行拨号", watchdogMaxTargets, budget)
	}
	if got := watchdogProbeBudget(0); got < dialTargetTimeout {
		t.Fatalf("budget(0) = %s, 目标数兜底失效", got)
	}
}

// ---------- R2-B3: 号码归一化拒绝退化输入 ----------

func TestNormalizeSMSNumberDegenerateInputs(t *testing.T) {
	cimi := "AT+CIMI\r\n460111234567890\r\nOK\r\n"
	for _, in := range []string{"00", "0-0", "0 0", "+", "+-", "00  -"} {
		got, err := normalizeSMSNumber(in, cimi)
		if err != nil {
			t.Fatalf("normalizeSMSNumber(%q) 不应报错: %v", in, err)
		}
		if got != "" {
			t.Fatalf("normalizeSMSNumber(%q) = %q, want 空号码(退化输入不得发起真实发送)", in, got)
		}
	}
	// 正常国际前缀不受影响。
	got, err := normalizeSMSNumber("008613800138000", cimi)
	if err != nil || got != "+8613800138000" {
		t.Fatalf("normalizeSMSNumber(0086...) = (%q, %v), want +8613800138000", got, err)
	}
}

// ---------- R2-B4: 防火墙规则文件写入往返 ----------

func TestWriteFirewallRulesFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall_ports.conf")
	rules := []firewallRule{{Action: "block", Port: "8080"}, {Action: "accept", Port: "22"}}
	if err := writeFirewallRulesFile(path, rules); err != nil {
		t.Fatalf("writeFirewallRulesFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取规则文件: %v", err)
	}
	if want := "block 8080\naccept 22\n"; string(data) != want {
		t.Fatalf("规则文件内容 = %q, want %q", data, want)
	}
}

// ---------- R2-B6: at_proxy 协议错误写回有写时限 ----------

// TestATProxyErrorWriteHasDeadline 通过源码断言保证读时限设置失败分支
// 在写错误前设置写时限(防半开对端无限阻塞写端)。
func TestATProxyErrorWriteHasDeadline(t *testing.T) {
	src, err := os.ReadFile("at_proxy.go")
	if err != nil {
		t.Fatalf("读取源码: %v", err)
	}
	idx := strings.Index(string(src), "设置读超时失败")
	if idx < 0 {
		t.Fatalf("未找到读超时失败分支")
	}
	start := idx - 400
	if start < 0 {
		start = 0
	}
	branch := string(src[start:idx])
	if !strings.Contains(branch, "SetWriteDeadline") {
		t.Fatalf("读时限设置失败分支在写错误前未设置写时限")
	}
}

// ---------- R2-B7: at-client 截断响应给出明确错误 ----------

func TestATClientExchangeReportsTruncation(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "at_trunc.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		// 读完请求行后回一个远超上限且不带换行的"响应"。
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write(bytes.Repeat([]byte("x"), 4096))
	}()

	original := atClientMaxResponseBytes
	atClientMaxResponseBytes = 64
	t.Cleanup(func() { atClientMaxResponseBytes = original })

	_, err = atClientExchange(sock, []byte(`{"command":"ATI"}`))
	if err == nil || !strings.Contains(err.Error(), "截断") {
		t.Fatalf("超限响应应报截断错误,实际: %v", err)
	}
}

// ---------- R2-B8: 失败同步不刷新"最近同步时间" ----------

func TestTimeSyncFailureKeepsLastSyncTime(t *testing.T) {
	withTempTimeSyncEnv(t)

	queries, _ := withFakeTimeSync(t, 100*time.Millisecond, nil, nil)
	if result := runTimeSyncOnce(nil, "ntp.aliyun.com"); result["ok"] != true {
		t.Fatalf("首次同步应成功: %v", result)
	}
	if *queries != 1 {
		t.Fatalf("queries = %d, want 1", *queries)
	}
	okStatus := currentTimeSyncStatus()
	successTime, _ := okStatus["lastSyncTime"].(string)
	if successTime == "" {
		t.Fatalf("成功同步后 lastSyncTime 不应为空")
	}

	// 稍等保证若错误刷新时间戳则必然不同。
	time.Sleep(1100 * time.Millisecond)

	withFakeTimeSync(t, 0, errors.New("no route to host"), nil)
	if result := runTimeSyncOnce(nil, "ntp.aliyun.com"); result["ok"] != false {
		t.Fatalf("第二次同步应失败: %v", result)
	}
	failStatus := currentTimeSyncStatus()
	if failStatus["lastSyncOK"] != false || failStatus["lastError"] == "" {
		t.Fatalf("失败状态未记录: %v", failStatus)
	}
	if got, _ := failStatus["lastSyncTime"].(string); got != successTime {
		t.Fatalf("失败同步不得刷新最近同步时间: got %q, want %q", got, successTime)
	}
}

// ---------- R2-B10: set_ttl 返回应用成败 ----------

func TestSetTTLReportsApplyStatus(t *testing.T) {
	ts, client, _ := ttlFixesMockServer(t)

	get := func(query string) map[string]any {
		resp, err := client.Get(ts.URL + "/api/set_ttl?" + query)
		if err != nil {
			t.Fatalf("GET set_ttl?%s: %v", query, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("set_ttl status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("读取响应: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("set_ttl body not json: %v", err)
		}
		return payload
	}

	if ok := get("ttlvalue=65"); ok["ok"] != true {
		t.Fatalf("合法 ttlvalue 应 ok:true,实际: %v", ok)
	}
	if bad := get("ttlvalue=abc"); bad["ok"] != false || bad["error"] == nil {
		t.Fatalf("非法 ttlvalue 应 ok:false 且带 error,实际: %v", bad)
	}
	if out := get("ttlvalue=999"); out["ok"] != false {
		t.Fatalf("越界 ttlvalue 应 ok:false,实际: %v", out)
	}
}

// ---------- R2-B9: 控制台页面回车(\r)语义 ----------

func TestConsoleHTMLHandlesCarriageReturn(t *testing.T) {
	if strings.Contains(nativeConsoleHTML, ".replace(/\\r/g,'')") {
		t.Fatalf("控制台页面仍在无条件删除 \\r,进度条等原行刷新输出会无限追加")
	}
	for _, marker := range []string{"removeCurrentLine", "crPending", "\\r\\n"} {
		if !strings.Contains(nativeConsoleHTML, marker) {
			t.Fatalf("控制台页面缺少回车处理逻辑: %s", marker)
		}
	}
}
