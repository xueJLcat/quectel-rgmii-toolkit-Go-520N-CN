package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// shrinkNR5GLockDelays 将守护锁定的等待时长调至毫秒级,
// 避免单测真实等待 8s/3s;测试结束自动恢复运行期值。
func shrinkNR5GLockDelays(t *testing.T) {
	t.Helper()
	oldSettle, oldVerify := nr5gLockSettleDelay, nr5gLockVerifyDelay
	nr5gLockSettleDelay = time.Millisecond
	nr5gLockVerifyDelay = time.Millisecond
	t.Cleanup(func() {
		nr5gLockSettleDelay = oldSettle
		nr5gLockVerifyDelay = oldVerify
	})
}

// servingCellNR5GSALine 是实机抓取的 NR5G-SA 驻留行(电信 46011,
// n1 FDD,频点 428910,PCI 277),scs 字段为本机恒定的 "-"。
const servingCellNR5GSALine = `+QENG: "servingcell","NOCONN","NR5G-SA","FDD",460,11,2426AB483,277,242400,428910,1,6,-65,-11,25,0,-`

// ---------- parseServingCellAT ----------

func TestParseServingCellATNR5GSA(t *testing.T) {
	cell := parseServingCellAT("AT+QENG=\"servingcell\"\r\n" + servingCellNR5GSALine + "\r\nOK\r\n")
	wants := map[string]string{"rat": "NR5G-SA", "pci": "277", "freq": "428910", "band": "1", "scs": ""}
	for k, want := range wants {
		if cell[k] != want {
			t.Fatalf("cell[%s] = %q, want %q (cell=%v)", k, cell[k], want, cell)
		}
	}

	// scs 字段为纯数字时原样返回(其它固件可能给出有效值)。
	numeric := strings.Replace(servingCellNR5GSALine, ",0,-", ",0,30", 1)
	cell = parseServingCellAT(numeric + "\r\nOK\r\n")
	if cell["scs"] != "30" {
		t.Fatalf("cell[scs] = %q, want 30 for numeric scs field (cell=%v)", cell["scs"], cell)
	}
}

func TestParseServingCellATLTE(t *testing.T) {
	raw := "AT+QENG=\"servingcell\"\r\n" +
		`+QENG: "servingcell","NOCONN","LTE","FDD",460,01,5A29C0B,465,1650,3,5,5,DE10,-69,-5,-65,21,51` +
		"\r\nOK\r\n"
	cell := parseServingCellAT(raw)
	wants := map[string]string{"rat": "LTE", "pci": "465", "freq": "1650"}
	for k, want := range wants {
		if cell[k] != want {
			t.Fatalf("cell[%s] = %q, want %q (cell=%v)", k, cell[k], want, cell)
		}
	}
}

func TestParseServingCellATGarbage(t *testing.T) {
	if cell := parseServingCellAT(""); cell["rat"] != "UNKNOWN" {
		t.Fatalf("empty raw rat = %q, want UNKNOWN", cell["rat"])
	}
	if cell := parseServingCellAT("GARBAGE\r\nOK\r\n"); cell["rat"] != "UNKNOWN" {
		t.Fatalf("garbage raw rat = %q, want UNKNOWN", cell["rat"])
	}
	// 残缺的 NR5G-SA 行(字段不足 11)只返回制式,不得越界取字段。
	short := `+QENG: "servingcell","NOCONN","NR5G-SA","FDD",460,11`
	cell := parseServingCellAT(short + "\r\nOK\r\n")
	if cell["rat"] != "NR5G-SA" || len(cell) != 1 {
		t.Fatalf("short NR5G-SA line cell = %v, want only rat=NR5G-SA", cell)
	}
	// 未驻网形态:制式字段非已知可锁定制式时原样返回。
	noservice := `+QENG: "servingcell","NOCONN","NOSERVICE","FDD",460,11,0,0,0,0,0,0,0,0,0,0,-`
	cell = parseServingCellAT(noservice + "\r\nOK\r\n")
	if cell["rat"] != "NOSERVICE" {
		t.Fatalf("noservice rat = %q, want NOSERVICE", cell["rat"])
	}
}

