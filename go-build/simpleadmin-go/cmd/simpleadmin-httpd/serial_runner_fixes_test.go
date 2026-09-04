package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLockGlobalATFileTimesOutWhileHeld(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "at.lock")
	t.Setenv("SIMPLEADMIN_AT_LOCK_FILE", lockPath)

	prevTimeout, prevInterval := atGlobalLockAcquireTimeout, atGlobalLockPollInterval
	atGlobalLockAcquireTimeout = 700 * time.Millisecond
	atGlobalLockPollInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		atGlobalLockAcquireTimeout = prevTimeout
		atGlobalLockPollInterval = prevInterval
	})

	holderUnlock, err := lockGlobalATFile()
	if err != nil {
		t.Fatalf("holder lockGlobalATFile: %v", err)
	}

	start := time.Now()
	unlock, err := lockGlobalATFile()
	elapsed := time.Since(start)
	if err == nil {
		unlock()
		holderUnlock()
		t.Fatalf("lockGlobalATFile succeeded while another holder keeps the lock")
	}
	if !strings.Contains(err.Error(), "AT lock held by another process") {
		t.Fatalf("lockGlobalATFile error = %v, want mention of AT lock held by another process", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("blocked lock acquisition took %s, want bounded by the injected acquire timeout", elapsed)
	}

	holderUnlock()
	unlock, err = lockGlobalATFile()
	if err != nil {
		t.Fatalf("lockGlobalATFile after release: %v", err)
	}
	unlock()
}

func TestSMSListGroupedCommandTimeoutGetsExtraBudget(t *testing.T) {
	smsList := smsListATCommand()
	if !strings.Contains(smsList, "+CMGL=4") {
		t.Fatalf("smsListATCommand = %q, expected to contain +CMGL=4", smsList)
	}
	got := atCommandTimeoutMS(smsList)
	if got < 20000 {
		t.Fatalf("atCommandTimeoutMS(sms list) = %d ms, want >= 20000 so a full inbox can transfer", got)
	}
	if wait := atCacheWaitTimeout(smsList); wait < time.Duration(got)*time.Millisecond {
		t.Fatalf("atCacheWaitTimeout(sms list) = %s, want >= command timeout %d ms", wait, got)
	}

	if other := atGroupedCommandTimeoutMS("AT+CSQ;+QTEMP"); other != 2000 {
		t.Fatalf("atGroupedCommandTimeoutMS(AT+CSQ;+QTEMP) = %d, want 2000 (no CMGL budget)", other)
	}
	if single := atSingleCommandTimeoutMS("ATI"); single != 1000 {
		t.Fatalf("atSingleCommandTimeoutMS(ATI) = %d, want 1000", single)
	}
	if mpdn := atCommandTimeoutMS(`AT+QMAP="MPDN_RULE",0`); mpdn != 10000 {
		t.Fatalf("atCommandTimeoutMS(MPDN_RULE) = %d, want 10000", mpdn)
	}
	if qscan := atGroupedCommandTimeoutMS("AT+QSCAN=3,1;+CSQ"); qscan != 181000 {
		t.Fatalf("atGroupedCommandTimeoutMS(QSCAN grouped) = %d, want 181000", qscan)
	}
}

func TestSMDWriterCommandRunsInOwnProcessGroup(t *testing.T) {
	cmd := newSMDWriterCommand("/dev/null", "AT\r\n")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SMD writer command must set Setpgid so the whole group can be killed on timeout")
	}
}

func TestSMDWriterTimeoutKillsProcessGroup(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "blocked.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	prev := smdATWriteTimeout
	smdATWriteTimeout = 400 * time.Millisecond
	t.Cleanup(func() { smdATWriteTimeout = prev })

	// 无人读取的 FIFO:cat 的写端 open 永久阻塞,构造写超时场景。
	reader := &smdATReader{path: fifo, notify: make(chan struct{}, 1)}
	start := time.Now()
	err := reader.writePayload("AT\r\n")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("writePayload to a never-opened FIFO should time out")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("writePayload error = %v, want timeout error", err)
	}
	if elapsed < 300*time.Millisecond || elapsed > 15*time.Second {
		t.Fatalf("writePayload took %s, want roughly the injected %s write timeout", elapsed, smdATWriteTimeout)
	}

	// 超时后不得有残留写者:若还有存活的 cat(阻塞在 open 或持着写端),
	// 此时打开读端会让它完成 open 并把 payload 写出来。
	if smdFixtureFifoHasLiveWriter(fifo, time.Second) {
		t.Fatalf("residual SMD writer survived the timeout kill on %s", fifo)
	}
}

