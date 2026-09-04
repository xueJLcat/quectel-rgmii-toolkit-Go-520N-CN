//go:build linux
// +build linux

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// consoleFixesDial 对 /api/console/ws 发起手工 WebSocket 握手,
// 复用会话 Cookie 通过登录鉴权(与 wsFixesDial 同构,仅路径不同)。
func consoleFixesDial(t *testing.T, ts *httptest.Server, client *http.Client) *wsFixesConn {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	cookieHeader := ""
	for _, cookie := range client.Jar.Cookies(u) {
		cookieHeader += cookie.Name + "=" + cookie.Value + "; "
	}
	req := "GET /api/console/ws HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: " + ts.URL + "\r\n" +
		"Cookie: " + strings.TrimSuffix(cookieHeader, "; ") + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("console ws handshake write: %v", err)
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("console ws handshake failed: %q err=%v", statusLine, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("console ws handshake headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &wsFixesConn{t: t, conn: conn, br: br}
}

// consoleReadFrame 读取一个服务端帧(服务端帧不带掩码)。
func consoleReadFrame(c *wsFixesConn) (byte, []byte, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c.br, hdr); err != nil {
		return 0, nil, err
	}
	length := int64(hdr[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0] & 0x0f, payload, nil
}

// consoleReadUntil 累积数据帧内容直到包含 substr,连接被断开/超时则返回错误。
func consoleReadUntil(c *wsFixesConn, substr string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var sb strings.Builder
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return sb.String(), fmt.Errorf("timed out waiting for %q", substr)
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			return sb.String(), err
		}
		opcode, payload, err := consoleReadFrame(c)
		if err != nil {
			return sb.String(), err
		}
		switch opcode {
		case 0x1, 0x2, 0x0:
			sb.Write(payload)
			if strings.Contains(sb.String(), substr) {
				return sb.String(), nil
			}
		}
	}
}

// consoleWaitShell 等待终端直达 shell(无登录层),返回累积的输出。
func consoleWaitShell(t *testing.T, ws *wsFixesConn) string {
	t.Helper()
	output, err := consoleReadUntil(ws, "native Go console", 10*time.Second)
	if err != nil {
		t.Fatalf("console did not reach shell banner: %v (output=%q)", err, output)
	}
	if strings.Contains(output, "login: ") || strings.Contains(output, "Terminal login required") {
		t.Fatalf("console still prompts terminal login: %q", output)
	}
	return output
}

// TestParseConsoleResizeMessage 覆盖 resize 控制帧解析的合法与非法输入:
// 非法输入必须返回 ok=false 且调用方忽略,绝不能让服务端崩溃。
func TestParseConsoleResizeMessage(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		wantRows uint16
		wantCols uint16
		wantOK   bool
	}{
		{"valid", `{"type":"resize","cols":80,"rows":24}`, 24, 80, true},
		{"valid surrounding space", `  {"type":"resize","cols":120,"rows":32}  `, 32, 120, true},
		{"type case insensitive", `{"type":"RESIZE","cols":80,"rows":24}`, 24, 80, true},
		{"invalid json", `{not json`, 0, 0, false},
		{"empty payload", ``, 0, 0, false},
		{"wrong type", `{"type":"input","cols":80,"rows":24}`, 0, 0, false},
		{"missing type", `{"cols":80,"rows":24}`, 0, 0, false},
		{"missing fields", `{"type":"resize"}`, 0, 0, false},
		{"zero cols", `{"type":"resize","cols":0,"rows":24}`, 0, 0, false},
		{"negative rows", `{"type":"resize","cols":80,"rows":-3}`, 0, 0, false},
		{"oversized", `{"type":"resize","cols":5000,"rows":24}`, 0, 0, false},
		{"json array", `[1,2,3]`, 0, 0, false},
		{"json null", `null`, 0, 0, false},
	}
	for _, tc := range cases {
		rows, cols, ok := parseConsoleResizeMessage([]byte(tc.payload))
		if ok != tc.wantOK || rows != tc.wantRows || cols != tc.wantCols {
			t.Errorf("%s: parseConsoleResizeMessage(%q) = (rows=%d, cols=%d, ok=%v), want (rows=%d, cols=%d, ok=%v)",
				tc.name, tc.payload, rows, cols, ok, tc.wantRows, tc.wantCols, tc.wantOK)
		}
	}
}

// TestConsoleHandleResizeFrameUpdatesSize 验证连接记录的窗口尺寸仅在
// 合法控制帧时更新,非法帧保持原值。
func TestConsoleHandleResizeFrameUpdatesSize(t *testing.T) {
	ws := &nativeWSConn{rows: nativeConsoleDefaultRows, cols: nativeConsoleDefaultCols}
	if ws.handleResizeFrame([]byte("garbage")) {
		t.Fatalf("garbage control frame reported as handled")
	}
	if ws.rows != nativeConsoleDefaultRows || ws.cols != nativeConsoleDefaultCols {
		t.Fatalf("size changed by invalid frame: rows=%d cols=%d", ws.rows, ws.cols)
	}
	if !ws.handleResizeFrame([]byte(`{"type":"resize","cols":100,"rows":30}`)) {
		t.Fatalf("valid resize frame not handled")
	}
	if ws.rows != 30 || ws.cols != 100 {
		t.Fatalf("size after valid frame = rows=%d cols=%d, want rows=30 cols=100", ws.rows, ws.cols)
	}
}

