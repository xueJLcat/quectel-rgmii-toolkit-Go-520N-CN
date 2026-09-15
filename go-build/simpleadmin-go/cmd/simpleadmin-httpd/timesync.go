// timesync.go 提供时间同步功能:按可配置间隔轮询单一 NTP 源(缺省阿里云
// ntp.aliyun.com),查询成功即直接步进修改系统时间;另提供手动同步一次入口。
// 配置持久化为 runtimeTTLValueFile 同目录下的 timesync.conf。
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	timeSyncConfigFileName         = "timesync.conf"
	defaultTimeSyncServer          = "ntp.aliyun.com"
	defaultTimeSyncIntervalMinutes = 60
	timeSyncMinIntervalMinutes     = 1
	timeSyncMaxIntervalMinutes     = 1440
	timeSyncNTPPort                = 123
	timeSyncQueryTimeout           = 8 * time.Second
	// 启用后首次同步等待:短暂等待 WAN 就绪;设备无电池时钟,首次同步不宜
	// 等满一个间隔,失败后也按 timeSyncFailureRetry 快速重试。
	timeSyncFirstDelay = 30 * time.Second
	// 同步失败后的重试间隔(配置间隔更小时按配置间隔)。
	timeSyncFailureRetry = 5 * time.Minute
	// NTP 时间戳 epoch(1900-01-01)与 Unix epoch(1970-01-01)的秒差。
	ntpEpochOffset = 2208988800
	// 单次同步允许的最大回拨幅度(约 1 年):本地时钟已合理时,向后大幅
	// 步进(本地超前)只可能来自此前的坏校时/手工设置,拒绝写入系统时间,
	// 避免异常应答把时钟打到离谱的值。向前大幅步进(本地落后)不受此限:
	// 那是无电池时钟设备的常态,见 timeSyncMinPlausibleTime 的说明。
	timeSyncMaxStep = 365 * 24 * time.Hour
	// 单次采样允许的最大往返时延:蜂窝链路严重拥塞/不对称时单样本误差
	// 可达秒级,直接写钟会引入同量级误差;超限丢弃本次样本等下轮重试。
	timeSyncMaxRoundTrip = 5 * time.Second
)

// timeSyncMinPlausibleTime 是本地时钟"合理"的下界,仅用于向后大幅步进
// (回拨)的护栏豁免:设备无电池时钟,冷启动时系统时间是错误值(1970 年),
// 本地时钟早于该下界时跳过回拨幅度检查直接校正。
// 向前大幅步进(本地落后)一律放行、不再依赖本下界:开机时钟也可能是固件
// 构建日期(注释与实机行为均已确认),构建日期恒"晚于 2020"且随时间推移
// 与真实时间的偏差必然超过 timeSyncMaxStep——固定下界判"时钟已合理"是
// 时间炸弹,2026 年任何 2025 年前构建的固件冷启动校时都会被永久拒绝,
// 时间同步在其最主要场景(纠正开机大偏差)完全失效。应答可信性由报文
// 防线约束:KoD(stratum=0)、LI=3、originate 回显绑定(拒绝路径内注入)、
// t3≥t2 与往返时延上限,全部保持生效。
var timeSyncMinPlausibleTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// timeSyncConfig 时间同步配置;Server 为单一 NTP 源,空值回退缺省服务器。
type timeSyncConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Server          string `json:"server"`
}

// timeSyncState 时间同步运行状态,进程内保存,重启清零。
var timeSyncState = struct {
	mu           sync.Mutex
	syncing      bool
	lastSyncTime time.Time
	lastOK       bool
	lastError    string
	lastOffsetMs int64
	lastServer   string
}{}

// timeSyncPoller 生命周期状态:轮询循环仅在配置启用时运行(与看门狗同一模式)。
var (
	timeSyncPollerMu   sync.Mutex
	timeSyncPollerStop chan struct{}
)

// timeSyncPollWait 等待一个轮询间隔,stop 关闭时立即返回 false,可在测试中替换。
var timeSyncPollWait = func(d time.Duration, stop <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-stop:
		return false
	}
}

// timeSyncQuery 执行一次 NTP 查询,可在测试中替换为假实现。
var timeSyncQuery = defaultTimeSyncQuery

