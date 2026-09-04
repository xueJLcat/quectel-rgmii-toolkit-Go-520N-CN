//go:build linux
// +build linux

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func isSMDATDevice(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "smd")
}

func openPersistentSMDATCommandSession(path string) (*atCommandSession, error) {
	// /dev/smd11 behaves like the verified shell workflow:
	//   dd if=/dev/smd11 bs=4096 ... &
	//   payload > /dev/smd11
	// Keep one process-local reader helper open for the lifetime of the service,
	// using the same 4096-byte read size as the verified dd command. Each AT
	// payload is written through shell redirection fed from stdin, so the payload is
	// not embedded in a printf command and does not need shell escaping.
	// Response completion is decided by terminal AT lines such as OK,
	// ERROR, +CME ERROR, or +CMS ERROR; no runtime kill/rebuild loop is used.
	reader, err := getSMDATReader(path)
	if err != nil {
		return nil, err
	}
	return &atCommandSession{device: path, smdReader: reader}, nil
}

const (
	smdATReadBufferSize     = 4096
	smdATMaxBufferedBytes   = 512 * 1024
	smdATReaderReadyFDEnv   = "SIMPLEADMIN_SMD_READY_FD"
	smdATReaderReadyTimeout = 2 * time.Second
	// smdATReaderRestartMaxDelay 是读取器子进程重启失败时的退避上限。
	smdATReaderRestartMaxDelay = 30 * time.Second
	// smdATEchoGracePeriod is how long we keep waiting for the command echo
	// after a terminal token already arrived. If the echo never shows up the
	// reader falls back to legacy behaviour, so ports with echo disabled are
	// not penalized beyond this window.
	smdATEchoGracePeriod = 200 * time.Millisecond
	// smdATDirtyDrainMaxDuration / smdATDirtyDrainIdle 是脏通道的加长清理窗口:
	// 上一笔事务超时后模块可能仍在流式输出迟到响应,普通 30ms 静默/百毫秒级
	// 窗口吸不干;加长窗口把残留字节清出缓冲,静默持续 100ms 才认定通道干净。
	smdATDirtyDrainMaxDuration = 2 * time.Second
	smdATDirtyDrainIdle        = 100 * time.Millisecond
	// smdATDrainIdle 是正常通道的静默判据。
	smdATDrainIdle = 30 * time.Millisecond
)

// smdATWriteTimeout bounds a single /dev/smd* payload write so a blocked
// device cannot hold the device lock and global file lock forever.
// Declared as a variable so tests can inject a smaller bound.
var smdATWriteTimeout = 5 * time.Second

var smdATReaderRegistry = struct {
	mu      sync.Mutex
	readers map[string]*smdATReader
}{readers: make(map[string]*smdATReader)}

type smdATReader struct {
	path   string
	notify chan struct{}

	mu      sync.Mutex
	buffer  []byte
	lastErr error
	// dirty 表示上一笔事务以超时收尾,模块可能仍在输出迟到响应。
	// 下一次 drain 自动改用加长窗口,确认静默后才清除。
	dirty bool
}

func getSMDATReader(path string) (*smdATReader, error) {
	smdATReaderRegistry.mu.Lock()
	defer smdATReaderRegistry.mu.Unlock()
	if smdATReaderRegistry.readers == nil {
		smdATReaderRegistry.readers = make(map[string]*smdATReader)
	}
	if reader := smdATReaderRegistry.readers[path]; reader != nil {
		return reader, nil
	}
	reader, err := startSMDATReader(path)
	if err != nil {
		return nil, err
	}
	smdATReaderRegistry.readers[path] = reader
	return reader, nil
}

func startSMDATReader(path string) (*smdATReader, error) {
	reader := &smdATReader{path: path, notify: make(chan struct{}, 1)}
	stdout, cmd, err := startSMDReaderProcess(path)
	if err != nil {
		return nil, err
	}
	go reader.readLoop(stdout, cmd)
	logATDebug("started persistent SMD AT reader helper: %s pid=%d", path, cmd.Process.Pid)
	return reader, nil
}

func openSMDReaderFile(path string) (*os.File, error) {
	logATDebug("open persistent SMD reader: %s", path)
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func startSMDReaderProcess(path string) (io.ReadCloser, *exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer readyR.Close()

	cmd := exec.Command(exe, "__smd-reader", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	cmd.ExtraFiles = []*os.File{readyW}
	cmd.Env = append(os.Environ(), smdATReaderReadyFDEnv+"=3")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = readyW.Close()
		return nil, nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = readyW.Close()
		_ = stdout.Close()
		return nil, nil, err
	}
	_ = readyW.Close()
	if err := waitSMDReaderReady(readyR, cmd); err != nil {
		_ = stdout.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil, nil, err
	}
	return stdout, cmd, nil
}

func waitSMDReaderReady(readyR *os.File, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		buf := []byte{0}
		n, err := readyR.Read(buf)
		if n > 0 {
			done <- nil
			return
		}
		if err == nil {
			err = io.EOF
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("SMD reader helper did not report ready: %w", err)
		}
		return nil
	case <-time.After(smdATReaderReadyTimeout):
		return fmt.Errorf("SMD reader helper did not open device before timeout; pid=%d", cmd.Process.Pid)
	}
}

