package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// diagDoerFunc 把函数适配成 diagDoer,供测试注入 mock transport。
type diagDoerFunc func(req *http.Request) (*http.Response, error)

func (f diagDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func diagCall(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handleDiagData(w, httptest.NewRequest(http.MethodGet, "/api/diag_data?"+query, nil))
	return w
}

func diagDecode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("body not json: %v (%s)", err, w.Body.String())
	}
	return data
}

// ---------- http_probe ----------

func TestDiagHTTPProbeResponseFields(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantStatus int
	}{
		{"200 正常响应", http.StatusOK, http.StatusOK},
		{"500 服务器错误仍算探测成功", http.StatusInternalServerError, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("User-Agent"); got != "simpleadmin-diag" {
					t.Errorf("User-Agent = %q, want simpleadmin-diag", got)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("probe-body"))
			}))
			defer ts.Close()
			w := diagCall(t, "action=http_probe&target="+url.QueryEscape(ts.URL))
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
			}
			data := diagDecode(t, w)
			if data["ok"] != true {
				t.Fatalf("ok = %v, want true (%v)", data["ok"], data)
			}
			if code, _ := data["statusCode"].(float64); int(code) != tc.wantStatus {
				t.Fatalf("statusCode = %v, want %d", data["statusCode"], tc.wantStatus)
			}
			lat, exists := data["latencyMs"].(float64)
			if !exists || lat < 0 {
				t.Fatalf("latencyMs = %v, want 存在的非负数字段", data["latencyMs"])
			}
			if data["target"] != ts.URL {
				t.Fatalf("target = %v, want %s", data["target"], ts.URL)
			}
		})
	}
}

func TestDiagDefaultActionIsHTTPProbe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	w := diagCall(t, "target="+url.QueryEscape(ts.URL))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != true {
		t.Fatalf("ok = %v, want true(缺省 action 应走 http_probe) (%v)", data["ok"], data)
	}
}

func TestDiagHTTPProbeInvalidTarget(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"空 target", "action=http_probe&target="},
		{"缺 target", "action=http_probe"},
		{"file 协议", "action=http_probe&target=" + url.QueryEscape("file:///etc/passwd")},
		{"ftp 协议", "action=http_probe&target=" + url.QueryEscape("ftp://example.com/x")},
		{"host 含空格", "action=http_probe&target=" + url.QueryEscape("exa mple.com")},
		{"只有端口没有 host", "action=http_probe&target=" + url.QueryEscape("http://:8080")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := diagCall(t, tc.query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("HTTP status = %d, want 400 (%s)", w.Code, w.Body.String())
			}
			data := diagDecode(t, w)
			if data["ok"] != false {
				t.Fatalf("ok = %v, want false", data["ok"])
			}
			if s, _ := data["error"].(string); s == "" {
				t.Fatalf("缺 error 说明: %v", data)
			}
		})
	}
}

func TestDiagHTTPProbeConnectionRefused(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := ts.URL
	ts.Close()
	cases := []struct {
		name   string
		target string
	}{
		{"已关闭的 httptest 服务", closedURL},
		{"回环无监听端口", "http://127.0.0.1:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := diagCall(t, "action=http_probe&target="+url.QueryEscape(tc.target))
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200(运行时失败不用 4xx) (%s)", w.Code, w.Body.String())
			}
			data := diagDecode(t, w)
			if data["ok"] != false {
				t.Fatalf("ok = %v, want false", data["ok"])
			}
			if s, _ := data["error"].(string); s == "" {
				t.Fatalf("缺 error 说明: %v", data)
			}
			if data["target"] != tc.target {
				t.Fatalf("target = %v, want %s", data["target"], tc.target)
			}
		})
	}
}

func TestDiagHTTPProbeMockDoerInjection(t *testing.T) {
	orig := diagHTTPClient
	t.Cleanup(func() { diagHTTPClient = orig })

	// 注入 mock doer:host[:port] 自动拼 http://,不触网,直接返回固定响应。
	diagHTTPClient = diagDoerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://example.test:8080" {
			t.Errorf("探测 URL = %s, want http://example.test:8080", req.URL.String())
		}
		if got := req.Header.Get("User-Agent"); got != "simpleadmin-diag" {
			t.Errorf("User-Agent = %q, want simpleadmin-diag", got)
		}
		return &http.Response{
			StatusCode: http.StatusTeapot,
			Body:       io.NopCloser(strings.NewReader("mock")),
			Header:     make(http.Header),
		}, nil
	})
	w := diagCall(t, "action=http_probe&target="+url.QueryEscape("example.test:8080"))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != true {
		t.Fatalf("ok = %v, want true", data["ok"])
	}
	if code, _ := data["statusCode"].(float64); int(code) != http.StatusTeapot {
		t.Fatalf("statusCode = %v, want 418", data["statusCode"])
	}

	// 注入返回错误的 mock doer:失败必须如实上报 ok:false,绝不伪装成成功。
	diagHTTPClient = diagDoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("mock boom")
	})
	w = diagCall(t, "action=http_probe&target="+url.QueryEscape("http://example.test"))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data = diagDecode(t, w)
	if data["ok"] != false {
		t.Fatalf("ok = %v, want false", data["ok"])
	}
	if s, _ := data["error"].(string); !strings.Contains(s, "mock boom") {
		t.Fatalf("error = %q, want 包含底层原因 mock boom", s)
	}
}

