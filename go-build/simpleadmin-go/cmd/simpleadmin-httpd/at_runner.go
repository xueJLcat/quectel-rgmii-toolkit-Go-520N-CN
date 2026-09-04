package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

func atCommandTimeoutMS(command string) int {
	return atGroupedCommandTimeoutMS(command)
}

// atSMSListExtraTimeoutMS 是短信全量列表读取(+CMGL=4 / +CMGL="ALL")的额外
// 读取预算。短信较多时输出很大,一秒默认预算会假超时、半截响应被当有效数据,
// 故在单命令预算上追加。单条与组合命令统一在 atSingleCommandTimeoutMS 计入,
// 保证手动 AT 终端 / at-client 直接执行裸 +CMGL=4 时也有足够预算。
const atSMSListExtraTimeoutMS = 15000

func atGroupedCommandTimeoutMS(command string) int {
	parts := splitATCommandParts(sanitizeATCommand(command))
	if len(parts) <= 1 {
		return atSingleCommandTimeoutMS(command)
	}
	total := 0
	for _, part := range normalizeATCommandParts(parts) {
		total += atSingleCommandTimeoutMS(part)
	}
	if total <= 0 {
		return atSingleCommandTimeoutMS(command)
	}
	return total
}

// atSingleCommandTimeoutMS 返回单条 AT 命令的等待上限。超时策略定义点:
// 全频扫网命令实机经常超过两分钟,预留三分钟;IP 透传规则查询需要十秒;
// MBN 配置查询需模块扫描 flash 文件并输出大段列表,一秒预算会假超时,
// 超时后的盲目重发还会把迟到的第一条响应混入后续事务,故放宽到八秒;
// 短信全量列表输出大,追加额外预算;其余命令一秒。
// 组合命令由 atGroupedCommandTimeoutMS 按子命令累加。
func atSingleCommandTimeoutMS(command string) int {
	upper := strings.ToUpper(strings.TrimSpace(command))
	switch {
	case upper == `AT+QMAP="MPDN_RULE",0`:
		return 10000
	case strings.Contains(upper, "QSCAN"):
		return 180000
	case strings.Contains(upper, "NR5G_MEAS_INFO"):
		// 邻区扫描用的 +QNWCFG nr5g_meas_info 测量信息查询为手册未收录命令,
		// 模块整理测量结果需要时间,一秒默认预算会假超时,留足余量。
		return 3000
	case strings.Contains(upper, "+QMBNCFG"):
		return 8000
	case strings.Contains(upper, "+CMGL=4"), strings.Contains(upper, `+CMGL="ALL"`):
		return 1000 + atSMSListExtraTimeoutMS
	default:
		return 1000
	}
}

func runATCommandUntilDone(command string, startTimeoutMS, maxTimeoutMS int) (string, error) {
	_ = startTimeoutMS
	command = sanitizeATCommand(command)
	if command == "" {
		return "", nil
	}
	logATDebug("run AT command: command=%q timeout_ms=%d", command, maxTimeoutMS)
	return runATPayload(command+"\r\n", maxTimeoutMS)
}

func runATCommand(command string, timeoutMS int) (string, error) {
	command = sanitizeATCommand(command)
	if command == "" {
		return "", nil
	}
	return runATPayload(command+"\r\n", timeoutMS)
}

// AT transactions are serialized by a global file lock and a per-device lock.
// The module AT port is accessed directly through /dev/smd11; the runtime does
// not create or rebuild socat, ttyIN, ttyOUT, or cat bridge processes.
var atDeviceLockRegistry = struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}{locks: make(map[string]*sync.Mutex)}

func lockForATDevice(device string) *sync.Mutex {
	atDeviceLockRegistry.mu.Lock()
	defer atDeviceLockRegistry.mu.Unlock()
	if atDeviceLockRegistry.locks == nil {
		atDeviceLockRegistry.locks = make(map[string]*sync.Mutex)
	}
	mu := atDeviceLockRegistry.locks[device]
	if mu == nil {
		mu = &sync.Mutex{}
		atDeviceLockRegistry.locks[device] = mu
	}
	return mu
}

