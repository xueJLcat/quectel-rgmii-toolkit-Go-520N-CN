package main

import (
	"encoding/json"
	"strings"
	"sync"
)

const (
	mockATPayloadKindDashboard = "dashboard"
	mockATPayloadKindQCAInfo   = "qcainfo"
	mockATPayloadKindQENG      = "qeng"
)

type mockATPayloadState struct {
	mu        sync.RWMutex
	dashboard string
	qcainfo   string
	qeng      string
}

var mockATPayloads mockATPayloadState

func setMockATPayload(kind, payload string) bool {
	kind = normalizeMockATPayloadKind(kind)
	if kind == "" {
		return false
	}
	payload = normalizeMockATPayloadText(payload)
	mockATPayloads.mu.Lock()
	switch kind {
	case mockATPayloadKindDashboard:
		mockATPayloads.dashboard = payload
	case mockATPayloadKindQCAInfo:
		mockATPayloads.qcainfo = payload
	case mockATPayloadKindQENG:
		mockATPayloads.qeng = payload
	}
	mockATPayloads.mu.Unlock()
	atCommandCache.invalidateReadCache()
	return true
}

func clearMockATPayload(kind string) bool {
	kind = normalizeMockATPayloadKind(kind)
	mockATPayloads.mu.Lock()
	defer mockATPayloads.mu.Unlock()
	switch kind {
	case "":
		mockATPayloads.dashboard = ""
		mockATPayloads.qcainfo = ""
		mockATPayloads.qeng = ""
	case mockATPayloadKindDashboard:
		mockATPayloads.dashboard = ""
	case mockATPayloadKindQCAInfo:
		mockATPayloads.qcainfo = ""
	case mockATPayloadKindQENG:
		mockATPayloads.qeng = ""
	default:
		return false
	}
	atCommandCache.invalidateReadCache()
	return true
}

func getMockATPayload(kind string) (string, bool) {
	kind = normalizeMockATPayloadKind(kind)
	mockATPayloads.mu.RLock()
	defer mockATPayloads.mu.RUnlock()
	var payload string
	switch kind {
	case mockATPayloadKindDashboard:
		payload = mockATPayloads.dashboard
	case mockATPayloadKindQCAInfo:
		payload = mockATPayloads.qcainfo
	case mockATPayloadKindQENG:
		payload = mockATPayloads.qeng
	default:
		return "", false
	}
	return payload, strings.TrimSpace(payload) != ""
}

func mockATPayloadStatusText() string {
	mockATPayloads.mu.RLock()
	defer mockATPayloads.mu.RUnlock()
	return "dashboard=" + mockPayloadOnOff(mockATPayloads.dashboard) +
		", qcainfo=" + mockPayloadOnOff(mockATPayloads.qcainfo) +
		", qeng=" + mockPayloadOnOff(mockATPayloads.qeng)
}

func mockATPayloadStatusMap() map[string]any {
	mockATPayloads.mu.RLock()
	defer mockATPayloads.mu.RUnlock()
	return map[string]any{
		"dashboard": mockPayloadOnOff(mockATPayloads.dashboard),
		"qcainfo":   mockPayloadOnOff(mockATPayloads.qcainfo),
		"qeng":      mockPayloadOnOff(mockATPayloads.qeng),
	}
}

func mockPayloadOnOff(payload string) string {
	if strings.TrimSpace(payload) == "" {
		return "default"
	}
	return "manual"
}

func normalizeMockATPayloadKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "at", "all", "full", "dashboard", "at-test", "at_test_payload":
		return mockATPayloadKindDashboard
	case "qca", "qcainfo", "qca-test", "qcainfo-test", "qcainfo_test_payload":
		return mockATPayloadKindQCAInfo
	case "qeng", "qeng-test", "qeng_test_payload":
		return mockATPayloadKindQENG
	default:
		return ""
	}
}

func normalizeMockATPayloadText(payload string) string {
	payload = strings.ReplaceAll(payload, "\r\n", "\n")
	payload = strings.ReplaceAll(payload, "\r", "\n")
	return strings.TrimSpace(payload)
}

