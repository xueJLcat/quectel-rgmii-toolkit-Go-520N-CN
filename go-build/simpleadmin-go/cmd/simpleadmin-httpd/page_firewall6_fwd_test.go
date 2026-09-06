package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 真实格式的 ip6tables -vnL -x --line-numbers 输出(多链/多规则/K,M,G 计数)。
const sampleIP6TablesDump = `Chain INPUT (policy ACCEPT 1234 packets, 567890 bytes)
num   pkts bytes target     prot opt in     out     source               destination
1     9876  4321 SADMIN_FW  all  --  *      *       ::/0                 ::/0
2       20  1024 DROP       tcp  --  *      *       ::/0                 ::/0                 tcp dpt:23

Chain FORWARD (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 100K packets, 5M bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain SADMIN_FW (1 references)
num   pkts bytes target     prot opt in     out     source               destination
1     100K    5M ACCEPT     tcp  --  *      *       ::/0                 ::/0                 tcp dpt:8080
2       2G     0 DROP       tcp  --  *      *       ::/0                 ::/0                 tcp dpt:80
`

// nat 表转储样本:未安装 SADMIN_FWD 链与跳转。
const natDumpWithoutFwd = `Chain PREROUTING (policy ACCEPT 100 packets, 6000 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination
`

// nat 表转储样本:SADMIN_FWD 链与 PREROUTING 跳转均已安装。
const natDumpWithFwd = `Chain PREROUTING (policy ACCEPT 100 packets, 6000 bytes)
num   pkts bytes target     prot opt in     out     source               destination
1       50  3000 SADMIN_FWD  all  --  *      *       0.0.0.0/0            0.0.0.0/0

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain SADMIN_FWD (1 references)
num   pkts bytes target     prot opt in     out     source               destination
1       10   600 DNAT       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            tcp dpt:8080 to:192.168.1.10:80
`

// nat 表转储样本:链存在但 PREROUTING 跳转被手动删除。
const natDumpChainNoJump = `Chain PREROUTING (policy ACCEPT 100 packets, 6000 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain SADMIN_FWD (0 references)
num   pkts bytes target     prot opt in     out     source               destination
1       10   600 DNAT       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            tcp dpt:8080 to:192.168.1.10:80
`

// overrideFirewallRuntime 把转发规则文件重定向到临时目录并替换命令 runner 为 mock,
// 测试结束自动恢复;返回临时转发规则文件路径。
func overrideFirewallRuntime(t *testing.T, runner firewallCommandRunner) string {
	t.Helper()
	oldFile, oldRunner := runtimeFirewallFwdFile, runtimeFirewallCommandRunner
	fwdFile := filepath.Join(t.TempDir(), "firewall_fwd.conf")
	runtimeFirewallFwdFile = fwdFile
	runtimeFirewallCommandRunner = runner
	t.Cleanup(func() {
		runtimeFirewallFwdFile = oldFile
		runtimeFirewallCommandRunner = oldRunner
	})
	return fwdFile
}

// callFirewallAPI 直接调用 handleFirewallData 并返回响应记录器。
func callFirewallAPI(t *testing.T, mockMode bool, query string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &simpleAdminServer{cfg: serverConfig{mockMode: mockMode}}
	rr := httptest.NewRecorder()
	srv.handleFirewallData(rr, httptest.NewRequest(http.MethodPost, "/api/firewall_data?"+query, nil))
	return rr
}

func decodeFirewallBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatalf("响应非 JSON: %v (body=%s)", err, rr.Body.String())
	}
	return data
}

// decodeFirewallFwdRules 把响应中的规则数组解码回强类型列表便于比对。
func decodeFirewallFwdRules(t *testing.T, v any) []firewallFwdRule {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("规则再序列化失败: %v", err)
	}
	var rules []firewallFwdRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("规则解码失败: %v", err)
	}
	return rules
}

// natDumpRunner 构造按 -vnL 参数分发转储样本、其余命令记录为写操作的 mock runner。
func natDumpRunner(t *testing.T, dump *string, writes *[][]string, failAtWrite int, failErr error) firewallCommandRunner {
	t.Helper()
	return func(command string, args []string) (string, error) {
		if command != firewallIPTablesCommand {
			t.Fatalf("意外命令: %s %v", command, args)
		}
		if strings.Join(args, " ") == "-t nat -vnL -x --line-numbers" {
			return *dump, nil
		}
		*writes = append(*writes, args)
		if failAtWrite > 0 && len(*writes) == failAtWrite {
			return "", failErr
		}
		return "", nil
	}
}

