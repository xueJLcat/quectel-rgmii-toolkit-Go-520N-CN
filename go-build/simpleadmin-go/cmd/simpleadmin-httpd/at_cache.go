package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	atCachePendingText       = "AT command is running in background; cached data is not ready yet."
	atCacheReadMaxAge        = 3 * time.Second
	atCacheSemiStaticMaxAge  = 30 * time.Second
	atCacheConfigMaxAge      = 60 * time.Second
	atCacheStaticMaxAge      = 10 * time.Minute
	atCachePeriodicInterval  = 15 * time.Second
	atCacheFirstRefreshDelay = 500 * time.Millisecond
	atCacheBootGracePeriod   = 35 * time.Second
	// 周期刷新最近请求窗口:页面公共命令与非页面命令一律只在窗口内被
	// Fetch 请求过才参与周期刷新——页面打开时前端轮询(最慢 60s 一条)
	// 持续续热,关闭页面/收起标签后后台刷新最多再持续一个窗口即停摆,
	// 空闲态 AT 通道与 CPU 只保留短信转发与自动化等允许的常驻服务。
	// 窗口须大于最慢前端轮询周期;条目淘汰仍由 atCacheIdleEvictAfter 独立控制。
	atCacheRecentRequestWindow = 2 * time.Minute
	atCacheIdleEvictAfter      = 15 * time.Minute
	// worker 无可执行命令时的轮询间隔,等待保护期结束,避免忙等。
	atCacheWorkerIdleSleep = 500 * time.Millisecond
	// atCacheNegativeTTL 是失败负缓存有效期:命令执行失败后短期内不再重跑,
	// Fetch/周期刷新直接返回缓存的错误文本。失败不入缓存会令每次请求都
	// 重新入队执行,慢命令连续超时时形成重试风暴占死串行通道。
	atCacheNegativeTTL = 3 * time.Second
)

type atCacheEntry struct {
	command   string
	response  string
	errorText string
	updatedAt time.Time
	// failedAt 记录最近一次执行失败的时刻,用于失败负缓存判定:
	// 距失败未超过 atCacheNegativeTTL 时不再重跑,直接返回缓存的错误文本。
	// 执行成功即清零。
	failedAt time.Time
	// lastRequested 记录最近一次经请求路径(Fetch)访问的时间,用于判断
	// 非页面命令是否仍被关注:超出最近请求窗口不再周期刷新,超出空闲阈值
	// 后整条淘汰。
	lastRequested time.Time
	running       bool
	// pendingForce 记录条目执行期间到达的并发动作命令请求数。动作命令的
	// 契约是"不缓存,每次执行"(见 at_cache_policy.go):并发同命令请求若
	// 静默合并进在途执行,第二次动作根本不下发却报成功(如双击删除短信,
	// 模块只删一次,两个请求都拿到第一次的 OK)。当前执行完成后按计数补跑,
	// 补跑完成前 waiters 不唤醒。读命令不计数:并发读合并进在途执行拿到的
	// 就是刚产出的新鲜数据,去重是收益不是缺陷。
	pendingForce int
	waiters      []chan struct{}
}

type atCommandCacheManager struct {
	mu       sync.Mutex
	entries  map[string]*atCacheEntry
	queue    chan string
	started  bool
	mockMode bool
	readyAt  time.Time
	// invalidateGen 是读缓存失效代数:每次 invalidateReadCache 递增。
	// 读命令在执行前记录代数,提交结果时代数已变说明执行期间有动作命令
	// 完成并失效了读缓存——本次读到的可能是动作前的旧数据(设备读取先于
	// 动作完成、缓存提交又被调度延迟到失效之后,溢出协程与 worker 并发时
	// 窗口真实存在),不得按新鲜数据写回,丢弃本次结果等待重跑。
	invalidateGen uint64
}

var atCommandCache = &atCommandCacheManager{
	entries: make(map[string]*atCacheEntry),
	queue:   make(chan string, 64),
}

// atCacheOverflowRunning 统计队列满时绕过 worker 直接执行的在途协程数。
var atCacheOverflowRunning int64

// atCacheOverflowMax 是溢出执行并发上限。声明为变量以便测试注入。
var atCacheOverflowMax int64 = 8

