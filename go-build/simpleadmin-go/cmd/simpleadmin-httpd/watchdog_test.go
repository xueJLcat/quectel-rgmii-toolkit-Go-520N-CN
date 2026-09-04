package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// setupWatchdogTest 将配置文件指向临时目录,替换检测/动作函数为可控实现,
// 并在测试结束后恢复全部全局状态,确保不触发真实 AT/重启动作。
// radioCalls 统计无线电重注册动作调用次数(升级策略测试用)。
func setupWatchdogTest(t *testing.T) (checkCalls *int, actionCalls *int, online *bool) {
	t.Helper()
	dir := t.TempDir()
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")

	calls := 0
	actions := 0
	onlineValue := false
	oldCheck := watchdogCheckOnline
	oldAction := watchdogRebootAction
	oldRadio := watchdogRadioResetAction
	watchdogCheckOnline = func(ctx context.Context, targets []string) bool {
		calls++
		return onlineValue
	}
	watchdogRebootAction = func(s *simpleAdminServer) string {
		actions++
		return "OK"
	}
	watchdogRadioResetAction = func(s *simpleAdminServer) string {
		return "OK"
	}
	resetWatchdogForTest()

	t.Cleanup(func() {
		watchdogCheckOnline = oldCheck
		watchdogRebootAction = oldAction
		watchdogRadioResetAction = oldRadio
		runtimeTTLValueFile = oldTTLFile
		resetWatchdogForTest()
	})
	return &calls, &actions, &onlineValue
}

func writeWatchdogTestConfig(t *testing.T, cfg watchdogConfig) {
	t.Helper()
	if err := writeWatchdogConfig(cfg); err != nil {
		t.Fatalf("write watchdog config: %v", err)
	}
}

func TestWatchdogConfigRoundTrip(t *testing.T) {
	setupWatchdogTest(t)
	want := watchdogConfig{
		Enabled:              true,
		FailThreshold:        7,
		CooldownMinutes:      12,
		CheckIntervalMinutes: 9,
		Targets:              []string{"192.168.1.1:53", "8.8.4.4:53"},
		ActionPolicy:         watchdogPolicyEscalate,
	}
	writeWatchdogTestConfig(t, want)

	got := readWatchdogConfig()
	if len(got.Targets) != len(want.Targets) || got.Targets[0] != want.Targets[0] || got.Targets[1] != want.Targets[1] {
		t.Fatalf("readWatchdogConfig() targets = %+v, want %+v", got.Targets, want.Targets)
	}
	got.Targets = nil
	want.Targets = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readWatchdogConfig() = %+v, want %+v", got, want)
	}

	info, err := os.Stat(watchdogConfigFile())
	if err != nil {
		t.Fatalf("stat watchdog config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("watchdog config perm = %o, want 0600", perm)
	}
}

func TestWatchdogConfigMissingOrInvalidFallsBackToDefaults(t *testing.T) {
	setupWatchdogTest(t)
	want := defaultWatchdogConfig()

	if got := readWatchdogConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing config: got %+v, want %+v", got, want)
	}

	if err := os.WriteFile(watchdogConfigFile(), []byte("{invalid json"), 0600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	if got := readWatchdogConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid json: got %+v, want %+v", got, want)
	}

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 0, CooldownMinutes: -3, CheckIntervalMinutes: 999, ActionPolicy: "bogus"})
	got := readWatchdogConfig()
	wantInvalid := watchdogConfig{
		Enabled:              true,
		FailThreshold:        defaultWatchdogFailThreshold,
		CooldownMinutes:      defaultWatchdogCooldownMinutes,
		CheckIntervalMinutes: defaultWatchdogCheckIntervalMinutes,
		ActionPolicy:         watchdogPolicyReboot,
	}
	if !reflect.DeepEqual(got, wantInvalid) {
		t.Fatalf("invalid values: got %+v, want %+v", got, wantInvalid)
	}
}

