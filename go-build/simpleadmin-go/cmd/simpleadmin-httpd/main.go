package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultStaticDir      = "/usrdata/simpleadmin/www"
	defaultAuthFile       = "/usrdata/simpleadmin/simpleadmin.auth"
	defaultCertFile       = "/usrdata/simpleadmin/server.crt"
	defaultKeyFile        = "/usrdata/simpleadmin/server.key"
	defaultCACertFile     = "/usrdata/simpleadmin/zbims-ca.crt"
	defaultCAKeyFile      = "/usrdata/simpleadmin/zbims-ca.key"
	defaultATDevice       = "/dev/smd11"
	defaultATDevicesFile  = "/usrdata/simpleadmin/at_devices.conf"
	defaultTTLValueFile   = "/usrdata/simpleadmin/ttlvalue"
	legacyTTLValueFile    = "/usrdata/simplefirewall/ttlvalue"
	languageConfigRelPath = "config/get_language.json"
	defaultLanguage       = "zh-CN"
	themeConfigRelPath    = "config/get_theme.json"
	defaultTheme          = "light"
)

type serverConfig struct {
	staticDir     string
	authFile      string
	certFile      string
	keyFile       string
	caCertFile    string
	caKeyFile     string
	httpAddr      string
	httpsAddr     string
	ttlFile       string
	firewallFile  string
	atDevices     string
	atDevicesFile string
	noTLS         bool
	mockMode      bool
	atDebug       bool
	directAPI     bool
	atProxySock   string
}

type authConfig struct {
	Username string
	Password string
}

var processStartTime = time.Now()
var runtimeTTLValueFile = defaultTTLValueFile
var runtimeATDevices = defaultATDeviceCandidates()
var atDebugEnabled = envBool("SIMPLEADMIN_AT_DEBUG")

type languageConfig struct {
	Language string `json:"language"`
}