func TestDiagHTTPProbeRedirectLimit(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r"))
		if n <= 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/r%d", n-1), http.StatusFound)
	}))
	defer ts.Close()
	w := diagCall(t, "action=http_probe&target="+url.QueryEscape(ts.URL+"/r5"))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != true {
		t.Fatalf("ok = %v, want true(超过重定向上限应返回最后一个 3xx) (%v)", data["ok"], data)
	}
	if code, _ := data["statusCode"].(float64); int(code) != http.StatusFound {
		t.Fatalf("statusCode = %v, want 302", data["statusCode"])
	}
	// 最多跟随 3 次重定向:原始请求 + 3 次跟随 = 4 次命中。
	if got := atomic.LoadInt32(&hits); got != 4 {
		t.Fatalf("服务端命中次数 = %d, want 4(跟随不超过 3 次)", got)
	}
}

// ---------- dns_query ----------

func TestDiagDNSQueryInvalidParams(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"缺 domain", "action=dns_query"},
		{"空 domain", "action=dns_query&domain="},
		{"domain 含空格", "action=dns_query&domain=" + url.QueryEscape("exa mple.com")},
		{"domain 含斜杠", "action=dns_query&domain=" + url.QueryEscape("example.com/x")},
		{"server 端口越界", "action=dns_query&domain=localhost&server=" + url.QueryEscape("127.0.0.1:99999")},
		{"server 含斜杠", "action=dns_query&domain=localhost&server=" + url.QueryEscape("127.0.0.1/53")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := diagCall(t, tc.query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("HTTP status = %d, want 400 (%s)", w.Code, w.Body.String())
			}
			data := diagDecode(t, w)
			if data["ok"] != false {
				t.Fatalf("ok = %v, want false", data["ok"])
			}
			if s, _ := data["error"].(string); s == "" {
				t.Fatalf("缺 error 说明: %v", data)
			}
		})
	}
}

func TestDiagDNSQueryLocalhost(t *testing.T) {
	// 只解析 localhost(系统 resolver 命中本地 hosts 文件),不触外网 DNS。
	w := diagCall(t, "action=dns_query&domain=localhost")
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != true {
		t.Fatalf("ok = %v, want true (%v)", data["ok"], data)
	}
	addrs, _ := data["addresses"].([]any)
	if len(addrs) == 0 {
		t.Fatalf("addresses = %v, want 非空", data["addresses"])
	}
	if _, exists := data["latencyMs"]; !exists {
		t.Fatalf("缺 latencyMs 字段: %v", data)
	}
	if data["domain"] != "localhost" {
		t.Fatalf("domain = %v, want localhost", data["domain"])
	}
}

func TestDiagDNSQueryMockDialerInjection(t *testing.T) {
	orig := diagResolverDialer
	t.Cleanup(func() { diagResolverDialer = orig })

	// 注入 mock dialer:不触网,记录拨号地址并返回错误。
	var mu sync.Mutex
	var dialedTo []string
	diagResolverDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialedTo = append(dialedTo, address)
		mu.Unlock()
		return nil, errors.New("mock dial refused")
	}

	cases := []struct {
		name       string
		server     string
		wantDialed string
	}{
		{"显式端口", "127.0.0.1:5353", "127.0.0.1:5353"},
		{"缺省端口补 53", "127.0.0.1", "127.0.0.1:53"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			dialedTo = nil
			mu.Unlock()
			w := diagCall(t, "action=dns_query&domain=example.invalid&server="+url.QueryEscape(tc.server))
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200(运行时失败不用 4xx) (%s)", w.Code, w.Body.String())
			}
			data := diagDecode(t, w)
			if data["ok"] != false {
				t.Fatalf("ok = %v, want false", data["ok"])
			}
			if s, _ := data["error"].(string); !strings.Contains(s, "mock dial refused") {
				t.Fatalf("error = %q, want 包含底层原因 mock dial refused", s)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(dialedTo) == 0 {
				t.Fatalf("自定义 dialer 未被调用(server=%s 应走注入拨号)", tc.server)
			}
			for _, addr := range dialedTo {
				if addr != tc.wantDialed {
					t.Fatalf("拨号地址 = %s, want %s", addr, tc.wantDialed)
				}
			}
		})
	}
}

func TestDiagDNSQueryUnreachableServer(t *testing.T) {
	// 真实 dialer 拨回环无监听端口,验证不可达 server 如实返回 ok:false。
	w := diagCall(t, "action=dns_query&domain=example.invalid&server="+url.QueryEscape("127.0.0.1:1"))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != false {
		t.Fatalf("ok = %v, want false", data["ok"])
	}
	if s, _ := data["error"].(string); s == "" {
		t.Fatalf("缺 error 说明: %v", data)
	}
	if data["server"] != "127.0.0.1:1" {
		t.Fatalf("server = %v, want 127.0.0.1:1", data["server"])
	}
}

// ---------- unknown action ----------

func TestDiagUnknownAction(t *testing.T) {
	w := diagCall(t, "action=bogus")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("HTTP status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	data := diagDecode(t, w)
	if data["ok"] != false {
		t.Fatalf("ok = %v, want false", data["ok"])
	}
	if data["error"] != "unknown action" {
		t.Fatalf("error = %v, want \"unknown action\"", data["error"])
	}
}
