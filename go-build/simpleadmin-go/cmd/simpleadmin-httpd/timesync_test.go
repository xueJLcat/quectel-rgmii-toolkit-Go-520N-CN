package main

import (
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempTimeSyncEnv 将配置文件指向临时目录,并清空时间同步运行状态。
func withTempTimeSyncEnv(t *testing.T) {
	t.Helper()
	oldTTL := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(t.TempDir(), "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTL })
	resetTimeSyncForTest()
}

// withFakeTimeSync 替换 NTP 查询与系统时钟写入,避免真实网络访问与改钟。
// 返回查询计数与最近一次写入的时间。
func withFakeTimeSync(t *testing.T, offset time.Duration, queryErr error, applyErr error) (*int, *time.Time) {
	t.Helper()
	queries := 0
	applied := time.Time{}
	oldQuery, oldApply := timeSyncQuery, timeSyncApply
	timeSyncQuery = func(server string, timeout time.Duration) (ntpResult, error) {
		queries++
		if queryErr != nil {
			return ntpResult{}, queryErr
		}
		return ntpResult{Offset: offset, RoundTrip: 20 * time.Millisecond, ServerTime: time.Now().Add(offset)}, nil
	}
	timeSyncApply = func(now time.Time) error {
		if applyErr != nil {
			return applyErr
		}
		applied = now
		return nil
	}
	t.Cleanup(func() {
		timeSyncQuery = oldQuery
		timeSyncApply = oldApply
	})
	return &queries, &applied
}

func TestTimeSyncConfigRoundTrip(t *testing.T) {
	withTempTimeSyncEnv(t)

	want := timeSyncConfig{Enabled: true, IntervalMinutes: 120, Server: "ntp.aliyun.com"}
	if err := writeTimeSyncConfig(want); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	if got := readTimeSyncConfig(); got != want {
		t.Fatalf("readTimeSyncConfig = %+v, want %+v", got, want)
	}
	info, err := os.Stat(timeSyncConfigFile())
	if err != nil {
		t.Fatalf("stat timesync.conf: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("timesync.conf perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestTimeSyncConfigDefaults(t *testing.T) {
	withTempTimeSyncEnv(t)

	got := readTimeSyncConfig()
	if got.Enabled {
		t.Fatalf("enabled = true, want disabled by default")
	}
	if got.IntervalMinutes != defaultTimeSyncIntervalMinutes {
		t.Fatalf("intervalMinutes = %d, want %d", got.IntervalMinutes, defaultTimeSyncIntervalMinutes)
	}
	if got.Server != defaultTimeSyncServer {
		t.Fatalf("server = %q, want %s", got.Server, defaultTimeSyncServer)
	}
}

func TestTimeSyncConfigInvalidFallsBack(t *testing.T) {
	withTempTimeSyncEnv(t)

	if err := os.WriteFile(timeSyncConfigFile(), []byte(`not json`), 0600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	if got := readTimeSyncConfig(); got != defaultTimeSyncConfig() {
		t.Fatalf("broken config = %+v, want full defaults", got)
	}

	cases := []struct {
		raw          string
		wantInterval int
		wantServer   string
	}{
		{`{"enabled":true,"intervalMinutes":0,"server":"ntp.aliyun.com"}`, defaultTimeSyncIntervalMinutes, defaultTimeSyncServer},
		{`{"enabled":true,"intervalMinutes":9999,"server":"ntp.aliyun.com"}`, defaultTimeSyncIntervalMinutes, defaultTimeSyncServer},
		{`{"enabled":true,"intervalMinutes":30,"server":"bad host"}`, 30, defaultTimeSyncServer},
		{`{"enabled":true,"intervalMinutes":30,"server":"pool.ntp.org:123"}`, 30, defaultTimeSyncServer},
		{`{"enabled":true,"intervalMinutes":30,"server":""}`, 30, defaultTimeSyncServer},
		{`{"enabled":true,"intervalMinutes":1,"server":"my.ntp.local"}`, 1, "my.ntp.local"},
	}
	for _, tc := range cases {
		if err := os.WriteFile(timeSyncConfigFile(), []byte(tc.raw), 0600); err != nil {
			t.Fatalf("write config %q: %v", tc.raw, err)
		}
		got := readTimeSyncConfig()
		if got.IntervalMinutes != tc.wantInterval || got.Server != tc.wantServer {
			t.Errorf("config %q = (%d, %q), want (%d, %q)",
				tc.raw, got.IntervalMinutes, got.Server, tc.wantInterval, tc.wantServer)
		}
	}
}

func TestNormalizeTimeSyncServer(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"ntp.aliyun.com", "ntp.aliyun.com"},
		{"  pool.ntp.org  ", "pool.ntp.org"},
		{"", defaultTimeSyncServer},
		{"ntp://ntp.aliyun.com", "ntp.aliyun.com"},
		{"host:123", defaultTimeSyncServer},
		{"bad host", defaultTimeSyncServer},
		{"host/path", defaultTimeSyncServer},
		{"host?q=1", defaultTimeSyncServer},
		{strings.Repeat("a", 300), defaultTimeSyncServer},
	}
	for _, tc := range cases {
		if got := normalizeTimeSyncServer(tc.raw); got != tc.want {
			t.Errorf("normalizeTimeSyncServer(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// buildNTPResponse 构造测试用 48 字节 NTP 应答报文。
func buildNTPResponse(t *testing.T, mode, stratum byte, receive, transmit time.Time) []byte {
	t.Helper()
	packet := make([]byte, 48)
	packet[0] = mode
	packet[1] = stratum
	writeNTPTime := func(offset int, value time.Time) {
		if value.IsZero() {
			return
		}
		unix := value.Unix() + ntpEpochOffset
		fraction := uint32((uint64(value.Nanosecond()) << 32) / uint64(time.Second))
		binary.BigEndian.PutUint32(packet[offset:], uint32(unix))
		binary.BigEndian.PutUint32(packet[offset+4:], fraction)
	}
	writeNTPTime(32, receive)
	writeNTPTime(40, transmit)
	return packet
}

func TestParseNTPResponseOffset(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0)
	// 服务器时钟比本地快 500ms;服务器处理耗时 10ms。
	t2 := t1.Add(500 * time.Millisecond)
	t3 := t2.Add(10 * time.Millisecond)
	t4 := t1.Add(60 * time.Millisecond)

	packet := buildNTPResponse(t, 0x24, 2, t2, t3) // LI=0, VN=4, Mode=4(server)
	result, err := parseNTPResponse(packet, t1, t4)
	if err != nil {
		t.Fatalf("parseNTPResponse: %v", err)
	}
	// θ = ((500) + (510-60)) / 2 = 475ms(NTP 32 位小数存在纳秒级量化误差)。
	if diff := result.Offset - 475*time.Millisecond; diff < -time.Millisecond || diff > time.Millisecond {
		t.Fatalf("offset = %s, want ≈ 475ms", result.Offset)
	}
	// δ = 60 - 10 = 50ms
	if diff := result.RoundTrip - 50*time.Millisecond; diff < -time.Millisecond || diff > time.Millisecond {
		t.Fatalf("roundTrip = %s, want ≈ 50ms", result.RoundTrip)
	}
	if diff := result.ServerTime.Sub(t3); diff < -time.Millisecond || diff > time.Millisecond {
		t.Fatalf("serverTime = %v, want ≈ %v", result.ServerTime, t3)
	}
}

func TestParseNTPResponseRejectsBadPackets(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0)
	t4 := t1.Add(60 * time.Millisecond)
	valid := buildNTPResponse(t, 0x24, 2, t1.Add(time.Second), t1.Add(time.Second))

	cases := []struct {
		name   string
		packet []byte
	}{
		{"short", valid[:40]},
		{"client mode", buildNTPResponse(t, 0x23, 2, t1.Add(time.Second), t1.Add(time.Second))},
		{"kiss of death stratum", buildNTPResponse(t, 0x24, 0, t1.Add(time.Second), t1.Add(time.Second))},
		{"stratum too high", buildNTPResponse(t, 0x24, 16, t1.Add(time.Second), t1.Add(time.Second))},
		{"zero transmit", buildNTPResponse(t, 0x24, 2, t1.Add(time.Second), time.Time{})},
	}
	for _, tc := range cases {
		if _, err := parseNTPResponse(tc.packet, t1, t4); err == nil {
			t.Errorf("%s: parseNTPResponse accepted invalid packet", tc.name)
		}
	}
}

func TestRunTimeSyncOnceAppliesOffset(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, applied := withFakeTimeSync(t, 800*time.Millisecond, nil, nil)

	before := time.Now()
	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("runTimeSyncOnce = %v, want ok", result)
	}
	if *queries != 1 {
		t.Fatalf("query count = %d, want 1", *queries)
	}
	want := before.Add(800 * time.Millisecond)
	if applied.Sub(want).Abs() > 5*time.Second {
		t.Fatalf("applied = %v, want ≈ %v", *applied, want)
	}

	status := currentTimeSyncStatus()
	if status["lastSyncOK"] != true || status["lastSyncTime"] == "" || status["lastError"] != "" {
		t.Fatalf("status after sync = %v, want recorded success", status)
	}
	if status["lastOffsetMs"] != int64(800) {
		t.Fatalf("lastOffsetMs = %v, want 800", status["lastOffsetMs"])
	}
	if status["lastServer"] != "ntp.aliyun.com" {
		t.Fatalf("lastServer = %v, want ntp.aliyun.com", status["lastServer"])
	}
}

func TestRunTimeSyncOnceQueryFailure(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, applied := withFakeTimeSync(t, 0, errors.New("no route to host"), nil)

	result := runTimeSyncOnce(nil, "")
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("runTimeSyncOnce = %v, want failure", result)
	}
	if *queries != 1 || !applied.IsZero() {
		t.Fatalf("queries = %d, applied = %v; want 1 query and no clock write", *queries, *applied)
	}
	status := currentTimeSyncStatus()
	if status["lastSyncOK"] != false || status["lastError"] == "" {
		t.Fatalf("status after failure = %v, want recorded error", status)
	}
	// 空服务器回退缺省源。
	if status["lastServer"] != defaultTimeSyncServer {
		t.Fatalf("lastServer = %v, want default %s", status["lastServer"], defaultTimeSyncServer)
	}
}

func TestRunTimeSyncOnceRejectsHugeOffset(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, applied := withFakeTimeSync(t, 400*24*time.Hour, nil, nil)

	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("runTimeSyncOnce = %v, want rejection", result)
	}
	if result["error"] != "offset out of range" {
		t.Fatalf("error = %v, want offset out of range", result["error"])
	}
	if *queries != 1 || !applied.IsZero() {
		t.Fatalf("queries = %d, applied = %v; want no clock write", *queries, *applied)
	}
}

func TestRunTimeSyncOnceApplyFailure(t *testing.T) {
	withTempTimeSyncEnv(t)
	withFakeTimeSync(t, time.Second, nil, errors.New("operation not permitted"))

	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("runTimeSyncOnce = %v, want failure", result)
	}
	if errText, _ := result["error"].(string); !strings.Contains(errText, "set system time failed") {
		t.Fatalf("error = %v, want set system time failed", result["error"])
	}
	status := currentTimeSyncStatus()
	if status["lastSyncOK"] != false {
		t.Fatalf("status = %v, want failure recorded", status)
	}
}

func TestRunTimeSyncOnceMockMode(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, _ := withFakeTimeSync(t, 0, errors.New("must not be called"), nil)

	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	result := runTimeSyncOnce(srv, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("mock runTimeSyncOnce = %v, want ok", result)
	}
	if *queries != 0 {
		t.Fatalf("mock mode issued %d NTP queries, want 0", *queries)
	}
}

// TestTimeSyncPollerSyncsToConfig 轮询器按配置启停:禁用不启动,启用启动,
// 重复同步幂等,再禁用立即停止。
func TestTimeSyncPollerSyncsToConfig(t *testing.T) {
	withTempTimeSyncEnv(t)
	oldWait := timeSyncPollWait
	timeSyncPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		<-stop
		return false
	}
	t.Cleanup(func() { timeSyncPollWait = oldWait })

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: false}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	syncTimeSyncPoller(nil)
	if timeSyncPollerRunning() {
		t.Fatalf("poller running with disabled config, want stopped")
	}

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: true}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	syncTimeSyncPoller(nil)
	if !timeSyncPollerRunning() {
		t.Fatalf("poller not running after enabling, want running")
	}
	syncTimeSyncPoller(nil)
	if !timeSyncPollerRunning() {
		t.Fatalf("poller stopped by idempotent sync, want still running")
	}

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: false}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	syncTimeSyncPoller(nil)
	if timeSyncPollerRunning() {
		t.Fatalf("poller still running after disabling, want stopped")
	}
	stopTimeSyncPoller() // 幂等停止不 panic
}

