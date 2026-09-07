package main

// 第六轮功能审查回归测试:每个测试对应本轮修复的一个功能性缺陷,
// 防止后续迭代回退。缺陷编号 R6-Bxx 与修复说明一一对应。

import (
	"net"
	"strings"
	"testing"
	"time"
)

// ---------- R6-B1: 流量计数器字段序按官方手册(<bytes_sent>,<bytes_recv>) ----------

// TestParseDashboardCounterFieldOrderMatchesManual 两个计数器均为 sent 在前
// (RG520N 手册 §9.7 与 EC2x 手册 §10.15);实机抓取样例(main_test.go 真机
// transcript)满足"总量逐方向 ≥ NR 分量"不变量。旧实现把 QGDNRCNT parts[0]
// 当接收字节,仪表盘上下行恰好颠倒。
func TestParseDashboardCounterFieldOrderMatchesManual(t *testing.T) {
	raw := "+QSIMSTAT: 0,1\r\n+QGDNRCNT: 1961485,17217894\r\n+QGDCNT: 1979589,17236170\r\nOK"
	data := parseDashboardAT(raw)
	// QGDNRCNT: sent=1961485, recv=17217894;QGDCNT: sent=1979589, recv=17236170。
	// 逐方向 max 合并后:tx=1979589(总量), rx=17236170(总量)。
	if data["nr_tx_bytes"] != int64(1979589) {
		t.Fatalf("nr_tx_bytes = %v, want 1979589(sent 在前)", data["nr_tx_bytes"])
	}
	if data["nr_rx_bytes"] != int64(17236170) {
		t.Fatalf("nr_rx_bytes = %v, want 17236170(recv 在后)", data["nr_rx_bytes"])
	}
	// 下载 > 上传,符合 CPE 真实流量画像(若字段序颠倒此处会反转)。
	rx, _ := data["nr_rx_bytes"].(int64)
	tx, _ := data["nr_tx_bytes"].(int64)
	if rx <= tx {
		t.Fatalf("实机样例应呈下载主导: rx=%d tx=%d", rx, tx)
	}
}

// ---------- R6-B2: 冷启动大偏差不得被 maxStep 拒绝 ----------

func TestRunTimeSyncOnceColdBootHugeOffsetAllowed(t *testing.T) {
	withTempTimeSyncEnv(t)
	// 模拟无电池时钟冷启动:本地时钟早于合理下界(把下界推到未来)。
	oldFloor := timeSyncMinPlausibleTime
	timeSyncMinPlausibleTime = time.Now().Add(time.Hour)
	t.Cleanup(func() { timeSyncMinPlausibleTime = oldFloor })

	hugeOffset := 50 * 365 * 24 * time.Hour // 1970 → 2020 量级
	queries, applied := withFakeTimeSync(t, hugeOffset, nil, nil)
	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("冷启动大偏差校时被拒绝(功能在其主场景失效): %v", result)
	}
	if *queries != 1 || applied.IsZero() {
		t.Fatalf("queries=%d applied=%v, want 校时已写入", *queries, applied)
	}
}

func TestRunTimeSyncOnceRejectsHugeRoundTrip(t *testing.T) {
	withTempTimeSyncEnv(t)
	oldQuery, oldApply := timeSyncQuery, timeSyncApply
	t.Cleanup(func() { timeSyncQuery, timeSyncApply = oldQuery, oldApply })
	applied := false
	timeSyncQuery = func(server string, timeout time.Duration) (ntpResult, error) {
		// 蜂窝链路严重拥塞样本:偏差很小但往返时延超上限,不可信。
		return ntpResult{Offset: 100 * time.Millisecond, RoundTrip: timeSyncMaxRoundTrip + time.Second, ServerTime: time.Now()}, nil
	}
	timeSyncApply = func(now time.Time) error {
		applied = true
		return nil
	}
	result := runTimeSyncOnce(nil, "ntp.aliyun.com")
	if ok, _ := result["ok"].(bool); ok {
		t.Fatalf("超大往返时延样本应被丢弃: %v", result)
	}
	if errText, _ := result["error"].(string); !strings.Contains(errText, "round trip") {
		t.Fatalf("error = %v, want round trip too large", result["error"])
	}
	if applied {
		t.Fatalf("被丢弃的样本不得写入系统时钟")
	}
}

// ---------- R6-B3: NTP 应答合法性(t3≥t2、LI=3 拒绝) ----------

