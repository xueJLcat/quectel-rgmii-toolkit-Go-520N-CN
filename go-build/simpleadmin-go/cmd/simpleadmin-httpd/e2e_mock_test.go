package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// e2eStaticDir 返回真实前端目录(与服务器默认静态目录一致),供 e2e 测试共用。
func e2eStaticDir(t *testing.T) string {
	t.Helper()
	staticDir := filepath.Clean(filepath.Join("..", "..", "..", "..", "development", "simpleadmin", "www"))
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		t.Fatalf("real www dir missing for e2e test: %v", err)
	}
	return staticDir
}

func e2eTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	staticDir := e2eStaticDir(t)
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	// 运行期配置(看门狗/定时任务等)写入临时目录,避免测试污染 /usrdata。
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })
	cfg := serverConfig{
		staticDir: staticDir,
		authFile:  authFile,
		ttlFile:   filepath.Join(dir, "ttlvalue"),
		noTLS:     true,
		mockMode:  true,
	}
	srv := &simpleAdminServer{cfg: cfg}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	resp, err := client.PostForm(ts.URL+"/api/login", form)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return ts, client
}

func TestE2EMockModeServesRealFrontend(t *testing.T) {
	ts, client := e2eTestServer(t)

	resp, err := client.Get(ts.URL + "/api/module_model")
	if err != nil {
		t.Fatalf("module_model: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"RG520N-CN"`) {
		t.Fatalf("module_model = %s, want contain RG520N-CN", body)
	}

	resp, err = client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), appVersionPlaceholder) {
		t.Fatalf("index.html served with unreplaced version placeholder")
	}
	for _, asset := range []string{"js/simpleadmin-api.js", "js/simpleadmin-lang.js", "js/simpleadmin-ui.js", "js/simpleadmin-reboot.js", "js/simpleadmin-poll.js", "css/tailwind.css"} {
		assetResp, err := client.Get(ts.URL + "/" + asset)
		if err != nil {
			t.Fatalf("GET %s: %v", asset, err)
		}
		assetResp.Body.Close()
		if assetResp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", asset, assetResp.StatusCode)
		}
	}
}

