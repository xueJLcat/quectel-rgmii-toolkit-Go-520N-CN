package main

import (
	"testing"
	"time"
)

func TestRecordMetricsHistoryRespectsMinInterval(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	data := map[string]any{
		"cpuUsagePercent":  36,
		"ramUsagePercent":  58,
		"signalPercentage": 86,
		"rsrpLTE":          "-",
		"rsrpNR":           "-86",
		"nr_rx_bytes":      int64(1000),
		"nr_tx_bytes":      int64(500),
	}
	recordMetricsHistoryFromDashboard(data)
	recordMetricsHistoryFromDashboard(data) // 间隔不足,应被丢弃
	recordMetricsHistoryFromDashboard(data)

	points := snapshotMetricsHistory()
	if len(points) != 1 {
		t.Fatalf("points = %d, want 1 (min interval not honored)", len(points))
	}
	p := points[0]
	if p.CPUPercent != 36 || p.RAMPercent != 58 || p.SignalPercent != 86 {
		t.Fatalf("point = %+v, want cpu=36 ram=58 sig=86", p)
	}
	if p.RsrpBest != -86 {
		t.Fatalf("rsrp best = %d, want -86 (NR)", p.RsrpBest)
	}
	if p.RxBytesTotal != 1000 || p.TxBytesTotal != 500 {
		t.Fatalf("rx/tx = %d/%d, want 1000/500", p.RxBytesTotal, p.TxBytesTotal)
	}
}

func TestRecordMetricsHistoryComputesRatesAndCaps(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	base := map[string]any{
		"cpuUsagePercent": 10, "ramUsagePercent": 20, "signalPercentage": 50,
		"rsrpLTE": "-90", "rsrpNR": "-", "nr_rx_bytes": int64(0), "nr_tx_bytes": int64(0),
	}
	recordMetricsHistoryFromDashboard(base)

	// 伪造上一条采样时间为 60 秒前,使速率计算与间隔判断成立。
	metricsHistory.mu.Lock()
	metricsHistory.last = time.Now().Add(-70 * time.Second)
	metricsHistory.points[0].Timestamp = time.Now().Add(-70 * time.Second).Unix()
	metricsHistory.mu.Unlock()

	next := map[string]any{
		"cpuUsagePercent": 12, "ramUsagePercent": 22, "signalPercentage": 60,
		"rsrpLTE": "-85", "rsrpNR": "-", "nr_rx_bytes": int64(6000), "nr_tx_bytes": int64(3000),
	}
	recordMetricsHistoryFromDashboard(next)

	points := snapshotMetricsHistory()
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	lastPoint := points[1]
	if lastPoint.RsrpBest != -85 {
		t.Fatalf("rsrp best = %d, want -85 (LTE)", lastPoint.RsrpBest)
	}
	if lastPoint.RxRateBytesSec <= 0 || lastPoint.TxRateBytesSec <= 0 {
		t.Fatalf("rates = %d/%d, want > 0", lastPoint.RxRateBytesSec, lastPoint.TxRateBytesSec)
	}
}

func TestMetricsHistoryRingBufferCapsAtCapacity(t *testing.T) {
	resetMetricsHistoryForTest()
	defer resetMetricsHistoryForTest()

	metricsHistory.mu.Lock()
	for i := 0; i < metricsHistoryCapacity; i++ {
		metricsHistory.points = append(metricsHistory.points, metricsHistoryPoint{Timestamp: int64(i)})
	}
	metricsHistory.last = time.Now().Add(-2 * time.Minute)
	metricsHistory.mu.Unlock()

	recordMetricsHistoryFromDashboard(map[string]any{"signalPercentage": 99})

	metricsHistory.mu.Lock()
	count := len(metricsHistory.points)
	newest := metricsHistory.points[count-1]
	metricsHistory.mu.Unlock()
	if count != metricsHistoryCapacity {
		t.Fatalf("capacity = %d, want capped at %d", count, metricsHistoryCapacity)
	}
	if newest.SignalPercent != 99 {
		t.Fatalf("newest sig = %d, want 99", newest.SignalPercent)
	}
}
