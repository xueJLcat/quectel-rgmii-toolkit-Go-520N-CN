package main

import (
	"strings"
	"testing"
	"time"
)

func TestFindEchoMarker(t *testing.T) {
	checks := []struct {
		name   string
		buf    string
		marker string
		want   int
	}{
		{"exact", "AT+CGMI\r\nQuectel\r\nOK\r\n", "AT+CGMI", 0},
		{"case-insensitive", "at+cgmi\r\nOK\r\n", "AT+CGMI", 0},
		{"after stale data", "STALE\r\nOK\r\nAT+CGMI\r\nQuectel\r\nOK\r\n", "AT+CGMI", 11},
		{"missing", "Quectel\r\nOK\r\n", "AT+CGMI", -1},
		{"empty marker", "AT+CGMI\r\n", "", -1},
		{"restart after partial", "AT+CGX\r\nAT+CGMI\r\nOK\r\n", "AT+CGMI", 8},
	}
	for _, check := range checks {
		if got := findEchoMarker([]byte(check.buf), check.marker); got != check.want {
			t.Fatalf("%s: findEchoMarker = %d, want %d", check.name, got, check.want)
		}
	}
}

func TestAtPayloadEchoMarker(t *testing.T) {
	checks := []struct {
		payload string
		want    string
	}{
		{`AT+CGMI`, "AT+CGMI"},
		{"AT+QSIMSTAT?;+CSQ;+QTEMP\r\n", "AT+QSIMSTAT?;+CSQ;+QTEMP"},
		{"at+cimi\r\n", "AT+CIMI"},
		{"", ""},
		{"HELLO", ""},
		{"\r\n", ""},
	}
	for _, check := range checks {
		if got := atPayloadEchoMarker(check.payload); got != check.want {
			t.Fatalf("atPayloadEchoMarker(%q) = %q, want %q", check.payload, got, check.want)
		}
	}
}

func TestTrimOutputBeforeEchoMarker(t *testing.T) {
	out := "STALE DATA\r\nOK\r\nAT+CGMI\r\nQuectel\r\nOK\r\n"
	got := trimOutputBeforeEchoMarker(out, "AT+CGMI")
	if !strings.HasPrefix(got, "AT+CGMI") || strings.Contains(got, "STALE") {
		t.Fatalf("trimmed output = %q, want start with AT+CGMI and no stale data", got)
	}
	if got := trimOutputBeforeEchoMarker(out, ""); got != out {
		t.Fatalf("empty marker should return output unchanged, got %q", got)
	}
	if got := trimOutputBeforeEchoMarker("no echo here", "AT+CGMI"); got != "no echo here" {
		t.Fatalf("missing marker should return output unchanged, got %q", got)
	}
}

func newTestSMDReader() *smdATReader {
	return &smdATReader{path: "test", notify: make(chan struct{}, 1)}
}

