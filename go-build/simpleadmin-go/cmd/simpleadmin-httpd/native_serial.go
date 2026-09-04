//go:build linux
// +build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	tioCGPTN   = 0x80045430
	tioCSPTLCK = 0x40045431
)

func openNativeTTY(path string) (*os.File, error) {
	return openNativeTTYWithFlags(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, "O_RDWR")
}

func openNativeTTYReadOnly(path string) (*os.File, error) {
	return openNativeTTYWithFlags(path, os.O_RDONLY|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, "O_RDONLY")
}

func openNativeTTYWriteOnly(path string) (*os.File, error) {
	return openNativeTTYWithFlags(path, os.O_WRONLY|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, "O_WRONLY")
}

func openNativeTTYWithFlags(path string, flags int, label string) (*os.File, error) {
	logATDebug("open native TTY %s: %s", label, path)
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(int(f.Fd()), true); err != nil {
		_ = f.Close()
		return nil, err
	}
	if shouldSkipTermiosForATDevice(path) {
		logATDebug("skip termios for AT device: %s", path)
		return f, nil
	}
	if err := configureTTYRaw(f.Fd()); err != nil {
		logATDebug("termios init failed on %s: %v", path, err)
		// Some modem character devices can pass AT commands but do not fully
		// support TCGETS/TCSETS. Treat termios setup as best-effort instead
		// of rejecting an otherwise usable AT port.
		log.Printf("AT 设备 %s termios 初始化失败，继续尝试原始读写: %v", path, err)
	}
	return f, nil
}

func shouldSkipTermiosForATDevice(path string) bool {
	return isSMDATDevice(path)
}

func configureTTYRaw(fd uintptr) error {
	return configureTTYRawWithEcho(fd, false)
}

func configureTTYRawWithEcho(fd uintptr, echo bool) error {
	var term syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, uintptr(unsafe.Pointer(&term))); err != nil {
		return err
	}

	term.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY | syscall.IMAXBEL
	term.Oflag &^= syscall.OPOST | syscall.ONLCR
	term.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	if echo {
		term.Lflag |= syscall.ECHO
	}
	term.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB
	term.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL
	term.Cc[syscall.VMIN] = 0
	term.Cc[syscall.VTIME] = 1
	term.Ispeed = syscall.B115200
	term.Ospeed = syscall.B115200

	return ioctl(fd, syscall.TCSETS, uintptr(unsafe.Pointer(&term)))
}

func ioctl(fd uintptr, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func waitReadableFD(fd int, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = 20 * time.Millisecond
	}
	for {
		var readSet syscall.FdSet
		setFDSetBit(&readSet, fd)
		tv := syscall.NsecToTimeval(timeout.Nanoseconds())
		n, err := syscall.Select(fd+1, &readSet, nil, nil, &tv)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		return n > 0, nil
	}
}

func setFDSetBit(set *syscall.FdSet, fd int) {
	if fd < 0 || fd >= 1024 {
		return
	}
	bytes := (*[128]byte)(unsafe.Pointer(set))
	bytes[fd/8] |= 1 << uint(fd%8)
}

func drainTTY(f *os.File, maxDuration time.Duration) {
	if f == nil || maxDuration <= 0 {
		return
	}
	deadline := time.Now().Add(maxDuration)
	buf := make([]byte, 1024)
	fd := int(f.Fd())
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 20*time.Millisecond {
			remaining = 20 * time.Millisecond
		}
		ready, err := waitReadableFD(fd, remaining)
		if err != nil {
			logATDebug("AT drain select err=%v", err)
			return
		}
		if !ready {
			return
		}

		n, err := syscall.Read(fd, buf)
		logATDebug("AT drain chunk n=%d err=%v", n, err)
		if n > 0 {
			continue
		}
		if err != nil && isTemporaryTTYReadError(err) {
			continue
		}
		return
	}
}

