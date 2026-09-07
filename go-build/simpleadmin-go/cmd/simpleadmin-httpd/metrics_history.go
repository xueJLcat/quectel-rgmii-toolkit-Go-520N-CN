package main

import (
	"sync"
	"time"
)

// metricsHistoryPoint 是单条历史采样,字段保持紧凑以控制 JSON 体积。
// Timestamp 为 Unix 秒;速率字段为两次采样间的增量换算出的平均 bytes/s。
// 数值字段不加 omitempty:记录的都是"就绪"的真实采样,0 是合法观测值
// (空闲期速率 0、无信号期信号 0%),omitempty 会把它们从 JSON 中删掉,
// 前端把"字段缺失"映射为 null,趋势图上合法零点变成断线缺口,与 meta
// 行"0 B/s"的显示自相矛盾,"信号掉到 0"这一关键事实不可见。
type metricsHistoryPoint struct {
	Timestamp      int64 `json:"t"`
	CPUPercent     int   `json:"cpu"`
	RAMPercent     int   `json:"ram"`
	SignalPercent  int   `json:"sig"`
	RsrpBest       int   `json:"rsrp"`
	RxBytesTotal   int64 `json:"rx"`
	TxBytesTotal   int64 `json:"tx"`
	RxRateBytesSec int64 `json:"rxr"`
	TxRateBytesSec int64 `json:"txr"`
}

const (
	// 采样最小间隔与保留时长:每分钟一条,保留 24 小时(1440 点)。
	metricsHistoryMinInterval = 60 * time.Second
	metricsHistoryCapacity    = 1440
)

// metricsHistory 是进程内环形缓冲。仅内存保存,重启清零;
// 采样来源是首页数据解析成功后的回调,页面打开期间自动积累。
var metricsHistory = struct {
	mu     sync.Mutex
	points []metricsHistoryPoint
	last   time.Time
}{points: make([]metricsHistoryPoint, 0, metricsHistoryCapacity)}

// recordMetricsHistoryFromDashboard 从首页结构化数据中提取采样点。
// 两次采样间隔不足最小间隔时跳过,避免高频轮询产生密集点。
func recordMetricsHistoryFromDashboard(data map[string]any) {
	now := time.Now()

	metricsHistory.mu.Lock()
	defer metricsHistory.mu.Unlock()
	if !metricsHistory.last.IsZero() && now.Sub(metricsHistory.last) < metricsHistoryMinInterval {
		return
	}

	point := metricsHistoryPoint{Timestamp: now.Unix()}
	point.CPUPercent = percentFromAny(data["cpuUsagePercent"])
	point.RAMPercent = percentFromAny(data["ramUsagePercent"])
	point.SignalPercent = percentFromAny(data["signalPercentage"])
	point.RsrpBest = bestRsrpFromDashboard(data)
	point.RxBytesTotal = int64FromAny(data["nr_rx_bytes"])
	point.TxBytesTotal = int64FromAny(data["nr_tx_bytes"])

	if n := len(metricsHistory.points); n > 0 {
		prev := metricsHistory.points[n-1]
		// 向前断层上限 = 缓冲保留期(1440 点 × 1 分钟 = 24 小时):相邻采样
		// 的墙钟间隔超过保留期,要么时钟被向前步进(冷启动校时,1970/构建
		// 日期跳到真实年代可达数十年),要么是超出保留承诺的陈旧会话残留。
		// 两种情形下旧时间轴都已失效:继续追加会让趋势图 x 轴横跨两个年代
		// (有效数据全部挤压在右缘,桥接点速率恒 0),且旧年代点要按每分钟
		// 一条的节奏 24 小时后才被挤出。与向后步进同款处理:丢弃重新开始。
		maxGapSeconds := int64((metricsHistoryCapacity * metricsHistoryMinInterval) / time.Second)
		if point.Timestamp <= prev.Timestamp || point.Timestamp-prev.Timestamp > maxGapSeconds {
			// 时钟被向后步进(NTP 校时/故障)时继续追加还会产生时间倒序的
			// 点(趋势图 x 轴回折)。采样间隔闸门走单调钟不受影响,
			// 下一分钟即恢复记录。
			metricsHistory.points = metricsHistory.points[:0]
			metricsHistory.points = append(metricsHistory.points, point)
			metricsHistory.last = now
			return
		}
		elapsed := point.Timestamp - prev.Timestamp
		if elapsed > 0 {
			if dRx := point.RxBytesTotal - prev.RxBytesTotal; dRx > 0 {
				point.RxRateBytesSec = dRx / elapsed
			}
			if dTx := point.TxBytesTotal - prev.TxBytesTotal; dTx > 0 {
				point.TxRateBytesSec = dTx / elapsed
			}
		}
	}

	if len(metricsHistory.points) >= metricsHistoryCapacity {
		metricsHistory.points = append(metricsHistory.points[1:], point)
	} else {
		metricsHistory.points = append(metricsHistory.points, point)
	}
	metricsHistory.last = now
}

func bestRsrpFromDashboard(data map[string]any) int {
	lte := toInt(stringValue(data["rsrpLTE"]))
	nr := toInt(stringValue(data["rsrpNR"]))
	// RSRP 为负 dBm 值;0 表示该制式无数据,不参与比较。
	switch {
	case lte == 0:
		return nr
	case nr == 0:
		return lte
	default:
		if nr > lte {
			return nr
		}
		return lte
	}
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	default:
		return 0
	}
}

// snapshotMetricsHistory 返回按时间升序的采样副本。
func snapshotMetricsHistory() []metricsHistoryPoint {
	metricsHistory.mu.Lock()
	defer metricsHistory.mu.Unlock()
	out := make([]metricsHistoryPoint, len(metricsHistory.points))
	copy(out, metricsHistory.points)
	return out
}

func resetMetricsHistoryForTest() {
	metricsHistory.mu.Lock()
	metricsHistory.points = metricsHistory.points[:0]
	metricsHistory.last = time.Time{}
	metricsHistory.mu.Unlock()
}