func smdFixtureFifoHasLiveWriter(path string, wait time.Duration) bool {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	deadline := time.Now().Add(wait)
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		n, readErr := f.Read(buf)
		if n > 0 {
			return true
		}
		if readErr != nil && !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EWOULDBLOCK) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestHTTPSRedirectLocationKeepsNonDefaultPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://router.local/index.html?next=1", nil)
	req.Host = "router.local:80"

	checks := []struct {
		name      string
		httpsAddr string
		want      string
	}{
		{"non default port", ":8443", "https://router.local:8443/index.html?next=1"},
		{"explicit host and port", "192.168.1.1:8443", "https://router.local:8443/index.html?next=1"},
		{"default port", ":443", "https://router.local/index.html?next=1"},
		{"empty addr", "", "https://router.local/index.html?next=1"},
	}
	for _, check := range checks {
		if got := httpsRedirectLocation(check.httpsAddr, req); got != check.want {
			t.Fatalf("%s: httpsRedirectLocation(%q) = %q, want %q", check.name, check.httpsAddr, got, check.want)
		}
	}

	noPortReq := httptest.NewRequest(http.MethodGet, "http://router.local/", nil)
	if got, want := httpsRedirectLocation(":8443", noPortReq), "https://router.local:8443/"; got != want {
		t.Fatalf("host without port: location = %q, want %q", got, want)
	}
}

// setupSMSTransactionTest 隔离全局锁与设备候选,注入 sendSMSOnDevice 桩,
// 返回调用计数;测试结束自动恢复。
func setupSMSTransactionTest(t *testing.T, stub func(device, sendCmd, message string) (string, error)) *int {
	t.Helper()
	t.Setenv("SIMPLEADMIN_AT_LOCK_FILE", filepath.Join(t.TempDir(), "at.lock"))
	prevDevices := runtimeATDevices
	runtimeATDevices = []string{"/dev/null"}
	t.Cleanup(func() { runtimeATDevices = prevDevices })

	calls := 0
	prevSend := sendSMSOnDevice
	sendSMSOnDevice = func(device, sendCmd, message string) (string, error) {
		calls++
		return stub(device, sendCmd, message)
	}
	t.Cleanup(func() { sendSMSOnDevice = prevSend })
	return &calls
}

// TestSMSTransactionNoRetryOnResultUnknown 覆盖短信防重发:正文已交给模块
// 但未等到终结结果(超时/读取异常)时,短信可能已发送成功,绝不能自动重试
// 或换设备,且哨兵语义经 errors.Is 对调用方可判定。
func TestSMSTransactionNoRetryOnResultUnknown(t *testing.T) {
	calls := setupSMSTransactionTest(t, func(device, sendCmd, message string) (string, error) {
		return "> ", fmt.Errorf("%w: read timeout", errSMSResultUnknown)
	})

	out, err := runSMSTransaction("AT+CMGF=0;+CMGS=10", "001100")
	if !errors.Is(err, errSMSResultUnknown) {
		t.Fatalf("err = %v, want errors.Is(err, errSMSResultUnknown)", err)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d, want 1(结果未知不得重试,防止短信重复发送)", *calls)
	}
	if !strings.Contains(out, ">") {
		t.Fatalf("out = %q, want 保留已拿到的部分 transcript", out)
	}
}

// TestSMSTransactionStillRetriesDeviceErrors 覆盖设备级错误(命令未送达
// 模块)仍允许自动重试一次,与"结果未知"语义区分。
func TestSMSTransactionStillRetriesDeviceErrors(t *testing.T) {
	calls := setupSMSTransactionTest(t, func(device, sendCmd, message string) (string, error) {
		return "", errors.New("device or resource busy")
	})

	_, err := runSMSTransaction("AT+CMGF=0;+CMGS=10", "001100")
	if err == nil {
		t.Fatalf("busy device should fail the transaction")
	}
	if *calls != 2 {
		t.Fatalf("calls = %d, want 2(设备级错误应重试一次)", *calls)
	}
}

// TestAbortSMSInputModeMarksChannelDirty 覆盖短信异常中止后的通道置脏:
// 模块可能仍在输出迟到内容,下一笔事务写前的 drain 需加长窗口清理。
func TestAbortSMSInputModeMarksChannelDirty(t *testing.T) {
	reader := newTestSMDReader()
	// writeATSessionString 经 shell cat 写入,指向临时文件避免副作用。
	reader.path = filepath.Join(t.TempDir(), "smd11")
	session := &atCommandSession{device: reader.path, smdReader: reader}
	abortSMSInputMode(session)
	if !smdReaderIsDirty(reader) {
		t.Fatalf("abortSMSInputMode did not mark channel dirty")
	}
}
