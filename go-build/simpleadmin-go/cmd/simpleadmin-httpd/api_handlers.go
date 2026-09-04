package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *simpleAdminServer) handleGetATCommand(w http.ResponseWriter, r *http.Request) {
	s.handleGetATCache(w, r)
}

func (s *simpleAdminServer) handleGetPing(w http.ResponseWriter, r *http.Request) {
	if s.cfg.mockMode {
		writeText(w, http.StatusOK, "OK")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if nativePing(ctx) {
		writeText(w, http.StatusOK, "OK")
		return
	}
	writeText(w, http.StatusOK, "ERROR")
}

func (s *simpleAdminServer) handleGetSMS(w http.ResponseWriter, r *http.Request) {
	atCommandCache.Start(s.cfg.mockMode)
	writeText(w, http.StatusOK, atCommandCache.Fetch(smsListATCommand(), boolQuery(r, "force", false), nil))
}

func (s *simpleAdminServer) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	phoneNumber := sanitizeATCommand(requestValue(r, "number"))
	message := strings.TrimSpace(requestValue(r, "msg"))
	if phoneNumber == "" || message == "" {
		writeText(w, http.StatusOK, "missing number or msg")
		return
	}
	result := s.sendSMSBusiness(phoneNumber, message)
	if ok, _ := result["ok"].(bool); !ok {
		writeText(w, http.StatusBadRequest, fmt.Sprint(result["error"]))
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("OK segments=%v number=%v", result["segments"], result["number"]))
}
func (s *simpleAdminServer) handleGetTTLStatus(w http.ResponseWriter, r *http.Request) {
	value, enabled := currentTTLValue()
	writeJSON(w, http.StatusOK, map[string]any{"isEnabled": enabled, "ttl": value})
}

func (s *simpleAdminServer) handleSetTTL(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ttlvalue")
	ttl, err := strconv.Atoi(stripNonDigits(raw))
	if err != nil || ttl < 0 || ttl > 255 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid ttlvalue", "debug_logs": []string{"invalid ttlvalue"}})
		return
	}

	logs := []string{fmt.Sprintf("Received parameter: ttlvalue=%d", ttl)}
	var ok bool
	if s.cfg.mockMode {
		var mockLogs []string
		mockLogs, ok = mockSetNativeTTL(ttl)
		logs = append(logs, mockLogs...)
	} else {
		var applyLogs []string
		applyLogs, ok = setNativeTTLWithStatus(ttl)
		logs = append(logs, applyLogs...)
	}
	// 应用失败如实上报:旧实现恒返回 200 且无成败标志,前端只能显示"已保存",
	// 用户无从得知 iptables 规则其实没有生效。
	result := map[string]any{"ok": ok, "debug_logs": logs}
	if !ok {
		result["error"] = "TTL 规则应用失败"
	}
	writeJSON(w, http.StatusOK, result)
}

// mockSetNativeTTL 在 mock 模式下模拟 TTL 应用(不执行任何内核命令),
// 并按与真实路径一致的逻辑持久化配置值;返回日志与应用成败。
func mockSetNativeTTL(value int) ([]string, bool) {
	logs := []string{"Mock mode: TTL rules are simulated only"}
	if value <= 0 {
		logs = append(logs, "TTL disabled")
	} else {
		logs = append(logs, fmt.Sprintf("TTL mock enabled with value: %d", value))
	}
	if err := writeTTLValue(value); err != nil {
		logs = append(logs, "failed to write ttlvalue: "+err.Error())
		return logs, false
	}
	logs = append(logs, "TTL value saved")
	return logs, true
}

func (s *simpleAdminServer) handleGetUptime(w http.ResponseWriter, r *http.Request) {
	if s.cfg.mockMode {
		writeText(w, http.StatusOK, mockUptimeText())
		return
	}
	writeText(w, http.StatusOK, nativeUptimeText())
}

// handleHistoryData 返回信号/资源/流量历史采样,供首页趋势图渲染。
func (s *simpleAdminServer) handleHistoryData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"intervalSeconds": int(metricsHistoryMinInterval / time.Second),
		"points":          snapshotMetricsHistory(),
	})
}

func (s *simpleAdminServer) handleGetWatchdog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentWatchdogStatus())
}