// TestTimeSyncTickFollowsConfig tick 仅在启用时执行同步,禁用竞态下不触发;
// 返回值为本次同步是否成功,供轮询器决定下次等待时长。
func TestTimeSyncTickFollowsConfig(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, _ := withFakeTimeSync(t, time.Second, nil, nil)

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: false}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	if ok := timeSyncTick(nil); ok {
		t.Fatalf("disabled tick reported success")
	}
	if *queries != 0 {
		t.Fatalf("disabled tick issued %d queries, want 0", *queries)
	}

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: true, Server: "pool.ntp.org"}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	if ok := timeSyncTick(nil); !ok {
		t.Fatalf("enabled tick reported failure, want success")
	}
	if *queries != 1 {
		t.Fatalf("enabled tick issued %d queries, want 1", *queries)
	}
	if got := currentTimeSyncStatus()["lastServer"]; got != "pool.ntp.org" {
		t.Fatalf("lastServer = %v, want pool.ntp.org", got)
	}
}

// TestTimeSyncNextDelay 成功后按配置间隔;失败后快速重试(不超过配置间隔)。
func TestTimeSyncNextDelay(t *testing.T) {
	withTempTimeSyncEnv(t)

	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: true, IntervalMinutes: 60}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	if got := timeSyncNextDelay(true); got != 60*time.Minute {
		t.Fatalf("next delay after success = %s, want 60m", got)
	}
	if got := timeSyncNextDelay(false); got != timeSyncFailureRetry {
		t.Fatalf("next delay after failure = %s, want %s", got, timeSyncFailureRetry)
	}

	// 配置间隔小于失败重试间隔时,按配置间隔。
	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: true, IntervalMinutes: 1}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}
	if got := timeSyncNextDelay(false); got != time.Minute {
		t.Fatalf("next delay after failure = %s, want 1m (configured)", got)
	}
}