func TestSMDReaderDiscardsStaleDataBeforeEcho(t *testing.T) {
	reader := newTestSMDReader()
	// Stale trailing output of a previous transaction arrives first.
	reader.append([]byte("STALE\r\n+QENG: \"stale\"\r\nOK\r\n"))

	done := make(chan string, 1)
	go func() {
		out, _ := reader.readUntilTokensWithEcho(2*time.Second, "AT+CGMI", "OK", "ERROR")
		done <- out
	}()

	time.Sleep(50 * time.Millisecond)
	reader.append([]byte("AT+CGMI\r\nQuectel\r\nOK\r\n"))

	select {
	case out := <-done:
		if strings.Contains(out, "STALE") {
			t.Fatalf("stale data leaked into response: %q", out)
		}
		if !strings.HasPrefix(out, "AT+CGMI") {
			t.Fatalf("response should start at echoed command: %q", out)
		}
		if !strings.Contains(out, "Quectel") || !containsATFinalLine(out, "OK") {
			t.Fatalf("response missing payload/OK: %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("readUntilTokensWithEcho did not return in time")
	}
}

func TestSMDReaderEchoArrivesAfterStaleTerminal(t *testing.T) {
	reader := newTestSMDReader()
	// Stale terminal token of the previous transaction arrives before our echo.
	reader.append([]byte("OK\r\n"))

	done := make(chan string, 1)
	go func() {
		out, _ := reader.readUntilTokensWithEcho(2*time.Second, "AT+CGMI", "OK", "ERROR")
		done <- out
	}()

	// Echo + real response arrive within the grace period.
	time.Sleep(50 * time.Millisecond)
	reader.append([]byte("AT+CGMI\r\nQuectel\r\nOK\r\n"))

	select {
	case out := <-done:
		if strings.Contains(out, "OK\r\nAT+CGMI") == false && strings.HasPrefix(out, "OK") {
			t.Fatalf("stale OK should have been discarded: %q", out)
		}
		if !strings.HasPrefix(out, "AT+CGMI") {
			t.Fatalf("response should start at echoed command: %q", out)
		}
		if !strings.Contains(out, "Quectel") {
			t.Fatalf("response missing real payload: %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("readUntilTokensWithEcho did not return in time")
	}
}

func TestSMDReaderFallsBackWithoutEcho(t *testing.T) {
	reader := newTestSMDReader()
	done := make(chan string, 1)
	go func() {
		out, _ := reader.readUntilTokensWithEcho(2*time.Second, "AT+CGMI", "OK", "ERROR")
		done <- out
	}()
	// Modem responds without echoing the command (echo disabled).
	reader.append([]byte("Quectel\r\nOK\r\n"))

	select {
	case out := <-done:
		if !strings.Contains(out, "Quectel") || !containsATFinalLine(out, "OK") {
			t.Fatalf("fallback should return full response: %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("readUntilTokensWithEcho did not fall back in time")
	}
}

func smdReaderIsDirty(reader *smdATReader) bool {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.dirty
}

// TestSMDReaderDirtyDrainAbsorbsLateResponse 覆盖超时置脏后的再同步:
// 事务超时后模块仍在流式输出迟到响应,下一次 drain 必须自动改用加长
// 窗口把残留字节吸干净并清除脏标记;干净通道仍走原来的短窗口。
func TestSMDReaderDirtyDrainAbsorbsLateResponse(t *testing.T) {
	reader := newTestSMDReader()

	// 干净通道:无数据时 30ms 静默即返回,不受脏窗口影响。
	start := time.Now()
	reader.drain(150 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > 120*time.Millisecond {
		t.Fatalf("clean drain took %s, want quick idle return", elapsed)
	}

	reader.markDirty()
	if !smdReaderIsDirty(reader) {
		t.Fatalf("markDirty did not set dirty flag")
	}

	// 迟到响应持续流入:块间隔小于脏静默判据(100ms),模拟模块流式输出;
	// 总时长远超原 150ms 写前窗口,验证加长窗口把它全部吸干净。
	go func() {
		reader.append([]byte("LATE DATA\r\n"))
		for i := 0; i < 7; i++ {
			time.Sleep(60 * time.Millisecond)
			reader.append([]byte("LATE DATA\r\n"))
		}
	}()

	start = time.Now()
	reader.drain(150 * time.Millisecond) // 脏窗口自动加长
	elapsed := time.Since(start)
	if elapsed < 350*time.Millisecond {
		t.Fatalf("dirty drain returned after %s, want extended window absorbing late data", elapsed)
	}
	if out, _ := reader.snapshot(); out != "" {
		t.Fatalf("buffer not drained after dirty drain: %q", out)
	}
	if smdReaderIsDirty(reader) {
		t.Fatalf("dirty flag not cleared after quiet drain")
	}
}

// TestSMDReaderDirtyDrainKeepsDirtyWhileStreaming 覆盖脏窗口到期仍未安静
// 的场景:脏标记必须保留,让下一笔事务继续加长清理。
func TestSMDReaderDirtyDrainKeepsDirtyWhileStreaming(t *testing.T) {
	reader := newTestSMDReader()
	reader.markDirty()

	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				reader.append([]byte("STREAM\r\n"))
			}
		}
	}()

	start := time.Now()
	reader.drain(150 * time.Millisecond)
	elapsed := time.Since(start)
	close(stop)
	if elapsed < smdATDirtyDrainMaxDuration-100*time.Millisecond {
		t.Fatalf("dirty drain returned after %s, want full extended window while streaming", elapsed)
	}
	if !smdReaderIsDirty(reader) {
		t.Fatalf("dirty flag must persist while late data keeps streaming")
	}
}

func TestMarkATSessionDirty(t *testing.T) {
	reader := newTestSMDReader()
	markATSessionDirty(&atCommandSession{device: "/dev/smd11", smdReader: reader})
	if !smdReaderIsDirty(reader) {
		t.Fatalf("markATSessionDirty did not mark smd reader dirty")
	}
	// 非持久读取器会话与 nil 会话是空操作。
	markATSessionDirty(nil)
	markATSessionDirty(&atCommandSession{device: "/dev/ttyUSB0"})
}