func TestWatchdogDisabledSkipsCheck(t *testing.T) {
	checkCalls, actionCalls, _ := setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: false, FailThreshold: 1, CooldownMinutes: 1})

	watchdogTick(nil)
	if *checkCalls != 0 || *actionCalls != 0 {
		t.Fatalf("disabled watchdog ran check=%d action=%d, want 0/0", *checkCalls, *actionCalls)
	}
}

func TestWatchdogConsecutiveFailuresTriggerAction(t *testing.T) {
	checkCalls, actionCalls, online := setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 3, CooldownMinutes: 30})

	watchdogTick(nil)
	watchdogTick(nil)
	if *actionCalls != 0 {
		t.Fatalf("action fired after %d failures, want none before threshold", 2)
	}
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 2 {
		t.Fatalf("consecutiveFailures = %v, want 2", status["consecutiveFailures"])
	}

	watchdogTick(nil)
	if *actionCalls != 1 {
		t.Fatalf("action calls = %d, want 1 after reaching threshold", *actionCalls)
	}
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 0 {
		t.Fatalf("consecutiveFailures after action = %v, want reset to 0", status["consecutiveFailures"])
	}
	if status := currentWatchdogStatus(); status["lastActionTime"] == "" {
		t.Fatalf("lastActionTime empty after action, want recorded")
	}

	*online = true
	watchdogTick(nil)
	if *checkCalls != 4 || *actionCalls != 1 {
		t.Fatalf("online tick: check=%d action=%d, want 4/1", *checkCalls, *actionCalls)
	}
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 0 {
		t.Fatalf("consecutiveFailures after online = %v, want 0", status["consecutiveFailures"])
	}
}

func TestWatchdogOnlineResetsFailureCount(t *testing.T) {
	_, actionCalls, online := setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 3, CooldownMinutes: 30})

	watchdogTick(nil)
	watchdogTick(nil)
	*online = true
	watchdogTick(nil)
	*online = false
	watchdogTick(nil)
	watchdogTick(nil)
	if *actionCalls != 0 {
		t.Fatalf("action calls = %d, want 0 because online tick reset the count", *actionCalls)
	}
	if status := currentWatchdogStatus(); status["consecutiveFailures"] != 2 {
		t.Fatalf("consecutiveFailures = %v, want 2", status["consecutiveFailures"])
	}
}

func TestWatchdogCooldownBlocksRepeatAction(t *testing.T) {
	_, actionCalls, _ := setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 2, CooldownMinutes: 30})

	watchdogTick(nil)
	watchdogTick(nil)
	if *actionCalls != 1 {
		t.Fatalf("action calls = %d, want 1", *actionCalls)
	}

	watchdogTick(nil)
	watchdogTick(nil)
	watchdogTick(nil)
	if *actionCalls != 1 {
		t.Fatalf("action calls within cooldown = %d, want still 1", *actionCalls)
	}

	watchdogState.mu.Lock()
	watchdogState.lastActionTime = time.Now().Add(-31 * time.Minute)
	watchdogState.mu.Unlock()
	watchdogTick(nil)
	if *actionCalls != 2 {
		t.Fatalf("action calls after cooldown expired = %d, want 2", *actionCalls)
	}
}

func TestWatchdogStatusFields(t *testing.T) {
	setupWatchdogTest(t)
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 4, CooldownMinutes: 15})

	status := currentWatchdogStatus()
	if status["enabled"] != true || status["failThreshold"] != 4 || status["cooldownMinutes"] != 15 {
		t.Fatalf("status config fields = %v, want enabled/4/15", status)
	}
	if status["checkIntervalMinutes"] != defaultWatchdogCheckIntervalMinutes {
		t.Fatalf("checkIntervalMinutes = %v, want default %d", status["checkIntervalMinutes"], defaultWatchdogCheckIntervalMinutes)
	}
	if status["actionPolicy"] != watchdogPolicyReboot {
		t.Fatalf("actionPolicy = %v, want %s", status["actionPolicy"], watchdogPolicyReboot)
	}
	if _, ok := status["consecutiveFailures"]; !ok {
		t.Fatalf("status missing consecutiveFailures: %v", status)
	}
	lastAction, ok := status["lastActionTime"].(string)
	if !ok {
		t.Fatalf("lastActionTime type = %T, want string", status["lastActionTime"])
	}
	if lastAction != "" {
		t.Fatalf("lastActionTime = %q, want empty before any action", lastAction)
	}
	if status["lastActionType"] != "" {
		t.Fatalf("lastActionType = %v, want empty before any action", status["lastActionType"])
	}
	if status["pollerRunning"] != false {
		t.Fatalf("pollerRunning = %v, want false in test without poller", status["pollerRunning"])
	}
}

