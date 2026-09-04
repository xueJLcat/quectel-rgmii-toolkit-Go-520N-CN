package main

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubExecuteCachedATCommand 替换缓存条目的实际执行入口,测试结束自动恢复。
func stubExecuteCachedATCommand(t *testing.T, stub func(command string, mockMode bool) (string, error)) {
	t.Helper()
	old := executeCachedATCommand
	executeCachedATCommand = stub
	t.Cleanup(func() { executeCachedATCommand = old })
}

func TestIdleNonPageCacheEntriesEvicted(t *testing.T) {
	pageCommand := commonATCacheCommands()[0]
	if !isCommonATCacheCommand(pageCommand) {
		t.Fatalf("page command %q not recognized as common cache command", pageCommand)
	}
	if isCommonATCacheCommand("ATI") {
		t.Fatalf("ATI unexpectedly classified as page command")
	}

	idleRequested := time.Now().Add(-atCacheIdleEvictAfter - time.Minute)
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{
		"ATI":                   {command: "ATI", response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: idleRequested},
		pageCommand:             {command: pageCommand, response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: idleRequested},
		"AT+QRSRP":              {command: "AT+QRSRP", response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
		`AT+QENG="servingcell"`: {command: `AT+QENG="servingcell"`, running: true, lastRequested: idleRequested},
	}}

	mgr.evictIdleEntries()

	if _, ok := mgr.entries["ATI"]; ok {
		t.Fatalf("idle non-page command ATI was not evicted")
	}
	if _, ok := mgr.entries[pageCommand]; !ok {
		t.Fatalf("page command %q was evicted despite being in the common set", pageCommand)
	}
	if _, ok := mgr.entries["AT+QRSRP"]; !ok {
		t.Fatalf("recently requested non-page command was evicted")
	}
	if _, ok := mgr.entries[`AT+QENG="servingcell"`]; !ok {
		t.Fatalf("running entry was evicted")
	}
}

func TestRefreshSelectionRequiresRecentRequestForNonPageCommands(t *testing.T) {
	pageCommand := commonATCacheCommands()[0]
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{
		"ATI":       {command: "ATI", response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now().Add(-atCacheRecentRequestWindow - time.Minute)},
		"AT+QRSRP":  {command: "AT+QRSRP", response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
		pageCommand: {command: pageCommand, response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now().Add(-24 * time.Hour)},
	}}

	commands := mgr.cachedReadCommandsNeedingRefresh()
	set := make(map[string]bool, len(commands))
	for _, command := range commands {
		set[command] = true
	}
	if set["ATI"] {
		t.Fatalf("non-page command not requested within %s must not be refreshed: %#v", atCacheRecentRequestWindow, commands)
	}
	if !set["AT+QRSRP"] {
		t.Fatalf("recently requested non-page command missing from refresh list: %#v", commands)
	}
	if !set[pageCommand] {
		t.Fatalf("stale page command missing from refresh list despite old lastRequested: %#v", commands)
	}

	// 短信列表的既有抑制规则不受最近请求影响。
	suppressed := &atCommandCacheManager{entries: map[string]*atCacheEntry{
		smsListATCommand(): {command: smsListATCommand(), response: "OK", updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
	}}
	if got := suppressed.cachedReadCommandsNeedingRefresh(); len(got) != 0 {
		t.Fatalf("suppressed commands must stay suppressed even when recently requested: %#v", got)
	}
}

func TestATCacheRunFailureKeepsStaleTimestamp(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return "+CME ERROR: 10", errors.New("+CME ERROR: 10")
	})

	staleAt := time.Now().Add(-time.Hour)
	mgr := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{
			"AT+CSQ": {command: "AT+CSQ", response: "+CSQ: 20,99", updatedAt: staleAt, lastRequested: time.Now()},
		},
		queue: make(chan string, 4),
	}
	mgr.run("AT+CSQ")

	entry := mgr.entries["AT+CSQ"]
	if !entry.updatedAt.Equal(staleAt) {
		t.Fatalf("failed execution refreshed updatedAt: got %s, want %s", entry.updatedAt, staleAt)
	}
	if entry.errorText == "" {
		t.Fatalf("failed execution did not record error text")
	}
	if entry.running {
		t.Fatalf("entry still marked running after failed execution")
	}
	// 条目保持过期态(失败不算有效缓存),但记录失败时刻进入负缓存:
	// 短期内不再立即重试,避免慢命令连续超时引发重试风暴。
	_, _, _, stale, _ := mgr.snapshot("AT+CSQ")
	if !stale {
		t.Fatalf("failed entry must remain stale so it is not served as fresh cache")
	}
	if entry.failedAt.IsZero() {
		t.Fatalf("failed execution must record failedAt for the negative cache")
	}

	// 首次执行失败保持零时间戳(无任何缓存数据)。
	mgr2 := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{
			"AT+CIMI": {command: "AT+CIMI", lastRequested: time.Now()},
		},
		queue: make(chan string, 4),
	}
	mgr2.run("AT+CIMI")
	if entry2 := mgr2.entries["AT+CIMI"]; !entry2.updatedAt.IsZero() {
		t.Fatalf("first failed execution cached a timestamp: %s", entry2.updatedAt)
	}
}

