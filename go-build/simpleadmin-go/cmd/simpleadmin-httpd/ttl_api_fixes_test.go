package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ttlFixesMockServer 构造 mock 模式直连 API 服务器,
// 并将运行期 TTL 文件指向临时目录,避免测试污染 /usrdata。
func ttlFixesMockServer(t *testing.T) (*httptest.Server, *http.Client, string) {
	t.Helper()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("make static dir: %v", err)
	}
	ttlFile := filepath.Join(dir, "ttlvalue")
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = ttlFile
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	cfg := serverConfig{
		staticDir: staticDir,
		authFile:  authFile,
		ttlFile:   ttlFile,
		noTLS:     true,
		mockMode:  true,
		directAPI: true,
	}
	srv := &simpleAdminServer{cfg: cfg}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	directAPILogin(t, ts, client)
	return ts, client, ttlFile
}

func TestMockModeSetTTLPersistsWithoutRealCommands(t *testing.T) {
	ts, client, ttlFile := ttlFixesMockServer(t)
	// 清空 PATH:若 mock 路径误执行真实命令必然失败并留下 "failed" 日志。
	t.Setenv("PATH", t.TempDir())

	resp, err := client.Get(ts.URL + "/api/set_ttl?ttlvalue=65")
	if err != nil {
		t.Fatalf("GET set_ttl: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set_ttl status = %d, want 200, body = %s", resp.StatusCode, body)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("set_ttl body not json: %v, body = %s", err, body)
	}
	logsRaw, _ := payload["debug_logs"].([]any)
	logs := make([]string, 0, len(logsRaw))
	for _, line := range logsRaw {
		if s, ok := line.(string); ok {
			logs = append(logs, s)
		}
	}
	joined := strings.Join(logs, "\n")
	if len(logs) == 0 || !strings.Contains(joined, "Received parameter: ttlvalue=65") {
		t.Fatalf("debug_logs = %#v, want set_ttl success logs", logs)
	}
	if strings.Contains(joined, "failed") {
		t.Fatalf("mock set_ttl must not run real commands or fail: %s", joined)
	}
	if !strings.Contains(joined, "Mock mode: TTL rules are simulated only") || !strings.Contains(joined, "TTL value saved") {
		t.Fatalf("debug_logs = %#v, want simulated apply logs", logs)
	}

	data, err := os.ReadFile(ttlFile)
	if err != nil {
		t.Fatalf("ttlvalue file not persisted in mock mode: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "65" {
		t.Fatalf("ttlvalue file = %q, want 65", got)
	}

	resp, err = client.Get(ts.URL + "/api/get_ttl_status")
	if err != nil {
		t.Fatalf("GET get_ttl_status: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("get_ttl_status body not json: %v, body = %s", err, body)
	}
	if status["isEnabled"] != true || status["ttl"] != float64(65) {
		t.Fatalf("get_ttl_status = %#v, want isEnabled=true ttl=65", status)
	}
}

func TestSetNativeTTLApplyFailureDoesNotPersist(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real iptables apply path is linux-only")
	}
	dir := t.TempDir()
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })
	// 清空 PATH,令 iptables/ip6tables 必然执行失败。
	t.Setenv("PATH", t.TempDir())

	logs := setNativeTTL(65)
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "not applied") || !strings.Contains(joined, "not persisted") {
		t.Fatalf("logs = %q, want not-applied/not-persisted marker on failure", joined)
	}
	if _, err := os.Stat(runtimeTTLValueFile); !os.IsNotExist(err) {
		t.Fatalf("ttlvalue file must not be written when apply fails, stat err = %v", err)
	}
	if value, enabled := currentTTLValue(); enabled || value != 0 {
		t.Fatalf("currentTTLValue = (%d, %t), want disabled after failed apply", value, enabled)
	}
}

func TestApplySavedTTLAtStartupInitializesMissingFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("startup TTL apply is linux-only")
	}
	dir := t.TempDir()
	oldTTLFile := runtimeTTLValueFile
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	applySavedTTLAtStartup()

	data, err := os.ReadFile(runtimeTTLValueFile)
	if err != nil {
		t.Fatalf("missing ttlvalue file should be initialized: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "0" {
		t.Fatalf("initialized ttlvalue = %q, want 0", got)
	}
}

func TestApplySavedTTLAtStartupKeepsFileOnReadError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("startup TTL apply is linux-only")
	}
	dir := t.TempDir()
	oldTTLFile := runtimeTTLValueFile
	// 将 TTL 文件路径指向一个目录,读取时产生 EISDIR 错误(非 NotExist)。
	runtimeTTLValueFile = filepath.Join(dir, "ttlvalue")
	if err := os.MkdirAll(runtimeTTLValueFile, 0755); err != nil {
		t.Fatalf("make dir over ttlvalue path: %v", err)
	}
	t.Cleanup(func() { runtimeTTLValueFile = oldTTLFile })

	if _, err := readTTLValue(); errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readTTLValue error = %v, want non-NotExist read error", err)
	}

	applySavedTTLAtStartup()

	info, err := os.Stat(runtimeTTLValueFile)
	if err != nil {
		t.Fatalf("ttlvalue path disappeared after read error: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("read error must not overwrite existing ttlvalue path")
	}
}

func TestHandleSendSMSAcceptsPOSTFormBody(t *testing.T) {
	ts, client, _ := ttlFixesMockServer(t)

	resp, err := client.PostForm(ts.URL+"/api/send_sms", url.Values{
		"number": {"10001"},
		"msg":    {"hello mock"},
	})
	if err != nil {
		t.Fatalf("POST send_sms: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	text := strings.TrimSpace(string(body))
	if strings.Contains(text, "missing number or msg") {
		t.Fatalf("send_sms still misses POST form params: %s", text)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(text, "OK segments=") {
		t.Fatalf("send_sms status = %d, body = %q, want OK segments=...", resp.StatusCode, text)
	}
}
