package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const (
	defaultSchedulerRebootTime = "04:00"
	schedulerCheckInterval     = 30 * time.Second
	schedulerRebootCommand     = "AT+CFUN=1,1"
	// schedulerCatchUpMaxAge 是补跑判定可接受的 LastCheck 最大陈旧度。
	// 设备无电池时钟,开机初期系统时间是错误值(如 1970 年),此时写入的
	// LastCheck 不代表真实时间轴;NTP 校时把时钟向前步进数年后,若无此
	// 护栏,陈旧的 LastCheck 会让"补跑"分支误判进程错过了当天设定时刻,
	// 设备在开机约 1 分钟后无预警重启。合法补跑场景(当天/前一天错过
	// 设定时刻)的 LastCheck 距 target 不超过 24 小时左右,48 小时窗口
	// 足够宽松;更陈旧的记录一律按"无历史记录"处理,不补跑。
	schedulerCatchUpMaxAge = 48 * time.Hour
)

var schedulerTimeRegexp = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// 可在测试中替换：当前时间与重启动作均可注入，避免触发真实重启。
var schedulerNow = time.Now
var schedulerRunReboot = func(s *simpleAdminServer) string {
	return s.runPageAction(schedulerRebootCommand)
}

type schedulerConfig struct {
	RebootEnabled bool   `json:"rebootEnabled"`
	RebootTime    string `json:"rebootTime"`
}

var (
	schedulerMu          sync.Mutex
	schedulerLastRunDate string
)

func schedulerConfigPath() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), "scheduler.conf")
}

// schedulerState 持久化的调度运行状态，写入运行期配置目录（与 scheduler.conf
// 同目录，即 runtimeTTLValueFile 所在目录）下的 scheduler.state：
// LastRunDate 为上次触发重启的日期（设备本地时间），供进程重启后当日去重；
// LastCheck 为上次调度检查时刻（RFC3339），用于判定是否真实错过了设定时刻。
type schedulerState struct {
	LastRunDate string `json:"lastRunDate"`
	LastCheck   string `json:"lastCheck"`
}

func schedulerStatePath() string {
	return filepath.Join(filepath.Dir(runtimeTTLValueFile), "scheduler.state")
}

func readSchedulerState() schedulerState {
	data, err := os.ReadFile(schedulerStatePath())
	if err != nil {
		return schedulerState{}
	}
	var st schedulerState
	if err := json.Unmarshal(data, &st); err != nil {
		return schedulerState{}
	}
	return st
}

// writeSchedulerState 原子写状态:撕裂后读回零值会丢失"当日已执行定时重启"
// 的去重依据,进程/设备重启后同一自然日可能二次触发重启。
func writeSchedulerState(st schedulerState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return atomicWriteFile(schedulerStatePath(), data, 0600)
}

// schedulerTargetTime 由 "HH:MM" 配置计算 now 所在自然日的目标时刻，
// 使用设备本地时间（now 的时区）。
func schedulerTargetTime(rebootTime string, now time.Time) (time.Time, bool) {
	var hour, minute int
	if _, err := fmt.Sscanf(rebootTime, "%d:%d", &hour, &minute); err != nil {
		return time.Time{}, false
	}
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()), true
}

func defaultSchedulerConfig() schedulerConfig {
	return schedulerConfig{RebootEnabled: false, RebootTime: defaultSchedulerRebootTime}
}

// normalizeSchedulerRebootTime 校验定时重启时刻,非法值回退缺省时间。
func normalizeSchedulerRebootTime(value string) string {
	if schedulerTimeRegexp.MatchString(value) {
		return value
	}
	return defaultSchedulerRebootTime
}

func readSchedulerConfig() schedulerConfig {
	cfg := defaultSchedulerConfig()
	data, err := os.ReadFile(schedulerConfigPath())
	if err != nil {
		return cfg
	}
	var parsed schedulerConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return cfg
	}
	cfg.RebootEnabled = parsed.RebootEnabled
	if schedulerTimeRegexp.MatchString(parsed.RebootTime) {
		cfg.RebootTime = parsed.RebootTime
	}
	return cfg
}