func signalSMDReaderReady() {
	fdText := strings.TrimSpace(os.Getenv(smdATReaderReadyFDEnv))
	if fdText == "" {
		return
	}
	fd, err := strconv.Atoi(fdText)
	if err != nil || fd < 0 {
		return
	}
	f := os.NewFile(uintptr(fd), "smd-reader-ready")
	if f == nil {
		return
	}
	_, _ = f.Write([]byte{1})
	_ = f.Close()
}

func runSMDReaderCommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "missing smd reader device")
		os.Exit(2)
	}
	f, err := openSMDReaderFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open smd reader failed: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	signalSMDReaderReady()
	buf := make([]byte, smdATReadBufferSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if writeErr := writeAll(os.Stdout, buf[:n]); writeErr != nil {
				fmt.Fprintf(os.Stderr, "smd reader stdout write failed: %v\n", writeErr)
				os.Exit(1)
			}
		}
		if err == nil {
			continue
		}
		if isTemporaryTTYReadError(err) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		fmt.Fprintf(os.Stderr, "smd reader read failed: %v\n", err)
		os.Exit(1)
	}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[n:]
	}
	return nil
}

func (r *smdATReader) readLoop(stdout io.ReadCloser, cmd *exec.Cmd) {
	buf := make([]byte, smdATReadBufferSize)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			r.append(buf[:n])
		}
		if err == nil {
			continue
		}
		logATDebug("persistent SMD reader helper ended on %s: %v", r.path, err)
		r.setLastErr(err)
		_ = stdout.Close()
		_ = cmd.Wait()
		time.Sleep(250 * time.Millisecond)
		// 设备消失时无限快速重启会产生持续 fork 与日志噪音;
		// 采用指数退避并在成功重启后复位。
		retryDelay := time.Second
		for {
			newStdout, newCmd, openErr := startSMDReaderProcess(r.path)
			if openErr == nil {
				logATDebug("persistent SMD reader helper restarted: %s pid=%d", r.path, newCmd.Process.Pid)
				r.setLastErr(nil)
				stdout = newStdout
				cmd = newCmd
				break
			}
			logATDebug("persistent SMD reader helper restart failed on %s: %v (retry in %s)", r.path, openErr, retryDelay)
			r.setLastErr(openErr)
			time.Sleep(retryDelay)
			if retryDelay < smdATReaderRestartMaxDelay {
				retryDelay *= 2
				if retryDelay > smdATReaderRestartMaxDelay {
					retryDelay = smdATReaderRestartMaxDelay
				}
			}
		}
	}
}

func (r *smdATReader) append(data []byte) {
	r.mu.Lock()
	r.buffer = append(r.buffer, data...)
	if len(r.buffer) > smdATMaxBufferedBytes {
		r.buffer = append([]byte(nil), r.buffer[len(r.buffer)-smdATMaxBufferedBytes:]...)
	}
	r.mu.Unlock()
	r.signal()
}

func (r *smdATReader) setLastErr(err error) {
	r.mu.Lock()
	r.lastErr = err
	r.mu.Unlock()
	r.signal()
}

func (r *smdATReader) signal() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

// markDirty 在事务超时(未等到终结 token)后调用:模块可能还在输出该命令的
// 迟到响应,后续第一次 drain 必须用加长窗口把残留字节吸干净,否则脏数据会
// 污染下一笔事务的读取。
func (r *smdATReader) markDirty() {
	r.mu.Lock()
	r.dirty = true
	r.mu.Unlock()
	logATDebug("persistent SMD AT reader marked dirty: %s", r.path)
}

func (r *smdATReader) drain(maxDuration time.Duration) {
	r.mu.Lock()
	r.buffer = nil
	dirty := r.dirty
	r.mu.Unlock()
	if maxDuration <= 0 {
		logATDebug("drained persistent SMD AT reader buffer: %s", r.path)
		return
	}
	idleDuration := smdATDrainIdle
	if dirty {
		if maxDuration < smdATDirtyDrainMaxDuration {
			maxDuration = smdATDirtyDrainMaxDuration
		}
		idleDuration = smdATDirtyDrainIdle
	}
	deadline := time.NewTimer(maxDuration)
	defer deadline.Stop()
	idle := time.NewTimer(idleDuration)
	defer idle.Stop()
	for {
		select {
		case <-r.notify:
			r.mu.Lock()
			r.buffer = nil
			r.mu.Unlock()
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleDuration)
		case <-idle.C:
			if dirty {
				r.mu.Lock()
				r.dirty = false
				r.mu.Unlock()
			}
			logATDebug("drained persistent SMD AT reader buffer after idle (dirty=%v): %s", dirty, r.path)
			return
		case <-deadline.C:
			logATDebug("drained persistent SMD AT reader buffer after max duration (dirty=%v): %s", dirty, r.path)
			return
		}
	}
}

