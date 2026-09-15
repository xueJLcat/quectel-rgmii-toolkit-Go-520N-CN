//go:build linux
// +build linux

package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// 终端默认窗口尺寸(客户端连接后会通过 resize 控制帧上报实际尺寸)。
const (
	nativeConsoleDefaultRows = 32
	nativeConsoleDefaultCols = 120
)

// 与 api_websocket.go 保持一致的超时保护,声明为变量便于测试注入。
//
// nativeConsoleReadTimeout 是读循环的空闲上限:每读一帧前置位并续期。
// 客户端断电/拔线后连接停留在半开状态时,读协程靠它超时退出,
// 随连接清理一起终止 PTY shell,避免协程与进程永久泄漏。
// 健康但无键入的会话由服务端心跳 ping 续期:浏览器协议层自动回 pong,
// pong 帧同样续期——用户长时间观看 top/ping 输出不敲键盘时,连接不再
// 被空闲超时误杀、前台任务不被连带 SIGKILL。
//
// nativeConsoleWriteTimeout 限制单帧写入时间,防止卡死客户端阻塞写端。
var (
	nativeConsoleReadTimeout  = 5 * time.Minute
	nativeConsoleWriteTimeout = 5 * time.Second
	// nativeConsolePingInterval 服务端心跳 ping 间隔,须显著小于读空闲超时。
	nativeConsolePingInterval = 60 * time.Second
	// nativeConsoleSessionRecheckInterval 会话复核间隔:root shell 是权限
	// 最高的长连接,握手后不能豁免复核(见主循环处注释)。
	nativeConsoleSessionRecheckInterval = 60 * time.Second
)

// nativeConsoleResizeMessage 是客户端→服务端的窗口尺寸控制帧。
// 终端键入始终以二进制帧(0x2)原样写入 PTY,文本帧(0x1)保留给
// JSON 控制消息,两类帧互不冲突。
type nativeConsoleResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type nativeWSConn struct {
	conn net.Conn
	br   *bufio.Reader
	mu   sync.Mutex

	// rows/cols 记录最近一次客户端上报的窗口尺寸,
	// shell 启动前收到的 resize 帧先暂存于此,启动时套用。
	rows uint16
	cols uint16
}

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

func (s *simpleAdminServer) handleNativeConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/console" && r.URL.Path != "/console/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(nativeConsoleHTML))
}