func TestATCacheRunSuccessRefreshesTimestamp(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return "+CSQ: 31,99\r\nOK", nil
	})

	staleAt := time.Now().Add(-time.Hour)
	mgr := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{
			"AT+CSQ": {command: "AT+CSQ", response: "+CSQ: 20,99", updatedAt: staleAt, lastRequested: time.Now()},
		},
		queue: make(chan string, 4),
	}
	mgr.run("AT+CSQ")

	entry := mgr.entries["AT+CSQ"]
	if !entry.updatedAt.After(staleAt) {
		t.Fatalf("successful execution did not refresh updatedAt: %s", entry.updatedAt)
	}
}

func TestActionCommandNotBlockedByWaitingReadCommand(t *testing.T) {
	var mu sync.Mutex
	var order []string
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		// 共享 AT 缓存的后台周期刷新协程可能在本测试桩安装期间执行页面
		// 命令(见 api_websocket_fixes_test.go 的说明),只记录本测试自己
		// 入队的两条命令,避免后台噪音污染顺序断言。
		if command == "AT+CIMI" || command == "AT+CMGD=1" {
			mu.Lock()
			order = append(order, command)
			mu.Unlock()
		}
		return command + "\r\nOK", nil
	})

	mgr := &atCommandCacheManager{
		entries: make(map[string]*atCacheEntry),
		queue:   make(chan string, 8),
		readyAt: time.Now().Add(time.Minute),
	}
	go mgr.worker()

	// 读命令先入队,但保护期内只能等待。
	readDone := mgr.enqueue("AT+CIMI", false)
	// 动作命令总是就绪,不应被队头等待的读命令阻塞。
	actionDone := mgr.enqueue("AT+CMGD=1", true)

	select {
	case <-actionDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("action command was blocked by a waiting read command at the queue head")
	}

	mgr.mu.Lock()
	graceActive := time.Now().Before(mgr.readyAt)
	entry := mgr.entries["AT+CIMI"]
	readRunning := entry != nil && entry.running
	mgr.mu.Unlock()
	if !graceActive {
		t.Fatalf("grace window ended before the assertion; test scenario invalid")
	}
	if !readRunning {
		t.Fatalf("read command ran despite active grace period")
	}

	// 保护期结束后,暂存的读命令应当被执行。
	mgr.mu.Lock()
	mgr.readyAt = time.Time{}
	mgr.mu.Unlock()
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("deferred read command did not run after grace period ended")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "AT+CMGD=1" || order[1] != "AT+CIMI" {
		t.Fatalf("execution order = %#v, want the action command first", order)
	}
}

// TestRebootGracePeriodCaseInsensitive 覆盖审计修复:动作识别
// (isATActionCommand)走 ToUpper 匹配,CFUN 重启后的读保护期判定也必须大小写
// 一致,否则小写 at+cfun=1,1 触发重启后不会重新进入读保护期、读命令也不会
// 在模块恢复后重放。
func TestRebootGracePeriodCaseInsensitive(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return command + "\r\nOK", nil
	})

	for _, command := range []string{
		"AT+CFUN=1,1",
		"at+cfun=1,1",
		`at+egmr=1,7,"012345678901234";+cfun=1,1`,
	} {
		mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 4)}
		before := time.Now()
		mgr.run(command)
		mgr.mu.Lock()
		readyAt := mgr.readyAt
		mgr.mu.Unlock()
		want := before.Add(atCacheBootGracePeriod)
		if readyAt.Before(want.Add(-2*time.Second)) || readyAt.After(want.Add(2*time.Second)) {
			t.Fatalf("command %q 未重新应用重启保护期: readyAt=%s, want 约 %s", command, readyAt, want)
		}
	}
}