func (m *atCommandCacheManager) Start(mockMode bool) {
	m.mu.Lock()
	m.mockMode = mockMode
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.readyAt = time.Now().Add(atCacheStartupDelay(mockMode))
	readyAt := m.readyAt
	m.mu.Unlock()

	if delay := time.Until(readyAt); delay > 0 {
		log.Printf("AT 缓存启动保护: 系统刚开机，%.0f 秒后再执行读类 AT 指令", delay.Seconds())
	}

	go m.worker()
	go m.periodicRefresh()
	go m.initialRefresh()
}

// worker 从队列取命令执行。保护期内未就绪的读命令不阻塞队头:取出后若
// 尚未就绪,先暂存到 deferred,优先检查并执行队列中已就绪的命令(动作
// 命令总是就绪);没有可执行命令时短睡眠轮询,避免忙等。保护期内读命令
// 对外仍立即返回后台处理中(由 Fetch 的 startupDelayedRead 分支保证)。
// 正常运行期动作命令同样优先:每轮把队列中已到达的命令全部收进 deferred,
// 先挑动作命令执行,再按 FIFO 挑读命令——动作命令(锁频/重启/改配置)的
// Fetch 等待预算有限,排在长读命令(短信列表最坏 42s)之后会让调用方超时
// 误报 pending(锁频假失败/重启假成功),优先取出把排队等待压缩到"至多
// 一条在执行中的命令"。
func (m *atCommandCacheManager) worker() {
	var deferred []string
	for {
		if len(deferred) == 0 {
			deferred = append(deferred, <-m.queue)
		} else {
			select {
			case command := <-m.queue:
				deferred = append(deferred, command)
			case <-time.After(atCacheWorkerIdleSleep):
			}
		}
	drain:
		for {
			select {
			case command := <-m.queue:
				deferred = append(deferred, command)
			default:
				break drain
			}
		}
		index := -1
		for i, command := range deferred {
			if isATActionCommand(command) && m.readyToRunNow(command) {
				index = i
				break
			}
		}
		if index < 0 {
			for i, command := range deferred {
				if m.readyToRunNow(command) {
					index = i
					break
				}
			}
		}
		if index < 0 {
			continue
		}
		command := deferred[index]
		deferred = append(deferred[:index], deferred[index+1:]...)
		m.run(command)
	}
}

// readyToRunNow 判断命令当前是否可以立即执行:动作命令不受开机保护期限制,
// 其余读命令需等待保护期结束。
func (m *atCommandCacheManager) readyToRunNow(command string) bool {
	if isATActionCommand(command) || m.currentMockMode() {
		return true
	}
	return m.startupDelayRemaining() <= 0
}

func (m *atCommandCacheManager) periodicRefresh() {
	ticker := time.NewTicker(atCachePeriodicInterval)
	defer ticker.Stop()
	for range ticker.C {
		if m.startupDelayRemaining() > 0 {
			continue
		}
		m.evictIdleEntries()
		for _, command := range m.cachedReadCommandsNeedingRefresh() {
			m.enqueue(command, false)
		}
	}
}

// evictIdleEntries 淘汰不再被关注的条目:不在页面公共命令集合内、未在
// 执行中且超过空闲阈值没有请求的条目从缓存删除,防止一次性手动命令的
// 条目无限滞留。周期循环顺带执行,无额外定时器。
func (m *atCommandCacheManager) evictIdleEntries() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for command, entry := range m.entries {
		if entry.running || isCommonATCacheCommand(command) {
			continue
		}
		if time.Since(entry.lastRequested) >= atCacheIdleEvictAfter {
			delete(m.entries, command)
		}
	}
}

func (m *atCommandCacheManager) initialRefresh() {
	time.Sleep(atCacheFirstRefreshDelay)
	m.waitUntilReadyFor("")
	m.enqueueInitialReadCommands()
}

