package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

const apiWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// apiWebSocketWriteTimeout 限制单帧写入时间,防止卡死客户端阻塞写端。
const apiWebSocketWriteTimeout = 5 * time.Second

// apiWebSocketReadTimeout 是读循环的空闲上限:每次收到任何帧都会续期。
// 客户端断电/拔线后连接停留在半开状态时,读协程靠它超时退出,避免永久泄漏。
// 前端每次请求都会发帧,正常使用不会触碰该超时;无轮询的静态页面由服务端
// 心跳 ping 续期(浏览器协议层自动回 pong,pong 帧同样续期)。
const apiWebSocketReadTimeout = 5 * time.Minute

// apiWebSocketPingInterval 是服务端心跳 ping 的发送间隔:读空闲超时只被
// 客户端帧续期,长期无请求帧的连接(停留在不轮询的页面)会在 5 分钟后被
// 误杀,前端只能走重连退避。心跳同时让半开连接在一个间隔内即被写失败
// 检出,而不是等满读超时。
var apiWebSocketPingInterval = 60 * time.Second

// apiWebSocketMaxMessageBytes 限制单条消息(含分片重组后)总大小,
// 超限直接关闭连接,防止恶意分片撑爆内存。
const apiWebSocketMaxMessageBytes = 1 << 20

// apiWebSocketMaxConcurrentDispatches 是单连接在途请求分发上限:WS 网关对
// 每帧派生独立协程(消除队头阻塞),无上限时失控/被劫持的客户端可用高频
// 慢请求(扫网 ~180s、强制刷新在 AT 锁上排队)在小内存 armv7 设备上堆出
// 无界协程与响应缓冲,构成 OOM 风险。超限的请求立即回 503 由客户端退避。
const apiWebSocketMaxConcurrentDispatches = 16

type apiWebSocketConn struct {
	conn net.Conn
	br   *bufio.Reader
	mu   sync.Mutex
	// dispatchSem 限制本连接在途 dispatch 协程数(见上);
	// 测试手工构造的连接可为 nil,表示不限制。
	dispatchSem chan struct{}
}

type apiWebSocketRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type apiWebSocketResponse struct {
	ID      string              `json:"id"`
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    string              `json:"body"`
	Error   string              `json:"error,omitempty"`
}

type apiWebSocketEventMessage struct {
	Type  string            `json:"type"`
	Event string            `json:"event"`
	Data  map[string]string `json:"data,omitempty"`
}

var apiWebSocketEventHub = struct {
	mu      sync.Mutex
	clients map[*apiWebSocketConn]struct{}
}{clients: make(map[*apiWebSocketConn]struct{})}

func registerAPIWebSocketClient(ws *apiWebSocketConn) {
	apiWebSocketEventHub.mu.Lock()
	apiWebSocketEventHub.clients[ws] = struct{}{}
	apiWebSocketEventHub.mu.Unlock()
}

func unregisterAPIWebSocketClient(ws *apiWebSocketConn) {
	apiWebSocketEventHub.mu.Lock()
	delete(apiWebSocketEventHub.clients, ws)
	apiWebSocketEventHub.mu.Unlock()
}

func broadcastAPIWebSocketEvent(event string, data map[string]string) {
	message := apiWebSocketEventMessage{Type: "event", Event: event, Data: data}
	apiWebSocketEventHub.mu.Lock()
	clients := make([]*apiWebSocketConn, 0, len(apiWebSocketEventHub.clients))
	for ws := range apiWebSocketEventHub.clients {
		clients = append(clients, ws)
	}
	apiWebSocketEventHub.mu.Unlock()

	for _, ws := range clients {
		if err := ws.writeJSON(message); err != nil {
			// 与 dispatch 失败路径对齐:注销之外必须关闭连接。writeFrame 在
			// 同一写超时下分两次 Write(帧头、载荷),载荷写失败时连接上已留
			// 半截帧、字节流失去帧边界,后续响应帧只会加剧错位;不关连接则
			// 该客户端"请求-响应看似正常"却永久收不到事件推送,故障不可见。
			// Close 同时让读循环退出,deferred unregister 幂等。
			unregisterAPIWebSocketClient(ws)
			_ = ws.conn.Close()
		}
	}
}