// TestInvalidateReadCacheClearsStaleResponseAndError 覆盖审计修复 B2:
// 动作(重启/改 IMEI)后失效读缓存时,必须同时清空残留的响应与错误文本,
// 否则保护期内 Fetch 在 has=false 时会优先返回上一次失败遗留的错误文本
// (如 "timeout waiting for OK/ERROR"),而不是设计中的"后台处理中"。
func TestInvalidateReadCacheClearsStaleResponseAndError(t *testing.T) {
	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{
		"AT+CSQ":                {command: "AT+CSQ", response: "timeout waiting for OK/ERROR", errorText: "timeout waiting for OK/ERROR", updatedAt: time.Now().Add(-time.Hour)},
		`AT+QENG="servingcell"`: {command: `AT+QENG="servingcell"`, response: "+QENG: data", updatedAt: time.Now(), running: true},
		"AT+CFUN=1,1":           {command: "AT+CFUN=1,1", response: "OK", updatedAt: time.Now()},
	}}
	mgr.invalidateReadCache()

	entry := mgr.entries["AT+CSQ"]
	if !entry.updatedAt.IsZero() || entry.response != "" || entry.errorText != "" {
		t.Fatalf("read entry not fully invalidated: updatedAt=%s response=%q errorText=%q",
			entry.updatedAt, entry.response, entry.errorText)
	}
	// 执行中的条目与动作命令条目不在失效范围内。
	if running := mgr.entries[`AT+QENG="servingcell"`]; running.response == "" || running.updatedAt.IsZero() {
		t.Fatalf("running entry must not be invalidated")
	}
	if action := mgr.entries["AT+CFUN=1,1"]; action.response == "" || action.updatedAt.IsZero() {
		t.Fatalf("action entry must not be invalidated")
	}
}

// TestGracePeriodFetchReturnsPendingNotStaleError 覆盖审计修复 B2 的完整
// 场景:预置含错误文本的条目 -> 重启动作触发失效并进入保护期 -> 保护期内
// Fetch 返回 pending 文本,不返回旧错误。
func TestGracePeriodFetchReturnsPendingNotStaleError(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return command + "\r\nOK", nil
	})

	const staleError = "timeout waiting for OK/ERROR"
	mgr := &atCommandCacheManager{
		entries: map[string]*atCacheEntry{
			"AT+CSQ": {command: "AT+CSQ", response: staleError, errorText: staleError, updatedAt: time.Now().Add(-time.Hour), lastRequested: time.Now()},
		},
		queue: make(chan string, 8),
	}

	// 重启动作执行成功:失效读缓存并重新应用 35s 保护期。
	mgr.run("AT+CFUN=1,1")
	if mgr.startupDelayRemaining() <= 0 {
		t.Fatalf("reboot action did not re-apply the boot grace period")
	}

	got := mgr.Fetch("AT+CSQ", false, nil)
	if got != atCachePendingText {
		t.Fatalf("grace-period Fetch = %q, want pending text %q", got, atCachePendingText)
	}
	if strings.Contains(got, "timeout") {
		t.Fatalf("grace-period Fetch leaked stale error text: %q", got)
	}
}