// ---------- c5gregRegistered ----------

func TestC5gregRegistered(t *testing.T) {
	checks := []struct {
		resp string
		want bool
	}{
		{"AT+C5GREG?\r\n+C5GREG: 0,1\r\nOK\r\n", true},  // 本地网已注册
		{"AT+C5GREG?\r\n+C5GREG: 0,5\r\nOK\r\n", true},  // 漫游已注册
		{"AT+C5GREG?\r\n+C5GREG: 0,0\r\nOK\r\n", false}, // 未注册
		{"AT+C5GREG?\r\n+C5GREG: 0,2\r\nOK\r\n", false}, // 搜索中
		{"AT+C5GREG?\r\nOK\r\n", false},                 // 无 +C5GREG 行
		{"", false},
		{"GARBAGE", false},
	}
	for _, check := range checks {
		if got := c5gregRegistered(check.resp); got != check.want {
			t.Fatalf("c5gregRegistered(%q) = %v, want %v", check.resp, got, check.want)
		}
	}
}

// ---------- parseEarfcnLockAT ----------

func TestParseEarfcnLockAT(t *testing.T) {
	// 实机启用形态:`+QNWCFG: "lte_earfcn_lock",1,1650`。
	nr, lte := parseEarfcnLockAT("AT+QNWCFG=\"lte_earfcn_lock\"\r\n+QNWCFG: \"lte_earfcn_lock\",1,1650\r\nOK\r\n")
	if len(nr) != 0 || len(lte) != 1 || lte[0] != "1650" {
		t.Fatalf("enabled lte: nr=%v lte=%v, want []/[1650]", nr, lte)
	}
	// 实机清零形态:`+QNWCFG: "lte_earfcn_lock",0`。
	nr, lte = parseEarfcnLockAT("+QNWCFG: \"lte_earfcn_lock\",0\r\nOK\r\n")
	if len(nr) != 0 || len(lte) != 0 {
		t.Fatalf("disabled lte: nr=%v lte=%v, want []/[]", nr, lte)
	}
	// 混合:NR5G 两个频点 + LTE 禁用;数量字段约束实际截取长度。
	raw := "AT+QNWCFG=\"nr5g_earfcn_lock\";+QNWCFG=\"lte_earfcn_lock\"\r\n" +
		"+QNWCFG: \"nr5g_earfcn_lock\",2,627264,428910\r\n" +
		"+QNWCFG: \"lte_earfcn_lock\",0\r\nOK\r\n"
	nr, lte = parseEarfcnLockAT(raw)
	if len(nr) != 2 || nr[0] != "627264" || nr[1] != "428910" || len(lte) != 0 {
		t.Fatalf("mixed: nr=%v lte=%v, want [627264 428910]/[]", nr, lte)
	}
	// 垃圾输入不产生频点。
	nr, lte = parseEarfcnLockAT("GARBAGE\r\nOK\r\n")
	if len(nr) != 0 || len(lte) != 0 {
		t.Fatalf("garbage: nr=%v lte=%v, want []/[]", nr, lte)
	}
}

// ---------- lockNR5GCellGuarded ----------