// e2eWebSocketCall 通过真实 /api/ws 通道调用业务接口。
func e2eWebSocketCall(t *testing.T, ts *httptest.Server, client *http.Client, id, method, path, body string) map[string]any {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	cookieHeader := ""
	for _, cookie := range client.Jar.Cookies(u) {
		cookieHeader += cookie.Name + "=" + cookie.Value + "; "
	}
	req := "GET /api/ws HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: " + ts.URL + "\r\n" +
		"Cookie: " + strings.TrimSuffix(cookieHeader, "; ") + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("ws handshake write: %v", err)
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("ws handshake failed: %q err=%v", statusLine, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("ws handshake headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}

	request, _ := json.Marshal(map[string]any{
		"id": id, "method": method, "path": path,
		"headers": map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		"body":    body,
	})
	payload := request
	mask := []byte{0x11, 0x22, 0x33, 0x44}
	frame := []byte{0x81}
	if len(payload) < 126 {
		frame = append(frame, byte(0x80|len(payload)))
	} else {
		frame = append(frame, 0x80|126)
		frame = append(frame, 0, 0)
		binary.BigEndian.PutUint16(frame[len(frame)-2:], uint16(len(payload)))
	}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("ws write: %v", err)
	}

	for {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(br, hdr); err != nil {
			t.Fatalf("ws read header: %v", err)
		}
		length := int64(hdr[1] & 0x7F)
		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(br, ext); err != nil {
				t.Fatalf("ws read ext: %v", err)
			}
			length = int64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(br, ext); err != nil {
				t.Fatalf("ws read ext: %v", err)
			}
			length = int64(binary.BigEndian.Uint64(ext))
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(br, data); err != nil {
			t.Fatalf("ws read payload: %v", err)
		}
		opcode := hdr[0] & 0x0F
		if opcode == 0x9 {
			continue
		}
		if opcode != 0x1 {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatalf("ws json: %v", err)
		}
		if message["type"] == "event" {
			continue
		}
		if message["id"] != id {
			continue
		}
		return message
	}
}

func TestE2EMockModePageAPIsOverWebSocket(t *testing.T) {
	ts, client := e2eTestServer(t)

	dashboard := e2eWebSocketCall(t, ts, client, "d1", "POST", "/api/dashboard_data", "")
	if status := fmt.Sprint(dashboard["status"]); status != "200" {
		t.Fatalf("dashboard_data status = %v, want 200 (error=%v)", dashboard["status"], dashboard["error"])
	}
	var dashboardData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(dashboard["body"])), &dashboardData); err != nil {
		t.Fatalf("dashboard body not json: %v", err)
	}
	if got := fmt.Sprint(dashboardData["network_provider"]); got != "中国电信" {
		t.Fatalf("dashboard network_provider = %q, want 中国电信", got)
	}
	if got := fmt.Sprint(dashboardData["network_mode"]); !strings.HasPrefix(got, "NR5G-SA") {
		t.Fatalf("dashboard network_mode = %q, want NR5G-SA*", got)
	}

	deviceInfo := e2eWebSocketCall(t, ts, client, "i1", "POST", "/api/device_info_data", "")
	var deviceData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(deviceInfo["body"])), &deviceData); err != nil {
		t.Fatalf("device_info body not json: %v", err)
	}
	if got := fmt.Sprint(deviceData["modelName"]); got != "RG520N-CN" {
		t.Fatalf("device_info modelName = %q, want RG520N-CN (hardcoded)", got)
	}
	if got := fmt.Sprint(deviceData["firmwareVersion"]); got != mockFirmwareVersion {
		t.Fatalf("device_info firmwareVersion = %q, want %q", got, mockFirmwareVersion)
	}

	for _, path := range []string{"/api/network_data", "/api/network_config_data", "/api/system_data", "/api/sms_data"} {
		resp := e2eWebSocketCall(t, ts, client, "p:"+path, "POST", path, "")
		if status := fmt.Sprint(resp["status"]); status != "200" {
			t.Fatalf("%s status = %v, want 200 (error=%v)", path, resp["status"], resp["error"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
			t.Fatalf("%s body not json: %v", path, err)
		}
	}

	signal := e2eWebSocketCall(t, ts, client, "sig1", "POST", "/api/signal_data", "")
	if status := fmt.Sprint(signal["status"]); status != "200" {
		t.Fatalf("signal_data status = %v, want 200 (error=%v)", signal["status"], signal["error"])
	}
	var signalData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(signal["body"])), &signalData); err != nil {
		t.Fatalf("signal_data body not json: %v", err)
	}
	if signalData["ok"] != true || signalData["rat"] != "NR5G" {
		t.Fatalf("signal_data ok/rat = %v/%v, want true/NR5G", signalData["ok"], signalData["rat"])
	}
	antennas, _ := signalData["antennas"].([]any)
	if len(antennas) != 4 {
		t.Fatalf("signal_data antennas = %#v, want 4 rows", signalData["antennas"])
	}
	carriers, _ := signalData["carriers"].([]any)
	if len(carriers) != 1 {
		t.Fatalf("signal_data carriers = %#v, want 1 PCC", signalData["carriers"])
	}

	atResp := e2eWebSocketCall(t, ts, client, "at1", "POST", "/api/at_data", "action=manual_at&command=ATI")
	if status := fmt.Sprint(atResp["status"]); status != "200" {
		t.Fatalf("at_data status = %v, want 200 (error=%v)", atResp["status"], atResp["error"])
	}
	var atData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(atResp["body"])), &atData); err != nil {
		t.Fatalf("at_data body not json: %v", err)
	}
	if atData["ok"] != true {
		t.Fatalf("at_data ok = %v, want true", atData["ok"])
	}
	if !strings.Contains(fmt.Sprint(atData["response"]), "Quectel") {
		t.Fatalf("at_data response = %q, want contain Quectel", fmt.Sprint(atData["response"]))
	}

	sms := e2eWebSocketCall(t, ts, client, "s2", "POST", "/api/sms_data", "")
	var smsData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(sms["body"])), &smsData); err != nil {
		t.Fatalf("sms_data body not json: %v", err)
	}
	messages, ok := smsData["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("sms_data messages = %#v, want 2 entries", smsData["messages"])
	}
}

func TestE2EMockModeAutomationAndHistoryAPIs(t *testing.T) {
	ts, client := e2eTestServer(t)

	// 三个自动化状态接口默认值可读。
	for _, path := range []string{"/api/get_watchdog", "/api/get_scheduler", "/api/get_sms_webhook"} {
		resp := e2eWebSocketCall(t, ts, client, "a:"+path, "POST", path, "")
		if status := fmt.Sprint(resp["status"]); status != "200" {
			t.Fatalf("%s status = %v, want 200", path, resp["status"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
			t.Fatalf("%s body not json: %v", path, err)
		}
	}

	// 设置看门狗参数后状态回读一致。
	setResp := e2eWebSocketCall(t, ts, client, "w1", "POST", "/api/set_watchdog", "enabled=1&failThreshold=3&cooldownMinutes=10")
	if status := fmt.Sprint(setResp["status"]); status != "200" {
		t.Fatalf("set_watchdog status = %v, want 200 (error=%v)", setResp["status"], setResp["error"])
	}
	getResp := e2eWebSocketCall(t, ts, client, "w2", "POST", "/api/get_watchdog", "")
	var watchdogStatus map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(getResp["body"])), &watchdogStatus); err != nil {
		t.Fatalf("get_watchdog body not json: %v", err)
	}
	if fmt.Sprint(watchdogStatus["failThreshold"]) != "3" || fmt.Sprint(watchdogStatus["enabled"]) != "true" {
		t.Fatalf("watchdog status = %#v, want enabled=true failThreshold=3", watchdogStatus)
	}

	// 历史数据接口返回合法 JSON(points 可为空)。
	history := e2eWebSocketCall(t, ts, client, "h1", "POST", "/api/history_data", "")
	if status := fmt.Sprint(history["status"]); status != "200" {
		t.Fatalf("history_data status = %v, want 200", history["status"])
	}
	var historyData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(history["body"])), &historyData); err != nil {
		t.Fatalf("history_data body not json: %v", err)
	}
	if _, ok := historyData["points"]; !ok {
		t.Fatalf("history_data missing points field: %#v", historyData)
	}
}