func runATPayload(payload string, timeoutMS int) (string, error) {
	return runATPayloadWaitFor(payload, time.Duration(timeoutMS)*time.Millisecond, "OK", "ERROR")
}

func runATPayloadWaitFor(payload string, timeout time.Duration, terminalTokens ...string) (string, error) {
	unlock, err := lockGlobalATFile()
	if err != nil {
		return "", err
	}
	defer unlock()

	if timeout < 300*time.Millisecond {
		timeout = 300 * time.Millisecond
	}
	return runATPayloadAcrossCandidates(payload, timeout, terminalTokens...)
}

func runATPayloadAcrossCandidates(payload string, timeout time.Duration, terminalTokens ...string) (string, error) {
	devices := atDeviceCandidates()
	logATDebug("AT payload start: %s timeout=%s tokens=%v candidates=%s", atPayloadSummary(payload), timeout, terminalTokens, strings.Join(devices, ","))
	logATDeviceStat("payload candidate", devices)
	if len(devices) == 0 {
		return "", errors.New("no AT device candidates configured")
	}
	if existing := existingATDeviceCandidates(devices); len(existing) > 0 {
		logATDebug("existing AT candidates: %s", strings.Join(existing, ","))
		devices = existing
	} else {
		logATDebug("no configured AT candidate currently exists; trying full candidate list")
	}

	out, err, details, busyOnSMD, retryable := tryATPayloadOnce(devices, payload, timeout, terminalTokens...)
	if err == nil {
		return out, nil
	}

	// 动作类(写/重启/删除)命令超时后可能已在模块上执行,盲目重试会重复执行;
	// 一律不自动重试,由调用方决定。
	if isATActionCommand(payload) {
		return out, fmt.Errorf("all AT device candidates failed (action not retried): %s", strings.Join(details, "; "))
	}

	if busyOnSMD {
		recoverBusyATDevices()
		out, err, retryDetails, _, _ := tryATPayloadOnce(devices, payload, timeout, terminalTokens...)
		if err == nil {
			return out, nil
		}
		return out, fmt.Errorf("all AT device candidates failed after smd recovery retry: %s", strings.Join(retryDetails, "; "))
	}

	if retryable {
		time.Sleep(250 * time.Millisecond)
		out, err, retryDetails, _, _ := tryATPayloadOnce(devices, payload, timeout, terminalTokens...)
		if err == nil {
			return out, nil
		}
		return out, fmt.Errorf("all AT device candidates failed after retry: %s", strings.Join(retryDetails, "; "))
	}

	return out, fmt.Errorf("all AT device candidates failed: %s", strings.Join(details, "; "))
}

func existingATDeviceCandidates(devices []string) []string {
	out := make([]string, 0, len(devices))
	for _, device := range devices {
		if _, err := os.Stat(device); err == nil {
			out = append(out, device)
		}
	}
	return out
}

func tryATPayloadOnce(devices []string, payload string, timeout time.Duration, terminalTokens ...string) (string, error, []string, bool, bool) {
	details := make([]string, 0, len(devices))
	busyOnSMD := false
	retryable := false
	lastOutput := ""

	for _, device := range devices {
		logATDebug("try AT device: %s", device)
		deviceMu := lockForATDevice(device)
		deviceMu.Lock()
		start := time.Now()
		out, err := nativeATWriteRead(device, payload, timeout, terminalTokens...)
		elapsed := time.Since(start)
		deviceMu.Unlock()
		lastOutput = out
		if err != nil {
			logATDebug("device %s failed after %s: err=%v output=%s", device, elapsed, err, outputSummary(out))
		} else {
			logATDebug("device %s success after %s: output=%s", device, elapsed, outputSummary(out))
		}

		if err == nil {
			return out, nil, details, busyOnSMD, retryable
		}

		details = append(details, fmt.Sprintf("%s: %v", device, err))
		if shouldRecoverBusyATDevice(device, err) {
			busyOnSMD = true
		}
		if !shouldTryNextATDevice(err) {
			return out, err, details, busyOnSMD, retryable
		}
		retryable = true
	}
	return lastOutput, errors.New("all AT device candidates failed"), details, busyOnSMD, retryable
}

