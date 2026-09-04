package main

import (
	"context"
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
	watchdogConfigFileName              = "watchdog.conf"
	watchdogRebootCommand               = "AT+CFUN=1,1"
	watchdogRadioOffCommand             = "AT+CFUN=0"
	watchdogRadioOnCommand              = "AT+CFUN=1"
	watchdogRadioResetDelay             = 10 * time.Second
	defaultWatchdogFailThreshold        = 5
	defaultWatchdogCooldownMinutes      = 30
	defaultWatchdogCheckIntervalMinutes = 3
	watchdogMaxCheckIntervalMinutes     = 30
	watchdogMaxTargets                  = 4
	// 动作策略:reboot=直接重启模块(缺省,兼容旧配置);
	// escalate=先无线电重注册,上次重注册后仍未恢复(或重注册被拒绝)时升级为重启模块。
	watchdogPolicyReboot   = "reboot"
	watchdogPolicyEscalate = "escalate"
	// 动作类型,记录于运行状态,供升级策略与前端展示使用。
	watchdogActionTypeRadio  = "radio"
	watchdogActionTypeReboot = "reboot"
)

// dialTargetTimeout 是单个探测目标的 TCP 拨号超时;看门狗、总览连通性探测
// 与 /api/get_ping 共用(dialAnyTarget 逐目标串行拨号)。
const dialTargetTimeout = 1500 * time.Millisecond

// watchdogDefaultTargets 内置探测目标:公共 DNS 的 53 端口,任一拨号成功即视为在线。
var watchdogDefaultTargets = []string{"223.5.5.5:53", "1.1.1.1:53", "8.8.8.8:53"}

// watchdogConfig 断网自愈看门狗配置,持久化为 runtimeTTLValueFile 同目录下的
// watchdog.conf JSON 文件;配置缺失或非法时使用缺省值。
// Targets 为空表示使用内置探测目标;ActionPolicy 取值见 watchdogPolicy* 常量。
type watchdogConfig struct {
	Enabled              bool     `json:"enabled"`
	FailThreshold        int      `json:"failThreshold"`
	CooldownMinutes      int      `json:"cooldownMinutes"`
	CheckIntervalMinutes int      `json:"checkIntervalMinutes"`
	Targets              []string `json:"targets"`
	ActionPolicy         string   `json:"actionPolicy"`
}

// watchdogState 看门狗运行状态,进程内保存,重启清零。
// lastEnabled 记录上一轮配置的启用状态,用于在禁用→启用切换时清零失败计数;
// lastActionType 记录上次自愈动作类型,供升级策略决定本次动作。
var watchdogState = struct {
	mu                  sync.Mutex
	consecutiveFailures int
	lastActionTime      time.Time
	lastEnabled         bool
	lastActionType      string
}{}

// watchdogPoller 生命周期状态:轮询循环仅在配置启用时运行,关闭开关后
// 立即停止,不保留空转的常驻循环(与短信转发轮询器同一模式)。
var (
	watchdogPollerMu   sync.Mutex
	watchdogPollerStop chan struct{}
)

// watchdogPollWait 等待一个轮询间隔,stop 关闭时立即返回 false,可在测试中替换。
var watchdogPollWait = func(d time.Duration, stop <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-stop:
		return false
	}
}

// syncWatchdogPoller 按配置同步轮询器运行状态:启用时启动(幂等),
// 禁用时停止(幂等)并复位失败计数。服务启动时与保存配置后调用。
func syncWatchdogPoller(s *simpleAdminServer) {
	if readWatchdogConfig().Enabled {
		startWatchdogPoller(s)
		return
	}
	stopWatchdogPoller()
	resetWatchdogFailureState()
}

// resetWatchdogFailureState 禁用看门狗时复位失败计数与启用标记。轮询器仅在
// 启用期间运行,禁用期间没有任何 tick 能把 lastEnabled 置回 false;若禁用时不
// 主动复位,重新启用后首轮 tick 看到残留的 lastEnabled=true,watchdogTick 里
// "禁用→启用清零"分支(依赖 !lastEnabled)永不触发,禁用前累积的失败计数会让
// 设备在用户只观察到少量新失败时就提前触发自愈重启。保留 lastActionTime 以
// 延续冷却保护,避免禁用-启用循环后立即再次重启。纯进程内状态,不涉及设备。
func resetWatchdogFailureState() {
	watchdogState.mu.Lock()
	watchdogState.consecutiveFailures = 0
	watchdogState.lastEnabled = false
	watchdogState.lastActionType = ""
	watchdogState.mu.Unlock()
}

