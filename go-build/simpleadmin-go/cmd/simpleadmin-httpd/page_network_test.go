package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// networkDataTestServer 构造直连 handleNetworkData 的最小服务器实例。
func networkDataTestServer() *simpleAdminServer {
	return &simpleAdminServer{cfg: serverConfig{mockMode: true}}
}

// callNetworkData 以表单方式调用 handleNetworkData 并返回状态码与 JSON 响应体。
func callNetworkData(t *testing.T, s *simpleAdminServer, form url.Values) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/network_data", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleNetworkData(rec, req)
	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("response body not json: %v (body=%s)", err, rec.Body.String())
	}
	return rec.Code, data
}

// TestLockBandsRejectsInvalidValues 覆盖审计修复 B4:非空但无效输入
// ("abc"/"###")经 safeBandList 清洗后为空串,拼出的空值命令会按 Quectel
// 语义静默恢复全部频段;锁频路径必须返回 400 + 明确错误,且不下发任何
// 频段写命令。
func TestLockBandsRejectsInvalidValues(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK", nil
	})

	s := networkDataTestServer()
	cases := []struct {
		mode, values, wantError string
	}{
		{"LTE", "abc", "invalid band list"},
		{"LTE", "###", "invalid band list"},
		{"NSA", "---", "invalid band list"},
		{"SA", "xyz:", "invalid band list"},
		{"SA", "::", "invalid band list"},
		{"LTE", "", "missing values"},
		{"LTE", "   ", "missing values"},
		{"", "1:3", "invalid mode"},
		{"GSM", "1:3", "invalid mode"},
	}
	for _, tc := range cases {
		form := url.Values{"action": {"lock_bands"}, "mode": {tc.mode}, "values": {tc.values}}
		status, data := callNetworkData(t, s, form)
		if status != http.StatusBadRequest {
			t.Fatalf("lock_bands mode=%q values=%q status = %d, want 400 (body=%v)", tc.mode, tc.values, status, data)
		}
		if data["ok"] != false {
			t.Fatalf("lock_bands mode=%q values=%q ok = %v, want false", tc.mode, tc.values, data["ok"])
		}
		if got := fmt.Sprint(data["error"]); got != tc.wantError {
			t.Fatalf("lock_bands mode=%q values=%q error = %q, want %q", tc.mode, tc.values, got, tc.wantError)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, command := range executed {
		if strings.Contains(command, `QNWPREFCFG="lte_band",`) ||
			strings.Contains(command, `QNWPREFCFG="nsa_nr5g_band",`) ||
			strings.Contains(command, `QNWPREFCFG="nr5g_band",`) {
			t.Fatalf("invalid lock_bands input must not issue band write command, got %q", command)
		}
	}
}

// TestLockBandsValidValuesUnaffected 合法输入不受修复影响:200 + ok=true,
// 下发的写命令携带清洗后的频段列表(非空值语义)。
func TestLockBandsValidValuesUnaffected(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK", nil
	})

	s := networkDataTestServer()
	cases := []struct {
		mode, values, wantCommand string
	}{
		{"LTE", "1:3:5", `AT+QNWPREFCFG="lte_band",1:3:5`},
		{"nsa", " 1 : 3 ", `AT+QNWPREFCFG="nsa_nr5g_band",1:3`},
		{"SA", "78", `AT+QNWPREFCFG="nr5g_band",78`},
	}
	for _, tc := range cases {
		form := url.Values{"action": {"lock_bands"}, "mode": {tc.mode}, "values": {tc.values}}
		status, data := callNetworkData(t, s, form)
		if status != http.StatusOK {
			t.Fatalf("lock_bands mode=%q values=%q status = %d, want 200 (body=%v)", tc.mode, tc.values, status, data)
		}
		if data["ok"] != true {
			t.Fatalf("lock_bands mode=%q values=%q ok = %v, want true (response=%v)", tc.mode, tc.values, data["ok"], data["response"])
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, tc := range cases {
		found := false
		for _, command := range executed {
			if command == tc.wantCommand {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected band write command %q not executed, got %#v", tc.wantCommand, executed)
		}
	}
}

// TestResetBandsKeepsEmptyValueSemantics reset_bands 的语义是"恢复全部
// 频段"(前端二次确认"恢复全部"),空值正是达成该目的的 Quectel 语义,
// 与 lock_bands 的锁频意图不同,修复不得影响该路径。
func TestResetBandsKeepsEmptyValueSemantics(t *testing.T) {
	var mu sync.Mutex
	executed := []string{}
	stubExecuteCachedATCommand(t, func(command string, mockMode bool) (string, error) {
		mu.Lock()
		executed = append(executed, command)
		mu.Unlock()
		return command + "\r\nOK", nil
	})

	s := networkDataTestServer()
	status, data := callNetworkData(t, s, url.Values{"action": {"reset_bands"}, "lte": {""}, "nsa": {""}, "sa": {""}})
	if status != http.StatusOK {
		t.Fatalf("reset_bands status = %d, want 200 (body=%v)", status, data)
	}
	if data["ok"] != true {
		t.Fatalf("reset_bands ok = %v, want true (response=%v)", data["ok"], data["response"])
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, command := range executed {
		if strings.Contains(command, `AT+QNWPREFCFG="lte_band",`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reset_bands did not issue restore command, got %#v", executed)
	}
}
