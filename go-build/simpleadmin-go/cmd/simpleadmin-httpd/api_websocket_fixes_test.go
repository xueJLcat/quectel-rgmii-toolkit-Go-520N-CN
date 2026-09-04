package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// wsFixesConn 是一个保持长连接的手工 WebSocket 客户端,
// 允许在同一连接上连续发送多帧(含分片帧)并逐个读取响应,
// 比 e2eWebSocketCall(每次调用独立连接、只读一条响应)更适合修复项验证。
type wsFixesConn struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
}

func wsFixesDial(t *testing.T, ts *httptest.Server, client *http.Client) *wsFixesConn {
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
	req := "GET /api/ws HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: " + ts.URL + "\r\n" +
		"Cookie: " + strings.TrimSuffix(cookieHeader, "; ") + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("ws handshake write: %v", err)
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("ws handshake failed: %q err=%v", statusLine, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("ws handshake headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &wsFixesConn{t: t, conn: conn, br: br}
}

// writeFrame 写一个带掩码的客户端帧,可控制 FIN 与操作码,用于构造分片帧。
func (c *wsFixesConn) writeFrame(fin bool, opcode byte, payload []byte) {
	c.t.Helper()
	first := opcode
	if fin {
		first |= 0x80
	}
	frame := []byte{first}
	switch {
	case len(payload) < 126:
		frame = append(frame, byte(0x80|len(payload)))
	case len(payload) <= 0xffff:
		frame = append(frame, 0x80|126)
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(len(payload)))
		frame = append(frame, ext...)
	default:
		frame = append(frame, 0x80|127)
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(len(payload)))
		frame = append(frame, ext...)
	}
	mask := [4]byte{0x51, 0x62, 0x73, 0x84}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	if _, err := c.conn.Write(frame); err != nil {
		c.t.Fatalf("ws frame write: %v", err)
	}
}

func (c *wsFixesConn) writeJSONRequest(id, method, path, body string) {
	c.t.Helper()
	request, err := json.Marshal(map[string]any{
		"id": id, "method": method, "path": path,
		"headers": map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		"body":    body,
	})
	if err != nil {
		c.t.Fatalf("marshal request: %v", err)
	}
	c.writeFrame(true, 0x1, request)
}

// readResponse 读取下一个非事件的文本 JSON 响应帧,超时则失败。
func (c *wsFixesConn) readResponse(timeout time.Duration) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.t.Fatalf("timed out waiting for websocket response frame")
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			c.t.Fatalf("set read deadline: %v", err)
		}
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(c.br, hdr); err != nil {
			c.t.Fatalf("ws read header: %v", err)
		}
		length := int64(hdr[1] & 0x7F)
		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.br, ext); err != nil {
				c.t.Fatalf("ws read ext: %v", err)
			}
			length = int64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.br, ext); err != nil {
				c.t.Fatalf("ws read ext: %v", err)
			}
			length = int64(binary.BigEndian.Uint64(ext))
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(c.br, data); err != nil {
			c.t.Fatalf("ws read payload: %v", err)
		}
		if hdr[0]&0x0F != 0x1 {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			c.t.Fatalf("ws json: %v", err)
		}
		if message["type"] == "event" {
			continue
		}
		return message
	}
}

// TestAPIWebSocketInvalidMethodReturns400WithoutCrash 验证非法 method 不再打崩进程:
// 旧实现用 httptest.NewRequest 构造请求,遇到 "BAD METHOD" 直接 panic,
// 读循环无 recover,整个服务进程退出。修复后应返回带 id 的 400 帧,
// 且同一连接继续可用、服务继续工作。
func TestAPIWebSocketInvalidMethodReturns400WithoutCrash(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := wsFixesDial(t, ts, client)

	ws.writeJSONRequest("bad1", "BAD METHOD", "/api/module_model", "")
	resp := ws.readResponse(5 * time.Second)
	if resp["id"] != "bad1" {
		t.Fatalf("invalid method response id = %v, want bad1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "400" {
		t.Fatalf("invalid method status = %v, want 400 (resp=%v)", status, resp)
	}
	if errMsg := fmt.Sprint(resp["error"]); !strings.Contains(errMsg, "invalid request") {
		t.Fatalf("invalid method error = %q, want contain \"invalid request\"", errMsg)
	}

	// 连接不断:同一连接上继续发送合法请求,仍应正常应答。
	ws.writeJSONRequest("ok1", "GET", "/api/module_model", "")
	resp = ws.readResponse(5 * time.Second)
	if resp["id"] != "ok1" {
		t.Fatalf("follow-up response id = %v, want ok1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("follow-up status = %v, want 200 (resp=%v)", status, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["body"]), mockModuleModel) {
		t.Fatalf("follow-up body = %v, want contain %s", resp["body"], mockModuleModel)
	}
}

// TestAPIWebSocketValidRequestStill200 守护常规链路:合法请求经 WS 网关仍为 200。
func TestAPIWebSocketValidRequestStill200(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "m1", "GET", "/api/module_model", "")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("module_model status = %v, want 200 (error=%v)", status, resp["error"])
	}
	if !strings.Contains(fmt.Sprint(resp["body"]), mockModuleModel) {
		t.Fatalf("module_model body = %v, want contain %s", resp["body"], mockModuleModel)
	}
}