// TestTimeSyncPollerFirstDelayAndRetry 轮询循环时序:启用后先等 30 秒再首次
// 同步;失败按重试间隔快速重试,成功按配置间隔。
func TestTimeSyncPollerFirstDelayAndRetry(t *testing.T) {
	withTempTimeSyncEnv(t)
	queries, _ := withFakeTimeSync(t, 0, errors.New("network unreachable"), nil)
	if err := writeTimeSyncConfig(timeSyncConfig{Enabled: true, IntervalMinutes: 60}); err != nil {
		t.Fatalf("writeTimeSyncConfig: %v", err)
	}

	delays := make(chan time.Duration, 8)
	count := 0
	oldWait := timeSyncPollWait
	timeSyncPollWait = func(d time.Duration, stop <-chan struct{}) bool {
		count++
		delays <- d
		if count < 3 {
			return true
		}
		// 记录到第三次等待后挂起,等测试停止轮询器。
		<-stop
		return false
	}
	t.Cleanup(func() { timeSyncPollWait = oldWait })

	startTimeSyncPoller(nil)
	var got []time.Duration
	for i := 0; i < 3; i++ {
		select {
		case d := <-delays:
			got = append(got, d)
		case <-time.After(5 * time.Second):
			t.Fatalf("poller stalled, delays so far: %v", got)
		}
	}
	stopTimeSyncPoller()

	want := []time.Duration{timeSyncFirstDelay, timeSyncFailureRetry, timeSyncFailureRetry}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delay[%d] = %s, want %s (%v)", i, got[i], want[i], got)
		}
	}
	if *queries != 2 {
		t.Fatalf("queries = %d, want 2 (first sync + one retry)", *queries)
	}
}