func readTTYUntilIdle(f *os.File, maxDuration, idleDuration time.Duration) (string, error) {
	deadline := time.Now().Add(maxDuration)
	idleDeadline := time.Now().Add(idleDuration)
	buf := make([]byte, 1024)
	var out strings.Builder

	for time.Now().Before(deadline) {
		n, err := f.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			idleDeadline = time.Now().Add(idleDuration)
			continue
		}
		if err != nil && !isTemporaryTTYReadError(err) {
			logATDebug("AT read non-temporary err=%v current=%s", err, outputSummary(out.String()))
			if errors.Is(err, io.EOF) {
				if out.Len() > 0 {
					return out.String(), nil
				}
				// Some SMD character devices report EOF while no response is ready yet.
				// Keep waiting until the command timeout instead of treating it as a
				// terminal failure.
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return out.String(), err
		}
		time.Sleep(20 * time.Millisecond)
		if out.Len() > 0 && time.Now().After(idleDeadline) {
			break
		}
	}
	return out.String(), nil
}

func isTemporaryTTYReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return isTemporaryTTYReadError(pathErr.Err)
	}
	return false
}

func killProcessByCmdline(needle string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		data, err := os.ReadFile(cmdlinePath)
		if err != nil || len(data) == 0 {
			continue
		}
		cmdline := strings.ReplaceAll(string(data), "\x00", " ")
		if strings.Contains(cmdline, needle) {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(pid, syscall.SIGKILL)
			log.Printf("已停止匹配 %s 的旧端口桥进程: pid=%d", needle, pid)
		}
	}
}

// errATLockHeldByAnotherProcess 在限时窗口内仍拿不到全局 AT 文件锁时返回。
var errATLockHeldByAnotherProcess = errors.New("AT lock held by another process")

var (
	// atGlobalLockAcquireTimeout 限制全局 AT 文件锁的等待上限。长命令
	// (扫网约 120 秒、短信事务约 70 秒)持锁期间,并发请求必须排队等到
	// 其完成,而不是 30 秒即失败;200 秒覆盖最大命令时长 180 秒并留有余量,
	// 到点后仍返回明确错误,避免被异常持锁者无限拖死。
	// 声明为变量以便测试注入更小的上限。
	atGlobalLockAcquireTimeout = 200 * time.Second
	// atGlobalLockPollInterval 是非阻塞抢锁轮询间隔。
	atGlobalLockPollInterval = 500 * time.Millisecond
)