func currentMockDashboardATResponse(command string) string {
	base := defaultMockDashboardATResponse(command)
	if payload, ok := getMockATPayload(mockATPayloadKindDashboard); ok {
		return ensureMockATEnvelope(command, payload)
	}
	if payload, ok := getMockATPayload(mockATPayloadKindQCAInfo); ok {
		base = replaceMockATLines(base, "+QCAINFO:", payload)
	}
	if payload, ok := getMockATPayload(mockATPayloadKindQENG); ok {
		base = replaceMockATLines(base, "+QENG:", payload)
	}
	return ensureMockATEnvelope(command, base)
}

func currentMockQCAInfoATResponse(command string) string {
	if payload, ok := getMockATPayload(mockATPayloadKindQCAInfo); ok {
		return ensureMockATEnvelope(command, payload)
	}
	return ensureMockATEnvelope(command, defaultMockQCAInfoPayload())
}

func currentMockQENGATResponse(command string) string {
	if payload, ok := getMockATPayload(mockATPayloadKindQENG); ok {
		return ensureMockATEnvelope(command, payload)
	}
	return ensureMockATEnvelope(command, defaultMockQENGPayload())
}

func currentMockDashboardParseJSON() string {
	data := parseDashboardAT(currentMockDashboardATResponse(pageATCommand(atKeyDashboard)))
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(buf)
}

func ensureMockATEnvelope(command, payload string) string {
	payload = normalizeMockATPayloadText(payload)
	if payload == "" {
		payload = "OK"
	}
	upper := strings.ToUpper(payload)
	if !strings.HasPrefix(strings.TrimSpace(upper), "AT") && strings.TrimSpace(command) != "" {
		payload = strings.TrimSpace(command) + "\r\n" + payload
	}
	if !strings.Contains(upper, "\nOK") && !strings.HasSuffix(strings.TrimSpace(upper), "OK") && !strings.Contains(upper, "ERROR") {
		payload += "\r\nOK"
	}
	return strings.ReplaceAll(payload, "\n", "\r\n") + "\r\n"
}

func replaceMockATLines(raw, prefix, payload string) string {
	payload = normalizeMockATPayloadText(payload)
	if payload == "" {
		return raw
	}
	prefixUpper := strings.ToUpper(prefix)
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	out := make([]string, 0, len(lines)+8)
	inserted := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), prefixUpper) {
			if !inserted {
				out = append(out, strings.Split(payload, "\n")...)
				inserted = true
			}
			continue
		}
		if !inserted && strings.EqualFold(trimmed, "OK") {
			out = append(out, strings.Split(payload, "\n")...)
			inserted = true
		}
		out = append(out, line)
	}
	if !inserted {
		out = append(out, strings.Split(payload, "\n")...)
	}
	return strings.Join(out, "\n")
}

// 以下默认载荷为首页/小区/载波的示例信号数据(中国电信 46011,5G SA n1 FDD 驻留),
// 与目标模块移远 RG520N-CN 的频段能力一致;身份类数据(IMEI/IMSI/ICCID)已在
// mock_at_responses.go 的常量中匿名化。
func defaultMockDashboardATResponse(command string) string {
	lines := []string{strings.TrimSpace(command), "", "+QSIMSTAT: 0,1", "", "+CSQ: 99,99", ""}
	lines = append(lines, mockQTEMPSensorLines()...)
	lines = append(lines,
		"",
		"+QUIMSLOT: 1",
		"",
		mockQSPNLine,
		"",
		`+QMAP: "WWAN",1,1,"IPV4","`+mockWWANIPv4+`"`,
		`+QMAP: "WWAN",1,1,"IPV6","`+mockWWANIPv6+`"`,
		"",
		`+QENG: "servingcell","NOCONN","NR5G-SA","FDD",460,11,24211A484,649,242000,428910,1,6,-86,-11,25,0,-`,
		"",
		`+QCAINFO: "PCC",428910,6,"NR5G BAND 1",649`,
		"",
		"+QGDNRCNT: 62817184,45605113",
		"",
		"+QGDCNT: 0,0",
		"",
		mockCGCONTRDP,
		"",
		"+QRSRP: -85,-86,-86,-83,NR5G",
		"",
		"OK",
	)
	return strings.Join(lines, "\r\n") + "\r\n"
}

func defaultMockQCAInfoPayload() string {
	return `+QCAINFO: "PCC",428910,6,"NR5G BAND 1",649`
}

func defaultMockQENGPayload() string {
	return `+QENG: "servingcell","NOCONN","NR5G-SA","FDD",460,11,24211A484,649,242000,428910,1,6,-86,-11,25,0,-`
}
