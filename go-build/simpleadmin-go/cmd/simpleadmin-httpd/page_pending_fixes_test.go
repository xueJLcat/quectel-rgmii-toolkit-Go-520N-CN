package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ---------- 只读聚合接口暴露 pending(mock 无保护期,应为 false) ----------

func TestMockModeAggregateEndpointsExposePendingFalse(t *testing.T) {
	ts, client := e2eTestServer(t)

	checks := []struct {
		id, path, body string
	}{
		{"pf-dash", "/api/dashboard_data", ""},
		{"pf-net", "/api/network_data", "action=settings"},
	}
	for _, check := range checks {
		resp := e2eWebSocketCall(t, ts, client, check.id, "POST", check.path, check.body)
		if status := fmt.Sprint(resp["status"]); status != "200" {
			t.Fatalf("%s status = %v, want 200 (error=%v)", check.path, resp["status"], resp["error"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
			t.Fatalf("%s body not json: %v", check.path, err)
		}
		pending, ok := data["pending"].(bool)
		if !ok {
			t.Fatalf("%s missing pending field: %#v", check.path, data)
		}
		if pending {
			t.Fatalf("%s pending = true, want false in mock mode", check.path)
		}
	}
}

// ---------- save_settings 空 APN 跳过 CGDCONT ----------

// mock 模式下无法直接观测命令序列,端到端仅断言成功;命令拼装由下方单元测试覆盖。
func TestMockModeSaveSettingsEmptyAPNSucceeds(t *testing.T) {
	ts, client := e2eTestServer(t)

	resp := e2eWebSocketCall(t, ts, client, "ss1", "POST", "/api/network_data", "action=save_settings&pdpType=IPV4V6&apn=&modePref=AUTO")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("save_settings status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("save_settings body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("save_settings ok = %v, want true (response=%v)", data["ok"], data["response"])
	}
}

func TestNetworkSettingsCommandsEmptyAPNSkipsCGDCONT(t *testing.T) {
	commands, err := networkSettingsCommands("IPV4V6", "", "AUTO", "")
	if err != nil {
		t.Fatalf("networkSettingsCommands: %v", err)
	}
	for _, command := range commands {
		if strings.Contains(command, "+CGDCONT") {
			t.Fatalf("empty APN must skip CGDCONT, got %#v", commands)
		}
	}
	if len(commands) != 1 || commands[0] != `+QNWPREFCFG="mode_pref",AUTO` {
		t.Fatalf("commands = %#v, want only mode_pref", commands)
	}

	// 空 APN 且无其它参数 → 无命令(上层按 "no changes" 处理)。
	commands, err = networkSettingsCommands("IPV4V6", "   ", "", "")
	if err != nil {
		t.Fatalf("networkSettingsCommands: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("commands = %#v, want empty for blank APN", commands)
	}
}

func TestNetworkSettingsCommandsNonEmptyAPNKeepsCGDCONT(t *testing.T) {
	commands, err := networkSettingsCommands("", " 5gnet ", "", "")
	if err != nil {
		t.Fatalf("networkSettingsCommands: %v", err)
	}
	if len(commands) != 1 || commands[0] != `+CGDCONT=1,"IPV4V6","5gnet"` {
		t.Fatalf("commands = %#v, want CGDCONT with default pdp and sanitized apn", commands)
	}

	commands, err = networkSettingsCommands("ipv6", "vnet.test-1", "", "1")
	if err != nil {
		t.Fatalf("networkSettingsCommands: %v", err)
	}
	want := []string{`+CGDCONT=1,"IPV6","vnet.test-1"`, `+QNWPREFCFG="nr5g_disable_mode",1`}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	for i := range want {
		if commands[i] != want[i] {
			t.Fatalf("commands[%d] = %q, want %q", i, commands[i], want[i])
		}
	}

	if _, err := networkSettingsCommands("BOGUS", "ctnet", "", ""); err == nil {
		t.Fatalf("invalid pdp type with non-empty APN must be rejected")
	}
}

// ---------- 手动锁小区参数数量校验 ----------

func TestLockLTEManualRejectsMismatchedCellCount(t *testing.T) {
	ts, client := e2eTestServer(t)

	assertRejected := func(id, body string) {
		t.Helper()
		resp := e2eWebSocketCall(t, ts, client, id, "POST", "/api/network_data", body)
		if status := fmt.Sprint(resp["status"]); status != "400" {
			t.Fatalf("%s status = %v, want 400 (body=%v)", id, resp["status"], resp["body"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
			t.Fatalf("%s body not json: %v", id, err)
		}
		if data["ok"] != false {
			t.Fatalf("%s ok = %v, want false", id, data["ok"])
		}
		if got := fmt.Sprint(data["error"]); got != "invalid cell parameters" {
			t.Fatalf("%s error = %q, want %q", id, got, "invalid cell parameters")
		}
	}

	// cellNum=2 但只提供 1 组有效参数。
	assertRejected("lk1", "action=lock_lte_manual&cellNum=2&pairs=1850,445")
	// 第二组参数全为非数字,stripNonDigits 后整对被丢弃,有效数量与 cellNum 不符。
	assertRejected("lk2", "action=lock_lte_manual&cellNum=2&pairs=1850,445&pairs=abc,def")

	// 数量一致时正常放行(mock 回 OK)。
	resp := e2eWebSocketCall(t, ts, client, "lk3", "POST", "/api/network_data", "action=lock_lte_manual&cellNum=1&pairs=1850,445")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("matched lock status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("matched lock body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("matched lock ok = %v, want true (response=%v)", data["ok"], data["response"])
	}
}