// TestE2EMockModeFrontendAssetsExist 守护 index.html 引用的本地前端资源真实存在:
// 提取所有 src="..."(script/img)与 href="..."(仅 .css)引用,跳过外链、
// /api/、/console 及纯锚点,逐个断言静态目录下文件存在且非目录。
// 任何页面脚本/样式被误删或改名都会让该测试失败。
func TestE2EMockModeFrontendAssetsExist(t *testing.T) {
	staticDir := e2eStaticDir(t)
	htmlBytes, err := os.ReadFile(filepath.Join(staticDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(htmlBytes)

	stripQuery := func(ref string) string {
		if i := strings.Index(ref, "?"); i >= 0 {
			return ref[:i]
		}
		return ref
	}

	var refs []string
	for _, m := range regexp.MustCompile(`src="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		refs = append(refs, m[1])
	}
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		if strings.HasSuffix(strings.ToLower(stripQuery(m[1])), ".css") {
			refs = append(refs, m[1])
		}
	}
	if len(refs) == 0 {
		t.Fatalf("index.html contains no asset references")
	}

	checked := 0
	for _, ref := range refs {
		path := stripQuery(ref)
		if path == "" || strings.HasPrefix(path, "#") {
			continue
		}
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") ||
			strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/console") {
			continue
		}
		info, err := os.Stat(filepath.Join(staticDir, path))
		if err != nil {
			t.Errorf("index.html references missing asset %q: %v", ref, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("index.html references directory %q, want regular file", ref)
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Fatalf("index.html yielded no local asset references to verify")
	}
}

// TestE2ETimeSyncAndSystemMonitor 经真实 /api/ws 网关验证新增的时间同步与
// 系统监控端点可用。
func TestE2ETimeSyncAndSystemMonitor(t *testing.T) {
	ts, client := e2eTestServer(t)

	get := e2eWebSocketCall(t, ts, client, "ts1", "POST", "/api/get_timesync", "")
	if status := fmt.Sprint(get["status"]); status != "200" {
		t.Fatalf("get_timesync status = %v, want 200 (error=%v)", get["status"], get["error"])
	}
	var tsStatus map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(get["body"])), &tsStatus); err != nil {
		t.Fatalf("get_timesync body not json: %v", err)
	}
	if got := fmt.Sprint(tsStatus["server"]); got != "ntp.aliyun.com" {
		t.Fatalf("get_timesync server = %q, want ntp.aliyun.com", got)
	}

	set := e2eWebSocketCall(t, ts, client, "ts2", "POST", "/api/set_timesync", "enabled=1&intervalMinutes=120")
	if status := fmt.Sprint(set["status"]); status != "200" {
		t.Fatalf("set_timesync status = %v, want 200 (error=%v)", set["status"], set["error"])
	}
	var tsSet map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(set["body"])), &tsSet); err != nil {
		t.Fatalf("set_timesync body not json: %v", err)
	}
	if ok, _ := tsSet["ok"].(bool); !ok {
		t.Fatalf("set_timesync = %v, want ok", tsSet)
	}

	// mock 模式下手动同步不访问网络、直接成功。
	now := e2eWebSocketCall(t, ts, client, "ts3", "POST", "/api/timesync_now", "")
	if status := fmt.Sprint(now["status"]); status != "200" {
		t.Fatalf("timesync_now status = %v, want 200 (error=%v)", now["status"], now["error"])
	}
	var tsNow map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(now["body"])), &tsNow); err != nil {
		t.Fatalf("timesync_now body not json: %v", err)
	}
	if ok, _ := tsNow["ok"].(bool); !ok {
		t.Fatalf("timesync_now = %v, want ok", tsNow)
	}

	for _, sort := range []string{"cpu", "mem"} {
		mon := e2eWebSocketCall(t, ts, client, "mon:"+sort, "POST", "/api/system_monitor?sort="+sort, "")
		if status := fmt.Sprint(mon["status"]); status != "200" {
			t.Fatalf("system_monitor sort=%s status = %v, want 200 (error=%v)", sort, mon["status"], mon["error"])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(mon["body"])), &data); err != nil {
			t.Fatalf("system_monitor body not json: %v", err)
		}
		processes, ok := data["processes"].([]any)
		if !ok || len(processes) == 0 || len(processes) > 20 {
			t.Fatalf("system_monitor processes = %d, want 1..20", len(processes))
		}
	}
}