func (m *atCommandCacheManager) Fetch(command string, force bool, waitOverride *bool) string {
	command = sanitizeATCommand(command)
	if command == "" {
		return ""
	}
	m.touchRequested(command)

	has, response, errorText, stale, running := m.snapshot(command)
	startupDelayedRead := m.startupDelayRemaining() > 0 && !isATActionCommand(command)
	// 失败负缓存有效期内不再重跑(强制刷新与动作命令除外),
	// 直接返回缓存的错误文本,防止慢命令连续超时引发重试风暴。
	negativeCached := !force && !isATActionCommand(command) && m.negativeCacheActive(command)
	mustRun := force || isATActionCommand(command) || ((!has || stale) && !negativeCached)
	var done <-chan struct{}
	if mustRun {
		done = m.enqueue(command, force || isATActionCommand(command))
		running = true
	}

	if startupDelayedRead {
		return m.responseOrPending(has, response, errorText, running)
	}

	wait := !has || force || isATActionCommand(command)
	if waitOverride != nil {
		wait = *waitOverride
	}
	if wait && done != nil {
		select {
		case <-done:
		case <-time.After(atCacheWaitTimeout(command)):
		}
		has, response, errorText, _, running = m.snapshot(command)
		// 溢出限流放弃路径:done 被关闭但条目从未执行(无数据、无错误文本、
		// running 已复位)。按 enqueue 处的注释承诺返回 pending 文本——空串
		// 会被各页面解析器(atReadFailed)当成"AT 读取失败"弹错误横幅,而
		// 实际只是本机过载限流,模块完好,语义上是可重试的未就绪态。
		if !has && strings.TrimSpace(errorText) == "" && !running {
			return atCachePendingText
		}
	}

	return m.responseOrPending(has, response, errorText, running)
}

func (m *atCommandCacheManager) responseOrPending(has bool, response, errorText string, running bool) string {
	if has {
		if strings.TrimSpace(response) != "" {
			return response
		}
		if strings.TrimSpace(errorText) != "" {
			return errorText
		}
		return ""
	}
	if strings.TrimSpace(errorText) != "" {
		return errorText
	}
	if running {
		return atCachePendingText
	}
	return ""
}

func (m *atCommandCacheManager) snapshot(command string) (bool, string, string, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[command]
	if e == nil {
		return false, "", "", true, false
	}
	has := !e.updatedAt.IsZero()
	stale := !has || time.Since(e.updatedAt) >= maxAgeForATCacheCommand(command)
	return has, e.response, e.errorText, stale, e.running
}

// negativeCacheActiveLocked 判断条目是否处于失败负缓存有效期内(最近一次
// 执行失败且距今未超过 atCacheNegativeTTL)。调用方必须持有 m.mu。
func (m *atCommandCacheManager) negativeCacheActiveLocked(e *atCacheEntry) bool {
	return e != nil && !e.failedAt.IsZero() && time.Since(e.failedAt) < atCacheNegativeTTL
}

func (m *atCommandCacheManager) negativeCacheActive(command string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.negativeCacheActiveLocked(m.entries[command])
}

// touchRequested 在请求路径记录条目的最近请求时间。只有 Fetch(用户/页面
// 请求)会调用它:周期刷新与动作后的补刷新不算请求,不能续命空闲条目。
func (m *atCommandCacheManager) touchRequested(command string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[command]
	if entry == nil {
		entry = &atCacheEntry{command: command}
		m.entries[command] = entry
	}
	entry.lastRequested = time.Now()
}

func (m *atCommandCacheManager) enqueue(command string, force bool) <-chan struct{} {
	command = sanitizeATCommand(command)
	done := make(chan struct{})
	if command == "" {
		close(done)
		return done
	}

	m.mu.Lock()
	e := m.entries[command]
	if e == nil {
		e = &atCacheEntry{command: command}
		m.entries[command] = e
	}
	if e.running {
		// 并发动作命令不得静默合并(见 pendingForce 注释):计数补跑。
		if force && isATActionCommand(command) {
			e.pendingForce++
		}
		e.waiters = append(e.waiters, done)
		m.mu.Unlock()
		return done
	}
	if !force && !e.updatedAt.IsZero() && time.Since(e.updatedAt) < maxAgeForATCacheCommand(command) {
		m.mu.Unlock()
		close(done)
		return done
	}
	if !force && m.negativeCacheActiveLocked(e) {
		m.mu.Unlock()
		close(done)
		return done
	}
	e.running = true
	e.waiters = append(e.waiters, done)
	m.mu.Unlock()

	select {
	case m.queue <- command:
	default:
		// 队列满时原本无条件 spawn 协程绕过 worker 直接执行,通道拥塞时
		// 无限协程堆在全局 AT 文件锁上滚雪球。此处限制溢出执行并发数:
		// 超限则放弃本次执行并立即唤醒等待者(按当前快照返回 pending/旧
		// 数据),命令保持未执行态,后续 Fetch/周期刷新会再入队。
		if atomic.AddInt64(&atCacheOverflowRunning, 1) > atCacheOverflowMax {
			atomic.AddInt64(&atCacheOverflowRunning, -1)
			m.mu.Lock()
			e.running = false
			waiters := e.waiters
			e.waiters = nil
			m.mu.Unlock()
			for _, waiter := range waiters {
				close(waiter)
			}
			log.Printf("AT 缓存队列已满且溢出执行达到上限,本次放弃入队: command=%q", command)
		} else {
			go func() {
				defer atomic.AddInt64(&atCacheOverflowRunning, -1)
				m.waitUntilReadyFor(command)
				m.run(command)
			}()
		}
	}
	return done
}