// TestLockGuardNR5GUnlocksAndFallsBack 守护流程核心回归:
// 首个候选锁定后未注册 → 必须自动解锁再尝试下一候选;
// 防止 scs 猜错把设备钉死在无法同步的小区导致断网(实机事故)。
func TestLockGuardNR5GUnlocksAndFallsBack(t *testing.T) {
	shrinkNR5GLockDelays(t)

	var mu sync.Mutex
	executed := []string{}
	registered := false
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		upper := strings.ToUpper(command)
		switch {
		case strings.Contains(upper, "+C5GREG"):
			mu.Lock()
			reg := registered
			mu.Unlock()
			if reg {
				return command + "\r\n+C5GREG: 0,1\r\nOK\r\n", nil
			}
			return command + "\r\n+C5GREG: 0,0\r\nOK\r\n", nil
		case strings.Contains(upper, `+QNWLOCK="COMMON/5G",0`):
			return command + "\r\nOK\r\n", nil
		case strings.Contains(upper, "+QNWLOCK"):
			// 仅 scs=30 的锁定能让设备注册成功,模拟首个候选 15 失步。
			if strings.Contains(command, ",30,") {
				mu.Lock()
				registered = true
				mu.Unlock()
			}
			return command + "\r\nOK\r\n", nil
		}
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	resp, scsUsed, ok := s.lockNR5GCellGuarded("277", "428910", "1", []string{"15", "30"})
	if !ok {
		t.Fatalf("lockNR5GCellGuarded ok = false, want true (second candidate should succeed)")
	}
	if scsUsed != "30" {
		t.Fatalf("scsUsed = %q, want 30", scsUsed)
	}
	if !atResponseOK(resp) {
		t.Fatalf("resp not OK-terminated: %q", resp)
	}

	mu.Lock()
	defer mu.Unlock()
	join := strings.Join(executed, "\n")
	if !strings.Contains(join, `AT+QNWLOCK="common/5g",277,428910,15,1`) {
		t.Fatalf("first candidate lock command missing, executed=%v", executed)
	}
	if !strings.Contains(join, `AT+QNWLOCK="common/5g",0`) {
		t.Fatalf("auto-unlock after failed registration missing, executed=%v", executed)
	}
	if !strings.Contains(join, `AT+QNWLOCK="common/5g",277,428910,30,1`) {
		t.Fatalf("second candidate lock command missing, executed=%v", executed)
	}
	// 解锁必须发生在两次锁定之间。
	first := strings.Index(join, ",15,")
	unlock := strings.Index(join, `"common/5g",0`)
	second := strings.Index(join, ",30,")
	if !(first < unlock && unlock < second) {
		t.Fatalf("unlock not between the two lock attempts: first=%d unlock=%d second=%d", first, unlock, second)
	}
}