// timeSyncApply 把校正后的时间写入系统时钟,可在测试中替换。
var timeSyncApply = setSystemTime

type ntpResult struct {
	// Offset 本地时钟相对 NTP 服务器的偏差(正值表示本地落后)。
	Offset time.Duration
	// RoundTrip 扣除服务器处理耗时后的往返时延。
	RoundTrip time.Duration
	// ServerTime 服务器应答中的发送时刻。
	ServerTime time.Time
}

func timeSyncConfigFile() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), timeSyncConfigFileName)
}

func defaultTimeSyncConfig() timeSyncConfig {
	return timeSyncConfig{
		Enabled:         false,
		IntervalMinutes: defaultTimeSyncIntervalMinutes,
		Server:          defaultTimeSyncServer,
	}
}

// normalizeTimeSyncServer 校验单一 NTP 源地址:仅接受主机名/IPv4(不带端口、
// 协议前缀与空白),非法返回缺省服务器。
func normalizeTimeSyncServer(value string) string {
	server := strings.TrimSpace(value)
	if server == "" {
		return defaultTimeSyncServer
	}
	server = strings.TrimPrefix(strings.TrimPrefix(server, "ntp://"), "ntps://")
	if strings.ContainsAny(server, " \t/#?:") {
		return defaultTimeSyncServer
	}
	if len(server) > 253 {
		return defaultTimeSyncServer
	}
	return server
}

// normalizeTimeSyncConfig 对单项配置做合法性收敛,非法值回退缺省。
func normalizeTimeSyncConfig(cfg timeSyncConfig) timeSyncConfig {
	if cfg.IntervalMinutes < timeSyncMinIntervalMinutes || cfg.IntervalMinutes > timeSyncMaxIntervalMinutes {
		cfg.IntervalMinutes = defaultTimeSyncIntervalMinutes
	}
	cfg.Server = normalizeTimeSyncServer(cfg.Server)
	return cfg
}

// readTimeSyncConfig 读取时间同步配置;文件缺失、JSON 非法时返回缺省值。
func readTimeSyncConfig() timeSyncConfig {
	cfg := defaultTimeSyncConfig()
	data, err := os.ReadFile(timeSyncConfigFile())
	if err != nil {
		return cfg
	}
	var parsed timeSyncConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		log.Printf("时间同步: 配置文件 %s 非法,使用缺省配置: %v", timeSyncConfigFile(), err)
		return cfg
	}
	return normalizeTimeSyncConfig(parsed)
}

