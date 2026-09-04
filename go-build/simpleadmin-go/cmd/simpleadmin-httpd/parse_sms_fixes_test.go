// parse_sms_fixes_test.go 覆盖本轮修复:
// atLines 过滤运行器错误文本(设备信息页不再把整段错误当作厂商);
// 文本模式正文保留以 "+" 开头的正文行、仍跳过协议行;
// 文本模式时间戳被逗号拆开后重新拼接;大写 "USBNET" 响应解析;
// 禁用 IP 透传响应结构不变且后台结果事件可广播。
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// ---------- atLines 过滤运行器错误文本 ----------

func TestATLinesFilterRunnerErrorText(t *testing.T) {
	// 错误文案按 at_runner.go / native_serial.go / smd_reader.go 的真实格式构造。
	errorLines := []string{
		"all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR",
		"all AT device candidates failed after smd recovery retry: /dev/smd11: read failed: timeout waiting for OK/ERROR",
		"all AT device candidates failed (action not retried): /dev/smd11: write failed: input/output error",
		"all AT device candidates failed: /dev/smd11: open /dev/smd11: no such file or directory",
		"no AT device candidates configured",
		"SMS AT transaction failed after retry: /dev/smd11: timeout waiting for SMS prompt",
		"open AT lock file /tmp/simpleadmin-go-at.lock: permission denied",
		"SMD shell stdin writer failed: write /dev/stdin: broken pipe",
	}
	for _, line := range errorLines {
		if !isRunnerErrorLine(line) {
			t.Fatalf("isRunnerErrorLine(%q) = false, want true", line)
		}
		if lines := atLines(line); len(lines) != 0 {
			t.Fatalf("atLines(%q) = %#v, want empty", line, lines)
		}
	}

	// 合法数据行不得误伤;错误行与数据行混排时只丢错误行。
	raw := "AT+CGMI\r\nQuectel\r\nall AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR\r\nOK\r\n"
	lines := atLines(raw)
	if len(lines) != 2 || lines[0] != "AT+CGMI" || lines[1] != "Quectel" {
		t.Fatalf("atLines(mixed) = %#v, want [AT+CGMI Quectel]", lines)
	}
	// 以 "+" 开头的模块协议行即使包含特征词也不按运行器错误过滤。
	if isRunnerErrorLine("+QIND: timeout waiting for something") {
		t.Fatalf("plus-prefixed module line must not be treated as runner error")
	}
	// atResponseOK 的 OK/ERROR 判定不受过滤逻辑影响。
	if !atResponseOK("AT+CGMI\r\nQuectel\r\nOK\r\n") {
		t.Fatalf("atResponseOK(normal response) = false, want true")
	}
	if atResponseOK("all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR") {
		t.Fatalf("atResponseOK(runner error text) = true, want false")
	}
}

func TestParseDeviceInfoIgnoresRunnerErrorText(t *testing.T) {
	raw := "all AT device candidates failed after retry: /dev/smd11: read failed: timeout waiting for OK/ERROR"
	data := parseDeviceInfoAT(raw)
	if got := stringValue(data["manufacturer"]); got != "-" {
		t.Fatalf("manufacturer = %q, want - (runner error text must not be parsed as vendor)", got)
	}
	if got := stringValue(data["modelName"]); got != "-" {
		t.Fatalf("modelName = %q, want -", got)
	}
	if got := stringValue(data["firmwareVersion"]); got != "-" {
		t.Fatalf("firmwareVersion = %q, want -", got)
	}
	if got := stringValue(data["imei"]); got != "-" {
		t.Fatalf("imei = %q, want -", got)
	}
}

// ---------- 文本模式正文保留 + 开头行 ----------

func TestParseSMSListTextBodyKeepsPlusLedLines(t *testing.T) {
	raw := `+CSCA: "+8613800138000",145
+CMGL: 1,1,"+8613800138000",,"26/08/30,10:20:30+32"
+1 2345 6789
+CPMS: 2,2,50
Call me back
OK`
	data := parseSMSListAT(raw, "ME")
	messages := data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1: %#v", len(messages), messages)
	}
	text := stringValue(messages[0]["text"])
	if !strings.Contains(text, "+1 2345 6789") {
		t.Fatalf("text = %q, want keep plus-led body line", text)
	}
	if !strings.Contains(text, "Call me back") {
		t.Fatalf("text = %q, want keep plain body line", text)
	}
	if strings.Contains(text, "+CPMS") {
		t.Fatalf("text = %q, must not contain protocol line", text)
	}

	// 已知协议前缀仍须跳过,普通 + 开头行保留。
	for _, protocol := range []string{"+CMGL: 2,1", "+CMGR: 0", "+CSCA: \"+86\"", "+CME ERROR: 500", "+CMS ERROR: 30"} {
		if !isSMSProtocolLine(protocol) {
			t.Fatalf("isSMSProtocolLine(%q) = false, want true", protocol)
		}
	}
	if isSMSProtocolLine("+1 2345 6789") {
		t.Fatalf("isSMSProtocolLine(+1 ...) = true, must stay body text")
	}
}