func TestHandleTimeSyncAPIs(t *testing.T) {
	withTempTimeSyncEnv(t)
	withFakeTimeSync(t, 250*time.Millisecond, nil, nil)
	srv := &simpleAdminServer{cfg: serverConfig{}}

	// get:缺省配置。
	rr := httptest.NewRecorder()
	srv.handleGetTimeSync(rr, httptest.NewRequest(http.MethodPost, "/api/get_timesync", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get_timesync status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"server":"ntp.aliyun.com"`) {
		t.Fatalf("get_timesync body = %s, want default server", rr.Body.String())
	}

	// set:启用 + 自定义间隔与服务器。
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/set_timesync",
		strings.NewReader("enabled=1&intervalMinutes=15&server=pool.ntp.org"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleSetTimeSync(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("set_timesync status = %d", rr.Code)
	}
	cfg := readTimeSyncConfig()
	if !cfg.Enabled || cfg.IntervalMinutes != 15 || cfg.Server != "pool.ntp.org" {
		t.Fatalf("config after set = %+v", cfg)
	}
	if !timeSyncPollerRunning() {
		t.Fatalf("poller not started by set_timesync")
	}

	// set:非法间隔被忽略,保留原值。
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/set_timesync", strings.NewReader("intervalMinutes=99999"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleSetTimeSync(rr, req)
	if got := readTimeSyncConfig().IntervalMinutes; got != 15 {
		t.Fatalf("interval after invalid set = %d, want 15 kept", got)
	}

	// set:提交空服务器重置回缺省源。
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/set_timesync", strings.NewReader("server="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleSetTimeSync(rr, req)
	if got := readTimeSyncConfig().Server; got != defaultTimeSyncServer {
		t.Fatalf("server after empty set = %q, want default %s", got, defaultTimeSyncServer)
	}

	// now:手动同步一次。
	rr = httptest.NewRecorder()
	srv.handleTimeSyncNow(rr, httptest.NewRequest(http.MethodPost, "/api/timesync_now", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("timesync_now status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("timesync_now body = %s, want ok", rr.Body.String())
	}

	// set:禁用后轮询器停止。
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/set_timesync", strings.NewReader("enabled=0"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleSetTimeSync(rr, req)
	if timeSyncPollerRunning() {
		t.Fatalf("poller still running after disable")
	}
}