func TestFirewall6StatusParsesIP6TablesDump(t *testing.T) {
	var gotCommand string
	var gotArgs []string
	overrideFirewallRuntime(t, func(command string, args []string) (string, error) {
		gotCommand, gotArgs = command, args
		return sampleIP6TablesDump, nil
	})

	rr := callFirewallAPI(t, false, "action=status6")
	if rr.Code != http.StatusOK {
		t.Fatalf("status6 code = %d, want 200", rr.Code)
	}
	if gotCommand != "ip6tables" || !reflect.DeepEqual(gotArgs, []string{"-vnL", "-x", "--line-numbers"}) {
		t.Fatalf("runner 收到命令 = %s %v, want ip6tables -vnL -x --line-numbers", gotCommand, gotArgs)
	}
	data := decodeFirewallBody(t, rr)
	if data["ok"] != true {
		t.Fatalf("status6 ok = %v, want true (body=%s)", data["ok"], rr.Body.String())
	}
	chains, _ := data["chains"].([]any)
	if len(chains) != 4 {
		t.Fatalf("链数量 = %d, want 4 (body=%s)", len(chains), rr.Body.String())
	}

	input, _ := chains[0].(map[string]any)
	if input["name"] != "INPUT" || input["policy"] != "ACCEPT" || input["pkts"] != float64(1234) || input["bytes"] != float64(567890) {
		t.Fatalf("INPUT 链头 = %v, want name=INPUT policy=ACCEPT pkts=1234 bytes=567890", input)
	}
	inputRules, _ := input["rules"].([]any)
	if len(inputRules) != 2 {
		t.Fatalf("INPUT 规则数 = %d, want 2", len(inputRules))
	}
	jump, _ := inputRules[0].(map[string]any)
	if jump["num"] != float64(1) || jump["pkts"] != float64(9876) || jump["bytes"] != float64(4321) ||
		jump["target"] != firewallChainName || jump["proto"] != "all" || jump["source"] != "::/0" || jump["extra"] != "" {
		t.Fatalf("INPUT 跳转规则 = %v", jump)
	}
	drop, _ := inputRules[1].(map[string]any)
	if drop["target"] != "DROP" || drop["proto"] != "tcp" || drop["extra"] != "tcp dpt:23" || drop["destination"] != "::/0" {
		t.Fatalf("INPUT DROP 规则 = %v", drop)
	}

	forward, _ := chains[1].(map[string]any)
	forwardRules, _ := forward["rules"].([]any)
	if forward["name"] != "FORWARD" || len(forwardRules) != 0 || forward["pkts"] != float64(0) {
		t.Fatalf("FORWARD 链 = %v, want 空链 0 计数", forward)
	}

	output, _ := chains[2].(map[string]any)
	if output["pkts"] != float64(100*1024) || output["bytes"] != float64(5*1024*1024) {
		t.Fatalf("OUTPUT 链头 K/M 计数 = %v/%v", output["pkts"], output["bytes"])
	}

	fw, _ := chains[3].(map[string]any)
	fwRules, _ := fw["rules"].([]any)
	if fw["name"] != firewallChainName || fw["policy"] != "" || len(fwRules) != 2 {
		t.Fatalf("%s 链 = %v, want 无 policy 2 条规则", firewallChainName, fw)
	}
	first, _ := fwRules[0].(map[string]any)
	if first["pkts"] != float64(100*1024) || first["bytes"] != float64(5*1024*1024) || first["extra"] != "tcp dpt:8080" {
		t.Fatalf("K/M 计数规则 = %v", first)
	}
	second, _ := fwRules[1].(map[string]any)
	if second["pkts"] != float64(2*1024*1024*1024) || second["bytes"] != float64(0) || second["target"] != "DROP" {
		t.Fatalf("G 计数规则 = %v", second)
	}
}