func (s *simpleAdminServer) handleAPIWebSocket(w http.ResponseWriter, r *http.Request) {
	if !apiWebSocketOriginAllowed(r) {
		http.Error(w, "websocket origin forbidden", http.StatusForbidden)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Key") == "" {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unavailable", http.StatusInternalServerError)
		return
	}
	// Hijack 前取出会话中间件已重签的 Set-Cookie:Hijack 后 ResponseWriter
	// 头缓冲被丢弃,手写 101 若不带上,续期额度已被消耗而 Cookie 从未送达
	// ——只走 WS 握手的客户端(自动化/重连场景)Cookie 的 Expires 永远停在
	// 登录时刻,持续活跃 24h 后仍被浏览器清 Cookie 强制登出,与
	// renewSessionCookie 声明的"覆盖 /api/ws 握手"相悖。
	pendingCookies := w.Header().Values("Set-Cookie")
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	ws := &apiWebSocketConn{conn: conn, br: rw.Reader, dispatchSem: make(chan struct{}, apiWebSocketMaxConcurrentDispatches)}
	accept := apiWebSocketAcceptKey(r.Header.Get("Sec-WebSocket-Key"))
	_, _ = rw.Writer.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	_, _ = rw.Writer.WriteString("Upgrade: websocket\r\n")
	_, _ = rw.Writer.WriteString("Connection: Upgrade\r\n")
	for _, cookie := range pendingCookies {
		_, _ = rw.Writer.WriteString("Set-Cookie: " + cookie + "\r\n")
	}
	_, _ = rw.Writer.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n\r\n")
	if err := rw.Writer.Flush(); err != nil {
		_ = conn.Close()
		return
	}
	registerAPIWebSocketClient(ws)
	defer unregisterAPIWebSocketClient(ws)
	defer conn.Close()

	// 服务端心跳 ping(见 apiWebSocketPingInterval):浏览器自动回 pong,
	// 续期读空闲超时并快速检出半开连接。写帧持连接级写互斥,与响应/广播
	// 并发写安全;写失败即退出心跳协程,连接由读循环的超时/错误路径收敛。
	pingStop := make(chan struct{})
	defer close(pingStop)
	go func() {
		ticker := time.NewTicker(apiWebSocketPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pingStop:
				return
			case <-ticker.C:
				if err := ws.writeFrame(0x9, nil); err != nil {
					return
				}
			}
		}
	}()

	// 分片重组状态:fragOpcode 为 0 表示当前没有进行中的分片消息。
	var fragOpcode byte
	var fragPayload []byte

	for {
		// 半开连接保护:每读一帧前设置读超时,收到任何帧后续期;
		// 客户端断电后读协程最多再等一个超时周期即退出。
		if err := conn.SetReadDeadline(time.Now().Add(apiWebSocketReadTimeout)); err != nil {
			return
		}
		fin, opcode, payload, err := ws.readClientFrame()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("websocket read idle timeout after %s, closing connection", apiWebSocketReadTimeout)
			}
			return
		}
		switch {
		case opcode == 0x8:
			_ = ws.writeClose()
			return
		case opcode == 0x9:
			if err := ws.writeFrame(0xA, payload); err != nil {
				return
			}
		case opcode == 0xA:
		case opcode == 0x0:
			// continuation 帧:必须处于分片消息中,否则是协议错误。
			if fragOpcode == 0 {
				log.Printf("websocket unexpected continuation frame, closing connection")
				_ = ws.writeClose()
				return
			}
			fragPayload = append(fragPayload, payload...)
			if len(fragPayload) > apiWebSocketMaxMessageBytes {
				log.Printf("websocket fragmented message exceeds %d bytes, closing connection", apiWebSocketMaxMessageBytes)
				_ = ws.writeClose()
				return
			}
			if fin {
				message := fragPayload
				fragOpcode, fragPayload = 0, nil
				s.dispatchAPIWebSocketRequestAsync(r, ws, message)
			}
		case opcode == 0x1 || opcode == 0x2:
			if fragOpcode != 0 {
				log.Printf("websocket new data frame during fragmented message, closing connection")
				_ = ws.writeClose()
				return
			}
			if !fin {
				if len(payload) > apiWebSocketMaxMessageBytes {
					log.Printf("websocket fragmented message exceeds %d bytes, closing connection", apiWebSocketMaxMessageBytes)
					_ = ws.writeClose()
					return
				}
				fragOpcode, fragPayload = opcode, payload
				continue
			}
			s.dispatchAPIWebSocketRequestAsync(r, ws, payload)
		default:
			// 0x3-0x7 / 0xB-0xF 保留操作码:记录日志并忽略,不崩溃也不挂起。
			log.Printf("websocket reserved opcode 0x%x ignored", opcode)
		}
	}
}