func TestParseNTPResponseRejectsInconsistentAndUnsynchronized(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0)
	t4 := t1.Add(60 * time.Millisecond)
	// t3 早于 t2:服务器时钟异常或报文损坏,负处理耗时会给 θ 混入伪偏差。
	bad := buildNTPResponse(t, 0x24, 2, t1.Add(2*time.Second), t1.Add(1*time.Second))
	if _, err := parseNTPResponse(bad, t1, t4); err == nil {
		t.Fatalf("t3<t2 的应答未被拒绝")
	}
	// LI=3(alarm):服务器自身未同步,应答不可用于校时。
	li3 := buildNTPResponse(t, 0x24|0xC0, 2, t1.Add(time.Second), t1.Add(time.Second))
	if _, err := parseNTPResponse(li3, t1, t4); err == nil || !strings.Contains(err.Error(), "LI=3") {
		t.Fatalf("LI=3 应答未被拒绝: %v", err)
	}
	// 正常应答不受影响。
	good := buildNTPResponse(t, 0x24, 2, t1.Add(time.Second), t1.Add(time.Second))
	if _, err := parseNTPResponse(good, t1, t4); err != nil {
		t.Fatalf("正常应答被误拒: %v", err)
	}
}

// ---------- R6-B4: 失败同步不得覆写上次成功的偏差 ----------

func TestTimeSyncFailureKeepsLastOffset(t *testing.T) {
	withTempTimeSyncEnv(t)
	withFakeTimeSync(t, 500*time.Millisecond, nil, nil)
	if result := runTimeSyncOnce(nil, "ntp.aliyun.com"); result["ok"] != true {
		t.Fatalf("首次同步应成功: %v", result)
	}
	if status := currentTimeSyncStatus(); status["lastOffsetMs"] != int64(500) {
		t.Fatalf("lastOffsetMs = %v, want 500", status["lastOffsetMs"])
	}
	withFakeTimeSync(t, 0, errTimeSyncTestQuery, nil)
	if result := runTimeSyncOnce(nil, "ntp.aliyun.com"); result["ok"] != false {
		t.Fatalf("第二次同步应失败: %v", result)
	}
	// 失败尝试不得把"最近成功同步"的偏差覆写成 0(与 lastSyncTime 同语义)。
	if status := currentTimeSyncStatus(); status["lastOffsetMs"] != int64(500) {
		t.Fatalf("失败后 lastOffsetMs = %v, want 保留成功值 500", status["lastOffsetMs"])
	}
}

var errTimeSyncTestQuery = &round6TestError{"no route to host"}

type round6TestError struct{ msg string }

func (e *round6TestError) Error() string { return e.msg }

// ---------- R6-B5: at-client 恰好达上限的合法响应不判截断 ----------

func TestATClientExchangeExactLimitResponseNotTruncated(t *testing.T) {
	sock := round6UnixSock(t)
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = readOneLine(conn)
		// JSON 响应体恰好 atClientMaxResponseBytes 字节 + 结尾换行(共 N+1):
		// 旧实现 LimitReader(N) 读不到换行,把该合法边界响应误判为截断。
		body := strings.Repeat("y", int(round6SavedLimit))
		_, _ = conn.Write([]byte(body + "\n"))
	}()

	raw, err := atClientExchange(sock, []byte(`{"command":"ATI"}`+"\n"))
	if err != nil {
		t.Fatalf("恰好达上限的响应被误判: %v", err)
	}
	if int64(len(raw)) != round6SavedLimit+1 {
		t.Fatalf("响应长度 = %d, want %d", len(raw), round6SavedLimit+1)
	}
}

var round6SavedLimit int64 = 64

func round6UnixSock(t *testing.T) string {
	t.Helper()
	original := atClientMaxResponseBytes
	atClientMaxResponseBytes = round6SavedLimit
	t.Cleanup(func() { atClientMaxResponseBytes = original })
	return t.TempDir() + "/at_exact.sock"
}

func readOneLine(conn net.Conn) (string, error) {
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	return string(buf[:n]), err
}

// ---------- R6-B6: dnsmasq 无限租约(expiry=0)不得当已到期丢弃 ----------

func TestParseDnsmasqLeasesKeepsInfiniteLease(t *testing.T) {
	now := time.Unix(1000000, 0)
	content := strings.Join([]string{
		"0 aa:bb:cc:dd:ee:01 192.168.5.9 static-host *",   // dnsmasq 无限租约写法
		"999999 aa:bb:cc:dd:ee:02 192.168.5.10 expired *", // 真过期,仍须丢弃
	}, "\n")
	got := parseDnsmasqLeases(content, now)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%v)", len(got), got)
	}
	if got[0].MAC != "AA:BB:CC:DD:EE:01" || got[0].LeaseSeconds != -1 {
		t.Fatalf("无限租约 = %+v, want LeaseSeconds=-1(与 ARP 静态设备同语义)", got[0])
	}
}

// ---------- R6-B7: 短间隔采样不得前移基线(速率卡 0 修复) ----------