// ip6tables 不存在/执行失败:运行时失败按契约定为 200 {ok:false,error},附明确原因,
// 绝不伪装成空链列表。
func TestFirewall6StatusRunnerError(t *testing.T) {
	overrideFirewallRuntime(t, func(command string, args []string) (string, error) {
		return "", errors.New("ip6tables -vnL -x --line-numbers 执行失败: executable file not found in $PATH")
	})

	rr := callFirewallAPI(t, false, "action=status6")
	if rr.Code != http.StatusOK {
		t.Fatalf("status6 失败 code = %d, want 200", rr.Code)
	}
	data := decodeFirewallBody(t, rr)
	if data["ok"] != false {
		t.Fatalf("ok = %v, want false (body=%s)", data["ok"], rr.Body.String())
	}
	if msg := fmt.Sprint(data["error"]); !strings.Contains(msg, "not found in $PATH") {
		t.Fatalf("error = %q, want 包含执行失败原因", msg)
	}
	if _, hasChains := data["chains"]; hasChains {
		t.Fatalf("失败响应不应携带 chains: %v", data)
	}
}

func TestFirewallFwdStateFromChainsAndCommands(t *testing.T) {
	if chainExists, jumpInstalled := firewallFwdStateFromChains(parseIPTablesChainDump(natDumpWithFwd)); !chainExists || !jumpInstalled {
		t.Fatalf("已安装状态 = (%v,%v), want (true,true)", chainExists, jumpInstalled)
	}
	if chainExists, jumpInstalled := firewallFwdStateFromChains(parseIPTablesChainDump(natDumpWithoutFwd)); chainExists || jumpInstalled {
		t.Fatalf("未安装状态 = (%v,%v), want (false,false)", chainExists, jumpInstalled)
	}
	if chainExists, jumpInstalled := firewallFwdStateFromChains(parseIPTablesChainDump(natDumpChainNoJump)); !chainExists || jumpInstalled {
		t.Fatalf("链在跳转缺失状态 = (%v,%v), want (true,false)", chainExists, jumpInstalled)
	}

	rules := []firewallFwdRule{
		{ExtPort: 8080, IntIP: "192.168.1.10", IntPort: 80, Proto: "tcp", Enabled: true},
		{ExtPort: 53, IntIP: "192.168.1.11", IntPort: 5353, Proto: "udp", Enabled: false},
	}
	want := [][]string{
		{"-t", "nat", "-N", firewallFwdChainName},
		{"-t", "nat", "-I", "PREROUTING", "1", "-j", firewallFwdChainName},
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8080", "-j", "DNAT", "--to-destination", "192.168.1.10:80"},
	}
	if cmds := firewallFwdIPTablesCommands(false, false, rules); !reflect.DeepEqual(cmds, want) {
		t.Fatalf("未安装状态命令序列 = %v, want %v(enabled=false 不写入链)", cmds, want)
	}
	wantInstalled := [][]string{
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8080", "-j", "DNAT", "--to-destination", "192.168.1.10:80"},
	}
	if cmds := firewallFwdIPTablesCommands(true, true, rules); !reflect.DeepEqual(cmds, wantInstalled) {
		t.Fatalf("已安装状态命令序列 = %v, want %v(无 ensure 命令)", cmds, wantInstalled)
	}
	wantJumpOnly := [][]string{
		{"-t", "nat", "-I", "PREROUTING", "1", "-j", firewallFwdChainName},
		{"-t", "nat", "-F", firewallFwdChainName},
	}
	if cmds := firewallFwdIPTablesCommands(true, false, nil); !reflect.DeepEqual(cmds, wantJumpOnly) {
		t.Fatalf("链在跳转缺失命令序列 = %v, want %v(空规则仅补跳转+清空)", cmds, wantJumpOnly)
	}
}

func TestFirewallFwdRulesFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall_fwd.conf")
	in := []firewallFwdRule{
		{ExtPort: 8080, IntIP: "192.168.1.10", IntPort: 80, Proto: "tcp", Enabled: true},
		{ExtPort: 53, IntIP: "192.168.1.11", IntPort: 5353, Proto: "udp", Enabled: false},
	}
	if err := writeFirewallFwdRulesFile(path, in); err != nil {
		t.Fatalf("写入转发配置失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取转发配置失败: %v", err)
	}
	if want := "8080 tcp 192.168.1.10 80 1\n53 udp 192.168.1.11 5353 0\n"; string(data) != want {
		t.Fatalf("配置文件内容 = %q, want %q", string(data), want)
	}
	out, err := readFirewallFwdRulesFile(path)
	if err != nil {
		t.Fatalf("读取转发配置失败: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip = %v, want %v", out, in)
	}

	// 容错读取:非法行跳过、重复 (extPort,proto) 去重保序。
	broken := "8080 tcp 192.168.1.10 80 1\nbroken line\n70000 tcp 192.168.1.10 80 1\n" +
		"80 icmp 192.168.1.10 80 1\n443 tcp not-an-ip 443 1\n53 udp 192.168.1.11 5353 2\n" +
		"8080 tcp 192.168.1.99 99 1\n53 udp 192.168.1.11 5353 0\n"
	if err := os.WriteFile(path, []byte(broken), 0644); err != nil {
		t.Fatalf("写入混杂配置失败: %v", err)
	}
	out, err = readFirewallFwdRulesFile(path)
	if err != nil {
		t.Fatalf("容错读取报错: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("容错读取 = %v, want %v(非法行跳过且去重)", out, in)
	}

	missing, err := readFirewallFwdRulesFile(filepath.Join(t.TempDir(), "missing.conf"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("读取不存在文件 = %v, %v, want 空列表无错误", missing, err)
	}
}

func TestFirewallFwdSaveValidationMatrix(t *testing.T) {
	fwdFile := overrideFirewallRuntime(t, func(command string, args []string) (string, error) {
		t.Fatalf("校验失败前不应执行任何命令: %s %v", command, args)
		return "", nil
	})
	rule := func(extPort int, intIP string, intPort int, proto string, enabled bool) string {
		return fmt.Sprintf(`{"extPort":%d,"intIP":%q,"intPort":%d,"proto":%q,"enabled":%t}`, extPort, intIP, intPort, proto, enabled)
	}
	var many33, many32 []string
	for i := 1; i <= 33; i++ {
		item := rule(i, "192.168.1.10", 80, "tcp", true)
		many33 = append(many33, item)
		if i <= 32 {
			many32 = append(many32, item)
		}
	}

	cases := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"超过上限", "action=fwd_save&rules=" + url.QueryEscape("["+strings.Join(many33, ",")+"]"), "超过上限"},
		{"外部端口 0", "action=fwd_save&rules=" + url.QueryEscape("["+rule(0, "192.168.1.10", 80, "tcp", true)+"]"), "无效的外部端口"},
		{"外部端口 65536", "action=fwd_save&rules=" + url.QueryEscape("["+rule(65536, "192.168.1.10", 80, "tcp", true)+"]"), "无效的外部端口"},
		{"内部端口 0", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "192.168.1.10", 0, "tcp", true)+"]"), "无效的内部端口"},
		{"内部端口 65536", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "192.168.1.10", 65536, "tcp", true)+"]"), "无效的内部端口"},
		{"非法内网 IP", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "999.1.1.1", 80, "tcp", true)+"]"), "无效的内网 IPv4 地址"},
		{"IPv6 内网地址", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "fd00::1", 80, "tcp", true)+"]"), "无效的内网 IPv4 地址"},
		{"空内网地址", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "", 80, "tcp", true)+"]"), "无效的内网 IPv4 地址"},
		{"非法协议", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "192.168.1.10", 80, "icmp", true)+"]"), "无效的转发协议"},
		{"空协议", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "192.168.1.10", 80, "", true)+"]"), "无效的转发协议"},
		{"重复 extPort+proto", "action=fwd_save&rules=" + url.QueryEscape("["+rule(8080, "192.168.1.10", 80, "tcp", true)+","+rule(8080, "192.168.1.11", 81, "tcp", false)+"]"), "重复的转发规则"},
		{"同端口不同协议合法但 JSON 非法", "action=fwd_save&rules=not-json", "无效的转发规则 JSON"},
		{"缺少 rules 参数", "action=fwd_save", "无效的转发规则 JSON"},
	}
	for _, tc := range cases {
		rr := callFirewallAPI(t, true, tc.query)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: code = %d, want 400 (body=%s)", tc.name, rr.Code, rr.Body.String())
		}
		data := decodeFirewallBody(t, rr)
		if data["ok"] != false {
			t.Fatalf("%s: ok = %v, want false", tc.name, data["ok"])
		}
		if msg := fmt.Sprint(data["error"]); !strings.Contains(msg, tc.wantErr) {
			t.Fatalf("%s: error = %q, want 包含 %q", tc.name, msg, tc.wantErr)
		}
	}
	if _, err := os.Stat(fwdFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("校验全部失败后不应写入持久化文件: %v", err)
	}

	// 边界:恰好 32 条应通过(mock 模式仅校验+持久化)。
	rr := callFirewallAPI(t, true, "action=fwd_save&rules="+url.QueryEscape("["+strings.Join(many32, ",")+"]"))
	if rr.Code != http.StatusOK {
		t.Fatalf("32 条边界: code = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	data := decodeFirewallBody(t, rr)
	if data["ok"] != true {
		t.Fatalf("32 条边界: ok = %v, want true", data["ok"])
	}
	if saved := decodeFirewallFwdRules(t, data["rules"]); len(saved) != firewallMaxFwdRules {
		t.Fatalf("32 条边界: 保存规则数 = %d, want %d", len(saved), firewallMaxFwdRules)
	}
}

// 成功路径:链/跳转缺失时先安装(建链+挂 PREROUTING),随后 flush、逐条 DNAT
// (enabled=false 仅持久化不写入链),最后原子落盘;已安装时不再重复 ensure。
func TestFirewallFwdSaveSuccessCommandSequence(t *testing.T) {
	dump := natDumpWithoutFwd
	writes := [][]string{}
	fwdFile := overrideFirewallRuntime(t, natDumpRunner(t, &dump, &writes, 0, nil))

	rulesJSON := `[{"extPort":8080,"intIP":"192.168.1.10","intPort":80,"proto":"TCP","enabled":true},` +
		`{"extPort":53,"intIP":"192.168.1.11","intPort":5353,"proto":"udp","enabled":false}]`
	rr := callFirewallAPI(t, false, "action=fwd_save&rules="+url.QueryEscape(rulesJSON))
	if rr.Code != http.StatusOK {
		t.Fatalf("fwd_save code = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	data := decodeFirewallBody(t, rr)
	if data["ok"] != true {
		t.Fatalf("fwd_save ok = %v, want true (body=%s)", data["ok"], rr.Body.String())
	}
	wantRules := []firewallFwdRule{
		{ExtPort: 8080, IntIP: "192.168.1.10", IntPort: 80, Proto: "tcp", Enabled: true},
		{ExtPort: 53, IntIP: "192.168.1.11", IntPort: 5353, Proto: "udp", Enabled: false},
	}
	if got := decodeFirewallFwdRules(t, data["rules"]); !reflect.DeepEqual(got, wantRules) {
		t.Fatalf("响应规则 = %v, want %v(proto 归一化为小写)", got, wantRules)
	}

	wantWrites := [][]string{
		{"-t", "nat", "-N", firewallFwdChainName},
		{"-t", "nat", "-I", "PREROUTING", "1", "-j", firewallFwdChainName},
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8080", "-j", "DNAT", "--to-destination", "192.168.1.10:80"},
	}
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("写命令序列(%d 条) = %v, want %v", len(writes), writes, wantWrites)
	}

	persisted, err := os.ReadFile(fwdFile)
	if err != nil {
		t.Fatalf("读取持久化文件失败: %v", err)
	}
	if want := "8080 tcp 192.168.1.10 80 1\n53 udp 192.168.1.11 5353 0\n"; string(persisted) != want {
		t.Fatalf("持久化内容 = %q, want %q(enabled=false 以 0 落盘)", string(persisted), want)
	}

	// 第二次保存:链与跳转已安装,不再出现 ensure 命令,直接 flush+重建。
	dump = natDumpWithFwd
	writes = nil
	rules2 := `[{"extPort":5353,"intIP":"192.168.1.53","intPort":53,"proto":"udp","enabled":true}]`
	if rr := callFirewallAPI(t, false, "action=fwd_save&rules="+url.QueryEscape(rules2)); rr.Code != http.StatusOK {
		t.Fatalf("二次 fwd_save code = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	wantWrites2 := [][]string{
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "udp", "--dport", "5353", "-j", "DNAT", "--to-destination", "192.168.1.53:53"},
	}
	if !reflect.DeepEqual(writes, wantWrites2) {
		t.Fatalf("已安装状态写命令序列 = %v, want %v", writes, wantWrites2)
	}
}

// 中途失败:立即停止应用并按落盘旧规则重放恢复(flush+旧 DNAT),响应 200 ok:false,
// 持久化文件保持旧内容(应用失败不落盘)。
func TestFirewallFwdSaveRollsBackOnFailure(t *testing.T) {
	dump := natDumpWithFwd
	writes := [][]string{}
	stubErr := errors.New("iptables 模拟执行失败: 资源暂时不可用")
	fwdFile := overrideFirewallRuntime(t, natDumpRunner(t, &dump, &writes, 3, stubErr))
	if err := os.WriteFile(fwdFile, []byte("9000 udp 192.168.1.20 53 1\n"), 0644); err != nil {
		t.Fatalf("预置旧规则文件失败: %v", err)
	}

	rulesJSON := `[{"extPort":8080,"intIP":"192.168.1.10","intPort":80,"proto":"tcp","enabled":true},` +
		`{"extPort":8443,"intIP":"192.168.1.11","intPort":443,"proto":"tcp","enabled":true}]`
	rr := callFirewallAPI(t, false, "action=fwd_save&rules="+url.QueryEscape(rulesJSON))
	if rr.Code != http.StatusOK {
		t.Fatalf("失败 fwd_save code = %d, want 200", rr.Code)
	}
	data := decodeFirewallBody(t, rr)
	if data["ok"] != false {
		t.Fatalf("失败 fwd_save ok = %v, want false", data["ok"])
	}
	if msg := fmt.Sprint(data["error"]); !strings.Contains(msg, "模拟执行失败") {
		t.Fatalf("失败 fwd_save error = %q, want 包含原始错误", msg)
	}

	wantWrites := [][]string{
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8080", "-j", "DNAT", "--to-destination", "192.168.1.10:80"},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8443", "-j", "DNAT", "--to-destination", "192.168.1.11:443"},
		// 回滚:按旧规则重放(首条 flush 保证从干净状态重建)。
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "udp", "--dport", "9000", "-j", "DNAT", "--to-destination", "192.168.1.20:53"},
	}
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("写命令序列(%d 条) = %v, want 失败前命令 + 完整回滚序列 %v", len(writes), writes, wantWrites)
	}

	persisted, err := os.ReadFile(fwdFile)
	if err != nil {
		t.Fatalf("读取持久化文件失败: %v", err)
	}
	if string(persisted) != "9000 udp 192.168.1.20 53 1\n" {
		t.Fatalf("失败后持久化内容 = %q, want 保持旧规则不变", string(persisted))
	}
}