func (m *atCommandCacheManager) run(command string) {
	command = sanitizeATCommand(command)
	if command == "" {
		return
	}

	mockMode := m.currentMockMode()
	m.mu.Lock()
	startGen := m.invalidateGen
	m.mu.Unlock()
	response, err := executeCachedATCommand(command, mockMode)
	errorText := ""
	if err != nil {
		errorText = err.Error()
		if strings.TrimSpace(response) == "" {
			response = errorText
		}
	}

	m.mu.Lock()
	e := m.entries[command]
	if e == nil {
		e = &atCacheEntry{command: command}
		m.entries[command] = e
	}
	// 执行期间有动作命令完成并失效了读缓存:本次读到的可能是动作前的旧
	// 数据(设备读取先于动作完成、缓存提交被调度延迟到失效之后),不得按
	// 新鲜数据写回(见 invalidateGen)。丢弃本次结果,条目保持失效态,
	// Fetch 早退分支经 running=false 返回 pending,周期刷新会重跑。
	if !isATActionCommand(command) && startGen != m.invalidateGen {
		e.running = false
		staleWaiters := e.waiters
		e.waiters = nil
		m.mu.Unlock()
		for _, waiter := range staleWaiters {
			close(waiter)
		}
		log.Printf("AT 缓存: 执行期间读缓存已被动作命令失效,丢弃本次结果: command=%q", command)
		return
	}
	// 执行失败不得用部分输出/错误文本覆盖上次成功的响应:负缓存窗口内
	// (has==true 时 responseOrPending 优先返回 response)覆盖会把截断的
	// 半截列表当数据下发(如短信列表超时截断)。失败时保留最后成功值,
	// 仅在无历史数据时才写入运行器返回内容(早退分支 has==false 时经
	// errorText 返回错误信息,行为不变)。
	if err == nil || e.response == "" {
		e.response = response
	}
	e.errorText = errorText
	// 执行失败不把错误文本当作有效缓存:保持原更新时间(过期或无数据),
	// 避免在缓存期内一直返回旧的错误文本;本次请求仍通过
	// response/errorText 拿到错误信息,对外行为不变。
	// 但记录失败时刻进入负缓存:atCacheNegativeTTL 内 Fetch/周期刷新不再
	// 重跑该命令,防止慢命令连续超时时每次请求都重新执行、重试风暴
	// 占死串行的 AT 通道。负缓存过期后仍会按原语义立即重试。
	if err == nil {
		e.updatedAt = time.Now()
		e.failedAt = time.Time{}
	} else {
		e.failedAt = time.Now()
	}
	// 并发动作命令补跑(见 pendingForce 注释):计数未清零时保持 running,
	// waiters 留到补跑完成后再统一唤醒,本次不提交"已执行"假象。
	rerun := false
	if e.pendingForce > 0 {
		e.pendingForce--
		rerun = true
	} else {
		e.running = false
	}
	var waiters []chan struct{}
	if !rerun {
		waiters = e.waiters
		e.waiters = nil
	}
	m.mu.Unlock()

	for _, waiter := range waiters {
		close(waiter)
	}
	if err != nil {
		log.Printf("AT 后台缓存更新失败: command=%q error=%v", command, err)
	}
	// 只读命令刷新成功后通知已连接的前端,页面可据此静默刷新,无需轮询等待。
	// 异步广播:单个写超时已由 writeFrame 限制,这里再用独立协程,确保任何客户端
	// 问题都不会阻塞唯一的 AT 缓存 worker。
	if err == nil && !isATActionCommand(command) {
		go broadcastAPIWebSocketEvent("at_cache_updated", map[string]string{"command": command})
	}
	if isATActionCommand(command) {
		m.invalidateReadCache()
		// 模块经 CFUN 重启后 AT 口需要恢复时间:重新应用开机保护期,
		// 保护期内读取类命令返回后台处理中,避免在模块未就绪时连续失败
		// 并把错误文本写入缓存。
		restartGrace := time.Duration(0)
		// 动作识别(isATActionCommand)走 ToUpper 匹配,保护期重放判定也必须
		// 大小写一致,否则小写 at+cfun=1,1 触发重启后不会进入读保护期、
		// 读命令也不会在模块恢复后重放。
		if strings.Contains(strings.ToUpper(command), "+CFUN=1,1") && !m.currentMockMode() {
			m.mu.Lock()
			m.readyAt = time.Now().Add(atCacheBootGracePeriod)
			m.mu.Unlock()
			restartGrace = atCacheBootGracePeriod
			log.Printf("AT 缓存: 模块重启动作已执行,%s 内读取类命令进入保护期", atCacheBootGracePeriod)
		}
		go func() {
			time.Sleep(2*time.Second + restartGrace)
			m.enqueueCachedReadCommands()
		}()
	}
	if rerun {
		// 补跑优先走正常队列(worker 对动作命令优先取出);队列满时直接
		// 协程执行(动作命令不受开机保护期限制,设备访问由全局 AT 文件锁
		// 串行化,与 worker 并发执行安全)。
		select {
		case m.queue <- command:
		default:
			go m.run(command)
		}
	}
}