func (s *simpleAdminServer) handleNativeConsoleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !consoleWebSocketOriginAllowed(r) {
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
	// 留存握手会话令牌供连接期内周期复核:hijack 后请求对象不再可用。
	sessionToken := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		sessionToken = cookie.Value
	}
	// Hijack 前取出会话中间件已重签的 Set-Cookie 并写入手写 101:
	// 否则续期额度被消耗而 Cookie 从未送达(与 handleAPIWebSocket 同一修复)。
	pendingCookies := w.Header().Values("Set-Cookie")
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	ws := &nativeWSConn{conn: conn, br: rw.Reader, rows: nativeConsoleDefaultRows, cols: nativeConsoleDefaultCols}
	accept := websocketAcceptKey(r.Header.Get("Sec-WebSocket-Key"))
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

	defer conn.Close()

	shell, err := startNativeConsoleShell(ws.rows, ws.cols)
	if err != nil {
		_ = ws.writeBinary([]byte("failed to start shell: " + err.Error() + "\n"))
		_ = ws.writeClose()
		return
	}
	defer shell.close()

	consoleModel := deviceModelName
	_ = ws.writeBinary([]byte("==============================================================\r\n"))
	_ = ws.writeBinary([]byte(consoleModel + " native Go console\r\n"))
	_ = ws.writeBinary([]byte("AT channel: /dev/smd11 is used by " + consoleModel + "\r\n"))
	_ = ws.writeBinary([]byte("==============================================================\r\n"))

	// 服务端心跳 ping(见 nativeConsolePingInterval 注释):浏览器协议层
	// 自动回 pong,读循环收到任何帧都会续期空闲超时,健康但无键入的会话
	// 不再被误杀;写失败(半开连接)时退出心跳协程,由读循环超时收敛。
	pingStop := make(chan struct{})
	defer close(pingStop)
	go func() {
		ticker := time.NewTicker(nativeConsolePingInterval)
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := shell.master.Read(buf)
			if n > 0 {
				if writeErr := ws.writeBinary(buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// shell 退出后主循环可能正阻塞在读帧:直接关闭连接让挂起与后续的读写
	// 都立即返回(与 deferred Close 幂等),连接、协程与 shell 清理及时释放,
	// 前端也能立刻感知会话结束。旧实现的一次性 SetReadDeadline 会被下一轮
	// 读循环重新续期覆盖,清理最长延迟一个空闲超时周期(5 分钟)。
	go func() {
		<-done
		_ = conn.Close()
	}()

	lastAuthCheck := time.Now()
	for {
		opcode, payload, err := ws.readClientMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("console websocket read idle timeout after %s, closing connection", nativeConsoleReadTimeout)
			}
			return
		}
		switch opcode {
		case 0x2:
			// 二进制消息(分片已由 readClientMessage 重组)作为 shell 输入。
			if len(payload) > 0 {
				if _, err := shell.master.Write(payload); err != nil {
					return
				}
			}
		case 0x1:
			// 文本帧为控制消息(目前仅 resize)。解析失败或类型未知的
			// 控制帧直接忽略,不崩溃也不中断连接。
			if ws.handleResizeFrame(payload) {
				setPTYWindowSize(shell.master.Fd(), ws.rows, ws.cols)
			}
		case 0x8:
			_ = ws.writeClose()
			return
		case 0x9:
			// readClientMessage 已就地应答 ping;此处为防御性兜底。
			_ = ws.writeFrame(0xA, payload)
		case 0xA:
		default:
		}

		// 会话周期复核:root shell 是权限最高的长连接,不能只在握手时鉴权
		// 一次——会话过期、在别处登出、改密吊销后,存活的连接必须终止,
		// 与 /api/ws 网关"每请求复核"的既定策略对齐(防护强度不能倒挂)。
		// 复核按时间间隔节流:心跳 pong/键入帧都会唤醒主循环,顺带检查。
		if time.Since(lastAuthCheck) >= nativeConsoleSessionRecheckInterval {
			lastAuthCheck = time.Now()
			if sessionToken == "" || !s.validateSession(sessionToken) {
				log.Printf("console websocket 会话已失效,关闭 root shell 连接")
				_ = ws.writeClose()
				return
			}
		}

		select {
		case <-done:
			return
		default:
		}
	}
}

func consoleWebSocketOriginAllowed(r *http.Request) bool {
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
	return normalizeOriginHost(u.Host) == normalizeOriginHost(r.Host)
}

func normalizeOriginHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if strings.HasSuffix(host, ":80") {
		return strings.TrimSuffix(host, ":80")
	}
	if strings.HasSuffix(host, ":443") {
		return strings.TrimSuffix(host, ":443")
	}
	return host
}

// parseConsoleResizeMessage 解析客户端窗口尺寸控制帧。
// 非法 JSON、类型不是 resize、尺寸越界一律返回 ok=false,由调用方忽略。
func parseConsoleResizeMessage(payload []byte) (rows, cols uint16, ok bool) {
	var msg nativeConsoleResizeMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, 0, false
	}
	if !strings.EqualFold(strings.TrimSpace(msg.Type), "resize") {
		return 0, 0, false
	}
	if msg.Cols < 1 || msg.Cols > 1000 || msg.Rows < 1 || msg.Rows > 1000 {
		return 0, 0, false
	}
	return uint16(msg.Rows), uint16(msg.Cols), true
}

// handleResizeFrame 尝试把文本帧解析为 resize 控制消息并记录最新尺寸,
// 解析失败返回 false(调用方应忽略该帧)。
func (ws *nativeWSConn) handleResizeFrame(payload []byte) bool {
	rows, cols, ok := parseConsoleResizeMessage(payload)
	if !ok {
		return false
	}
	ws.rows, ws.cols = rows, cols
	return true
}