func (s *simpleAdminServer) handleSetWatchdog(w http.ResponseWriter, r *http.Request) {
	cfg := readWatchdogConfig()
	if len(requestValues(r, "enabled")) > 0 {
		cfg.Enabled = boolQuery(r, "enabled", cfg.Enabled)
	}
	if value := strings.TrimSpace(requestValue(r, "failThreshold")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= 60 {
			cfg.FailThreshold = n
		}
	}
	if value := strings.TrimSpace(requestValue(r, "cooldownMinutes")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= 1440 {
			cfg.CooldownMinutes = n
		}
	}
	if value := strings.TrimSpace(requestValue(r, "checkInterval")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= watchdogMaxCheckIntervalMinutes {
			cfg.CheckIntervalMinutes = n
		}
	}
	// 探测目标允许提交空值(清空自定义,回退内置目标),按参数存在性判断。
	if len(requestValues(r, "targets")) > 0 {
		cfg.Targets = parseWatchdogTargets(requestValue(r, "targets"))
	}
	if value := strings.TrimSpace(requestValue(r, "actionPolicy")); value != "" {
		if value == watchdogPolicyReboot || value == watchdogPolicyEscalate {
			cfg.ActionPolicy = value
		}
	}
	if err := writeWatchdogConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 轮询器按配置启停:禁用时不驻留循环,启用后立即开始轮询。
	syncWatchdogPoller(s)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": cfg})
}

func (s *simpleAdminServer) handleGetScheduler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentSchedulerStatus())
}

func (s *simpleAdminServer) handleGetTimeSync(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentTimeSyncStatus())
}

func (s *simpleAdminServer) handleSetTimeSync(w http.ResponseWriter, r *http.Request) {
	cfg := readTimeSyncConfig()
	if len(requestValues(r, "enabled")) > 0 {
		cfg.Enabled = boolQuery(r, "enabled", cfg.Enabled)
	}
	if value := strings.TrimSpace(requestValue(r, "intervalMinutes")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n >= timeSyncMinIntervalMinutes && n <= timeSyncMaxIntervalMinutes {
			cfg.IntervalMinutes = n
		}
	}
	// 按参数存在性判断:提交空服务器视为重置回缺省源(与短信转发 url 同语义)。
	if len(requestValues(r, "server")) > 0 {
		cfg.Server = normalizeTimeSyncServer(requestValue(r, "server"))
	}
	if err := writeTimeSyncConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 轮询器按配置启停:禁用时不驻留循环,启用后按新间隔轮询。
	syncTimeSyncPoller(s)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": cfg})
}

// handleTimeSyncNow 手动同步一次:立即从配置的单一 NTP 源查询并校正系统时间。
func (s *simpleAdminServer) handleTimeSyncNow(w http.ResponseWriter, r *http.Request) {
	cfg := readTimeSyncConfig()
	writeJSON(w, http.StatusOK, runTimeSyncOnce(s, cfg.Server))
}

func (s *simpleAdminServer) handleSetScheduler(w http.ResponseWriter, r *http.Request) {
	cfg := readSchedulerConfig()
	if len(requestValues(r, "rebootEnabled")) > 0 {
		cfg.RebootEnabled = boolQuery(r, "rebootEnabled", cfg.RebootEnabled)
	}
	if value := strings.TrimSpace(requestValue(r, "rebootTime")); value != "" {
		cfg.RebootTime = value
	}
	cfg.RebootTime = normalizeSchedulerRebootTime(cfg.RebootTime)
	if err := writeSchedulerConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": cfg})
}

func (s *simpleAdminServer) handleGetSMSWebhook(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentSMSWebhookStatus())
}

func (s *simpleAdminServer) handleSetSMSWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, _ := readSMSWebhookConfig()
	if len(requestValues(r, "enabled")) > 0 {
		cfg.Enabled = boolQuery(r, "enabled", cfg.Enabled)
	}
	if value, ok := r.URL.Query()["url"]; ok {
		cfg.URL = strings.TrimSpace(value[0])
	} else if len(requestValues(r, "url")) > 0 {
		cfg.URL = strings.TrimSpace(requestValue(r, "url"))
	}
	if err := writeSMSWebhookConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 按新配置启停后台轮询器：启用即开始周期检测，关闭立即停止。
	syncSMSWebhookPoller(s)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func currentTTLValue() (int, bool) {
	v, err := readTTLValue()
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// dialAnyTarget 对目标列表逐个发起 TCP 拨号,任一成功即返回 true。
// 供 /api/get_ping、总览探测与看门狗联网检测共用。
func dialAnyTarget(ctx context.Context, targets []string) bool {
	for _, address := range targets {
		d := net.Dialer{Timeout: dialTargetTimeout}
		conn, err := d.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func nativePing(ctx context.Context) bool {
	return dialAnyTarget(ctx, watchdogDefaultTargets)
}

func nativeUptimeText() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "up unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "up unknown"
	}
	secondsFloat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "up unknown"
	}
	totalMinutes := int(secondsFloat) / 60
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	return fmt.Sprintf("up %d day, %d hour, %d min", days, hours, minutes)
}