// ---------- 文本模式时间戳拼接 ----------

func TestParseSMSListTextModeJoinsSplitTimestamp(t *testing.T) {
	// 时间戳未加引号时被 csvFields 按逗号拆成两段,须拼回完整时间。
	raw := `+CMGL: 1,1,"+8613800138000","China Mobile",26/08/30,10:20:30+32
Hello world
OK`
	data := parseSMSListAT(raw, "ME")
	messages := data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1: %#v", len(messages), messages)
	}
	if got := stringValue(messages[0]["date"]); got != "26/08/30,10:20:30+32" {
		t.Fatalf("date = %q, want 26/08/30,10:20:30+32 (joined)", got)
	}
	if got := stringValue(messages[0]["text"]); got != "Hello world" {
		t.Fatalf("text = %q, want Hello world", got)
	}

	// 已加引号的完整时间戳(单字段)不受拼接逻辑影响。
	quoted := `+CMGL: 2,1,"+8613800138000",,"26/08/30,10:20:30+32"
Hi
OK`
	data = parseSMSListAT(quoted, "ME")
	messages = data["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1: %#v", len(messages), messages)
	}
	if got := stringValue(messages[0]["date"]); got != "26/08/30,10:20:30+32" {
		t.Fatalf("quoted date = %q, want unchanged 26/08/30,10:20:30+32", got)
	}
}

// ---------- 大写 "USBNET" 解析 ----------

func TestParseNetworkConfigStatusUSBNETCaseInsensitive(t *testing.T) {
	raw := `AT+QMAP="MPDN_RULE";+QMAP="DHCPV6DNS";+QCFG="usbnet";+QMAP="DMZ";+QMAP="DHCPV4DNS"
+QMAP: "MPDN_rule",0,0,0,0,0
+QMAP: "DHCPV6DNS","disable"
+QCFG: "USBNET",1
+QMAP: "DMZ",0,4
OK`
	data := parseNetworkConfigStatusAT(raw)
	if got := stringValue(data["currentUsbNetMode"]); got != "ECM" {
		t.Fatalf("currentUsbNetMode = %q, want ECM for uppercase \"USBNET\"", got)
	}

	lower := strings.Replace(raw, `"USBNET",1`, `"usbnet",3`, 1)
	data = parseNetworkConfigStatusAT(lower)
	if got := stringValue(data["currentUsbNetMode"]); got != "RNDIS" {
		t.Fatalf("currentUsbNetMode = %q, want RNDIS for lowercase code 3", got)
	}

	unknown := strings.Replace(raw, `"USBNET",1`, `"USBNET",9`, 1)
	data = parseNetworkConfigStatusAT(unknown)
	if got := stringValue(data["currentUsbNetMode"]); got != "未知" {
		t.Fatalf("currentUsbNetMode = %q, want 未知 for unknown code", got)
	}

	// 字段数不足时各字段位读取必须有保护,保持默认值不 panic。
	short := "+QMAP: \"MPDN_rule\"\n+QMAP: \"DMZ\"\n+QMAP: \"LANIP\"\nOK"
	data = parseNetworkConfigStatusAT(short)
	if data["ipPassStatus"] != false {
		t.Fatalf("ipPassStatus = %v, want false for short MPDN_rule record", data["ipPassStatus"])
	}
	if got := stringValue(data["dmzMode"]); got != "0" {
		t.Fatalf("dmzMode = %q, want default 0 for short DMZ record", got)
	}
	if got := stringValue(data["lanIpStart"]); got != "" {
		t.Fatalf("lanIpStart = %q, want empty for short LANIP record", got)
	}
}

// ---------- 禁用 IP 透传:响应结构不变 + 结果事件广播 ----------

func TestIPPassthroughDisableResponseUnchanged(t *testing.T) {
	ts, client := e2eTestServer(t)
	resp := e2eWebSocketCall(t, ts, client, "iptfix1", "POST", "/api/network_config_data", "action=ip_passthrough&enabled=0")
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("ip_passthrough disable status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("ip_passthrough disable body not json: %v", err)
	}
	if data["ok"] != true {
		t.Fatalf("ip_passthrough disable ok = %v, want true", data["ok"])
	}
	if data["reboot"] != true {
		t.Fatalf("ip_passthrough disable reboot = %v, want true", data["reboot"])
	}
	if got := fmt.Sprint(data["rebootCountdownSeconds"]); got != "40" {
		t.Fatalf("ip_passthrough disable rebootCountdownSeconds = %v, want 40", data["rebootCountdownSeconds"])
	}
	if got := fmt.Sprint(data["message"]); !strings.Contains(got, "重启") {
		t.Fatalf("ip_passthrough disable message = %q, want reboot notice", got)
	}
}