// TestATCacheNegativeCacheSuppressesImmediateRerun 覆盖失败负缓存:
// 慢命令连续超时时,失败条目在 atCacheNegativeTTL 内不再被每次请求重新
// 执行(否则重试风暴占死串行 AT 通道),强制刷新与过期后仍可重跑。
func TestATCacheNegativeCacheSuppressesImmediateRerun(t *testing.T) {
	// 只统计本测试命令的执行次数:共享缓存的后台刷新协程可能在本桩安装
	// 期间执行其它命令,全局计数会被污染。
	var executed int64
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		if command == "AT+CSQ" {
			atomic.AddInt64(&executed, 1)
		}
		return "", errors.New("all AT device candidates failed after retry: /dev/smd11: timeout waiting for OK/ERROR")
	})

	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 8)}
	go mgr.worker()

	mgr.run("AT+CSQ")
	if atomic.LoadInt64(&executed) != 1 {
		t.Fatalf("executed = %d, want 1 after first run", executed)
	}
	entry := mgr.entries["AT+CSQ"]
	if entry == nil || entry.failedAt.IsZero() || entry.errorText == "" {
		t.Fatalf("failed run must record failedAt and errorText")
	}

	// 负缓存有效期内:Fetch 直接返回缓存错误文本,不重新执行;
	// 周期刷新候选同样跳过该条目。
	got := mgr.Fetch("AT+CSQ", false, nil)
	if !strings.Contains(got, "timeout waiting for OK/ERROR") {
		t.Fatalf("Fetch within negative TTL = %q, want cached error text", got)
	}
	if atomic.LoadInt64(&executed) != 1 {
		t.Fatalf("Fetch within negative TTL re-executed the command: executed=%d", executed)
	}
	if refresh := mgr.cachedReadCommandsNeedingRefresh(); len(refresh) != 0 {
		t.Fatalf("negative-cached command must not be picked for periodic refresh: %#v", refresh)
	}

	// 强制刷新绕过负缓存。
	_ = mgr.Fetch("AT+CSQ", true, nil)
	if atomic.LoadInt64(&executed) != 2 {
		t.Fatalf("forced Fetch must bypass negative cache: executed=%d", executed)
	}

	// 负缓存过期后按原语义立即重试。
	mgr.mu.Lock()
	mgr.entries["AT+CSQ"].failedAt = time.Now().Add(-atCacheNegativeTTL - time.Second)
	mgr.mu.Unlock()
	_ = mgr.Fetch("AT+CSQ", false, nil)
	if atomic.LoadInt64(&executed) != 3 {
		t.Fatalf("expired negative cache must allow rerun: executed=%d", executed)
	}

	// 执行成功后负缓存清零,不再压制后续刷新。
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		if command == "AT+CSQ" {
			atomic.AddInt64(&executed, 1)
		}
		return "+CSQ: 31,99\r\nOK", nil
	})
	mgr.mu.Lock()
	mgr.entries["AT+CSQ"].failedAt = time.Now().Add(-atCacheNegativeTTL - time.Second)
	mgr.entries["AT+CSQ"].updatedAt = time.Time{}
	mgr.mu.Unlock()
	_ = mgr.Fetch("AT+CSQ", false, nil)
	if atomic.LoadInt64(&executed) != 4 {
		t.Fatalf("expected rerun after negative cache expiry: executed=%d", executed)
	}
	if mgr.entries["AT+CSQ"].failedAt != (time.Time{}) {
		t.Fatalf("successful execution must clear failedAt")
	}
}

// TestATCacheEnqueueOverflowCapped 覆盖队列满时的溢出限流:溢出执行达到
// 上限后,后续入队放弃执行并立即唤醒等待者,而不是无限 spawn 协程
// 堆在全局 AT 文件锁上。
func TestATCacheEnqueueOverflowCapped(t *testing.T) {
	// 按命令分别计数:共享缓存的后台刷新协程可能在本桩安装期间执行其它
	// 命令,只有本测试的两条命令参与断言。
	release := make(chan struct{})
	var firstRuns, secondRuns int64
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		switch command {
		case "AT+CSQ":
			atomic.AddInt64(&firstRuns, 1)
			<-release
		case `AT+QENG="servingcell"`:
			atomic.AddInt64(&secondRuns, 1)
		}
		return command + "\r\nOK", nil
	})

	prevMax := atCacheOverflowMax
	atCacheOverflowMax = 1
	t.Cleanup(func() { atCacheOverflowMax = prevMax })

	mgr := &atCommandCacheManager{entries: map[string]*atCacheEntry{}, queue: make(chan string, 1)}
	mgr.queue <- "AT+BLOCKED" // 占满队列且无 worker 消费,后续入队全部走溢出路径

	// 第一个溢出命令占用唯一名额(阻塞在执行中)。
	mgr.enqueue("AT+CSQ", false)
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt64(&firstRuns) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&firstRuns) != 1 {
		t.Fatalf("first overflow command did not start executing: firstRuns=%d", firstRuns)
	}

	// 第二个溢出命令被限流:等待者立即关闭,条目回到未执行态。
	done := mgr.enqueue(`AT+QENG="servingcell"`, false)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("capped overflow waiter was not closed immediately")
	}
	mgr.mu.Lock()
	entry := mgr.entries[`AT+QENG="servingcell"`]
	cappedRunning := entry != nil && entry.running
	mgr.mu.Unlock()
	if entry == nil {
		t.Fatalf("capped overflow command lost its cache entry")
	}
	if cappedRunning {
		t.Fatalf("capped overflow entry still marked running")
	}
	if atomic.LoadInt64(&secondRuns) != 0 {
		t.Fatalf("capped overflow command must not execute: secondRuns=%d", secondRuns)
	}

	close(release)
	deadline = time.Now().Add(3 * time.Second)
	for atomic.LoadInt64(&atCacheOverflowRunning) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&atCacheOverflowRunning); got != 0 {
		t.Fatalf("overflow counter leaked after completion: %d", got)
	}
	if atomic.LoadInt64(&firstRuns) != 1 {
		t.Fatalf("first overflow command executed more than once: firstRuns=%d", firstRuns)
	}
}