// fwd_list:读持久化文件并与实际 nat 链核对跳转状态;查询失败不得伪装成空结果。
func TestFirewallFwdListChecksPersistedFileAndChain(t *testing.T) {
	dump := natDumpWithoutFwd
	writes := [][]string{}
	fwdFile := overrideFirewallRuntime(t, natDumpRunner(t, &dump, &writes, 0, nil))
	wantRules := []firewallFwdRule{
		{ExtPort: 8080, IntIP: "192.168.1.10", IntPort: 80, Proto: "tcp", Enabled: true},
		{ExtPort: 53, IntIP: "192.168.1.11", IntPort: 5353, Proto: "udp", Enabled: false},
	}

	// 文件不存在:空列表合法,跳转状态仍来自实际链核对。
	rr := callFirewallAPI(t, false, "action=fwd_list")
	data := decodeFirewallBody(t, rr)
	if rr.Code != http.StatusOK || data["ok"] != true {
		t.Fatalf("空文件 fwd_list = %d %v, want 200 ok:true", rr.Code, data)
	}
	if got := decodeFirewallFwdRules(t, data["forwarded"]); len(got) != 0 {
		t.Fatalf("空文件 forwarded = %v, want 空列表", got)
	}
	if data["jumpInstalled"] != false {
		t.Fatalf("未安装链时 jumpInstalled = %v, want false", data["jumpInstalled"])
	}

	if err := os.WriteFile(fwdFile, []byte("8080 tcp 192.168.1.10 80 1\n53 udp 192.168.1.11 5353 0\nbroken line\n"), 0644); err != nil {
		t.Fatalf("写入持久化文件失败: %v", err)
	}
	dump = natDumpWithFwd
	data = decodeFirewallBody(t, callFirewallAPI(t, false, "action=fwd_list"))
	if data["ok"] != true {
		t.Fatalf("fwd_list ok = %v, want true", data["ok"])
	}
	if got := decodeFirewallFwdRules(t, data["forwarded"]); !reflect.DeepEqual(got, wantRules) {
		t.Fatalf("forwarded = %v, want %v(非法行跳过)", got, wantRules)
	}
	if data["jumpInstalled"] != true {
		t.Fatalf("已安装跳转时 jumpInstalled = %v, want true", data["jumpInstalled"])
	}

	dump = natDumpChainNoJump
	data = decodeFirewallFwdListOnly(t, callFirewallAPI(t, false, "action=fwd_list"))
	if data["jumpInstalled"] != false {
		t.Fatalf("链在跳转缺失时 jumpInstalled = %v, want false", data["jumpInstalled"])
	}

	// 转储失败:200 ok:false,不得返回空 forwarded 伪装成功。
	overrideFirewallRuntime(t, func(command string, args []string) (string, error) {
		return "", errors.New("iptables -t nat -vnL 执行失败: 资源暂时不可用")
	})
	rr = callFirewallAPI(t, false, "action=fwd_list")
	data = decodeFirewallBody(t, rr)
	if rr.Code != http.StatusOK || data["ok"] != false {
		t.Fatalf("转储失败 fwd_list = %d %v, want 200 ok:false", rr.Code, data)
	}
	if msg := fmt.Sprint(data["error"]); !strings.Contains(msg, "资源暂时不可用") {
		t.Fatalf("转储失败 error = %q, want 包含原因", msg)
	}
	if _, hasForwarded := data["forwarded"]; hasForwarded {
		t.Fatalf("失败响应不应携带 forwarded: %v", data)
	}
	if len(writes) != 0 {
		t.Fatalf("fwd_list 不应执行任何写命令: %v", writes)
	}
}