// invalidateReadCache 在动作命令(重启/改 IMEI 等)执行后使读缓存失效。
// 除清零 updatedAt 外,必须同时清空残留的响应与错误文本:失效后这些内容
// 已不代表模块当前状态,若保留,保护期内 Fetch 的早退分支在 has=false 时
// 会优先返回残留 errorText(如上一次失败的 "timeout waiting for OK/ERROR"),
// 而不是设计中的"后台处理中"。清空后条目彻底回到无数据态,早退分支经
// running 标记统一返回 pending 文本。
func (m *atCommandCacheManager) invalidateReadCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 代数递增:在途读命令(含溢出协程)的结果可能产自本次动作之前,
	// 提交时按代数变化丢弃(见 run 的 startGen 检查),因此 running 条目
	// 也一并清空——旧实现跳过 running 条目,在途读命令会把动作前读到的
	// 旧数据在失效之后写回缓存并标记为新鲜,形成"写成功但显示旧状态"。
	m.invalidateGen++
	for command, entry := range m.entries {
		if isATActionCommand(command) {
			continue
		}
		entry.updatedAt = time.Time{}
		entry.response = ""
		entry.errorText = ""
		// 动作执行后模块状态已变,旧失败也不再代表现状,负缓存一并清除,
		// 让读取命令在保护期结束后按正常流程重新执行。
		entry.failedAt = time.Time{}
	}
}

func (m *atCommandCacheManager) currentMockMode() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mockMode
}

func (m *atCommandCacheManager) cachedReadCommandsNeedingRefresh() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	commands := make([]string, 0, len(m.entries))
	for command, entry := range m.entries {
		if entry.running || isATActionCommand(command) || isPeriodicRefreshSuppressedATCommand(command) ||
			m.negativeCacheActiveLocked(entry) {
			continue
		}
		if !isATCacheRefreshEligible(command, entry.lastRequested) {
			continue
		}
		if entry.updatedAt.IsZero() || time.Since(entry.updatedAt) >= maxAgeForATCacheCommand(command) {
			commands = append(commands, command)
		}
	}
	return commands
}

func (m *atCommandCacheManager) enqueueInitialReadCommands() {
	commands := commonATCacheCommands()
	if len(commands) == 0 {
		return
	}
	m.enqueue(commands[0], false)
}

func (m *atCommandCacheManager) enqueueCachedReadCommands() {
	for _, command := range m.cachedReadCommandsNeedingRefresh() {
		m.enqueue(command, false)
	}
}

func (m *atCommandCacheManager) waitUntilReadyFor(command string) {
	if isATActionCommand(command) || m.currentMockMode() {
		return
	}
	if delay := m.startupDelayRemaining(); delay > 0 {
		time.Sleep(delay)
	}
}

func (m *atCommandCacheManager) startupDelayRemaining() time.Duration {
	m.mu.Lock()
	readyAt := m.readyAt
	m.mu.Unlock()
	if readyAt.IsZero() {
		return 0
	}
	if delay := time.Until(readyAt); delay > 0 {
		return delay
	}
	return 0
}

func atCacheStartupDelay(mockMode bool) time.Duration {
	if mockMode {
		return 0
	}
	uptime, ok := systemUptimeDuration()
	if !ok || uptime >= atCacheBootGracePeriod {
		return 0
	}
	return atCacheBootGracePeriod - uptime
}