func websocketAcceptKey(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// readClientMessage 读取一条完整的客户端消息,按 RFC 6455 重组分片的数据帧。
// 旧实现把 continuation 帧(0x0)当作独立消息直接写入 PTY、把分片文本控制消息的
// 首帧当完整消息丢弃,导致分片客户端的输入/控制错乱。控制帧不可分片但允许穿插
// 在数据分片之间:ping/pong 就地处理并继续累积,close 返回给调用方关闭连接。
// 返回的 opcode 取首个分片的操作码。
func (ws *nativeWSConn) readClientMessage() (byte, []byte, error) {
	var firstOpcode byte
	var buf []byte
	// 分片状态必须用独立布尔跟踪,不能以 buf==nil 判定:空的首分片经
	// append([]byte(nil), 空...) 仍为 nil,若据此判定会把后续全部延续帧
	// 误判为"孤立延续帧"丢弃,整条合法分片消息静默丢失。
	var inFragment bool
	for {
		opcode, fin, payload, err := ws.readClientFrame()
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case 0x8:
			// 关闭帧:无论是否处于分片中,直接交回调用方结束连接。
			return opcode, payload, nil
		case 0x9:
			// ping 可穿插在分片间:就地回 pong 并继续读取后续分片。
			_ = ws.writeFrame(0xA, payload)
			continue
		case 0xA:
			// 穿插的 pong:忽略并继续读取。
			continue
		}
		if opcode >= 0xB {
			// 0xB-0xF 为协议保留控制帧:忽略。
			continue
		}
		// 以下为数据帧(0x1 文本 / 0x2 二进制 / 0x0 延续;0x3-0x7 保留)。
		if !inFragment {
			if opcode == 0x0 {
				// 无前导帧的孤立延续帧:忽略。
				continue
			}
			if opcode > 0x2 {
				// 保留的数据帧操作码:忽略。
				continue
			}
			firstOpcode = opcode
			if fin {
				return firstOpcode, payload, nil
			}
			buf = append([]byte(nil), payload...)
			inFragment = true
			continue
		}
		if opcode != 0x0 {
			// 分片中又出现新的数据帧(协议违规):丢弃半成品分片,改从新帧重新跟踪。
			firstOpcode = opcode
			if fin {
				return firstOpcode, payload, nil
			}
			buf = append([]byte(nil), payload...)
			continue
		}
		buf = append(buf, payload...)
		if len(buf) > 1<<20 {
			return 0, nil, fmt.Errorf("websocket message too large: %d", len(buf))
		}
		if fin {
			return firstOpcode, buf, nil
		}
	}
}

// readClientFrame 读取单个原始帧,返回操作码、FIN 位与载荷。
func (ws *nativeWSConn) readClientFrame() (byte, bool, []byte, error) {
	// 半开连接保护:每读一帧前设置读空闲超时,收到任何帧后续期;
	// 客户端断电后读协程最多再等一个超时周期即退出,
	// 连接随之关闭,PTY shell 由 shell.close() 清理。
	if err := ws.conn.SetReadDeadline(time.Now().Add(nativeConsoleReadTimeout)); err != nil {
		return 0, false, nil, err
	}
	header := make([]byte, 2)
	if _, err := io.ReadFull(ws.br, header); err != nil {
		return 0, false, nil, err
	}
	opcode := header[0] & 0x0f
	fin := header[0]&0x80 != 0
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(ws.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(ws.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	if !masked {
		// RFC 6455 §5.1:客户端帧必须掩码,收到未掩码帧立即按协议错误
		// 断连(该通道直通 root PTY,协议收敛必须最严格);浏览器恒掩码。
		return 0, false, nil, fmt.Errorf("websocket client frame must be masked")
	}
	if opcode >= 0x8 && length > 125 {
		// RFC 6455 §5.5:控制帧载荷不得超过 125 字节。接受超大 ping
		// 并原样回 pong 会发出超限控制帧,严格客户端按帧错误断连。
		return 0, false, nil, fmt.Errorf("websocket control frame too large: %d", length)
	}
	if length > 1<<20 {
		return 0, false, nil, fmt.Errorf("websocket frame too large: %d", length)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(ws.br, mask[:]); err != nil {
			return 0, false, nil, err
		}
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(ws.br, payload); err != nil {
			return 0, false, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, fin, payload, nil
}

func (ws *nativeWSConn) writeBinary(payload []byte) error {
	return ws.writeFrame(0x2, payload)
}

func (ws *nativeWSConn) writeClose() error {
	return ws.writeFrame(0x8, nil)
}

func (ws *nativeWSConn) writeFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// 写超时保护:客户端发送窗口堵死时不能无限阻塞写端,
	// 否则会拖住 PTY 输出协程与登录回显。
	if err := ws.conn.SetWriteDeadline(time.Now().Add(nativeConsoleWriteTimeout)); err != nil {
		return err
	}

	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 0xffff:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		header = append(header, ext[:]...)
	}
	if _, err := ws.conn.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := ws.conn.Write(payload)
	return err
}

type nativeConsoleShell struct {
	master *os.File
	cmd    *exec.Cmd
	// exited 在 cmd.Wait 回收进程后关闭:close() 据此判断进程组是否已消亡,
	// 已回收则绝不再对 -pid 发信号(见 close 注释)。
	exited chan struct{}
}

func startNativeConsoleShell(rows, cols uint16) (*nativeConsoleShell, error) {
	master, slave, err := openConsolePTY()
	if err != nil {
		return nil, err
	}
	setPTYWindowSize(master.Fd(), rows, cols)

	shellPath := nativeConsoleShellPath()
	cmd := exec.Command(shellPath)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "SHELL="+shellPath)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}

	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, err
	}
	_ = slave.Close()

	shell := &nativeConsoleShell{master: master, cmd: cmd, exited: make(chan struct{})}
	go func() {
		defer close(shell.exited)
		_ = cmd.Wait()
		_ = master.Close()
	}()
	return shell, nil
}