func nativeATWriteRead(device, payload string, timeout time.Duration, terminalTokens ...string) (string, error) {
	logATDebug("nativeATWriteRead open: device=%s timeout=%s tokens=%v payload=%s", device, timeout, terminalTokens, atPayloadSummary(payload))
	session, err := openATCommandSession(device)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := closeATSession(session); err != nil {
			logATDebug("close AT session %s error: %v", device, err)
		}
	}()

	logATDebug("drain AT device %s before write", device)
	drainATSession(session, 150*time.Millisecond)
	echoMarker := atPayloadEchoMarker(payload)
	logATDebug("write AT payload to %s: %s", device, atPayloadSummary(payload))
	if err := writeATSessionString(session, payload); err != nil {
		return "", fmt.Errorf("write failed: %w", err)
	}

	logATDebug("read AT response from %s", device)
	out, err := readATSessionUntilTokens(session, timeout, echoMarker, terminalTokens...)
	logATDebug("read AT response done from %s: err=%v output=%s", device, err, outputSummary(out))
	if err != nil {
		return out, fmt.Errorf("read failed: %w", err)
	}
	// Swallow any trailing bytes of this transaction so they cannot leak into
	// the next command's captured response.
	drainATSession(session, 80*time.Millisecond)
	if containsAnyToken(out, terminalTokens...) {
		return out, nil
	}
	// 未等到终结 token:模块可能仍在输出本命令的迟到响应。标记通道为脏,
	// 让下一笔事务写前的 drain 用加长窗口吸干净残留字节,防止脏数据污染
	// 后续读取造成级联超时。
	markATSessionDirty(session)
	if strings.TrimSpace(out) != "" {
		return out, fmt.Errorf("timeout waiting for %s", strings.Join(terminalTokens, "/"))
	}
	return "", fmt.Errorf("timeout waiting for %s", strings.Join(terminalTokens, "/"))
}

func shouldTryNextATDevice(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENODEV) ||
		errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EIO) || errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EBUSY) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "no such device") ||
		strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "device or resource busy") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "eof")
}

// errSMSResultUnknown 表示短信正文已写入模块但未等到终结结果(超时或读取
// 异常):短信可能已发送成功,盲目重试会重复发送。携带该错误的失败不做
// 自动重试、不换设备,由调用方决定后续处理。
var errSMSResultUnknown = errors.New("SMS body sent but result unknown (no terminal response)")

func runSMSTransaction(sendCmd, message string) (string, error) {
	unlock, err := lockGlobalATFile()
	if err != nil {
		return "", err
	}
	defer unlock()

	devices := atDeviceCandidates()
	if existing := existingATDeviceCandidates(devices); len(existing) > 0 {
		devices = existing
	}

	out, err, details, busyOnSMD, retryable := trySMSTransactionOnce(devices, sendCmd, message)
	if err == nil {
		return out, nil
	}
	if busyOnSMD {
		recoverBusyATDevices()
		out, err, retryDetails, _, _ := trySMSTransactionOnce(devices, sendCmd, message)
		if err == nil {
			return out, nil
		}
		return out, fmt.Errorf("SMS AT transaction failed after smd recovery retry: %s", strings.Join(retryDetails, "; "))
	}
	if retryable {
		time.Sleep(250 * time.Millisecond)
		out, err, retryDetails, _, _ := trySMSTransactionOnce(devices, sendCmd, message)
		if err == nil {
			return out, nil
		}
		return out, fmt.Errorf("SMS AT transaction failed after retry: %s", strings.Join(retryDetails, "; "))
	}
	// 结果未知(短信可能已发送)是哨兵语义,必须用 %w 保留,调用方靠
	// errors.Is 判定;包装成普通文本错误会丢掉该语义。
	if errors.Is(err, errSMSResultUnknown) {
		return out, fmt.Errorf("SMS AT transaction failed: %s: %w", strings.Join(details, "; "), errSMSResultUnknown)
	}
	return out, fmt.Errorf("SMS AT transaction failed: %s", strings.Join(details, "; "))
}