// writeTimeSyncConfig 以 0600 权限持久化时间同步配置。
func writeTimeSyncConfig(cfg timeSyncConfig) error {
	cfg = normalizeTimeSyncConfig(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 原子写避免掉电撕裂配置(半截 JSON 回退缺省会悄悄关闭时间同步)。
	return atomicWriteFile(timeSyncConfigFile(), append(data, '\n'), 0600)
}

// syncTimeSyncPoller 按配置同步轮询器运行状态:启用时重启循环(先停后启,
// 幂等),禁用时停止(幂等)。服务启动时与保存配置后调用。
// 配置读取与启停决定必须在同一临界区内完成:锁外读取再锁外启停时,并发
// set_timesync 交错(启用请求读到 true → 禁用请求完成 stop → 启用请求
// 再 start)会留下"配置已禁用但轮询器常驻"的残留状态。
// 启用路径必须重启而非仅幂等启动:等待中的循环按开始等待时取的旧间隔
// 睡眠,修改间隔/更换 NTP 服务器后若不重启,新配置要等旧间隔(最长 24
// 小时)睡满才生效,与保存接口"立即生效"的响应语义相悖(与
// syncWatchdogPoller 对齐)。
func syncTimeSyncPoller(s *simpleAdminServer) {
	timeSyncPollerMu.Lock()
	defer timeSyncPollerMu.Unlock()
	if readTimeSyncConfig().Enabled {
		stopTimeSyncPollerLocked()
		startTimeSyncPollerLocked(s)
		return
	}
	stopTimeSyncPollerLocked()
}

// startTimeSyncPoller 启动时间同步轮询循环(幂等):启用后先等待
// timeSyncFirstDelay 再首次同步(短等 WAN 就绪,设备无电池时钟不宜等满间隔);
// 失败按 timeSyncNextDelay 快速重试,成功按配置间隔;间隔每轮从配置读取。
func startTimeSyncPoller(s *simpleAdminServer) {
	timeSyncPollerMu.Lock()
	defer timeSyncPollerMu.Unlock()
	startTimeSyncPollerLocked(s)
}

// startTimeSyncPollerLocked 启动轮询循环,调用方必须持有 timeSyncPollerMu。
func startTimeSyncPollerLocked(s *simpleAdminServer) {
	if timeSyncPollerStop != nil {
		return
	}
	stop := make(chan struct{})
	timeSyncPollerStop = stop
	go func() {
		if !timeSyncPollWait(timeSyncFirstDelay, stop) {
			return
		}
		for {
			ok := timeSyncTick(s)
			if !timeSyncPollWait(timeSyncNextDelay(ok), stop) {
				return
			}
		}
	}()
}

// stopTimeSyncPoller 停止时间同步轮询循环(幂等)。
func stopTimeSyncPoller() {
	timeSyncPollerMu.Lock()
	defer timeSyncPollerMu.Unlock()
	stopTimeSyncPollerLocked()
}

// stopTimeSyncPollerLocked 停止轮询循环,调用方必须持有 timeSyncPollerMu。
func stopTimeSyncPollerLocked() {
	if timeSyncPollerStop == nil {
		return
	}
	close(timeSyncPollerStop)
	timeSyncPollerStop = nil
}

// timeSyncPollerRunning 返回轮询循环当前是否在运行。
func timeSyncPollerRunning() bool {
	timeSyncPollerMu.Lock()
	defer timeSyncPollerMu.Unlock()
	return timeSyncPollerStop != nil
}

// timeSyncPollInterval 当前配置的同步间隔。
func timeSyncPollInterval() time.Duration {
	return time.Duration(readTimeSyncConfig().IntervalMinutes) * time.Minute
}

// timeSyncNextDelay 按本轮同步结果返回下次等待时长:成功按配置间隔;
// 失败快速重试(取 timeSyncFailureRetry 与配置间隔的较小值),
// 避免开机时钟错误时因一次失败而长时间停留在错误时间。
func timeSyncNextDelay(ok bool) time.Duration {
	interval := timeSyncPollInterval()
	if ok {
		return interval
	}
	if timeSyncFailureRetry < interval {
		return timeSyncFailureRetry
	}
	return interval
}

// timeSyncTick 执行一次轮询同步,返回是否同步成功;轮询器仅在启用时运行,
// 此处仍复查配置作为关闭竞态下的防御。
func timeSyncTick(s *simpleAdminServer) bool {
	cfg := readTimeSyncConfig()
	if !cfg.Enabled {
		return false
	}
	result := runTimeSyncOnce(s, cfg.Server)
	ok, _ := result["ok"].(bool)
	return ok
}

// parseNTPResponse 解析 48 字节 NTP 应答报文,结合本地发送/接收时刻计算时钟偏差。
// t1 为客户端发送时刻,t4 为应答接收时刻(均为本地时钟)。
func parseNTPResponse(packet []byte, t1, t4 time.Time) (ntpResult, error) {
	if len(packet) < 48 {
		return ntpResult{}, fmt.Errorf("ntp response too short: %d bytes", len(packet))
	}
	mode := packet[0] & 0x07
	if mode != 4 {
		return ntpResult{}, fmt.Errorf("unexpected ntp mode %d", mode)
	}
	// LI=3(alarm condition)表示服务器自身时钟未同步,其应答不可用于校时。
	if packet[0]>>6 == 3 {
		return ntpResult{}, fmt.Errorf("ntp server clock not synchronized (LI=3)")
	}
	stratum := packet[1]
	if stratum == 0 || stratum > 15 {
		return ntpResult{}, fmt.Errorf("unusable ntp stratum %d", stratum)
	}
	t2, ok2 := ntpTimestamp(packet[32:40])
	t3, ok3 := ntpTimestamp(packet[40:48])
	if !ok2 || !ok3 {
		return ntpResult{}, fmt.Errorf("ntp timestamps missing")
	}
	// t3(发送应答)不得早于 t2(收到请求):违反说明服务器时钟异常或
	// 报文损坏,负的处理耗时会给 θ 混入伪偏差。
	if t3.Before(t2) {
		return ntpResult{}, fmt.Errorf("ntp server timestamps inconsistent (transmit before receive)")
	}
	// θ = ((t2-t1) + (t3-t4)) / 2;δ = (t4-t1) - (t3-t2)
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	roundTrip := t4.Sub(t1) - t3.Sub(t2)
	if roundTrip < 0 {
		roundTrip = 0
	}
	return ntpResult{Offset: offset, RoundTrip: roundTrip, ServerTime: t3}, nil
}

// ntpTimestamp 把 NTP 64 位时间戳(32 位秒 + 32 位小数)解析为 time.Time;
// 全零时间戳返回 ok=false。
func ntpTimestamp(b []byte) (time.Time, bool) {
	seconds := binary.BigEndian.Uint32(b[0:4])
	fraction := binary.BigEndian.Uint32(b[4:8])
	if seconds == 0 && fraction == 0 {
		return time.Time{}, false
	}
	nsec := (int64(fraction) * int64(time.Second)) >> 32
	return time.Unix(int64(seconds)-ntpEpochOffset, nsec), true
}

// defaultTimeSyncQuery 向单一 NTP 源发送一次 SNTP 查询并计算时钟偏差。
func defaultTimeSyncQuery(server string, timeout time.Duration) (ntpResult, error) {
	address := net.JoinHostPort(server, strconv.Itoa(timeSyncNTPPort))
	conn, err := net.DialTimeout("udp", address, timeout)
	if err != nil {
		return ntpResult{}, err
	}
	defer conn.Close()

	packet := make([]byte, 48)
	packet[0] = 0x1B // LI=0, VN=3, Mode=3(client)
	// 请求写入 Transmit Timestamp(T1,RFC 4330 §5):服务器必须在应答的
	// Originate Timestamp(字节 24-31)原样回显,以此把应答绑定到本次请求,
	// 拒绝路径内注入的伪造/错发包(旧实现请求时间戳全零且不校验回显,
	// 任何能向该 UDP 四元组注入一个包的攻击者都能任意校时)。
	t1 := time.Now()
	binary.BigEndian.PutUint32(packet[40:], uint32(t1.Unix()+ntpEpochOffset))
	binary.BigEndian.PutUint32(packet[44:], uint32((uint64(t1.Nanosecond())<<32)/uint64(time.Second)))
	transmit := append([]byte(nil), packet[40:48]...)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(packet); err != nil {
		return ntpResult{}, err
	}
	reply := make([]byte, 512)
	n, err := conn.Read(reply)
	t4 := time.Now()
	if err != nil {
		return ntpResult{}, err
	}
	if n < 48 || !bytes.Equal(reply[24:32], transmit) {
		return ntpResult{}, fmt.Errorf("ntp originate timestamp mismatch (bogus reply)")
	}
	return parseNTPResponse(reply[:n], t1, t4)
}

// runTimeSyncOnce 从单一 NTP 源查询一次并直接步进修改系统时间;
// 并发调用(轮询与手动)互斥,同一时刻最多一次同步。结果记入运行状态。
func runTimeSyncOnce(s *simpleAdminServer, server string) map[string]any {
	server = normalizeTimeSyncServer(server)

	timeSyncState.mu.Lock()
	if timeSyncState.syncing {
		timeSyncState.mu.Unlock()
		return map[string]any{"ok": false, "error": "sync already in progress"}
	}
	timeSyncState.syncing = true
	timeSyncState.mu.Unlock()
	defer func() {
		timeSyncState.mu.Lock()
		timeSyncState.syncing = false
		timeSyncState.mu.Unlock()
	}()

	record := func(ok bool, offset time.Duration, errText string) map[string]any {
		timeSyncState.mu.Lock()
		// "最近同步时间"只记录成功的同步:失败的尝试(如无外网)若也刷新
		// 时间戳,状态页会把失败显示成一次新鲜同步,误导排障。
		if ok {
			timeSyncState.lastSyncTime = time.Now()
			// lastOffsetMs 与 lastSyncTime 同为"最近一次成功同步"语义:
			// 失败尝试不得把成功偏差覆写成 0,否则状态页出现"上次同步
			// 时间=昨天、偏差=0ms"的混合语义,误导排障。
			timeSyncState.lastOffsetMs = offset.Milliseconds()
		}
		timeSyncState.lastOK = ok
		timeSyncState.lastError = errText
		timeSyncState.lastServer = server
		timeSyncState.mu.Unlock()
		result := map[string]any{
			"ok":       ok,
			"server":   server,
			"offsetMs": offset.Milliseconds(),
		}
		if errText != "" {
			result["error"] = errText
		}
		if ok {
			result["systemTime"] = time.Now().Format("2006-01-02 15:04:05")
		}
		return result
	}

	if s != nil && s.cfg.mockMode {
		// mock 模式不访问网络、不修改时钟,模拟一次小幅校正。
		log.Printf("时间同步: mock 模式,模拟自 %s 同步", server)
		return record(true, 350*time.Millisecond, "")
	}

	result, err := timeSyncQuery(server, timeSyncQueryTimeout)
	if err != nil {
		log.Printf("时间同步: 查询 %s 失败: %v", server, err)
		return record(false, 0, err.Error())
	}
	// 回拨护栏仅约束"本地时钟已合理"的场景:冷启动 1970 时钟必须放行
	// (见 timeSyncMinPlausibleTime)。向前步进(本地落后,含冷启动 1970/
	// 固件构建日期与真实时间的数年偏差)一律放行,否则无电池时钟设备永远
	// 无法完成首次校时;本地超前一年以上的向后步进才是异常态,拒绝。
	if result.Offset < -timeSyncMaxStep && time.Now().After(timeSyncMinPlausibleTime) {
		log.Printf("时间同步: %s 应答偏差 %s 异常(本地时钟超前将被回拨超过上限),拒绝修改系统时间", server, result.Offset)
		return record(false, 0, "offset out of range")
	}
	if result.RoundTrip > timeSyncMaxRoundTrip {
		log.Printf("时间同步: %s 应答往返时延 %s 过大,丢弃本次样本", server, result.RoundTrip)
		return record(false, 0, fmt.Sprintf("round trip too large: %s", result.RoundTrip))
	}

	newTime := time.Now().Add(result.Offset)
	if err := timeSyncApply(newTime); err != nil {
		log.Printf("时间同步: 修改系统时间失败: %v", err)
		return record(false, 0, "set system time failed: "+err.Error())
	}
	log.Printf("时间同步: 已从 %s 校正系统时间,偏差 %d ms", server, result.Offset.Milliseconds())
	return record(true, result.Offset, "")
}

// currentTimeSyncStatus 返回时间同步当前配置与运行状态,供 API 使用;
// 从未成功同步过时 lastSyncTime 为空串。
func currentTimeSyncStatus() map[string]any {
	cfg := readTimeSyncConfig()
	timeSyncState.mu.Lock()
	lastTime := timeSyncState.lastSyncTime
	lastOK := timeSyncState.lastOK
	lastError := timeSyncState.lastError
	lastOffsetMs := timeSyncState.lastOffsetMs
	lastServer := timeSyncState.lastServer
	syncing := timeSyncState.syncing
	timeSyncState.mu.Unlock()

	lastTimeText := ""
	if !lastTime.IsZero() {
		lastTimeText = lastTime.Format("2006-01-02 15:04:05")
	}
	return map[string]any{
		"enabled":         cfg.Enabled,
		"intervalMinutes": cfg.IntervalMinutes,
		"server":          cfg.Server,
		"defaultServer":   defaultTimeSyncServer,
		"pollerRunning":   timeSyncPollerRunning(),
		"syncing":         syncing,
		"lastSyncTime":    lastTimeText,
		"lastSyncOK":      lastOK,
		"lastError":       lastError,
		"lastOffsetMs":    lastOffsetMs,
		"lastServer":      lastServer,
		"systemTime":      time.Now().Format("2006-01-02 15:04:05"),
	}
}

func resetTimeSyncForTest() {
	stopTimeSyncPoller()
	timeSyncState.mu.Lock()
	timeSyncState.syncing = false
	timeSyncState.lastSyncTime = time.Time{}
	timeSyncState.lastOK = false
	timeSyncState.lastError = ""
	timeSyncState.lastOffsetMs = 0
	timeSyncState.lastServer = ""
	timeSyncState.mu.Unlock()
}
