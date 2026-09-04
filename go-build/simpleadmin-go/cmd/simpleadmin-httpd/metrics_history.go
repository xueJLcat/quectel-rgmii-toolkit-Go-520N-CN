package main

import (
	"sync"
	"time"
)

// metricsHistoryPoint 是单条历史采样,字段保持紧凑以控制 JSON 体积。
// Timestamp 为 Unix 秒;速率字段为两次采样间的增量换算出的平均 bytes/s。
type metricsHistoryPoint struct {
	Timestamp      int64 `json:"t"`
	CPUPercent     int   `json:"cpu,omitempty"`
	RAMPercent     int   `json:"ram,omitempty"`
	SignalPercent  int   `json:"sig,omitempty"`
	RsrpBest       int   `json:"rsrp,omitempty"`
	RxBytesTotal   int64 `json:"rx,omitempty"`
	TxBytesTotal   int64 `json:"tx,omitempty"`
	RxRateBytesSec int64 `json:"rxr,omitempty"`
	TxRateBytesSec int64 `json:"txr,omitempty"`
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
