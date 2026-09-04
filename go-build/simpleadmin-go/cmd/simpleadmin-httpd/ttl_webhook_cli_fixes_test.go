package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- TTL 禁用路径:删除失败不持久化 ---

// TestSetNativeTTLDisableCleanupFailureDoesNotPersistZero 构造删除失败
// （清空 PATH 使 iptables 必然失败）：禁用路径不得持久化 0，
// 否则会出现"状态未激活但内核残留规则"的不一致。仅 linux 有效。
func TestSetNativeTTLDisableCleanupFailureDoesNotPersistZero(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real iptables cleanup path is linux-only")
	}
	dir := t.TempDir()
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	// 预置已激活状态：持久化值为 65。
	if err := writeTTLValue(65); err != nil {
		t.Fatalf("writeTTLValue: %v", err)
	}
	// 清空 PATH，令 iptables/ip6tables 的列表/删除命令必然失败。
	t.Setenv("PATH", t.TempDir())

	logs := setNativeTTL(0)
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "list failed") {
		t.Fatalf("logs = %q, want cleanup failure detail", joined)
	}
	if !strings.Contains(joined, "not persisted") || !strings.Contains(joined, "TTL disable failed") {
		t.Fatalf("logs = %q, want disable-failed/not-persisted marker", joined)
	}
	if strings.Contains(joined, "TTL disabled") {
		t.Fatalf("logs = %q, must not report success path on cleanup failure", joined)
	}

	data, err := os.ReadFile(runtimeTTLValueFile)
	if err != nil {
		t.Fatalf("ttlvalue file must survive failed disable: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "65" {
		t.Fatalf("ttlvalue file = %q, want 65 (0 must not be persisted when cleanup fails)", got)
	}
	if value, enabled := currentTTLValue(); !enabled || value != 65 {
		t.Fatalf("currentTTLValue = (%d, %t), want (65, true) after failed disable", value, enabled)
	}
}

// --- Webhook 回绕:重置为最大索引-1,最大索引消息仍通知 ---

// TestSMSWebhookWraparoundNotifiesCurrentMax 高水位 50、列表最大索引 3：
// 回绕重置为 3-1=2，索引 3 的消息触发一次通知；索引 2 的旧消息不补发
// （证明重置值恰为 2 而非更低）。此后可见索引 3 与更高水位状态。
func TestSMSWebhookWraparoundNotifiesCurrentMax(t *testing.T) {
	setupSMSWebhookTest(t)
	if err := writeSMSWebhookConfig(smsWebhookConfig{Enabled: true, URL: "http://example.com/hook"}); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	ch := installSMSWebhookRecorder(t)

	smsWebhookInitialized = map[string]bool{"ME": true}
	smsWebhookHighWater = map[string]int{"ME": 50}

	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/30,09:00:00+32", "旧消息", 2),
		smsWebhookTestMessage("+8613800000002", "26/08/30,09:01:00+32", "复用索引的新消息", 3),
	}, true)

	records := waitForSMSWebhookPosts(t, ch, 1)
	var payload map[string]any
	if err := json.Unmarshal(records[0].body, &payload); err != nil {
		t.Fatalf("通知内容不是合法 JSON: %v", err)
	}
	if int(payload["index"].(float64)) != 3 {
		t.Fatalf("应对当前最大索引 3 的消息通知一次，实际: %s", records[0].body)
	}
	expectNoSMSWebhookPosts(t, ch)

	if smsWebhookHighWater["ME"] != 3 {
		t.Fatalf("高水位应推进到当前最大索引 3，实际: %d", smsWebhookHighWater["ME"])
	}
	if status := currentSMSWebhookStatus(); status["lastNotifiedIndex"] != 3 {
		t.Fatalf("lastNotifiedIndex = %v, want 3", status["lastNotifiedIndex"])
	}

	// 同一最大索引再次扫描不重复通知。
	notifyNewSMSByWebhook([]map[string]any{
		smsWebhookTestMessage("+8613800000001", "26/08/30,09:00:00+32", "旧消息", 2),
		smsWebhookTestMessage("+8613800000002", "26/08/30,09:01:00+32", "复用索引的新消息", 3),
	}, true)
	expectNoSMSWebhookPosts(t, ch)
}

// --- CLI 锁超时错误提示 ---

// TestAnnotateATLockErrorAddsContentionHint 锁获取超时的错误经标注后
// 必须包含"可能被主服务占用、改用服务接口"的提示；其他错误原样返回。
func TestAnnotateATLockErrorAddsContentionHint(t *testing.T) {
	lockErr := errors.New("AT lock held by another process: file /tmp/simpleadmin-go-at.lock still locked after 30s")
	annotated := annotateATLockError(lockErr)
	if annotated == nil {
		t.Fatal("annotated error must not be nil")
	}
	msg := annotated.Error()
	for _, want := range []string{atLockHeldErrorMarker, "simpleadmin-httpd", "服务接口"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("annotated error = %q, want hint containing %q", msg, want)
		}
	}

	plain := errors.New("timeout waiting for OK")
	if annotateATLockError(plain) != plain {
		t.Fatal("non-lock error must be returned unchanged")
	}
	if annotateATLockError(nil) != nil {
		t.Fatal("nil error must stay nil")
	}
}

// TestCLILockTimeoutErrorCarriesHint 端到端验证：他人持锁时
// runATCommandUntilDone 返回锁超时错误，标注后带占用提示。仅 linux
// （非 linux 的锁桩恒成功，无超时路径）。
func TestCLILockTimeoutErrorCarriesHint(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("AT file lock is linux-only")
	}
	lockPath := filepath.Join(t.TempDir(), "at.lock")
	t.Setenv("SIMPLEADMIN_AT_LOCK_FILE", lockPath)

	prevTimeout, prevInterval := atGlobalLockAcquireTimeout, atGlobalLockPollInterval
	atGlobalLockAcquireTimeout = 300 * time.Millisecond
	atGlobalLockPollInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		atGlobalLockAcquireTimeout = prevTimeout
		atGlobalLockPollInterval = prevInterval
	})

	holderUnlock, err := lockGlobalATFile()
	if err != nil {
		t.Fatalf("holder lockGlobalATFile: %v", err)
	}
	defer holderUnlock()

	_, err = runATCommandUntilDone("ATI", 200, 1000)
	if err == nil {
		t.Fatal("runATCommandUntilDone should fail while the lock is held")
	}
	if !strings.Contains(err.Error(), atLockHeldErrorMarker) {
		t.Fatalf("error = %v, want lock-held marker", err)
	}
	msg := annotateATLockError(err).Error()
	if !strings.Contains(msg, "simpleadmin-httpd") || !strings.Contains(msg, "服务接口") {
		t.Fatalf("annotated CLI error = %q, want contention hint", msg)
	}
}