func lockGlobalATFile() (func(), error) {
	lockPath := os.Getenv("SIMPLEADMIN_AT_LOCK_FILE")
	if strings.TrimSpace(lockPath) == "" {
		lockPath = "/tmp/simpleadmin-go-at.lock"
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fmt.Errorf("open AT lock file %s: %w", lockPath, err)
	}
	if err := acquireGlobalATFlock(f, atGlobalLockAcquireTimeout, atGlobalLockPollInterval); err != nil {
		_ = f.Close()
		if errors.Is(err, errATLockHeldByAnotherProcess) {
			return nil, fmt.Errorf("%w: file %s still locked after %s", errATLockHeldByAnotherProcess, lockPath, atGlobalLockAcquireTimeout)
		}
		return nil, fmt.Errorf("lock AT lock file %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// acquireGlobalATFlock 用 LOCK_EX|LOCK_NB 轮询抢锁,替代无限阻塞的
// 阻塞式 flock:外部进程(含已崩溃但未释放锁的异常持有者之外的正常持锁者)
// 长时间持锁时,AT 功能在超时后快速失败而不是永久挂起。
func acquireGlobalATFlock(f *os.File, timeout, pollInterval time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return err
		}
		if time.Now().After(deadline) {
			return errATLockHeldByAnotherProcess
		}
		sleep := pollInterval
		if remaining := time.Until(deadline); remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

func shouldRecoverBusyATDevice(device string, err error) bool {
	return false
}

func recoverBusyATDevices() {
	// Direct /dev/smd11 mode does not kill reader or bridge processes at runtime.
	time.Sleep(250 * time.Millisecond)
}

type atCommandSession struct {
	device    string
	readFile  *os.File
	writeFile *os.File
	smdReader *smdATReader
}

func openATCommandSession(path string) (*atCommandSession, error) {
	logATDebug("open AT command session: %s", path)
	if isSMDATDevice(path) {
		return openPersistentSMDATCommandSession(path)
	}
	f, err := openNativeTTY(path)
	if err != nil {
		return nil, err
	}
	return &atCommandSession{device: path, readFile: f, writeFile: f}, nil
}

func closeATSession(session *atCommandSession) error {
	if session == nil {
		return nil
	}
	if session.smdReader != nil {
		// Keep the /dev/smd* reader alive across page polling and AT requests.
		return nil
	}
	var err error
	if session.writeFile != nil && session.writeFile != session.readFile {
		err = session.writeFile.Close()
	}
	if session.readFile != nil {
		if closeErr := session.readFile.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func writeATSessionString(session *atCommandSession, payload string) error {
	if session == nil {
		return errors.New("AT session is not open")
	}
	if session.smdReader != nil {
		return session.smdReader.writePayload(payload)
	}
	if session.writeFile == nil {
		return errors.New("AT session writer is not open")
	}
	return writeFileWithDeadline(session.device, session.writeFile, payload, 3*time.Second)
}

func writeFileWithDeadline(device string, f *os.File, payload string, timeout time.Duration) error {
	data := []byte(payload)
	written := 0
	deadline := time.Now().Add(timeout)
	for written < len(data) {
		if time.Now().After(deadline) {
			return fmt.Errorf("AT session write timed out after %d/%d bytes", written, len(data))
		}
		n, err := f.Write(data[written:])
		logATDebug("AT write chunk device=%s n=%d err=%v progress=%d/%d", device, n, err, written+n, len(data))
		if err != nil {
			if isTemporaryTTYReadError(err) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return err
		}
		if n <= 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		written += n
	}
	return nil
}

func drainATSession(session *atCommandSession, maxDuration time.Duration) {
	if session == nil {
		return
	}
	if session.smdReader != nil {
		session.smdReader.drain(maxDuration)
		return
	}
	if session.readFile == nil {
		return
	}
	drainTTY(session.readFile, maxDuration)
}

// markATSessionDirty 在事务超时后标记底层通道为脏:模块迟到响应可能仍在
// 流入持久读取器,下一次 drain 需要用加长窗口清理。非持久读取器的普通
// TTY 会话每次重新打开,无需标记。
func markATSessionDirty(session *atCommandSession) {
	if session == nil || session.smdReader == nil {
		return
	}
	session.smdReader.markDirty()
}

func readATSessionUntilTokens(session *atCommandSession, maxDuration time.Duration, echoMarker string, terminalTokens ...string) (string, error) {
	if session == nil {
		return "", errors.New("AT session is not open")
	}
	if session.smdReader != nil {
		return session.smdReader.readUntilTokensWithEcho(maxDuration, echoMarker, terminalTokens...)
	}
	if session.readFile == nil {
		return "", errors.New("AT session reader is not open")
	}
	out, err := readTTYUntilTokens(session.readFile, maxDuration, terminalTokens...)
	return trimOutputBeforeEchoMarker(out, echoMarker), err
}

func readTTYUntilTokens(f *os.File, maxDuration time.Duration, terminalTokens ...string) (string, error) {
	deadline := time.Now().Add(maxDuration)
	buf := make([]byte, 1024)
	var out strings.Builder
	fd := int(f.Fd())

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		ready, selectErr := waitReadableFD(fd, remaining)
		if selectErr != nil {
			logATDebug("AT read select err=%v current=%s", selectErr, outputSummary(out.String()))
			return out.String(), selectErr
		}
		if !ready {
			continue
		}

		n, err := syscall.Read(fd, buf)
		if n > 0 {
			chunk := string(buf[:n])
			logATDebug("AT read chunk: %s", outputSummary(chunk))
			out.WriteString(chunk)
			if containsAnyToken(out.String(), terminalTokens...) {
				return out.String(), nil
			}
			continue
		}
		if err != nil && !isTemporaryTTYReadError(err) {
			logATDebug("AT read non-temporary err=%v current=%s", err, outputSummary(out.String()))
			if errors.Is(err, io.EOF) {
				if out.Len() > 0 {
					return out.String(), nil
				}
				// Some SMD character devices report EOF while no response is ready yet.
				// Keep waiting until the command timeout instead of treating it as a
				// terminal failure. Sleep like readTTYUntilIdle does: select keeps
				// reporting the EOF condition as readable, so without a sleep this
				// loop would busy-spin at 100% CPU.
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return out.String(), err
		}
	}
	return out.String(), nil
}