func (r *smdATReader) snapshot() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(append([]byte(nil), r.buffer...)), r.lastErr
}

func newSMDWriterCommand(path, payload string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", `cat > "$1"`, "smd-writer", path)
	// 独立进程组:sh 会派生(或 exec 成)cat,超时只杀 sh 的 pid 会把 cat
	// 当孤儿留下继续持有设备;放进独立进程组后超时可以整组清掉。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = strings.NewReader(payload)
	return cmd
}

func (r *smdATReader) writePayload(payload string) error {
	// Keep the shell redirection semantics that work with /dev/smd11, but do not
	// use printf. The AT payload is passed over stdin and copied to the device by
	// the shell-opened redirection, which avoids quoting issues and keeps SMS Ctrl-Z
	// payloads byte-for-byte. This cat is write-only and does not read /dev/smd11.
	// 写入带超时保护:设备异常导致写阻塞时不能永久占用设备锁与全局文件锁。
	// 不用 exec.CommandContext:它的默认取消只杀 sh 一个进程,cat 会孤儿残留
	// 持有设备;这里自定义等待/取消逻辑,超时先杀整个进程组并回收后再返回。
	cmd := newSMDWriterCommand(r.path, payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("SMD shell stdin writer failed: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(smdATWriteTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return fmt.Errorf("SMD shell stdin writer failed: %w: %s", err, msg)
			}
			return fmt.Errorf("SMD shell stdin writer failed: %w", err)
		}
		return nil
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// 回收进程组,确保 cat 已退出、设备已释放,再带着超时错误返回。
		<-done
		return fmt.Errorf("SMD shell stdin writer timed out after %s", smdATWriteTimeout)
	}
}

func (r *smdATReader) readUntilTokens(maxDuration time.Duration, terminalTokens ...string) (string, error) {
	return r.readUntilTokensWithEcho(maxDuration, "", terminalTokens...)
}

// readUntilTokensWithEcho behaves like readUntilTokens but correlates the
// response with the echoed command line. Everything received before the echo
// (stale output of a previous command) is discarded once the echo shows up.
// When the modem echoes commands, this prevents stale trailing data from one
// command leaking into the next command's captured response. If no echo ever
// appears (echo disabled on the port) it falls back to the legacy behaviour
// after a short grace period.
func (r *smdATReader) readUntilTokensWithEcho(maxDuration time.Duration, echoMarker string, terminalTokens ...string) (string, error) {
	if maxDuration <= 0 {
		maxDuration = time.Second
	}
	echoFound := strings.TrimSpace(echoMarker) == ""
	deadline := time.NewTimer(maxDuration)
	defer deadline.Stop()
	var grace *time.Timer
	for {
		if !echoFound && r.discardBeforeEchoMarker(echoMarker) {
			echoFound = true
			if grace != nil {
				if !grace.Stop() {
					select {
					case <-grace.C:
					default:
					}
				}
				grace = nil
			}
			logATDebug("SMD persistent reader discarded stale data before echo: %s", atPayloadSummary(echoMarker))
		}
		out, lastErr := r.snapshot()
		if containsAnyToken(out, terminalTokens...) {
			if echoFound {
				return out, nil
			}
			if grace == nil {
				grace = time.NewTimer(smdATEchoGracePeriod)
				defer grace.Stop()
			}
		}
		var graceC <-chan time.Time
		if grace != nil {
			graceC = grace.C
		}
		select {
		case <-r.notify:
			continue
		case <-graceC:
			// A terminal token arrived but the command echo never showed up
			// (echo disabled on the port). Fall back to legacy behaviour.
			out, _ := r.snapshot()
			return out, nil
		case <-deadline.C:
			// 截止瞬间重新取缓冲并复判:循环顶部的快照与截止触发之间
			// 到达的完整应答不能丢——否则迟到的完整响应被误报超时,
			// 还会被下一事务当脏数据排空。
			if !echoFound {
				_ = r.discardBeforeEchoMarker(echoMarker)
			}
			out, lastErr = r.snapshot()
			if containsAnyToken(out, terminalTokens...) {
				return out, nil
			}
			if out != "" || lastErr == nil {
				return out, nil
			}
			return out, lastErr
		}
	}
}

// discardBeforeEchoMarker drops buffered bytes that precede the echoed AT
// command line. Returns true when the marker is present in the buffer.
func (r *smdATReader) discardBeforeEchoMarker(marker string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := findEchoMarker(r.buffer, marker)
	if idx < 0 {
		return false
	}
	if idx > 0 {
		kept := make([]byte, len(r.buffer)-idx)
		copy(kept, r.buffer[idx:])
		r.buffer = kept
	}
	return true
}