func TestNormalizeWatchdogTarget(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{"223.5.5.5", "223.5.5.5:53", true},
		{" 8.8.8.8:443 ", "8.8.8.8:443", true},
		{"dns.example.com", "dns.example.com:53", true},
		{"dns.example.com:8053", "dns.example.com:8053", true},
		{"2001:db8::1", "[2001:db8::1]:53", true},
		{"[2001:db8::1]:5353", "[2001:db8::1]:5353", true},
		{"", "", false},
		{"host:0", "", false},
		{"host:99999", "", false},
		{"host:notaport", "", false},
		{"ho st", "", false},
		{"host/path", "", false},
		{":53", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeWatchdogTarget(tc.raw)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("normalizeWatchdogTarget(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestParseWatchdogTargets(t *testing.T) {
	got := parseWatchdogTargets("223.5.5.5, 1.0.0.1:443\nbad host;8.8.8.8")
	want := []string{"223.5.5.5:53", "1.0.0.1:443", "8.8.8.8:53"}
	if len(got) != len(want) {
		t.Fatalf("parseWatchdogTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseWatchdogTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := parseWatchdogTargets("  ,;\n "); got != nil {
		t.Fatalf("parseWatchdogTargets(all invalid) = %v, want nil", got)
	}

	many := parseWatchdogTargets("1.1.1.1,2.2.2.2,3.3.3.3,4.4.4.4,5.5.5.5")
	if len(many) != watchdogMaxTargets {
		t.Fatalf("parseWatchdogTargets cap = %d targets, want %d", len(many), watchdogMaxTargets)
	}
}

func TestWatchdogProbeTargetsFallback(t *testing.T) {
	setupWatchdogTest(t)
	if got := watchdogProbeTargets(watchdogConfig{}); len(got) != len(watchdogDefaultTargets) {
		t.Fatalf("empty config targets = %v, want default %v", got, watchdogDefaultTargets)
	}
	custom := watchdogConfig{Targets: []string{"9.9.9.9:53"}}
	if got := watchdogProbeTargets(custom); len(got) != 1 || got[0] != "9.9.9.9:53" {
		t.Fatalf("custom targets = %v, want [9.9.9.9:53]", got)
	}
}

// TestWatchdogPollerSyncsToConfig 轮询器按配置启停:禁用不启动,启用启动,
// 重复同步幂等,再禁用立即停止。
func TestWatchdogPollerSyncsToConfig(t *testing.T) {
	setupWatchdogTest(t)
	oldWait := watchdogPollWait
	watchdogPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		return false
	}
	t.Cleanup(func() { watchdogPollWait = oldWait })

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: false, FailThreshold: 5, CooldownMinutes: 30})
	syncWatchdogPoller(nil)
	if watchdogPollerRunning() {
		t.Fatalf("poller running with disabled config, want stopped")
	}

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 5, CooldownMinutes: 30})
	syncWatchdogPoller(nil)
	if !watchdogPollerRunning() {
		t.Fatalf("poller not running after enabling, want running")
	}
	syncWatchdogPoller(nil)
	if !watchdogPollerRunning() {
		t.Fatalf("poller stopped by idempotent sync, want still running")
	}

	writeWatchdogTestConfig(t, watchdogConfig{Enabled: false, FailThreshold: 5, CooldownMinutes: 30})
	syncWatchdogPoller(nil)
	if watchdogPollerRunning() {
		t.Fatalf("poller still running after disabling, want stopped")
	}
	stopWatchdogPoller() // 幂等停止不 panic
}

