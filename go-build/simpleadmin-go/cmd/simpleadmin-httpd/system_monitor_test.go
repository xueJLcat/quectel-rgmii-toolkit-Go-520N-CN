package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseProcStatLine(t *testing.T) {
	// 字段:14=utime、15=stime、24=rss;名字含空格与括号。
	line := "4242 (kworker/u8:3 (busy)) S 2 4242 4242 0 -1 69238848 100 0 0 0 123 45 0 0 20 0 1 0 9876 409600 512 18446744073709551615"
	sample, ok := parseProcStatLine(line)
	if !ok {
		t.Fatalf("parseProcStatLine(%q) failed", line)
	}
	if sample.name != "kworker/u8:3 (busy)" {
		t.Fatalf("name = %q, want %q", sample.name, "kworker/u8:3 (busy)")
	}
	if sample.state != "S" {
		t.Fatalf("state = %q, want S", sample.state)
	}
	if sample.totalTicks != 123+45 {
		t.Fatalf("totalTicks = %d, want 168", sample.totalTicks)
	}
	if sample.rssPages != 512 {
		t.Fatalf("rssPages = %d, want 512", sample.rssPages)
	}

	cases := []string{
		"",
		"no parens at all",
		"12 (broken",
		"12 () S 2",
		"12 (short) S 2 1",
	}
	for _, tc := range cases {
		if _, ok := parseProcStatLine(tc); ok {
			t.Errorf("parseProcStatLine(%q) accepted invalid line", tc)
		}
	}
}

func TestParsePasswdUsers(t *testing.T) {
	content := "root:x:0:0:root:/root:/bin/sh\n" +
		"nobody:x:65534:65534:nobody:/nonexistent:/bin/false\n" +
		"dup:x:0:0:duplicate uid\n" +
		"broken::notanumber\n" +
		"short:x\n"
	users := parsePasswdUsers(content)
	if users[0] != "root" {
		t.Fatalf("uid 0 = %q, want root (first entry wins)", users[0])
	}
	if users[65534] != "nobody" {
		t.Fatalf("uid 65534 = %q, want nobody", users[65534])
	}
	if len(users) != 2 {
		t.Fatalf("users = %v, want 2 valid entries", users)
	}
}

func TestSortProcessList(t *testing.T) {
	list := []processInfo{
		{PID: 1, CPUPercent: 1.0, MemPercent: 9.0},
		{PID: 2, CPUPercent: 5.0, MemPercent: 2.0},
		{PID: 3, CPUPercent: 5.0, MemPercent: 7.0},
		{PID: 4, CPUPercent: 5.0, MemPercent: 7.0},
	}

	sortProcessList(list, sortProcessByCPU)
	wantCPU := []int{3, 4, 2, 1} // CPU 降序,次键内存降序,再 PID 升序
	for i, pid := range wantCPU {
		if list[i].PID != pid {
			t.Fatalf("cpu sort[%d] = pid %d, want %d (%v)", i, list[i].PID, pid, list)
		}
	}

	sortProcessList(list, sortProcessByMem)
	wantMem := []int{1, 3, 4, 2}
	for i, pid := range wantMem {
		if list[i].PID != pid {
			t.Fatalf("mem sort[%d] = pid %d, want %d (%v)", i, list[i].PID, pid, list)
		}
	}
}

func TestMockSystemMonitorData(t *testing.T) {
	for _, sortBy := range []string{sortProcessByCPU, sortProcessByMem} {
		data := mockSystemMonitorData(sortBy)
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal mock data: %v", err)
		}
		var decoded struct {
			CPUUsagePercent int           `json:"cpuUsagePercent"`
			RAMUsagePercent int           `json:"ramUsagePercent"`
			ProcessCount    int           `json:"processCount"`
			Processes       []processInfo `json:"processes"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal mock data: %v", err)
		}
		if decoded.CPUUsagePercent <= 0 || decoded.RAMUsagePercent <= 0 {
			t.Fatalf("mock metrics missing: %+v", decoded)
		}
		if len(decoded.Processes) != systemMonitorTopN {
			t.Fatalf("mock processes = %d, want %d", len(decoded.Processes), systemMonitorTopN)
		}
		if decoded.ProcessCount <= len(decoded.Processes) {
			t.Fatalf("processCount = %d, want total sampled (> top %d)", decoded.ProcessCount, len(decoded.Processes))
		}
		for i := 1; i < len(decoded.Processes); i++ {
			if sortBy == sortProcessByMem {
				if decoded.Processes[i-1].MemPercent < decoded.Processes[i].MemPercent {
					t.Fatalf("mock processes not sorted by mem at %d", i)
				}
			} else if decoded.Processes[i-1].CPUPercent < decoded.Processes[i].CPUPercent {
				t.Fatalf("mock processes not sorted by cpu at %d", i)
			}
		}
	}
}

func TestHandleSystemMonitorMock(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	for _, query := range []string{"", "?sort=mem", "?sort=cpu"} {
		rr := httptest.NewRecorder()
		srv.handleSystemMonitor(rr, httptest.NewRequest(http.MethodPost, "/api/system_monitor"+query, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("system_monitor%s status = %d", query, rr.Code)
		}
		var data map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
			t.Fatalf("system_monitor%s body not JSON: %v", query, err)
		}
		processes, ok := data["processes"].([]any)
		if !ok || len(processes) != systemMonitorTopN {
			t.Fatalf("system_monitor%s processes = %d, want %d", query, len(processes), systemMonitorTopN)
		}
		for _, key := range []string{"cpuUsagePercent", "ramUsagePercent", "ramUsedHuman", "ramTotalHuman", "loadAverage", "uptime"} {
			if _, exists := data[key]; !exists {
				t.Fatalf("system_monitor%s missing key %s", query, key)
			}
		}
	}
}

func TestHandleSystemMonitorSortParamNormalized(t *testing.T) {
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: true}}
	rr := httptest.NewRecorder()
	srv.handleSystemMonitor(rr, httptest.NewRequest(http.MethodPost, "/api/system_monitor?sort=bogus", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var data struct {
		Processes []processInfo `json:"processes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	for i := 1; i < len(data.Processes); i++ {
		if data.Processes[i-1].CPUPercent < data.Processes[i].CPUPercent {
			t.Fatalf("bogus sort param should fall back to cpu, order broken at %d", i)
		}
	}
}

// TestReadLoadAverageShape 负载字符串形状校验(真实 /proc 缺失时返回 "-")。
func TestReadLoadAverageShape(t *testing.T) {
	got := readLoadAverage()
	if got == "" {
		t.Fatalf("readLoadAverage returned empty string")
	}
	if got != "-" {
		fields := strings.Fields(got)
		if len(fields) != 3 {
			t.Fatalf("readLoadAverage = %q, want 3 fields or '-'", got)
		}
	}
}