// startWatchdogPoller 启动看门狗轮询循环(幂等):先等待一个间隔再首次探测,
// 避免启动时 WAN 未就绪即探测累计失败;间隔每轮从配置读取,支持运行中修改。
func startWatchdogPoller(s *simpleAdminServer) {
	watchdogPollerMu.Lock()
	defer watchdogPollerMu.Unlock()
	if watchdogPollerStop != nil {
		return
	}
	stop := make(chan struct{})
	watchdogPollerStop = stop
	go func() {
		for watchdogPollWait(watchdogPollInterval(), stop) {
			watchdogTick(s)
		}
	}()
}

// stopWatchdogPoller 停止看门狗轮询循环(幂等):等待间隔期间也能立即退出。
func stopWatchdogPoller() {
	watchdogPollerMu.Lock()
	defer watchdogPollerMu.Unlock()
	if watchdogPollerStop == nil {
		return
	}
	close(watchdogPollerStop)
	watchdogPollerStop = nil
}

// watchdogPollerRunning 返回轮询循环当前是否在运行。
func watchdogPollerRunning() bool {
	watchdogPollerMu.Lock()
	defer watchdogPollerMu.Unlock()
	return watchdogPollerStop != nil
}

// watchdogPollInterval 当前配置的检测间隔。
func watchdogPollInterval() time.Duration {
	return time.Duration(readWatchdogConfig().CheckIntervalMinutes) * time.Minute
}

// watchdogCheckOnline 对探测目标逐个拨号判定联网状态,可在测试中替换为假实现。
var watchdogCheckOnline = func(ctx context.Context, targets []string) bool {
	return dialAnyTarget(ctx, targets)
}

// watchdogRebootAction 执行模块重启动作,可在测试中替换为假实现。
var watchdogRebootAction = defaultWatchdogRebootAction

// watchdogRadioResetAction 执行无线电重注册动作,返回 AT 响应;可在测试中替换。
var watchdogRadioResetAction = defaultWatchdogRadioResetAction

func defaultWatchdogRebootAction(s *simpleAdminServer) string {
	if s == nil || s.cfg.mockMode {
		return ""
	}
	return s.runPageAction(watchdogRebootCommand)
}

// defaultWatchdogRadioResetAction 无线电重注册:CFUN=0 关闭射频,延迟后
// CFUN=1 恢复全功能并重新注册网络,比整模块重启(AT+CFUN=1,1)轻量。
// CFUN=0 被明确拒绝或无应答时原样返回,由调用方升级为重启。
func defaultWatchdogRadioResetAction(s *simpleAdminServer) string {
	if s == nil || s.cfg.mockMode {
		return ""
	}
	resp := s.runPageAction(watchdogRadioOffCommand)
	if !atResponseOK(resp) {
		return resp
	}
	time.Sleep(watchdogRadioResetDelay)
	return s.runPageAction(watchdogRadioOnCommand)
}

func watchdogConfigFile() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), watchdogConfigFileName)
}

func defaultWatchdogConfig() watchdogConfig {
	return watchdogConfig{
		Enabled:              false,
		FailThreshold:        defaultWatchdogFailThreshold,
		CooldownMinutes:      defaultWatchdogCooldownMinutes,
		CheckIntervalMinutes: defaultWatchdogCheckIntervalMinutes,
		Targets:              nil,
		ActionPolicy:         watchdogPolicyReboot,
	}
}

// normalizeWatchdogConfig 对单项配置做合法性收敛,非法值回退缺省。
func normalizeWatchdogConfig(cfg watchdogConfig) watchdogConfig {
	if cfg.FailThreshold <= 0 {
		cfg.FailThreshold = defaultWatchdogFailThreshold
	}
	if cfg.CooldownMinutes <= 0 {
		cfg.CooldownMinutes = defaultWatchdogCooldownMinutes
	}
	if cfg.CheckIntervalMinutes <= 0 || cfg.CheckIntervalMinutes > watchdogMaxCheckIntervalMinutes {
		cfg.CheckIntervalMinutes = defaultWatchdogCheckIntervalMinutes
	}
	if cfg.ActionPolicy != watchdogPolicyReboot && cfg.ActionPolicy != watchdogPolicyEscalate {
		cfg.ActionPolicy = watchdogPolicyReboot
	}
	return cfg
}

// readWatchdogConfig 读取看门狗配置;文件缺失、JSON 非法时返回缺省值,
// 单项数值非法时该项回退缺省值。
func readWatchdogConfig() watchdogConfig {
	cfg := defaultWatchdogConfig()
	data, err := os.ReadFile(watchdogConfigFile())
	if err != nil {
		return cfg
	}
	var parsed watchdogConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		log.Printf("看门狗: 配置文件 %s 非法,使用缺省配置: %v", watchdogConfigFile(), err)
		return cfg
	}
	return normalizeWatchdogConfig(parsed)
}