func TestUpdateIfaceRateSampleShortIntervalKeepsBaseline(t *testing.T) {
	resetIfaceRateSamplesForTest()
	t.Cleanup(resetIfaceRateSamplesForTest)

	t0 := time.Now()
	if rx, tx := updateIfaceRateSample("bridge0", 1000, 500, t0); rx != 0 || tx != 0 {
		t.Fatalf("首次采样 = %d/%d, want 0/0", rx, tx)
	}
	// 持续 100ms 短间隔采样:距基线 t0 累计不足 500ms 的采样全部沿用上次
	// 速率,且不得前移基线——旧实现基线不断前移,dt 永远达不到阈值。
	for i := 1; i <= 4; i++ {
		rx, _ := updateIfaceRateSample("bridge0", 1000+int64(i)*3000, 500, t0.Add(time.Duration(i)*100*time.Millisecond))
		if rx != 0 {
			t.Fatalf("第 %d 次短间隔采样 rx = %d, want 沿用 0", i, rx)
		}
	}
	// t0+500ms:距基线累计达到阈值,立即恢复计算 (16000-1000)/0.5 = 30000。
	// 旧实现基线已被前移到 t0+400ms(dt=100ms),此处仍沿用 0 且永远卡死。
	rx, _ := updateIfaceRateSample("bridge0", 16000, 500, t0.Add(500*time.Millisecond))
	if rx != 30000 {
		t.Fatalf("恢复采样 rx = %d, want 30000(基线被短间隔采样前移则恒为沿用值)", rx)
	}
}

// ---------- R6-B8: 诊断目标 scheme 判定只看前缀 ----------

func TestNormalizeDiagProbeTargetSchemePrefixOnly(t *testing.T) {
	// 无 scheme 但查询串内嵌 URL:旧实现 Contains("://") 误判已带 scheme,
	// 解析出空 scheme 后 400 拒绝。
	got, err := normalizeDiagProbeTarget("www.example.com/check?url=http://a.com")
	if err != nil {
		t.Fatalf("查询串含 :// 的目标被误拒: %v", err)
	}
	if !strings.HasPrefix(got, "http://www.example.com/check?url=") {
		t.Fatalf("规范化结果 = %q, want 自动补 http:// 前缀", got)
	}
	if _, err := normalizeDiagProbeTarget("https://example.com/x"); err != nil {
		t.Fatalf("显式 https 目标应接受: %v", err)
	}
	if _, err := normalizeDiagProbeTarget("ftp://example.com/x"); err == nil {
		t.Fatalf("非 http(s) scheme 应拒绝")
	}
	// 下划线前缀标签是 DNS 诊断高频合法目标(ACME/DKIM/SRV)。
	for _, domain := range []string{"_acme-challenge.example.com", "_dmarc.example.com", "_sip._tcp.example.com"} {
		if !diagDomainPattern.MatchString(domain) {
			t.Fatalf("diagDomainPattern 拒绝合法 DNS 名 %q", domain)
		}
	}
	if diagDomainPattern.MatchString("-bad.example.com") {
		t.Fatalf("连字符开头的非法标签被放行")
	}
}

// ---------- R6-B9: 信号页制式判定不依赖 +QRSRP 行序 ----------

func TestApplySignalAntennasRatOrderIndependent(t *testing.T) {
	newAntennas := func() []map[string]any {
		out := make([]map[string]any, 0, 4)
		for _, antenna := range signalAntennaIDs {
			out = append(out, map[string]any{"id": antenna.id, "label": antenna.label, "rsrp": "-", "percent": 0})
		}
		return out
	}
	// EN-DC 双行、NR5G 在前(固件行序颠倒场景):制式仍须为 NR5G。
	data := map[string]any{"rat": ""}
	applySignalAntennas([]string{
		"+QRSRP: -86,-87,-88,-89,NR5G",
		"+QRSRP: -95,-96,-97,-98,LTE",
	}, newAntennas(), data)
	if data["rat"] != "NR5G" {
		t.Fatalf("EN-DC(NR 行在前)rat = %v, want NR5G(旧实现最后写入者胜误标 LTE)", data["rat"])
	}
	// 旧式无后缀应答按 LTE 收纳,制式不得为空串。
	data2 := map[string]any{"rat": ""}
	applySignalAntennas([]string{"+QRSRP: -85,-86,-86,-83"}, newAntennas(), data2)
	if data2["rat"] != "LTE" {
		t.Fatalf("无后缀应答 rat = %q, want LTE", data2["rat"])
	}
}

// ---------- R6-B10: SCC PCI 聚合进 cell 摘要 ----------