type themeConfig struct {
	Theme string `json:"theme"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) > 1 && os.Args[1] == "__smd-reader" {
		runSMDReaderCommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "ttl" {
		runTTLCommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "at" {
		runATCLICommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "at-client" {
		runATClientCommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "dnsmasq-cleanup" {
		runDnsmasqCleanupCommand(os.Args[2:])
		return
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	runServeCommand(args)
}

func newServeFlagSet(cfg *serverConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.StringVar(&cfg.staticDir, "static", defaultStaticDir, "静态网页目录")
	fs.StringVar(&cfg.authFile, "auth-file", defaultAuthFile, "登录用户密码文件，格式为 username:password")
	fs.StringVar(&cfg.certFile, "cert", defaultCertFile, "HTTPS 服务器证书文件")
	fs.StringVar(&cfg.keyFile, "key", defaultKeyFile, "HTTPS 服务器私钥文件")
	fs.StringVar(&cfg.caCertFile, "ca-cert", defaultCACertFile, "HTTPS 本地根 CA 证书文件")
	fs.StringVar(&cfg.caKeyFile, "ca-key", defaultCAKeyFile, "HTTPS 本地根 CA 私钥文件")
	fs.StringVar(&cfg.httpAddr, "http", ":80", "HTTP 监听地址")
	fs.StringVar(&cfg.httpsAddr, "https", ":443", "HTTPS 监听地址")
	fs.StringVar(&cfg.ttlFile, "ttl-file", defaultTTLValueFile, "TTL 状态文件")
	fs.StringVar(&cfg.firewallFile, "firewall-file", defaultFirewallPortsFile, "SimpleFirewall 端口阻止配置文件")
	fs.StringVar(&cfg.atDevices, "at-devices", "", "AT 设备列表，多个路径用逗号分隔；默认直接使用 /dev/smd11")
	fs.StringVar(&cfg.atDevicesFile, "at-devices-file", defaultATDevicesFile, "AT 客户端设备配置文件，每行一个设备路径")
	fs.BoolVar(&cfg.noTLS, "no-tls", true, "只启用 HTTP，不启用 HTTPS")
	fs.BoolVar(&cfg.mockMode, "mock", false, "启用本地测试 mock 模式，不访问真实串口、systemd 或 TTL 规则")
	fs.BoolVar(&cfg.atDebug, "at-debug", envBool("SIMPLEADMIN_AT_DEBUG"), "启用 AT 详细调试日志")
	fs.BoolVar(&cfg.directAPI, "direct-api", envBool("SIMPLEADMIN_DIRECT_API"), "启用业务 API 直连模式，将业务接口直接注册为 HTTP 路由（默认仅经 /api/ws 网关分发）")
	fs.StringVar(&cfg.atProxySock, "at-proxy-sock", defaultATProxySock, "AT 代理 Unix Socket 路径;主进程经此暴露受限 AT 执行能力,外部程序用 at-client 子命令调用")
	return fs
}

// httpsAddrExplicitlySet reports whether -https was passed on the command
// line, so it can be warned about when -no-tls silently ignores it.
func httpsAddrExplicitlySet(fs *flag.FlagSet) bool {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "https" {
			explicit = true
		}
	})
	return explicit
}

func runServeCommand(args []string) {
	cfg := serverConfig{}
	fs := newServeFlagSet(&cfg)
	_ = fs.Parse(args)

	if cfg.noTLS && httpsAddrExplicitlySet(fs) {
		log.Printf("警告: 指定了 -https=%s 但 -no-tls 仍为 true,HTTPS 不会启用;如需启用 HTTPS 请追加 -no-tls=false", cfg.httpsAddr)
	}

	runtimeTTLValueFile = cfg.ttlFile
	runtimeFirewallPortsFile = cfg.firewallFile
	atDebugEnabled = cfg.atDebug
	runtimeATDevices = collectATDeviceCandidates(cfg.atDevices, cfg.atDevicesFile)
	log.Printf("AT debug logging: %v", atDebugEnabled)
	log.Printf("AT client device candidates: %s", strings.Join(runtimeATDevices, ", "))
	logATDeviceStat("client candidate", runtimeATDevices)
	if err := validateStaticDir(cfg.staticDir); err != nil {
		log.Fatalf("静态网页目录无效: %v", err)
	}
	if absStaticDir, err := filepath.Abs(cfg.staticDir); err == nil {
		log.Printf("Static web directory: %s", absStaticDir)
	} else {
		log.Printf("Static web directory: %s", cfg.staticDir)
	}
	if cfg.mockMode {
		log.Printf("ZBIMS mock mode enabled: hardware-dependent APIs return local test data")
		startMockATConsole(true)
	}

	if err := ensureAuthFile(cfg.authFile); err != nil {
		log.Fatalf("初始化认证文件失败: %v", err)
	}

	if !cfg.mockMode {
		log.Printf("AT APIs use direct /dev/smd11 access")
		applySavedTTLAtStartup()
		applySavedFirewallAtStartup()
		// 上游 DNS 状态自愈:异步执行,不阻塞 HTTP 启动;该函数不依赖 AT,
		// 仅文件操作与 systemctl,失败仅日志。
		go reconcileDNSUpstreamAtStartup()
	}
	atCommandCache.Start(cfg.mockMode)

	app := &simpleAdminServer{cfg: cfg}
	// 断网自愈看门狗轮询器按配置启停：未启用时不驻留循环。
	syncWatchdogPoller(app)
	startScheduler(app)
	// 时间同步轮询器按配置启停：未启用时不驻留循环。
	syncTimeSyncPoller(app)
	// 短信转发轮询器按配置启停：未启用时不驻留循环。
	syncSMSWebhookPoller(app)
	// 静态绑定补齐在后台延迟进行,避免与开机 AT 初始化竞争。
	go app.reconcileMacBindAtStartup()
	// 短信存储路由自愈(后台延迟,等待开机保护期):确保入站短信落在 ME。
	go app.assertSMSStorageRoutingAtStartup()
	handler := app.routes()

	// AT 代理:外部程序经 Unix Socket 复用本进程的 AT 通道,不再各自打开 /dev/smd11。
	// 启动失败(如套接字被占)仅告警,不影响 Web 服务。
	if _, err := startATProxy(cfg.atProxySock, cfg.mockMode); err != nil {
		log.Printf("警告: AT 代理未启动: %v", err)
	} else {
		go removeATProxyOnSignal(cfg.atProxySock)
	}

	if cfg.noTLS {
		log.Printf("ZBIMS HTTP 服务启动: %s", cfg.httpAddr)
		log.Fatal(http.ListenAndServe(cfg.httpAddr, handler))
		return
	}

	if err := ensureManagedTLSCertificate(cfg.certFile, cfg.keyFile, cfg.caCertFile, cfg.caKeyFile); err != nil {
		log.Fatalf("初始化 HTTPS 证书失败: %v", err)
	}

	go func() {
		redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, httpsRedirectLocation(cfg.httpsAddr, r), http.StatusMovedPermanently)
		})
		log.Printf("HTTP 重定向服务启动: %s", cfg.httpAddr)
		if err := http.ListenAndServe(cfg.httpAddr, redirectHandler); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 重定向服务退出: %v", err)
		}
	}()

	log.Printf("ZBIMS HTTPS 服务启动: %s", cfg.httpsAddr)
	log.Fatal(http.ListenAndServeTLS(cfg.httpsAddr, cfg.certFile, cfg.keyFile, handler))
}

// httpsRedirectLocation builds the HTTPS redirect target for an HTTP request.
// A non-default HTTPS listen port is preserved so the redirect keeps reaching
// the actual HTTPS listener; port 443 (or an address without an explicit
// port) keeps the plain https://host/... form.
func httpsRedirectLocation(httpsAddr string, r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if port := httpsListenPort(httpsAddr); port != "" && port != "443" {
		return "https://" + host + ":" + port + r.URL.RequestURI()
	}
	return "https://" + host + r.URL.RequestURI()
}

// removeATProxyOnSignal 收到 systemd 的停止信号时移除 Socket 文件,
// 避免残留文件让下次启动误判;Go 默认收到 SIGTERM 直接退出,不会走到任何 defer。
func removeATProxyOnSignal(sockPath string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	_ = os.Remove(sockPath)
	os.Exit(0)
}

// httpsListenPort extracts the port from the configured HTTPS listen address.
// It returns "" when the address carries no explicit port.
func httpsListenPort(httpsAddr string) string {
	addr := strings.TrimSpace(httpsAddr)
	if addr == "" {
		return ""
	}
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return ""
}