// drainATCacheInitialRefresh 确保共享 AT 缓存的一次性 initialRefresh 后台协程
// 已在本测试内(以 mock 模式)执行完毕。该协程在缓存首次 Start 后约 500ms 会把
// 一条读命令入队;若不先消化,它会泄漏到后续用例,污染其它测试临时替换的
// executeCachedATCommand 以及 mock 缓存数据。等待其对应条目落盘即可。
func drainATCacheInitialRefresh(t *testing.T) {
	t.Helper()
	atCommandCache.Start(true)
	commands := commonATCacheCommands()
	if len(commands) == 0 {
		return
	}
	initialCommand := commands[0]
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		has, _, _, _, running := atCommandCache.snapshot(initialCommand)
		if has && !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAPIWebSocketConcurrentDispatchNoHeadOfLineBlocking 验证慢请求不再阻塞同一连接
// 上的后续请求。构造方法:测试先持有 AT 全局文件锁,非 mock 服务上带 force 的
// 状态查询(/api/get_atcache)会阻塞在该锁上成为可控的慢请求;此时快请求
// (/api/get_uptime)必须先收到匹配 id 的响应,释放锁后慢请求才应答。
// 旧的串行分发实现会在慢请求上卡住读循环,快请求超时失败。
func TestAPIWebSocketConcurrentDispatchNoHeadOfLineBlocking(t *testing.T) {
	drainATCacheInitialRefresh(t)
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	lockFile := filepath.Join(dir, "at.lock")
	t.Setenv("SIMPLEADMIN_AT_LOCK_FILE", lockFile)

	// AT 候选设备指向一个不存在的普通(非 smd)路径:释放锁之后真实 AT 路径
	// 会在打开设备时快速失败返回,而不会走 smd 读取器子进程分支
	// (测试二进制被当作 __smd-reader 子进程重跑会引发递归执行整套用例)。
	oldDevices := runtimeATDevices
	runtimeATDevices = []string{filepath.Join(dir, "no-such-tty")}
	t.Cleanup(func() { runtimeATDevices = oldDevices })
	// 本测试会以非 mock 模式启动共享 AT 缓存的后台协程;测试结束先切回 mock
	// 再恢复设备列表,确保这些后台刷新协程后续不会以非 mock 模式去访问真实
	// /dev/smd 设备(那会触发 __smd-reader 子进程递归)。
	t.Cleanup(func() { atCommandCache.Start(true) })

	cfg := serverConfig{
		staticDir: e2eStaticDir(t),
		authFile:  authFile,
		ttlFile:   filepath.Join(dir, "ttlvalue"),
		noTLS:     true,
		mockMode:  false,
	}
	srv := &simpleAdminServer{cfg: cfg}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	loginResp, err := client.PostForm(ts.URL+"/api/login", form)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}

	// 持有 AT 全局文件锁:非 mock 模式下真实 AT 路径在该锁上阻塞,
	// 使慢请求的耗时完全由测试控制。
	lf, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock: %v", err)
	}
	released := false
	releaseLock := func() {
		if released {
			return
		}
		released = true
		_ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
		_ = lf.Close()
	}
	t.Cleanup(releaseLock)

	ws := wsFixesDial(t, ts, client)
	ws.writeJSONRequest("slow", "GET", "/api/get_atcache?atcmd=ATI&force=1&wait=1", "")
	ws.writeJSONRequest("fast", "GET", "/api/get_uptime", "")

	// 快请求必须先到达:串行分发下读循环卡在慢请求上,该读取会超时失败。
	first := ws.readResponse(5 * time.Second)
	if first["id"] != "fast" {
		t.Fatalf("first response id = %v, want fast (head-of-line blocking regression)", first["id"])
	}
	if status := fmt.Sprint(first["status"]); status != "200" {
		t.Fatalf("fast response status = %v, want 200 (resp=%v)", status, first)
	}

	releaseLock()

	second := ws.readResponse(15 * time.Second)
	if second["id"] != "slow" {
		t.Fatalf("second response id = %v, want slow", second["id"])
	}
	if status := fmt.Sprint(second["status"]); status != "200" {
		t.Fatalf("slow response status = %v, want 200 (resp=%v)", status, second)
	}
}

// TestAPIWebSocketFragmentedMessageReassembly 验证分片帧重组:
// 手工把一个请求拆成 0x1 非 FIN + 0x0 FIN 两帧发送,服务端应重组后整体处理;
// 同时验证保留操作码(0x3)只被记录日志并忽略,不影响后续请求。
// 旧实现把 0x0 落入 default 静默丢弃,分片请求永不应答。
func TestAPIWebSocketFragmentedMessageReassembly(t *testing.T) {
	ts, client := e2eTestServer(t)
	ws := wsFixesDial(t, ts, client)

	// 保留操作码:服务端应记录日志并忽略,连接保持可用。
	ws.writeFrame(true, 0x3, []byte("reserved opcode payload"))

	request, err := json.Marshal(map[string]any{
		"id": "frag1", "method": "GET", "path": "/api/module_model",
		"headers": map[string]string{}, "body": "",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	mid := len(request) / 2
	ws.writeFrame(false, 0x1, request[:mid])
	ws.writeFrame(true, 0x0, request[mid:])

	resp := ws.readResponse(5 * time.Second)
	if resp["id"] != "frag1" {
		t.Fatalf("fragmented response id = %v, want frag1", resp["id"])
	}
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("fragmented response status = %v, want 200 (resp=%v)", status, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["body"]), mockModuleModel) {
		t.Fatalf("fragmented response body = %v, want contain %s", resp["body"], mockModuleModel)
	}
}