func TestIPPassthroughResultBroadcast(t *testing.T) {
	configCommands := []string{`AT+QMAP="MPDN_RULE",0`, `AT+QMAPWAC=1`}
	rebootCommand := `AT+CFUN=1,1`
	okResponse := func(command string) string { return command + "\r\nOK\r\n" }

	// 成功路径:前两步均收到 OK 后,CFUN 即使完全无应答(模块先重启、
	// 后应答是常态)也必须判定成功,且三步按序执行。
	var ran []string
	ranAll := func(command string) string {
		ran = append(ran, command)
		if command == rebootCommand {
			return ""
		}
		return okResponse(command)
	}
	if got := ipPassthroughDisableOutcome(ranAll, configCommands, rebootCommand, 0); got != "true" {
		t.Fatalf("outcome = %q, want \"true\" when config steps succeed even if CFUN never answers", got)
	}
	if len(ran) != 3 || ran[0] != configCommands[0] || ran[1] != configCommands[1] || ran[2] != rebootCommand {
		t.Fatalf("executed commands = %#v, want both config steps then CFUN in order", ran)
	}

	// 第一步失败 → "false",且不得继续执行第二步与重启。
	ran = nil
	failFirst := func(command string) string {
		ran = append(ran, command)
		if command == configCommands[0] {
			return "ERROR"
		}
		return okResponse(command)
	}
	if got := ipPassthroughDisableOutcome(failFirst, configCommands, rebootCommand, 0); got != "false" {
		t.Fatalf("outcome = %q, want \"false\" when the first config step fails", got)
	}
	if len(ran) != 1 || ran[0] != configCommands[0] {
		t.Fatalf("executed commands = %#v, want only the failed first step and no reboot", ran)
	}

	// 第二步失败 → "false",同样不得执行重启。
	ran = nil
	failSecond := func(command string) string {
		ran = append(ran, command)
		if command == configCommands[1] {
			return "ERROR"
		}
		return okResponse(command)
	}
	if got := ipPassthroughDisableOutcome(failSecond, configCommands, rebootCommand, 0); got != "false" {
		t.Fatalf("outcome = %q, want \"false\" when the second config step fails", got)
	}
	if len(ran) != 2 || ran[1] != configCommands[1] {
		t.Fatalf("executed commands = %#v, want both config steps but no reboot", ran)
	}

	// 广播契约:负载值必须是字符串 "true"/"false"(前端按
	// String(detail.ok)==='false' 消费),经 WebSocket 原样送达。
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		serverConn.Close()
		clientConn.Close()
	})

	ws := &apiWebSocketConn{conn: serverConn, br: bufio.NewReader(strings.NewReader(""))}
	registerAPIWebSocketClient(ws)
	t.Cleanup(func() { unregisterAPIWebSocketClient(ws) })

	frameCh := make(chan []byte, 16)
	go func() {
		for {
			hdr := make([]byte, 2)
			if _, err := io.ReadFull(clientConn, hdr); err != nil {
				return
			}
			length := int64(hdr[1] & 0x7F)
			if length == 126 {
				ext := make([]byte, 2)
				if _, err := io.ReadFull(clientConn, ext); err != nil {
					return
				}
				length = int64(binary.BigEndian.Uint16(ext))
			}
			payload := make([]byte, length)
			if _, err := io.ReadFull(clientConn, payload); err != nil {
				return
			}
			frameCh <- payload
		}
	}()

	waitFrame := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case payload := <-frameCh:
				var msg apiWebSocketEventMessage
				// Data 为 map[string]string,JSON 布尔/数值无法解码进来,
				// 解码成功即证明负载值是字符串类型。
				if err := json.Unmarshal(payload, &msg); err != nil {
					t.Fatalf("broadcast frame not json with string payload: %v", err)
				}
				if msg.Type != "event" || msg.Event != "ip_passthrough_result" {
					continue
				}
				if msg.Data["ok"] != want {
					continue
				}
				return
			case <-deadline:
				t.Fatalf("timed out waiting for ip_passthrough_result frame with ok=%q", want)
			}
		}
	}

	// 失败路径帧:后台禁用流程只会广播 "true","false" 帧可由本次广播唯一确认。
	broadcastAPIWebSocketEvent("ip_passthrough_result", map[string]string{"ok": "false"})
	waitFrame("false")

	// 成功路径帧:其它测试后台禁用流程也可能广播同名同值 "true" 帧,
	// 契约相同,收到任一即满足。
	broadcastAPIWebSocketEvent("ip_passthrough_result", map[string]string{"ok": "true"})
	waitFrame("true")
}
