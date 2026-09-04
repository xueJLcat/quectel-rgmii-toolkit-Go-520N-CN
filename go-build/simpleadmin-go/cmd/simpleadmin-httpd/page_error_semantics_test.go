package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ---------- atReadFailed 读取失败判定 ----------

func TestATReadFailedClassification(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"空响应", "", true},
		{"纯运行器超时错误文本", "timeout waiting for response", true},
		{"纯运行器候选设备失败文本", "AT device candidates failed: no usable port", true},
		{"含 QENG 数据行", "AT+QENG=\"servingcell\"\r\n+QENG: \"LTE\",460,11,1650,100,5,5,131,-85,-10,-7,14,2\r\nOK\r\n", false},
		{"保护期未就绪", atCachePendingText, false},
		{"无卡但有 QSIMSTAT 状态行", "+QSIMSTAT: 0,1\r\nOK\r\n", false},
		{"无卡但有 CME ERROR 数据行", "+CME ERROR: 10", false},
	}
	for _, tc := range cases {
		if got := atReadFailed(tc.raw); got != tc.want {
			t.Fatalf("%s: atReadFailed(%q) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}

// ---------- 聚合/扫描接口暴露状态键 ----------

// mock 模式下数据就绪:dashboard/settings 不应误报读取失败,
// scan 必须带 ok 键与既有解析键,前端据此区分失败与空结果。
func TestMockModeErrorSemanticsKeys(t *testing.T) {
	ts, client := e2eTestServer(t)

	parseBody := func(id, path, body string) map[string]any {
		t.Helper()
		resp := e2eWebSocketCall(t, ts, client, id, "POST", path, body)
		if status := fmt.Sprint(resp["status"]); status != "200" {
			t.Fatalf("%s status = %v, want 200 (error=%v)", path, resp["status"], resp["error"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
			t.Fatalf("%s body not json: %v", path, err)
		}
		return data
	}

	dash := parseBody("es-dash", "/api/dashboard_data", "")
	if _, ok := dash["pending"]; !ok {
		t.Fatalf("dashboard_data missing pending key: %#v", dash)
	}
	if _, hasError := dash["error"]; hasError {
		t.Fatalf("dashboard_data unexpected read-failure error in mock mode: %v", dash["error"])
	}

	settings := parseBody("es-net", "/api/network_data", "action=settings")
	if _, ok := settings["pending"]; !ok {
		t.Fatalf("network_data settings missing pending key: %#v", settings)
	}
	if _, hasError := settings["error"]; hasError {
		t.Fatalf("network_data settings unexpected read-failure error in mock mode: %v", settings["error"])
	}

	scan := parseBody("es-scan", "/api/network_data", "action=scan&mode=Neighbour%20Scan")
	if _, ok := scan["ok"]; !ok {
		t.Fatalf("network_data scan missing ok key: %#v", scan)
	}
	for _, key := range []string{"nr5g_cells_parsed", "lte_cells_parsed"} {
		if _, ok := scan[key]; !ok {
			t.Fatalf("network_data scan missing %s key: %#v", key, scan)
		}
	}
}