// TestWatchdogEscalatePolicyAlternatesActions 升级策略:先无线电重注册,
// 重注册后仍失败升级为重启,重启后再失败回到重注册;联网恢复后重新从轻动作开始。
func TestWatchdogEscalatePolicyAlternatesActions(t *testing.T) {
	_, rebootCalls, online := setupWatchdogTest(t)
	radioCalls := 0
	oldRadio := watchdogRadioResetAction
	watchdogRadioResetAction = func(s *simpleAdminServer) string {
		radioCalls++
		return "OK"
	}
	t.Cleanup(func() { watchdogRadioResetAction = oldRadio })

	passCooldown := func() {
		watchdogState.mu.Lock()
		watchdogState.lastActionTime = time.Now().Add(-31 * time.Minute)
		watchdogState.mu.Unlock()
	}

	*online = false
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 1, CooldownMinutes: 30, ActionPolicy: watchdogPolicyEscalate})

	watchdogTick(nil) // 第 1 次:无线电重注册
	if radioCalls != 1 || *rebootCalls != 0 {
		t.Fatalf("first escalation action: radio=%d reboot=%d, want 1/0", radioCalls, *rebootCalls)
	}

	passCooldown()
	watchdogTick(nil) // 第 2 次:上次重注册未恢复,升级重启
	if radioCalls != 1 || *rebootCalls != 1 {
		t.Fatalf("second escalation action: radio=%d reboot=%d, want 1/1", radioCalls, *rebootCalls)
	}

	passCooldown()
	watchdogTick(nil) // 第 3 次:回到重注册
	if radioCalls != 2 || *rebootCalls != 1 {
		t.Fatalf("third escalation action: radio=%d reboot=%d, want 2/1", radioCalls, *rebootCalls)
	}

	*online = true
	watchdogTick(nil) // 联网恢复,重置动作类型
	*online = false
	passCooldown()
	watchdogTick(nil) // 再次失败应重新从重注册开始
	if radioCalls != 3 || *rebootCalls != 1 {
		t.Fatalf("after recovery: radio=%d reboot=%d, want 3/1", radioCalls, *rebootCalls)
	}
}

// TestWatchdogEscalatePolicyRejectsRadio 升级策略下无线电重注册被明确拒绝时,
// 同一次检查内立即升级为重启。
func TestWatchdogEscalatePolicyRejectsRadio(t *testing.T) {
	_, rebootCalls, online := setupWatchdogTest(t)
	radioCalls := 0
	oldRadio := watchdogRadioResetAction
	watchdogRadioResetAction = func(s *simpleAdminServer) string {
		radioCalls++
		return "ERROR"
	}
	t.Cleanup(func() { watchdogRadioResetAction = oldRadio })

	*online = false
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 1, CooldownMinutes: 30, ActionPolicy: watchdogPolicyEscalate})

	watchdogTick(nil)
	if radioCalls != 1 || *rebootCalls != 1 {
		t.Fatalf("rejected radio: radio=%d reboot=%d, want 1/1(立即升级重启)", radioCalls, *rebootCalls)
	}
}

// TestWatchdogRebootPolicyIgnoresEscalation reboot 策略(缺省)始终直接重启。
func TestWatchdogRebootPolicyIgnoresEscalation(t *testing.T) {
	_, rebootCalls, online := setupWatchdogTest(t)
	radioCalls := 0
	oldRadio := watchdogRadioResetAction
	watchdogRadioResetAction = func(s *simpleAdminServer) string {
		radioCalls++
		return "OK"
	}
	t.Cleanup(func() { watchdogRadioResetAction = oldRadio })

	*online = false
	writeWatchdogTestConfig(t, watchdogConfig{Enabled: true, FailThreshold: 1, CooldownMinutes: 30, ActionPolicy: watchdogPolicyReboot})

	watchdogTick(nil)
	watchdogState.mu.Lock()
	watchdogState.lastActionTime = time.Now().Add(-31 * time.Minute)
	watchdogState.mu.Unlock()
	watchdogTick(nil)
	if radioCalls != 0 || *rebootCalls != 2 {
		t.Fatalf("reboot policy: radio=%d reboot=%d, want 0/2", radioCalls, *rebootCalls)
	}
}
