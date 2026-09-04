package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempSchedulerEnv 将配置文件指向临时目录，并清空调度状态。
func withTempSchedulerEnv(t *testing.T) {
	t.Helper()
	oldTTL := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(t.TempDir(), "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTL })
	schedulerMu.Lock()
	schedulerLastRunDate = ""
	schedulerMu.Unlock()
}

// withFakeRebootAction 用计数器替换重启动作，避免触发真实重启。
func withFakeRebootAction(t *testing.T) *int {
	t.Helper()
	count := 0
	oldAction := schedulerRunReboot
	schedulerRunReboot = func(s *simpleAdminServer) string {
		count++
		return "OK"
	}
	t.Cleanup(func() { schedulerRunReboot = oldAction })
	return &count
}

func TestSchedulerConfigRoundTrip(t *testing.T) {
	withTempSchedulerEnv(t)

	want := schedulerConfig{RebootEnabled: true, RebootTime: "23:59"}
	if err := writeSchedulerConfig(want); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}
	if got := readSchedulerConfig(); got != want {
		t.Fatalf("readSchedulerConfig = %+v, want %+v", got, want)
	}
	info, err := os.Stat(schedulerConfigPath())
	if err != nil {
		t.Fatalf("stat scheduler.conf: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("scheduler.conf perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestSchedulerConfigDefaults(t *testing.T) {
	withTempSchedulerEnv(t)

	got := readSchedulerConfig()
	if got.RebootEnabled || got.RebootTime != defaultSchedulerRebootTime {
		t.Fatalf("readSchedulerConfig = %+v, want disabled %s", got, defaultSchedulerRebootTime)
	}
}

func TestSchedulerConfigInvalidTimeFallsBack(t *testing.T) {
	withTempSchedulerEnv(t)

	invalid := []byte(`{"rebootEnabled":true,"rebootTime":"25:00"}`)
	if err := os.WriteFile(schedulerConfigPath(), invalid, 0600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	got := readSchedulerConfig()
	if got.RebootTime != defaultSchedulerRebootTime {
		t.Fatalf("rebootTime = %q, want fallback %s", got.RebootTime, defaultSchedulerRebootTime)
	}
	if !got.RebootEnabled {
		t.Fatalf("rebootEnabled = false, want true preserved")
	}

	if err := os.WriteFile(schedulerConfigPath(), []byte(`not json`), 0600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	got = readSchedulerConfig()
	if got != (schedulerConfig{RebootTime: defaultSchedulerRebootTime}) {
		t.Fatalf("broken config = %+v, want full defaults", got)
	}

	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "9:99"}); err != nil {
		t.Fatalf("writeSchedulerConfig invalid time: %v", err)
	}
	if got := readSchedulerConfig(); got.RebootTime != defaultSchedulerRebootTime {
		t.Fatalf("written invalid time = %q, want normalized %s", got.RebootTime, defaultSchedulerRebootTime)
	}
}

func TestSchedulerCheckOnceTriggersOncePerDay(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	day1At := time.Date(2026, 1, 2, 4, 0, 0, 0, time.Local)
	if !schedulerCheckOnce(nil, day1At) {
		t.Fatalf("first tick at 04:00 should trigger")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1", *count)
	}

	if schedulerCheckOnce(nil, day1At.Add(20*time.Second)) {
		t.Fatalf("same-day tick within 04:00 should not re-trigger")
	}
	if schedulerCheckOnce(nil, day1At.Add(time.Hour)) {
		t.Fatalf("same-day tick outside 04:00 should not trigger")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1 after same-day ticks", *count)
	}

	day2At := time.Date(2026, 1, 3, 4, 0, 0, 0, time.Local)
	if !schedulerCheckOnce(nil, day2At) {
		t.Fatalf("next-day tick at 04:00 should trigger again")
	}
	if *count != 2 {
		t.Fatalf("action count = %d, want 2", *count)
	}
	if got := currentSchedulerStatus()["lastRebootDate"]; got != "2026-01-03" {
		t.Fatalf("lastRebootDate = %v, want 2026-01-03", got)
	}
}

func TestSchedulerCheckOnceSkipsWhenDisabledOrMismatch(t *testing.T) {
	withTempSchedulerEnv(t)
	count := withFakeRebootAction(t)

	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("default (disabled) config should not trigger")
	}
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: false, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 4, 0, 0, 0, time.Local)) {
		t.Fatalf("disabled config should not trigger")
	}
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}
	if schedulerCheckOnce(nil, time.Date(2026, 1, 2, 5, 30, 0, 0, time.Local)) {
		t.Fatalf("time mismatch should not trigger")
	}
	if *count != 0 {
		t.Fatalf("action count = %d, want 0", *count)
	}
}