// dispatchAPIWebSocketRequestAsync 在独立协程中执行单次请求分发,消除同一连接上
// 慢请求(如扫网 ~180s)对后续请求的队头阻塞。响应写回走 writeJSON,
// 其内部帧写函数已持连接级写互斥,与事件广播共用连接时并发写安全;
// 前端按响应帧中的 id 匹配,乱序到达无影响。
func (s *simpleAdminServer) dispatchAPIWebSocketRequestAsync(base *http.Request, ws *apiWebSocketConn, payload []byte) {
	if ws.dispatchSem != nil {
		select {
		case ws.dispatchSem <- struct{}{}:
		default:
			// 在途请求已达上限:不再派生协程,直接回 503(带请求 id,
			// 前端按既有错误路径处理),防止无界协程/缓冲堆积。
			// 拒绝响应必须同步写回:writeFrame 自带写超时,客户端不读响应
			// 高速灌帧时同步写形成背压;旧实现每帧派生一个写协程,洪泛下
			// 协程与 JSON 缓冲在写超时窗口内无界堆积,正是 dispatchSem
			// 要消除的 OOM 风险从拒绝分支漏了回来。写失败即关闭连接,
			// 读循环在下一帧读取失败后退出。
			var reject struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(payload, &reject)
			rejectResp := apiWebSocketResponse{ID: reject.ID, Status: http.StatusServiceUnavailable, Error: "too many concurrent requests"}
			if err := ws.writeJSON(rejectResp); err != nil {
				unregisterAPIWebSocketClient(ws)
				_ = ws.conn.Close()
			}
			return
		}
	}
	go func() {
		if ws.dispatchSem != nil {
			defer func() { <-ws.dispatchSem }()
		}
		resp := s.dispatchAPIWebSocketRequest(base, payload)
		if err := ws.writeJSON(resp); err != nil {
			log.Printf("websocket response write failed, closing connection: %v", err)
			unregisterAPIWebSocketClient(ws)
			_ = ws.conn.Close()
		}
	}()
}

func (s *simpleAdminServer) dispatchAPIWebSocketRequest(base *http.Request, payload []byte) (resp apiWebSocketResponse) {
	// 业务 handler 的任何 panic 只允许影响本次请求:兜底转换为 500 响应帧,
	// 绝不能让单次请求把整个服务进程带崩。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("websocket api dispatch panic recovered: %v", r)
			resp = apiWebSocketResponse{ID: resp.ID, Status: http.StatusInternalServerError, Body: "", Error: "internal server error"}
		}
	}()

	var req apiWebSocketRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return apiWebSocketResponse{Status: http.StatusBadRequest, Body: "", Error: "invalid json request: " + err.Error()}
	}
	resp = apiWebSocketResponse{ID: req.ID, Status: http.StatusOK}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		resp.Status = http.StatusBadRequest
		resp.Error = "missing api path"
		return resp
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.Path == "" {
		resp.Status = http.StatusBadRequest
		resp.Error = "invalid api path"
		return resp
	}
	if u.Path == "/api/ws" || u.Path == "/api/console/ws" || !strings.HasPrefix(u.Path, "/api/") {
		resp.Status = http.StatusNotFound
		resp.Error = "unsupported websocket api endpoint: " + u.Path
		return resp
	}

	handler := s.nativeAPIHandlers()[u.Path]
	if handler == nil {
		resp.Status = http.StatusNotFound
		resp.Error = "unsupported websocket api endpoint: " + u.Path
		return resp
	}

	// 不能用 httptest.NewRequest:它对非法 method(如 "BAD METHOD")直接 panic,
	// 会把整个服务进程打崩。http.NewRequest 返回 error,可安全转成 400 响应帧。
	body := strings.NewReader(req.Body)
	httpReq, err := http.NewRequest(method, u.RequestURI(), body)
	if err != nil {
		resp.Status = http.StatusBadRequest
		resp.Error = "invalid request"
		return resp
	}
	httpReq = httpReq.WithContext(base.Context())
	httpReq.Host = base.Host
	httpReq.RemoteAddr = base.RemoteAddr
	for name, values := range base.Header {
		if strings.EqualFold(name, "Connection") || strings.EqualFold(name, "Upgrade") || strings.EqualFold(name, "Sec-WebSocket-Key") || strings.EqualFold(name, "Sec-WebSocket-Version") {
			continue
		}
		for _, value := range values {
			httpReq.Header.Add(name, value)
		}
	}
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}
	if req.Body != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	if req.Body != "" {
		httpReq.ContentLength = int64(len(req.Body))
	}

	// 会话复核:升级握手只鉴权一次,会话过期或被他处销毁(如登出)后,
	// 存活的长连接不能继续调用业务 API。每请求用其携带的 Cookie 复核,
	// 复用 HTTP 路径同一会话校验(含滑动续期语义);公开端点不复核
	// (对照 isPublicAuthPath)。无效时返回 401 帧,前端既有
	// "响应 401 → 跳登录页" 逻辑随即生效。
	if !isPublicAuthPath(u.Path) && !s.isRequestAuthenticated(httpReq) {
		resp.Status = http.StatusUnauthorized
		resp.Error = "login required"
		return resp
	}

	rr := httptest.NewRecorder()
	handler(rr, httpReq)
	result := rr.Result()
	defer result.Body.Close()
	data, _ := io.ReadAll(result.Body)
	resp.Status = result.StatusCode
	resp.Headers = result.Header
	resp.Body = string(data)
	return resp
}

func apiWebSocketOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	if u.Scheme != expectedScheme {
		return false
	}
	return apiNormalizeOriginHost(u.Host) == apiNormalizeOriginHost(r.Host)
}

func apiNormalizeOriginHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if strings.HasSuffix(host, ":80") {
		return strings.TrimSuffix(host, ":80")
	}
	if strings.HasSuffix(host, ":443") {
		return strings.TrimSuffix(host, ":443")
	}
	return host
}

func apiWebSocketAcceptKey(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + apiWebSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (ws *apiWebSocketConn) readClientFrame() (fin bool, opcode byte, payload []byte, err error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(ws.br, header); err != nil {
			return false, 0, nil, err
		}
		fin = header[0]&0x80 != 0
		opcode = header[0] & 0x0f
		masked := header[1]&0x80 != 0
		length := uint64(header[1] & 0x7f)
		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(ws.br, ext); err != nil {
				return false, 0, nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(ws.br, ext); err != nil {
				return false, 0, nil, err
			}
			length = binary.BigEndian.Uint64(ext)
		}
		if !masked {
			// RFC 6455 §5.1:客户端帧必须掩码,服务端收到未掩码帧必须
			// 立即按协议错误断连(防代理缓存投毒);浏览器恒掩码。
			return false, 0, nil, fmt.Errorf("websocket client frame must be masked")
		}
		if opcode >= 0x8 && length > 125 {
			// RFC 6455 §5.5:控制帧载荷不得超过 125 字节。接受超大 ping
			// 并原样回 pong 会发出超限控制帧,严格客户端按帧错误断连。
			return false, 0, nil, fmt.Errorf("websocket control frame too large: %d", length)
		}
		if length > apiWebSocketMaxMessageBytes {
			return false, 0, nil, fmt.Errorf("websocket frame too large: %d", length)
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(ws.br, mask[:]); err != nil {
				return false, 0, nil, err
			}
		}
		payload = make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(ws.br, payload); err != nil {
				return false, 0, nil, err
			}
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		return fin, opcode, payload, nil
	}
}

func (ws *apiWebSocketConn) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(`{"status":500,"body":"","error":"json marshal failed"}`)
	}
	return ws.writeFrame(0x1, data)
}

func (ws *apiWebSocketConn) writeClose() error {
	return ws.writeFrame(0x8, nil)
}

func (ws *apiWebSocketConn) writeFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// 写超时保护:某个客户端发送窗口堵死时不能无限阻塞写端,
	// 否则会拖住调用方(如 AT 缓存 worker 的事件广播)。
	if deadlineErr := ws.conn.SetWriteDeadline(time.Now().Add(apiWebSocketWriteTimeout)); deadlineErr != nil {
		return deadlineErr
	}

	header := bytes.NewBuffer([]byte{0x80 | opcode})
	length := len(payload)
	switch {
	case length < 126:
		header.WriteByte(byte(length))
	case length <= 0xffff:
		header.WriteByte(126)
		_ = binary.Write(header, binary.BigEndian, uint16(length))
	default:
		header.WriteByte(127)
		_ = binary.Write(header, binary.BigEndian, uint64(length))
	}
	if _, err := ws.conn.Write(header.Bytes()); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := ws.conn.Write(payload)
	return err
}