func trySMSTransactionOnce(devices []string, sendCmd, message string) (string, error, []string, bool, bool) {
	details := make([]string, 0, len(devices))
	busyOnSMD := false
	retryable := false
	lastOutput := ""

	for _, device := range devices {
		deviceMu := lockForATDevice(device)
		deviceMu.Lock()
		out, err := sendSMSOnDevice(device, sendCmd, message)
		deviceMu.Unlock()
		lastOutput = out

		if err == nil {
			return out, nil, details, busyOnSMD, retryable
		}
		details = append(details, fmt.Sprintf("%s: %v", device, err))
		if shouldRecoverBusyATDevice(device, err) {
			busyOnSMD = true
		}
		// 短信正文已发送但结果未知:模块可能已发送成功,换设备或自动重试
		// 都可能造成重复发送,立即返回由调用方决定。
		if errors.Is(err, errSMSResultUnknown) {
			return out, err, details, busyOnSMD, retryable
		}
		if !shouldTryNextATDevice(err) {
			return out, err, details, busyOnSMD, retryable
		}
		retryable = true
	}
	return lastOutput, errors.New("all AT device candidates failed"), details, busyOnSMD, retryable
}

// sendSMSOnDevice 在指定设备上执行一次短信事务。声明为 var 便于测试注入
// 设备行为,运行期行为不变。
var sendSMSOnDevice = func(device, sendCmd, message string) (string, error) {
	session, err := openATCommandSession(device)
	if err != nil {
		return "", err
	}
	defer closeATSession(session)

	drainATSession(session, 150*time.Millisecond)
	// 防御性 ESC:上一次短信事务若异常中断,模块可能滞留在 CMGS ">" 输入态,
	// 会吞掉后续所有 AT 命令;ESC 在命令态下是无害空操作。
	_ = writeATSessionString(session, "\x1B")
	drainATSession(session, 100*time.Millisecond)
	if err := writeATSessionString(session, sendCmd+"\r"); err != nil {
		return "", fmt.Errorf("write CMGS failed: %w", err)
	}
	prompt, err := readATSessionUntilTokens(session, 10*time.Second, "", ">", "ERROR")
	if err != nil {
		abortSMSInputMode(session)
		return prompt, fmt.Errorf("read SMS prompt failed: %w", err)
	}
	if strings.Contains(prompt, "ERROR") {
		return prompt, nil
	}
	if !strings.Contains(prompt, ">") {
		abortSMSInputMode(session)
		return prompt, fmt.Errorf("timeout waiting for SMS prompt")
	}
	if err := writeATSessionString(session, message+"\x1A"); err != nil {
		abortSMSInputMode(session)
		return prompt, fmt.Errorf("write SMS body failed: %w", err)
	}
	body, err := readATSessionUntilTokens(session, 60*time.Second, "", "OK", "ERROR")
	if err != nil {
		abortSMSInputMode(session)
		return prompt + body, fmt.Errorf("%w: %v", errSMSResultUnknown, err)
	}
	// 到点仍未等到终结结果:按"结果未知"处理——既不返回成功(调用方会
	// 误判短信已发送),也不允许自动重试(模块可能已发送成功)。
	if !containsAnyToken(body, "OK", "ERROR") {
		abortSMSInputMode(session)
		return prompt + body, errSMSResultUnknown
	}
	return prompt + body, nil
}