func systemUptimeDuration() (time.Duration, bool) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

// executeCachedATCommand 是缓存条目的实际执行入口。声明为 var 以便测试
// 注入成功/失败桩,运行期行为不变。
var executeCachedATCommand = func(command string, mockMode bool) (string, error) {
	if mockMode {
		return mockATResponse(command), nil
	}
	return runATCommandUntilDone(command, 200, atCommandTimeoutMS(command))
}

func (s *simpleAdminServer) handleGetATCache(w http.ResponseWriter, r *http.Request) {
	command := sanitizeATCommand(requestValue(r, "atcmd"))
	if command == "" {
		writeText(w, http.StatusOK, "")
		return
	}
	atCommandCache.Start(s.cfg.mockMode)
	force := boolQuery(r, "force", false)
	var waitOverride *bool
	if len(requestValues(r, "wait")) > 0 {
		wait := boolQuery(r, "wait", false)
		waitOverride = &wait
	}
	writeText(w, http.StatusOK, atCommandCache.Fetch(command, force, waitOverride))
}

func requestValue(r *http.Request, name string) string {
	values := requestValues(r, name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func requestValues(r *http.Request, name string) []string {
	if err := r.ParseForm(); err == nil {
		if values, ok := r.Form[name]; ok {
			return values
		}
	}
	return r.URL.Query()[name]
}

func boolQuery(r *http.Request, name string, fallback bool) bool {
	values := requestValues(r, name)
	if len(values) == 0 {
		return fallback
	}
	value := strings.TrimSpace(strings.ToLower(values[0]))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		return parsed
	}
	return value == "1" || value == "yes" || value == "on"
}

// atCacheActionQueueAllowanceMS 是动作命令等待预算中的排队余量:worker 对
// 动作命令优先取出(见 worker 注释)后,排队等待被压缩到"至多一条正在
// 执行中的命令",运行期读命令的最坏耗时为短信列表的重试预算
// (2×21s+250ms≈42.3s),向上取整为 45s。不计入排队等待时,动作 Fetch 在
// 自身单次预算(约 3s)后即放弃并返回 pending:锁频会被 atResponseOK 误判
// "操作失败"(实际稍后生效),重启会被 atResponseRebootOK 误判成功
// (实际尚未执行),多步动作序列会在中途错误中止。
const atCacheActionQueueAllowanceMS = 45000

func atCacheWaitTimeout(command string) time.Duration {
	// 读取命令超时后运行器会重试一次(250ms 间隔),最坏耗时为
	// 2×命令超时+重试间隔;动作命令(写/重启/删除)不会重试,单次预算
	// 加上排队余量即可。等待预算必须覆盖对应最坏值,否则首次尝试超时后
	// Fetch 提前放弃返回 pending/旧数据,而执行/重试其实仍在进行。
	budget := atCommandTimeoutMS(command)
	if !isATActionCommand(command) {
		budget = 2*budget + 250
	} else {
		budget += atCacheActionQueueAllowanceMS
	}
	timeout := time.Duration(budget+2000) * time.Millisecond
	if timeout < 2*time.Second {
		return 2 * time.Second
	}
	return timeout
}

func commonATCacheCommands() []string {
	commands := make([]string, 0, 10)
	for _, key := range []string{
		atKeyDashboard,
		atKeyDeviceInfo,
		atKeyNetworkBands,
		atKeyNetworkSettings,
		atKeyNetworkConfigStatus,
		atKeySystemStatus,
	} {
		commands = append(commands, pageATCommands(key)...)
	}
	return commands
}

func smsListATCommand() string {
	return `AT+CSMS=1;+CSDH=0;+CNMI=2,1,0,0,0;+CMGF=0;+CPMS="ME","ME","ME";+CMGL=4`
}

// smsListSMATCommand 读取 SM(SIM)存储的短信。入站短信按 CPMS mem3 路由,
// 可能滞留 SM 而 ME 为空,列表需双存储合并(见 fetchSMSListDualStorage)。
// 该命令会把 mem1/2/3 切到 SM,因此必须在读取后由后续命令重新声明存储归属
// (ME 列表命令自带 +CPMS="ME","ME","ME",天然完成复位)。
func smsListSMATCommand() string {
	return `AT+CMGF=0;+CPMS="SM","SM","SM";+CMGL=4`
}