// decodeFirewallFwdListOnly 解析 fwd_list 响应并断言成功。
func decodeFirewallFwdListOnly(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	data := decodeFirewallBody(t, rr)
	if rr.Code != http.StatusOK || data["ok"] != true {
		t.Fatalf("fwd_list = %d %v, want 200 ok:true", rr.Code, data)
	}
	return data
}

// 开机恢复:读取持久化文件并经注入 runner 重建链;空/缺失文件不下发任何命令。
func TestFirewallFwdStartupRestoreUsesPersistedRules(t *testing.T) {
	dump := natDumpWithFwd
	writes := [][]string{}
	fwdFile := overrideFirewallRuntime(t, natDumpRunner(t, &dump, &writes, 0, nil))

	applySavedFirewallFwdAtStartup()
	if len(writes) != 0 {
		t.Fatalf("文件缺失时不应下发命令: %v", writes)
	}

	if err := os.WriteFile(fwdFile, []byte(""), 0644); err != nil {
		t.Fatalf("写入空文件失败: %v", err)
	}
	applySavedFirewallFwdAtStartup()
	if len(writes) != 0 {
		t.Fatalf("空规则时不应下发命令: %v", writes)
	}

	if err := os.WriteFile(fwdFile, []byte("8080 tcp 192.168.1.10 80 1\n53 udp 192.168.1.11 5353 0\n"), 0644); err != nil {
		t.Fatalf("写入规则文件失败: %v", err)
	}
	applySavedFirewallFwdAtStartup()
	wantWrites := [][]string{
		{"-t", "nat", "-F", firewallFwdChainName},
		{"-t", "nat", "-A", firewallFwdChainName, "-p", "tcp", "--dport", "8080", "-j", "DNAT", "--to-destination", "192.168.1.10:80"},
	}
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("开机恢复命令序列 = %v, want %v(disabled 规则不写入链)", writes, wantWrites)
	}
}