// abortSMSInputMode 在短信事务失败后发送 ESC,让模块退出 CMGS 输入态,
// 避免模块把后续 AT 命令当作短信内容消耗。同时把通道标记为脏:异常中止后
// 模块可能仍在输出迟到内容(发送结果 / CMS ERROR 等),150ms 吸收窗口未必
// 够,下一笔事务写前的 drain 需用加长窗口清理残留,防止脏数据污染后续读取。
func abortSMSInputMode(session *atCommandSession) {
	if session == nil {
		return
	}
	if err := writeATSessionString(session, "\x1B"); err != nil {
		logATDebug("abort SMS input mode write ESC failed: %v", err)
	}
	drainATSession(session, 150*time.Millisecond)
	markATSessionDirty(session)
}

func atDeviceCandidates() []string {
	if len(runtimeATDevices) == 0 {
		return defaultATDeviceCandidates()
	}
	return append([]string(nil), runtimeATDevices...)
}

func collectATDeviceCandidates(flagValue, filePath string) []string {
	var envCandidates []string
	var flagCandidates []string
	var fileCandidates []string
	addTokens := func(dst *[]string, value string) {
		for _, token := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			token = strings.TrimSpace(token)
			if token == "" || strings.HasPrefix(token, "#") {
				continue
			}
			*dst = append(*dst, token)
		}
	}

	addTokens(&envCandidates, os.Getenv("SIMPLEADMIN_AT_DEVICES"))
	addTokens(&flagCandidates, flagValue)
	if filePath != "" {
		if data, err := os.ReadFile(filePath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if idx := strings.Index(line, "#"); idx >= 0 {
					line = strings.TrimSpace(line[:idx])
				}
				addTokens(&fileCandidates, line)
			}
		}
	}

	// Precedence is explicit and non-additive:
	// env overrides CLI, CLI overrides file, file overrides defaults.
	// This keeps a field debug command like --devices /dev/smd11 from silently
	// appending unrelated configured candidates.
	candidates := envCandidates
	if len(candidates) == 0 {
		candidates = flagCandidates
	}
	if len(candidates) == 0 {
		candidates = fileCandidates
	}
	if len(candidates) == 0 {
		candidates = defaultATDeviceCandidates()
	}

	result := filterFixedATDeviceCandidates(uniqueDevicePaths(candidates))
	logATDebug("collect AT candidates: env=%q flag=%q file=%q result=%s", os.Getenv("SIMPLEADMIN_AT_DEVICES"), flagValue, filePath, strings.Join(result, ","))
	return result
}

func defaultATDeviceCandidates() []string {
	return []string{
		"/dev/smd11",
	}
}

func filterFixedATDeviceCandidates(paths []string) []string {
	allowed := make(map[string]struct{}, len(defaultATDeviceCandidates()))
	for _, device := range defaultATDeviceCandidates() {
		allowed[device] = struct{}{}
	}

	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := allowed[path]; ok {
			filtered = append(filtered, path)
		}
	}
	if len(filtered) == 0 {
		return defaultATDeviceCandidates()
	}
	return filtered
}

func uniqueDevicePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !strings.HasPrefix(path, "/dev/") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func containsAnyToken(text string, terminalTokens ...string) bool {
	if len(terminalTokens) == 0 {
		return true
	}
	for _, token := range terminalTokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		upperToken := strings.ToUpper(token)
		if upperToken == "OK" || upperToken == "ERROR" {
			if containsATFinalLine(text, upperToken) {
				return true
			}
			continue
		}
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func containsATFinalLine(text string, token string) bool {
	text = strings.ReplaceAll(text, "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if token == "OK" && upper == "OK" {
			return true
		}
		if token == "ERROR" && (upper == "ERROR" || strings.HasPrefix(upper, "+CME ERROR:") || strings.HasPrefix(upper, "+CMS ERROR:")) {
			return true
		}
	}
	return false
}