func TestCurrentSchedulerStatus(t *testing.T) {
	withTempSchedulerEnv(t)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "06:30"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}
	schedulerMu.Lock()
	schedulerLastRunDate = "2026-01-02"
	schedulerMu.Unlock()

	st := currentSchedulerStatus()
	if st["rebootEnabled"] != true || st["rebootTime"] != "06:30" || st["lastRebootDate"] != "2026-01-02" {
		t.Fatalf("currentSchedulerStatus = %v", st)
	}
}

// withRebootResponse 用返回固定响应文本的桩替换重启动作,避免触发真实重启。
func withRebootResponse(t *testing.T, response *string) *int {
	t.Helper()
	count := 0
	oldAction := schedulerRunReboot
	schedulerRunReboot = func(s *simpleAdminServer) string {
		count++
		return *response
	}
	t.Cleanup(func() { schedulerRunReboot = oldAction })
	return &count
}

func lastRunDates() (persisted, inMemory string) {
	persisted = readSchedulerState().LastRunDate
	schedulerMu.Lock()
	inMemory = schedulerLastRunDate
	schedulerMu.Unlock()
	return persisted, inMemory
}

// TestSchedulerRebootFailureKeepsRetryable 覆盖审计修复 B8:响应出现终结
// 错误行 -> 判定执行失败,不写 LastRunDate(持久化与内存均不更新),
// 下一轮调度可重试;重试成功后照常写入触发记录。
func TestSchedulerRebootFailureKeepsRetryable(t *testing.T) {
	withTempSchedulerEnv(t)
	response := "AT+CFUN=1,1\r\n+CME ERROR: unknown error"
	count := withRebootResponse(t, &response)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	day1At := time.Date(2026, 1, 2, 4, 0, 0, 0, time.Local)
	if schedulerCheckOnce(nil, day1At) {
		t.Fatalf("terminal error response must not count as triggered")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1", *count)
	}
	if persisted, inMemory := lastRunDates(); persisted != "" || inMemory != "" {
		t.Fatalf("failed run recorded LastRunDate: persisted=%q inMemory=%q, want both empty", persisted, inMemory)
	}

	// 未记录"已触发",下一轮调度重试;成功响应后写入当日触发记录。
	response = "AT+CFUN=1,1\r\nOK"
	if !schedulerCheckOnce(nil, day1At.Add(30*time.Second)) {
		t.Fatalf("next round after failed run should trigger again")
	}
	if *count != 2 {
		t.Fatalf("action count = %d, want 2", *count)
	}
	if persisted, inMemory := lastRunDates(); persisted != "2026-01-02" || inMemory != "2026-01-02" {
		t.Fatalf("successful run LastRunDate = %q/%q, want 2026-01-02", persisted, inMemory)
	}
}

// TestSchedulerRebootNoResponseCountsAsTriggered 覆盖审计修复 B8:重启命令
// "模块先重启、后应答",超时无应答是常态——响应无终结错误行时视为已触发,
// 照常写入 LastRunDate,当日不再重复触发。
func TestSchedulerRebootNoResponseCountsAsTriggered(t *testing.T) {
	withTempSchedulerEnv(t)
	response := "timeout waiting for OK/ERROR"
	count := withRebootResponse(t, &response)
	if err := writeSchedulerConfig(schedulerConfig{RebootEnabled: true, RebootTime: "04:00"}); err != nil {
		t.Fatalf("writeSchedulerConfig: %v", err)
	}

	day1At := time.Date(2026, 1, 2, 4, 0, 0, 0, time.Local)
	if !schedulerCheckOnce(nil, day1At) {
		t.Fatalf("no-response (runner timeout) must count as triggered for reboot commands")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1", *count)
	}
	if persisted, inMemory := lastRunDates(); persisted != "2026-01-02" || inMemory != "2026-01-02" {
		t.Fatalf("no-response run LastRunDate = %q/%q, want 2026-01-02", persisted, inMemory)
	}
	if schedulerCheckOnce(nil, day1At.Add(time.Minute)) {
		t.Fatalf("same-day tick after triggered run must not re-trigger")
	}
	if *count != 1 {
		t.Fatalf("action count = %d, want 1 after same-day tick", *count)
	}
}