// TestLockGuardNR5GAllCandidatesFail 所有候选均未注册:
// 每次都自动解锁,最终返回失败三元组。
func TestLockGuardNR5GAllCandidatesFail(t *testing.T) {
	shrinkNR5GLockDelays(t)

	var mu sync.Mutex
	unlocks := 0
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		upper := strings.ToUpper(command)
		switch {
		case strings.Contains(upper, "+C5GREG"):
			return command + "\r\n+C5GREG: 0,0\r\nOK\r\n", nil
		case strings.Contains(upper, `+QNWLOCK="COMMON/5G",0`):
			mu.Lock()
			unlocks++
			mu.Unlock()
			return command + "\r\nOK\r\n", nil
		}
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	resp, scsUsed, ok := s.lockNR5GCellGuarded("277", "428910", "1", []string{"30", "15"})
	if ok || resp != "" || scsUsed != "" {
		t.Fatalf("all-fail = (%q,%q,%v), want (\"\",\"\",false)", resp, scsUsed, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if unlocks != 2 {
		t.Fatalf("unlocks = %d, want 2 (one per failed candidate)", unlocks)
	}
}

// ---------- lock_serving_cell handler ----------

// TestHandleLockServingCellNR5GSA 驻留 NR5G-SA 时走守护锁定:
// 200 + ok=true,返回所用 SCS 与服务小区参数。
func TestHandleLockServingCellNR5GSA(t *testing.T) {
	shrinkNR5GLockDelays(t)

	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		upper := strings.ToUpper(command)
		switch {
		case strings.Contains(upper, `+QENG="SERVINGCELL"`):
			return command + "\r\n" + servingCellNR5GSALine + "\r\nOK\r\n", nil
		case strings.Contains(upper, "+C5GREG"):
			return command + "\r\n+C5GREG: 0,1\r\nOK\r\n", nil
		}
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"lock_serving_cell"}})
	if status != http.StatusOK {
		t.Fatalf("lock_serving_cell status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("lock_serving_cell ok = %v, want true (error=%v)", data["ok"], data["error"])
	}
	checks := map[string]string{"locked": "NR5G-SA", "scs": "30", "pci": "277", "freq": "428910", "band": "1"}
	for k, want := range checks {
		if got := fmt.Sprint(data[k]); got != want {
			t.Fatalf("data[%s] = %q, want %q", k, got, want)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, command := range executed {
		if command == `AT+QNWLOCK="common/5g",277,428910,30,1` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("guarded NR5G lock command with first candidate scs=30 not executed, got %v", executed)
	}
}

// TestHandleLockServingCellLTE 驻留 LTE 时直接下发 4g 锁定(无 scs 问题)。
func TestHandleLockServingCellLTE(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		upper := strings.ToUpper(command)
		if strings.Contains(upper, `+QENG="SERVINGCELL"`) {
			return command + "\r\n" +
				`+QENG: "servingcell","NOCONN","LTE","FDD",460,01,5A29C0B,465,1650,3,5,5,DE10,-69,-5,-65,21,51` +
				"\r\nOK\r\n", nil
		}
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"lock_serving_cell"}})
	if status != http.StatusOK {
		t.Fatalf("lock_serving_cell status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("lock_serving_cell ok = %v, want true (error=%v)", data["ok"], data["error"])
	}
	checks := map[string]string{"locked": "LTE", "pci": "465", "freq": "1650"}
	for k, want := range checks {
		if got := fmt.Sprint(data[k]); got != want {
			t.Fatalf("data[%s] = %q, want %q", k, got, want)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, command := range executed {
		if command == `AT+QNWLOCK="common/4g",1,1650,465` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("LTE lock command not executed, got %v", executed)
	}
}

// TestHandleLockServingCellNoCell 未驻留可锁定小区(无 servingcell 行):
// 200 + ok=false + 明确错误,不得下发任何锁定命令。
func TestHandleLockServingCellNoCell(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"lock_serving_cell"}})
	if status != http.StatusOK {
		t.Fatalf("lock_serving_cell status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != false {
		t.Fatalf("lock_serving_cell ok = %v, want false", data["ok"])
	}
	if got := fmt.Sprint(data["error"]); got != "未驻留可锁定的小区" {
		t.Fatalf("error = %q, want 未驻留可锁定的小区", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, command := range executed {
		if strings.Contains(command, "+QNWLOCK") {
			t.Fatalf("no lock command must be issued without serving cell, got %q", command)
		}
	}
}

// ---------- earfcn lock handlers ----------

// TestHandleEarfcnLockStatus 状态查询解析双制式频点锁。
func TestHandleEarfcnLockStatus(t *testing.T) {
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		return command + "\r\n" +
			"+QNWCFG: \"nr5g_earfcn_lock\",1,627264\r\n" +
			"+QNWCFG: \"lte_earfcn_lock\",1,1650\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"earfcn_lock_status"}})
	if status != http.StatusOK {
		t.Fatalf("earfcn_lock_status status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("earfcn_lock_status ok = %v, want true", data["ok"])
	}
	nr, _ := data["nr5g_arfcns"].([]any)
	lte, _ := data["lte_arfcns"].([]any)
	if len(nr) != 1 || fmt.Sprint(nr[0]) != "627264" {
		t.Fatalf("nr5g_arfcns = %v, want [627264]", data["nr5g_arfcns"])
	}
	if len(lte) != 1 || fmt.Sprint(lte[0]) != "1650" {
		t.Fatalf("lte_arfcns = %v, want [1650]", data["lte_arfcns"])
	}
}

// TestHandleSetEarfcnLockValidation 参数校验:超限/非数字/空列表/非法制式
// 一律 400,且不下发任何写命令;合法输入 200 并下发正确写命令。
func TestHandleSetEarfcnLockValidation(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	tooManyLTE := strings.TrimSuffix(strings.Repeat("1650,", 3), ",") // 3 > 上限 2
	tooManyNR := make([]string, 33)                                   // 33 > 上限 32
	for i := range tooManyNR {
		tooManyNR[i] = fmt.Sprintf("%d", 627264+i)
	}
	rejectCases := []struct {
		rat, arfcns string
	}{
		{"LTE", tooManyLTE},
		{"NR5G", strings.Join(tooManyNR, ",")},
		{"LTE", "16a0"},
		{"NR5G", "627264,-1"},
		{"LTE", ""},
		{"LTE", ",,"},
		{"GSM", "1650"},
	}
	for _, tc := range rejectCases {
		status, data := callNetworkData(t, s, url.Values{"action": {"set_earfcn_lock"}, "rat": {tc.rat}, "arfcns": {tc.arfcns}})
		if status != http.StatusBadRequest {
			t.Fatalf("set_earfcn_lock rat=%q arfcns=%q status = %d, want 400 (body=%v)", tc.rat, tc.arfcns, status, data)
		}
		if data["ok"] != false {
			t.Fatalf("set_earfcn_lock rat=%q arfcns=%q ok = %v, want false", tc.rat, tc.arfcns, data["ok"])
		}
	}

	status, data := callNetworkData(t, s, url.Values{"action": {"set_earfcn_lock"}, "rat": {"LTE"}, "arfcns": {"1650"}})
	if status != http.StatusOK || data["ok"] != true {
		t.Fatalf("valid set_earfcn_lock status=%d ok=%v, want 200/true (error=%v)", status, data["ok"], data["error"])
	}

	mu.Lock()
	defer mu.Unlock()
	for _, command := range executed {
		if strings.Contains(command, "+QNWCFG") && strings.Count(command, ",") > 1 && command != `AT+QNWCFG="lte_earfcn_lock",1,1650` {
			t.Fatalf("unexpected earfcn lock write command %q", command)
		}
	}
	found := false
	for _, command := range executed {
		if command == `AT+QNWCFG="lte_earfcn_lock",1,1650` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("valid lte earfcn lock write command not executed, got %v", executed)
	}
}

// TestHandleClearEarfcnLock 清零命令按制式正确下发。
func TestHandleClearEarfcnLock(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	for _, rat := range []string{"LTE", "NR5G"} {
		status, data := callNetworkData(t, s, url.Values{"action": {"clear_earfcn_lock"}, "rat": {rat}})
		if status != http.StatusOK || data["ok"] != true {
			t.Fatalf("clear_earfcn_lock rat=%q status=%d ok=%v, want 200/true", rat, status, data["ok"])
		}
	}
	status, data := callNetworkData(t, s, url.Values{"action": {"clear_earfcn_lock"}, "rat": {"GSM"}})
	if status != http.StatusBadRequest || data["ok"] != false {
		t.Fatalf("clear_earfcn_lock rat=GSM status=%d ok=%v, want 400/false", status, data["ok"])
	}

	mu.Lock()
	defer mu.Unlock()
	wantCommands := []string{`AT+QNWCFG="lte_earfcn_lock",0`, `AT+QNWCFG="nr5g_earfcn_lock",0`}
	for _, want := range wantCommands {
		found := false
		for _, command := range executed {
			if command == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("clear command %q not executed, got %v", want, executed)
		}
	}
}

// ---------- scan 未知模式 ----------

// TestCellScanUnknownModeRejected QSCAN 三模式已从接口移除,
// 仅剩 "Neighbour Scan";未知模式必须 400 且不下发任何命令。
func TestCellScanUnknownModeRejected(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK\r\n", nil
	})

	s := networkDataTestServer()
	for _, mode := range []string{"Full Scan", "LTE Only", "NR5G Only", "bogus"} {
		status, data := callNetworkData(t, s, url.Values{"action": {"scan"}, "mode": {mode}})
		if status != http.StatusBadRequest {
			t.Fatalf("scan mode=%q status = %d, want 400 (body=%v)", mode, status, data)
		}
		if data["ok"] != false {
			t.Fatalf("scan mode=%q ok = %v, want false", mode, data["ok"])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 0 {
		t.Fatalf("unknown scan modes must not execute AT commands, got %v", executed)
	}
}