// writeWatchdogConfig 以 0600 权限持久化看门狗配置。原子写避免掉电撕裂:
// 半截 JSON 会让读取静默回退缺省(看门狗被悄悄关闭、用户设置丢失)。
func writeWatchdogConfig(cfg watchdogConfig) error {
	cfg = normalizeWatchdogConfig(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(watchdogConfigFile(), append(data, '\n'), 0600)
}

// normalizeWatchdogTarget 把单个探测目标规范化为 host:port(缺省端口 53);
// 支持 IPv4/域名/IPv6 字面量(可带 [..]:port),非法返回 false。
func normalizeWatchdogTarget(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	host, port := value, "53"
	switch {
	case strings.HasPrefix(value, "["):
		// 带方括号的 IPv6 字面量,必须显式给出端口
		h, p, err := net.SplitHostPort(value)
		if err != nil || h == "" {
			return "", false
		}
		host, port = h, p
	case strings.Count(value, ":") > 1:
		// 不带端口的 IPv6 字面量,补缺省端口
	case strings.Contains(value, ":"):
		h, p, err := net.SplitHostPort(value)
		if err != nil || h == "" {
			return "", false
		}
		host, port = h, p
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", false
	}
	if strings.ContainsAny(host, " \t/#?") {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// parseWatchdogTargets 解析用户提交的探测目标列表(逗号/分号/换行分隔),
// 逐项规范化,丢弃非法项(含内部空格的条目视为非法),最多保留
// watchdogMaxTargets 个;无有效项返回 nil(使用内置目标)。
func parseWatchdogTargets(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	targets := make([]string, 0, len(fields))
	for _, field := range fields {
		target, ok := normalizeWatchdogTarget(field)
		if !ok {
			continue
		}
		targets = append(targets, target)
		if len(targets) >= watchdogMaxTargets {
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

// watchdogProbeTargets 返回生效的探测目标:未配置自定义目标时用内置目标。
func watchdogProbeTargets(cfg watchdogConfig) []string {
	if len(cfg.Targets) == 0 {
		return watchdogDefaultTargets
	}
	return cfg.Targets
}

// watchdogProbeBudget 计算一次联网探测的总预算:拨号按目标串行进行、
// 每目标独立超时 dialTargetTimeout,总预算 = 目标数×单目标超时 + 余量,
// 保证排在后面的目标不会因预算耗尽而失去完整探测机会。
func watchdogProbeBudget(targetCount int) time.Duration {
	if targetCount < 1 {
		targetCount = 1
	}
	return time.Duration(targetCount)*dialTargetTimeout + 500*time.Millisecond
}

// watchdogTick 执行一次看门狗检查:轮询器仅在启用时运行,此处仍复查配置
// 作为关闭竞态下的防御。连续失败达到阈值且距上次自愈动作超过冷却时间时
// 按动作策略执行自愈动作。
func watchdogTick(s *simpleAdminServer) {
	cfg := readWatchdogConfig()

	// 从禁用切换为启用时清零连续失败计数,避免延续禁用前累积的失败。
	watchdogState.mu.Lock()
	if cfg.Enabled && !watchdogState.lastEnabled && watchdogState.consecutiveFailures > 0 {
		log.Printf("看门狗: 重新启用,清零连续失败计数(此前 %d 次)", watchdogState.consecutiveFailures)
		watchdogState.consecutiveFailures = 0
	}
	watchdogState.lastEnabled = cfg.Enabled
	watchdogState.mu.Unlock()

	if !cfg.Enabled {
		return
	}

	check := watchdogCheckOnline
	if check == nil {
		check = func(ctx context.Context, targets []string) bool { return dialAnyTarget(ctx, targets) }
	}
	targets := watchdogProbeTargets(cfg)
	// 探测预算必须覆盖全部目标的串行拨号:每个目标 1.5s 独立超时,
	// 固定 5s 预算在 4 个自定义目标时最坏需 6s,末尾目标只剩零点几秒,
	// 前几个目标不可达时会把本可证明在线的目标掐死、误判离线。
	ctx, cancel := context.WithTimeout(context.Background(), watchdogProbeBudget(len(targets)))
	online := check(ctx, targets)
	cancel()

	watchdogState.mu.Lock()
	if online {
		if watchdogState.consecutiveFailures > 0 {
			log.Printf("看门狗: 联网恢复正常,清零连续失败计数(此前 %d 次)", watchdogState.consecutiveFailures)
		}
		watchdogState.consecutiveFailures = 0
		// 联网恢复后重置动作类型,下一次失败重新从轻量动作开始升级。
		watchdogState.lastActionType = ""
		watchdogState.mu.Unlock()
		return
	}

	watchdogState.consecutiveFailures++
	failures := watchdogState.consecutiveFailures
	log.Printf("看门狗: 联网检测失败,连续失败计数 %d/%d", failures, cfg.FailThreshold)
	if failures < cfg.FailThreshold {
		watchdogState.mu.Unlock()
		return
	}

	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	if !watchdogState.lastActionTime.IsZero() && time.Since(watchdogState.lastActionTime) < cooldown {
		watchdogState.mu.Unlock()
		log.Printf("看门狗: 距上次自愈动作不足 %d 分钟冷却时间,暂不执行", cfg.CooldownMinutes)
		return
	}
	lastActionType := watchdogState.lastActionType
	watchdogState.lastActionTime = time.Now()
	watchdogState.consecutiveFailures = 0
	watchdogState.mu.Unlock()

	actionType := watchdogRunAction(s, cfg, lastActionType)

	watchdogState.mu.Lock()
	watchdogState.lastActionType = actionType
	watchdogState.mu.Unlock()
}

// watchdogRunAction 按动作策略执行自愈动作,返回实际执行的动作类型。
// reboot 策略:直接重启模块;
// escalate 策略:上次动作不是无线电重注册时先尝试重注册(轻量恢复),
// 上次重注册后仍未恢复、或本次重注册未被接受时升级为重启模块。
func watchdogRunAction(s *simpleAdminServer, cfg watchdogConfig, lastActionType string) string {
	rebootAction := watchdogRebootAction
	if rebootAction == nil {
		rebootAction = defaultWatchdogRebootAction
	}
	runReboot := func(reason string) string {
		log.Printf("看门狗: %s,执行自愈动作 %s", reason, watchdogRebootCommand)
		resp := rebootAction(s)
		log.Printf("看门狗: 自愈动作已执行,响应: %s", outputSummary(resp))
		return watchdogActionTypeReboot
	}

	if cfg.ActionPolicy != watchdogPolicyEscalate {
		return runReboot(fmt.Sprintf("连续 %d 次联网检测失败", cfg.FailThreshold))
	}

	radioAction := watchdogRadioResetAction
	if radioAction == nil {
		radioAction = defaultWatchdogRadioResetAction
	}
	if lastActionType == watchdogActionTypeRadio {
		return runReboot("无线电重注册后联网仍未恢复,升级为重启模块")
	}
	log.Printf("看门狗: 连续 %d 次联网检测失败,先执行无线电重注册(%s → %s)",
		cfg.FailThreshold, watchdogRadioOffCommand, watchdogRadioOnCommand)
	resp := radioAction(s)
	if !atResponseOK(resp) {
		return runReboot(fmt.Sprintf("无线电重注册未被接受(%s),升级为重启模块", outputSummary(resp)))
	}
	log.Printf("看门狗: 无线电重注册已执行,响应: %s", outputSummary(resp))
	return watchdogActionTypeRadio
}

// currentWatchdogStatus 返回看门狗当前配置与运行状态,供 API 使用;
// lastActionTime/lastActionType 未执行过动作时为空串。
func currentWatchdogStatus() map[string]any {
	cfg := readWatchdogConfig()
	watchdogState.mu.Lock()
	failures := watchdogState.consecutiveFailures
	lastAction := watchdogState.lastActionTime
	lastActionType := watchdogState.lastActionType
	watchdogState.mu.Unlock()

	lastActionText := ""
	if !lastAction.IsZero() {
		lastActionText = lastAction.Format("2006-01-02 15:04:05")
	}
	targets := cfg.Targets
	if targets == nil {
		targets = []string{}
	}
	return map[string]any{
		"enabled":              cfg.Enabled,
		"failThreshold":        cfg.FailThreshold,
		"cooldownMinutes":      cfg.CooldownMinutes,
		"checkIntervalMinutes": cfg.CheckIntervalMinutes,
		"targets":              targets,
		"defaultTargets":       watchdogDefaultTargets,
		"actionPolicy":         cfg.ActionPolicy,
		"consecutiveFailures":  failures,
		"lastActionTime":       lastActionText,
		"lastActionType":       lastActionType,
		"pollerRunning":        watchdogPollerRunning(),
	}
}

func resetWatchdogForTest() {
	stopWatchdogPoller()
	watchdogState.mu.Lock()
	watchdogState.consecutiveFailures = 0
	watchdogState.lastActionTime = time.Time{}
	watchdogState.lastEnabled = false
	watchdogState.lastActionType = ""
	watchdogState.mu.Unlock()
}
