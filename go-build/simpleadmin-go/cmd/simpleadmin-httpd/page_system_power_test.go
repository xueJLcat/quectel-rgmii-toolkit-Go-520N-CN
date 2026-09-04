package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSystemDataPowerActionsMock mock 模式下重启/关机不执行系统命令,直接成功。
func TestSystemDataPowerActionsMock(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	for _, action := range []string{"reboot_device", "poweroff_device"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/system_data", strings.NewReader("action="+action))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.handleSystemData(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", action, rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, `"mock":true`) {
			t.Fatalf("%s body = %s, want ok mock result", action, body)
		}
	}
}

// TestSystemDataPowerActionNames 动作到底层命令的映射:
// reboot_device→reboot、poweroff_device→poweroff。
func TestSystemDataPowerActionNames(t *testing.T) {
	var got string
	old := systemPowerAction
	systemPowerAction = func(action string) error {
		got = action
		return nil
	}
	t.Cleanup(func() { systemPowerAction = old })

	srv := &simpleAdminServer{cfg: serverConfig{}}
	cases := []struct {
		apiAction string
		want      string
	}{
		{"reboot_device", powerActionReboot},
		{"poweroff_device", powerActionPoweroff},
	}
	for _, tc := range cases {
		got = ""
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/system_data", strings.NewReader("action="+tc.apiAction))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.handleSystemData(rr, req)
		if got != tc.want {
			t.Fatalf("%s dispatched %q, want %q", tc.apiAction, got, tc.want)
		}
		if !strings.Contains(rr.Body.String(), `"ok":true`) {
			t.Fatalf("%s body = %s, want ok", tc.apiAction, rr.Body.String())
		}
	}
}

// TestSystemDataPowerActionFailure 命令无法启动时返回明确失败,不误报成功。
func TestSystemDataPowerActionFailure(t *testing.T) {
	old := systemPowerAction
	systemPowerAction = func(action string) error { return errors.New("reboot 命令不可用") }
	t.Cleanup(func() { systemPowerAction = old })

	srv := &simpleAdminServer{cfg: serverConfig{}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system_data", strings.NewReader("action=reboot_device"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleSystemData(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "reboot 命令不可用") {
		t.Fatalf("body = %s, want ok=false with error", body)
	}
}

// TestNativeDevicePowerRejectsUnknownAction 未知电源动作在触碰系统前即被拒绝。
func TestNativeDevicePowerRejectsUnknownAction(t *testing.T) {
	if err := nativeDevicePower("halt"); err == nil {
		t.Fatalf("nativeDevicePower(halt) accepted unknown action")
	}
}