func TestParseSignalDetailAggregatesSCCPci(t *testing.T) {
	raw := strings.Join([]string{
		`+QENG: "servingcell","NOCONN","LTE","FDD",460,11,24211A484,649,242000,1650,1,5,5,-85,-9,-61,15`,
		`+QCAINFO: "PCC",1650,100,"LTE BAND 3",1,649,-85,-9,-61,10`,
		`+QCAINFO: "SCC",300,100,"LTE BAND 1",1,445,-96,-12,-74,6,0,-,-`,
		`+QCAINFO: "SCC",40590,100,"LTE BAND 41",1,418,-107,-15,-83,10`,
		"+QRSRP: -85,-86,-86,-83,LTE",
		"OK",
	}, "\r\n")
	data := parseSignalDetailAT(raw)
	cell, _ := data["cell"].(map[string]any)
	if cell == nil {
		t.Fatalf("cell 摘要缺失: %v", data)
	}
	// 旧实现 QCAINFO 数据源没接进 cell,scc_pci 恒 "-" 被前端隐藏。
	// LTE SCC 行的 PCI 取 p[5](qcaPCI 非 NR 分支),两个 SCC 按 " + " 聚合。
	if got := toTestString(cell["scc_pci"]); got != "445 + 418" {
		t.Fatalf("scc_pci = %q, want \"445 + 418\"", got)
	}
}

func toTestString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ---------- R6-B11: 带宽编码严格解析(占位字段显示 "-") ----------

func TestBwMHzRejectsPlaceholderCodes(t *testing.T) {
	if got := bwMHz("-", true); got != "-" {
		t.Fatalf("NR 占位字段 = %q, want -(旧实现 toInt 吞错落进编码 0 显示假 5MHz)", got)
	}
	if got := bwMHz("", false); got != "-" {
		t.Fatalf("LTE 空字段 = %q, want -(旧实现显示假 1.4MHz)", got)
	}
	if got := bwMHz("abc", true); got != "-" {
		t.Fatalf("非法字段 = %q, want -", got)
	}
	// 合法编码 0 的映射保持不变。
	if got := bwMHz("0", true); got != "5MHz" {
		t.Fatalf("NR 编码 0 = %q, want 5MHz", got)
	}
	if got := bwMHz("3", false); got != "10MHz" {
		t.Fatalf("LTE 编码 3 = %q, want 10MHz", got)
	}
}

// ---------- R6-B12: +COPS=? 读形态豁免动作判定 ----------

func TestATActionCommandCopsQueryExempt(t *testing.T) {
	if isATActionCommand("AT+COPS=?") {
		t.Fatalf("+COPS=? 是查询支持模式的读命令,不得判为动作(不缓存/不重试/触发读缓存全量失效)")
	}
	if !isATActionCommand(`AT+COPS=1,0,"46000"`) {
		t.Fatalf("手动选网写命令必须判为动作")
	}
	if isATActionCommand("AT+COPS?") {
		t.Fatalf("+COPS? 查询不得判为动作")
	}
}

// ---------- R6-B13: 历史采样时钟回拨护栏 ----------

func TestMetricsHistoryClockStepBackwardResets(t *testing.T) {
	resetMetricsHistoryForTest()
	t.Cleanup(resetMetricsHistoryForTest)

	// 预置一个"未来"时间戳的点(模拟校时把时钟向后步进前的旧时间轴)。
	future := time.Now().Add(time.Hour).Unix()
	metricsHistory.mu.Lock()
	metricsHistory.points = append(metricsHistory.points, metricsHistoryPoint{Timestamp: future, CPUPercent: 10})
	metricsHistory.last = time.Now().Add(-time.Hour) // 放开采样间隔闸门
	metricsHistory.mu.Unlock()

	recordMetricsHistoryFromDashboard(map[string]any{
		"cpuUsagePercent": 20, "ramUsagePercent": 30, "signalPercentage": 40,
		"nr_rx_bytes": int64(100), "nr_tx_bytes": int64(50),
	})

	points := snapshotMetricsHistory()
	if len(points) != 1 {
		t.Fatalf("回拨后序列长度 = %d, want 1(旧时间轴应被丢弃重建)", len(points))
	}
	if points[0].Timestamp >= future {
		t.Fatalf("未来时间戳旧点未清除: %v", points[0])
	}
}

// ---------- R6-B14: 负温度均值四舍五入 ----------

func TestParseDashboardNegativeTemperatureRounding(t *testing.T) {
	raw := strings.Join([]string{
		"+QSIMSTAT: 0,1",
		`+QTEMP: "aoss",-5`,
		`+QTEMP: "cpuss",-5`,
		`+QTEMP: "mdmss",-5`,
		`+QTEMP: "mdmq6",-2`,
		"OK",
	}, "\r\n")
	data := parseDashboardAT(raw)
	// 均值 -4.25,四舍五入到最近整数应为 -4;旧实现"+count/2"补偿在负和
	// 下向零截断得到 -3,偏差 1°C。
	if got := toTestString(data["temperature"]); got != "-4" {
		t.Fatalf("负温度均值 = %q, want -4", got)
	}
}
