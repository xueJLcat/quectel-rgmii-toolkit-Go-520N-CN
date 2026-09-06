// page_diag.go 提供网络诊断页的 /api/diag_data 端点(HTTP 探测与 DNS 查询)。
package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	diagProbeTimeout      = 10 * time.Second // HTTP 探测总超时
	diagProbeMaxBody      = 64 * 1024        // 探测响应体最多读取后丢弃的字节数
	diagProbeMaxRedirects = 3                // 最多跟随的重定向次数
	diagDNSTimeout        = 10 * time.Second // DNS 查询总超时
	diagDNSDefaultPort    = "53"             // 未显式给端口时的 DNS 服务器端口
)

// diagDoer 抽象 http.Client.Do,便于单测注入 mock transport。
type diagDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// diagHTTPClient 是 http_probe 使用的客户端,声明为 var 便于测试替换;
// 生产路径走 net/http,重定向超过 3 次时返回最后一个 3xx 响应而非报错。
var diagHTTPClient diagDoer = &http.Client{
	Timeout: diagProbeTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) > diagProbeMaxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// diagResolverDialer 是 dns_query 指定 server 时的拨号函数,声明为 var 便于
// 测试注入;生产路径用 net.Dialer 拨 udp 到指定服务器。
var diagResolverDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// diagDomainPattern 校验主机名格式:字母/数字/连字符标签以点连接,允许末尾根点。
var diagDomainPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*\.?$`)

// handleDiagData 处理网络诊断页 /api/diag_data 的各动作。
//
// 响应契约:
//   - 参数校验失败(非法 action/target/domain/server)→ 400 + ok:false;
//   - 运行时失败(探测不通、DNS 解析失败、超时)→ 200 + ok:false + error 说明,
//     绝不把失败伪装成空结果。
func handleDiagData(w http.ResponseWriter, r *http.Request) {
	action := strings.ToLower(strings.TrimSpace(requestValue(r, "action")))
	switch action {
	case "", "http_probe":
		handleDiagHTTPProbe(w, r)
	case "dns_query":
		handleDiagDNSQuery(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unknown action"})
	}
}

// handleDiagHTTPProbe 对目标 URL 发起 GET 探测,报告状态码与首字节延迟。
func handleDiagHTTPProbe(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(requestValue(r, "target"))
	probeURL, err := normalizeDiagProbeTarget(target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid target"})
		return
	}
	req.Header.Set("User-Agent", "simpleadmin-diag")
	start := time.Now()
	resp, err := diagHTTPClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "target": probeURL, "error": diagProbeErrorText(err), "latencyMs": latencyMs})
		return
	}
	defer resp.Body.Close()
	// 读取并丢弃响应体(限 64KB),避免大页面占内存,同时让连接可复用。
	_, _ = io.CopyN(io.Discard, resp.Body, diagProbeMaxBody)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "target": probeURL, "statusCode": resp.StatusCode, "latencyMs": latencyMs})
}

// normalizeDiagProbeTarget 校验并规范化探测目标:接受 http(s):// URL 或
// host[:port](自动拼 http://);file://、ftp:// 等其他 scheme 一律拒绝。
func normalizeDiagProbeTarget(target string) (string, error) {
	if target == "" {
		return "", errors.New("missing target")
	}
	raw := target
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid target")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("unsupported scheme: " + u.Scheme)
	}
	if u.Hostname() == "" {
		return "", errors.New("missing target host")
	}
	return u.String(), nil
}

// diagProbeErrorText 把 http.Client.Do 返回的底层错误归类成可读原因。
func diagProbeErrorText(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS 解析失败: " + dnsErr.Name
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "探测超时(10s)"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "探测超时"
	}
	return "连接失败: " + err.Error()
}

// handleDiagDNSQuery 解析域名,可选指定 DNS 服务器(默认走系统 resolver)。
func handleDiagDNSQuery(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(requestValue(r, "domain"))
	if domain == "" || len(domain) > 253 || !diagDomainPattern.MatchString(domain) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid domain"})
		return
	}
	server := strings.TrimSpace(requestValue(r, "server"))
	serverAddr := ""
	if server != "" {
		var err error
		serverAddr, err = normalizeDiagDNSServer(server)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagDNSTimeout)
	defer cancel()
	start := time.Now()
	var addresses []string
	var err error
	if serverAddr == "" {
		addresses, err = net.DefaultResolver.LookupHost(ctx, domain)
	} else {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return diagResolverDialer(ctx, network, serverAddr)
			},
		}
		addresses, err = resolver.LookupHost(ctx, domain)
	}
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "domain": domain, "server": serverAddr, "error": diagDNSErrorText(err), "latencyMs": latencyMs})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "domain": domain, "server": serverAddr, "addresses": addresses, "latencyMs": latencyMs})
}

// normalizeDiagDNSServer 解析 server 参数:host 或 host:port,缺省端口 53。
func normalizeDiagDNSServer(server string) (string, error) {
	host, port := server, diagDNSDefaultPort
	if h, p, err := net.SplitHostPort(server); err == nil {
		host, port = h, p
		if n, convErr := strconv.Atoi(port); convErr != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid dns server port")
		}
	}
	if host == "" || (net.ParseIP(host) == nil && !diagDomainPattern.MatchString(host)) {
		return "", errors.New("invalid dns server")
	}
	return net.JoinHostPort(host, port), nil
}

// diagDNSErrorText 把 resolver 错误归类成可读原因。
func diagDNSErrorText(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsTimeout:
			return "DNS 查询超时(10s)"
		case dnsErr.IsNotFound:
			return "域名不存在: " + dnsErr.Name
		}
		return "DNS 查询失败: " + dnsErr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DNS 查询超时(10s)"
	}
	return "DNS 查询失败: " + err.Error()
}
