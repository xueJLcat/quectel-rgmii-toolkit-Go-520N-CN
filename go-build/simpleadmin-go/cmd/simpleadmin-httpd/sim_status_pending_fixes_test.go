// sim_status_pending_fixes_test.go 覆盖短信发送前 SIM 检测在开机/模块重启
// 保护期内的语义修复:保护期应答是 pending 占位文本,不是设备状态证据,
// 不能被 smsSIMInserted 判成"未插卡"而以误导性原因拦截发送;
// 此时必须返回 inserted+pending 标记,把判定交给发送流程
// (发送流程里有精确的"模块未就绪"错误)。
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleSMSDataSimStatusPendingNotMisreportedAsAbsent 保护期内
// action=sim_status 返回 inserted=true + pending=true,
// 而不是 inserted=false(误报未插卡)。
func TestHandleSMSDataSimStatusPendingNotMisreportedAsAbsent(t *testing.T) {
	atCommandCache.mu.Lock()
	prevReadyAt := atCommandCache.readyAt
	prevMock := atCommandCache.mockMode
	prevStarted := atCommandCache.started
	// 置为保护期状态并屏蔽 Start 的 worker 派生,保证 Fetch 走
	// startupDelayedRead 早退分支返回 pending 占位文本。
	atCommandCache.readyAt = time.Now().Add(time.Hour)
	atCommandCache.mockMode = false
	atCommandCache.started = true
	atCommandCache.mu.Unlock()
	t.Cleanup(func() {
		atCommandCache.mu.Lock()
		atCommandCache.readyAt = prevReadyAt
		atCommandCache.mockMode = prevMock
		atCommandCache.started = prevStarted
		atCommandCache.mu.Unlock()
	})

	srv := &simpleAdminServer{cfg: serverConfig{}}
	req := httptest.NewRequest(http.MethodPost, "/api/sms_data", strings.NewReader("action=sim_status"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleSMSData(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("body not json: %v (body=%s)", err, rec.Body.String())
	}
	if inserted, _ := data["inserted"].(bool); !inserted {
		t.Fatalf("inserted = %v, want true during grace period: %#v", data["inserted"], data)
	}
	if pending, _ := data["pending"].(bool); !pending {
		t.Fatalf("pending = %v, want true during grace period: %#v", data["pending"], data)
	}
}

// TestE2EMockSMSSimStatusReadyInserted mock 模式就绪态下
// action=sim_status 按 +CPIN: READY 判已插卡,且不带 pending 标记。
func TestE2EMockSMSSimStatusReadyInserted(t *testing.T) {
	ts, client := e2eTestServer(t)

	resp := e2eWebSocketCall(t, ts, client, "sim-status-ready", "POST", "/api/sms_data", "action=sim_status")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if inserted, _ := data["inserted"].(bool); !inserted {
		t.Fatalf("inserted = %v, want true for mock +CPIN: READY: %#v", data["inserted"], data)
	}
	if pending, ok := data["pending"]; ok && pending == true {
		t.Fatalf("pending = %v, want absent/false in ready mock mode: %#v", pending, data)
	}
}