// close 关闭 PTY 并终止整个 shell 进程组。
// shell 以 Setsid 启动,自身是新会话/进程组的组长,组号即其 PID;
// 对 -pid 发信号才能连带的杀掉它拉起的子进程(top、sleep 等),
// 只杀 shell 本体可能留下孤儿进程继续占用 PTY。
// 连接超时、客户端断开、登出失败都会走到这里。
//
// 进程已被 Wait 协程回收(exited 已关闭)时绝不再发信号:syscall.Kill(-pid)
// 绕过了 os.Process 的 ErrProcessDone 保护,pid 被复用时(本服务每次 AT
// 写入都 fork 一个 Setpgid 的短命进程组,嵌入式 pid_max 默认 32768,复用
// 很快)SIGKILL 会误杀无关进程组。关闭 master 本身会让内核向 PTY 前台
// 进程组发 SIGHUP,覆盖 shell 仍存活的常规清理场景。
func (s *nativeConsoleShell) close() {
	if s == nil {
		return
	}
	if s.master != nil {
		_ = s.master.Close()
	}
	if s.cmd == nil || s.cmd.Process == nil || s.exited == nil {
		return
	}
	select {
	case <-s.exited:
		return
	default:
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	select {
	case <-s.exited:
		return
	case <-time.After(100 * time.Millisecond):
	}
	// SIGHUP 等待期内进程可能已退出并被回收,再查一次后才允许 SIGKILL;
	// 检查与发信号之间的纳秒级窗口无法根除(需 pidfd),但相比旧实现
	// "回收后最长 5 分钟仍无条件 kill" 已把误杀窗口压缩到可忽略。
	select {
	case <-s.exited:
		return
	default:
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func nativeConsoleShellPath() string {
	for _, shell := range []string{"/bin/sh", "/usr/bin/sh", "/bin/ash"} {
		if st, err := os.Stat(shell); err == nil && !st.IsDir() {
			return shell
		}
	}
	return "/bin/sh"
}

func openConsolePTY() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, nil, err
	}

	unlock := int32(0)
	if err := ioctl(master.Fd(), tioCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		_ = master.Close()
		return nil, nil, err
	}

	var ptyNumber uint32
	if err := ioctl(master.Fd(), tioCGPTN, uintptr(unsafe.Pointer(&ptyNumber))); err != nil {
		_ = master.Close()
		return nil, nil, err
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptyNumber)
	_ = os.Chown(slavePath, 0, 20)
	_ = os.Chmod(slavePath, 0620)
	slave, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0600)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func setPTYWindowSize(fd uintptr, rows, cols uint16) {
	ws := winsize{Row: rows, Col: cols}
	if err := ioctl(fd, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil && !errors.Is(err, syscall.ENOTTY) {
		log.Printf("设置控制台窗口大小失败: %v", err)
	}
}