func writeSchedulerConfig(cfg schedulerConfig) error {
	if !schedulerTimeRegexp.MatchString(cfg.RebootTime) {
		cfg.RebootTime = defaultSchedulerRebootTime
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	// 原子写避免掉电撕裂配置(半截 JSON 回退缺省会悄悄关闭定时重启)。
	return atomicWriteFile(schedulerConfigPath(), data, 0600)
}

func startScheduler(s *simpleAdminServer) {
	go func() {
		for {
			schedulerCheckOnce(s, schedulerNow())
			time.Sleep(schedulerCheckInterval)
		}
	}()
}

// schedulerRememberCheck 把本次检查时刻写入状态文件，作为"进程存活"证据供补跑判定。
// 写入按自然分钟节流，避免每次轮询都落盘。
func schedulerRememberCheck(now time.Time) {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	st := readSchedulerState()
	if last, err := time.Parse(time.RFC3339, st.LastCheck); err == nil &&
		last.Format("2006-01-02T15:04") == now.Format("2006-01-02T15:04") {
		return
	}
	st.LastCheck = now.Format(time.RFC3339)
	if err := writeSchedulerState(st); err != nil {
		log.Printf("scheduler: 写入状态文件失败: %v", err)
	}
}

// schedulerCheckOnce 执行一次调度判断，返回是否触发了重启。
// 配置每次重新读取；同一自然日（设备本地时间）内最多触发一次，去重日期
// 持久化于 scheduler.state，进程重启后当日不会重复触发。
//
// 触发条件（补跑语义）：当前时间 >= 当天设定时刻 且 今天尚未执行。
// 其中"已过设定时刻"的补跑额外要求状态文件中的上次检查时刻早于设定时刻
// 且陈旧度不超过 schedulerCatchUpMaxAge(防开机错误时钟污染,见该常量注释)，
// 以证明进程在设定时刻前已在运行、确实错过了该窗口（含进程当时未运行的情形），
// 避免无历史记录时（如首次启动即已过时刻）误补跑；当前分钟恰为设定时刻时
// 直接触发（保留原行为）。时间一律使用设备本地时间。
//
// 先执行后记录：到达触发时刻先执行重启命令,用 atResponseRebootOK 判定响应,
// 判定通过(无终结错误行)才写 LastRunDate 并返回 true;执行失败(出现终结
// 错误行)不算触发、不写 LastRunDate,下一轮调度可重试。
func schedulerCheckOnce(s *simpleAdminServer, now time.Time) bool {
	cfg := readSchedulerConfig()
	if !cfg.RebootEnabled {
		schedulerRememberCheck(now)
		return false
	}
	target, ok := schedulerTargetTime(cfg.RebootTime, now)
	if !ok {
		schedulerRememberCheck(now)
		return false
	}

	today := now.Format("2006-01-02") // 设备本地时间的自然日
	st := readSchedulerState()

	schedulerMu.Lock()
	if st.LastRunDate > schedulerLastRunDate {
		// 进程重启后内存去重状态丢失，从持久化状态恢复。
		schedulerLastRunDate = st.LastRunDate
	}
	if schedulerLastRunDate == today {
		schedulerMu.Unlock()
		schedulerRememberCheck(now)
		return false
	}

	triggered := false
	if !now.Before(target) {
		switch {
		case now.Format("15:04") == cfg.RebootTime:
			triggered = true
		default:
			if last, err := time.Parse(time.RFC3339, st.LastCheck); err == nil &&
				last.Before(target) && last.After(target.Add(-schedulerCatchUpMaxAge)) {
				triggered = true // 补跑：错过了设定时刻窗口
			}
		}
	}
	if !triggered {
		schedulerMu.Unlock()
		schedulerRememberCheck(now)
		return false
	}
	schedulerMu.Unlock()

	// 先执行、后判定,判定通过才持久化"已触发":重启命令是"模块先重启、
	// 后应答",超时无应答是常态;只有模块给出明确终结错误行
	// (ERROR/+CME ERROR/+CMS ERROR)才能断定重启未发生。因此按
	// atResponseRebootOK 判定(语义与依据见 at_parse_util.go 注释):
	//   - 出现终结错误行 -> 执行失败,不写 LastRunDate,下一轮调度可重试,
	//     避免先记"已触发"导致当日定时重启静默错过;
	//   - 无终结错误(含超时无应答的重启常态) -> 视为已触发,写
	//     LastRunDate,当日不再重复触发。
	log.Printf("scheduler: daily reboot triggering at %s %s", today, cfg.RebootTime)
	resp := schedulerRunReboot(s)
	if !atResponseRebootOK(resp) {
		// 失败不更新 LastCheck:补跑判定依赖 "LastCheck 早于设定时刻"。
		// 若此处把 LastCheck 写到失败时刻(已晚于设定时刻),离开设定分钟
		// 后补跑分支永不再命中,失败的重启会被静默推迟到次日;保持旧值,
		// 下一轮(30 秒后)即可按补跑语义重试。
		log.Printf("scheduler: daily reboot failed (terminal error), will retry later; response: %s", outputSummary(resp))
		return false
	}

	schedulerMu.Lock()
	schedulerLastRunDate = today
	schedulerMu.Unlock()

	st.LastRunDate = today
	st.LastCheck = now.Format(time.RFC3339)
	if err := writeSchedulerState(st); err != nil {
		log.Printf("scheduler: 写入状态文件失败: %v", err)
	}
	log.Printf("scheduler: daily reboot triggered at %s %s, response: %s", today, cfg.RebootTime, outputSummary(resp))
	return true
}

func currentSchedulerStatus() map[string]any {
	cfg := readSchedulerConfig()
	schedulerMu.Lock()
	lastRebootDate := schedulerLastRunDate
	schedulerMu.Unlock()
	return map[string]any{
		"rebootEnabled":  cfg.RebootEnabled,
		"rebootTime":     cfg.RebootTime,
		"lastRebootDate": lastRebootDate,
	}
}