// TestConsoleWebSocketIdleTimeoutClosesHalfOpenConnection 验证读空闲超时生效:
// 把超时注入为 200ms,客户端握手后一帧不发(模拟断电/拔线的半开连接),
// 服务端必须在超时后主动关闭连接,客户端读到 EOF,而不是永久挂住读循环。
func TestConsoleWebSocketIdleTimeoutClosesHalfOpenConnection(t *testing.T) {
	oldReadTimeout := nativeConsoleReadTimeout
	nativeConsoleReadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { nativeConsoleReadTimeout = oldReadTimeout })

	ts, client := e2eTestServer(t)
	ws := consoleFixesDial(t, ts, client)

	start := time.Now()
	_, err := consoleReadUntil(ws, "this-marker-never-appears", 5*time.Second)
	if err == nil {
		t.Fatalf("half-open console connection was never closed (read idle timeout ineffective)")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("console connection closed too late (%s), read idle timeout not effective", elapsed)
	}
}

// TestConsoleOpensShellDirectlyWithoutTerminalLogin 验证去除终端登录层:
// 已通过 Web 会话鉴权的连接应直达 shell,不再出现终端登录提示。
func TestConsoleOpensShellDirectlyWithoutTerminalLogin(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := consoleFixesDial(t, ts, client)
	consoleWaitShell(t, ws)
}

// TestConsoleResizeAppliedAndInvalidControlFramesIgnored 验证终端尺寸协议:
// 连接后上报的尺寸即时套用到 PTY;运行期再次上报即时生效;
// 期间穿插的非法控制帧(坏 JSON、未知类型、越界尺寸)被忽略,
// 连接保持可用,终端仍能正常回显命令输出。
func TestConsoleResizeAppliedAndInvalidControlFramesIgnored(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := consoleFixesDial(t, ts, client)

	// 连接建立即上报尺寸(与前端 onopen 行为一致)。
	ws.writeFrame(true, 0x1, []byte(`{"type":"resize","cols":80,"rows":24}`))
	// 非法控制帧必须被静默忽略:不能崩溃、不能断开连接、不能污染输入。
	ws.writeFrame(true, 0x1, []byte(`{invalid json`))
	ws.writeFrame(true, 0x1, []byte(`{"type":"unknown","x":1}`))
	ws.writeFrame(true, 0x1, []byte(`{"type":"resize","cols":-1,"rows":0}`))

	consoleWaitShell(t, ws)

	// 连接初期上报的尺寸应已套用到 PTY。
	ws.writeFrame(true, 0x2, []byte("stty size\r"))
	output, err := consoleReadUntil(ws, "24 80", 10*time.Second)
	if err != nil {
		t.Fatalf("initial resize not applied to PTY: %v (output=%q)", err, output)
	}

	// 运行期 resize:服务端收到后立即 setPTYWindowSize。
	ws.writeFrame(true, 0x1, []byte(`{"type":"resize","cols":100,"rows":30}`))
	ws.writeFrame(true, 0x2, []byte("stty size\r"))
	output, err = consoleReadUntil(ws, "30 100", 10*time.Second)
	if err != nil {
		t.Fatalf("runtime resize not applied to PTY: %v (output=%q)", err, output)
	}
}

// TestConsoleHTMLHasReconnectResizeAndCopyGuards 断言内嵌终端页面包含
// 本次修复的前端逻辑:重连按钮与提示、连接/窗口变化时的 resize 上报、
// Ctrl+Shift+C 复制放行、仅在底部附近才自动滚动。
func TestConsoleHTMLHasReconnectResizeAndCopyGuards(t *testing.T) {
	wants := []string{
		`id="reconnect"`,
		"重新连接",
		"连接已断开",
		"function connect()",
		"reconnectBtn.addEventListener('click',function(){ connect(); });",
		"sendResize();",
		`ws.send(JSON.stringify({type:'resize',cols:size.cols,rows:size.rows}));`,
		"setTimeout(sendResize,200)",
		`if(e.ctrlKey && e.shiftKey && (e.key==='c' || e.key==='C')) return;`,
		"if(term.scrollHeight-term.scrollTop-term.clientHeight<40) term.scrollTop=term.scrollHeight;",
	}
	for _, want := range wants {
		if !strings.Contains(nativeConsoleHTML, want) {
			t.Errorf("nativeConsoleHTML missing %q", want)
		}
	}
}
