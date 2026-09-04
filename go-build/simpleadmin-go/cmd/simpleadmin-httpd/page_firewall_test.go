package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseFirewallRuleLines(t *testing.T) {
	raw := strings.Join([]string{
		"block 80",
		"accept 8080",
		"443", // 旧格式:纯端口行视为 block
		"",
		"   ",
		"block 80",       // 同端口同动作去重
		"accept 8080  ",  // 带空白的重复行
		"reject 22",      // 非法动作跳过
		"block 0",        // 非法端口跳过
		"block 70000",    // 非法端口跳过
		"block abc",      // 非法端口跳过
		"block 80 extra", // 多余字段跳过
		"accept",         // 缺少端口跳过
	}, "\n")
	rules, err := parseFirewallRuleLines(raw)
	if err != nil {
		t.Fatalf("parseFirewallRuleLines 报错: %v", err)
	}
	want := []firewallRule{
		{Port: "80", Action: "block"},
		{Port: "8080", Action: "accept"},
		{Port: "443", Action: "block"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("parseFirewallRuleLines = %v, want %v", rules, want)
	}

	empty, err := parseFirewallRuleLines("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("parseFirewallRuleLines(空) = %v, %v, want 空列表无错误", empty, err)
	}
}

func TestValidateFirewallRules(t *testing.T) {
	rules, err := validateFirewallRules([]firewallRule{
		{Port: "80", Action: "block"},
		{Port: "80", Action: "block"},
		{Port: "8080", Action: "accept"},
	})
	if err != nil {
		t.Fatalf("validateFirewallRules(去重) 报错: %v", err)
	}
	want := []firewallRule{{Port: "80", Action: "block"}, {Port: "8080", Action: "accept"}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("validateFirewallRules(去重) = %v, want %v", rules, want)
	}

	rules, err = validateFirewallRules(nil)
	if err != nil || len(rules) != 0 {
		t.Fatalf("validateFirewallRules(空) = %v, %v, want 空列表无错误", rules, err)
	}

	_, err = validateFirewallRules([]firewallRule{
		{Port: "80", Action: "block"},
		{Port: "80", Action: "accept"},
	})
	if err == nil || !strings.Contains(err.Error(), "端口冲突: 80 同时被阻止和放行") {
		t.Fatalf("端口冲突错误 = %v, want 包含「端口冲突: 80 同时被阻止和放行」", err)
	}

	for _, port := range []string{"0", "70000", "abc", ""} {
		if _, err := validateFirewallRules([]firewallRule{{Port: port, Action: "block"}}); err == nil {
			t.Errorf("validateFirewallRules(端口 %q) 未报错, want 错误", port)
		}
	}
	if _, err := validateFirewallRules([]firewallRule{{Port: "80", Action: "reject"}}); err == nil {
		t.Errorf("validateFirewallRules(动作 reject) 未报错, want 错误")
	}

	var many []firewallRule
	for i := 1; i <= firewallMaxPorts+1; i++ {
		many = append(many, firewallRule{Port: strconv.Itoa(i), Action: "block"})
	}
	if _, err := validateFirewallRules(many); err == nil {
		t.Fatalf("validateFirewallRules(65 条) 未报错, want 超过上限错误")
	}
}

func TestFirewallIPTablesCommandsAcceptBeforeBlock(t *testing.T) {
	rules := []firewallRule{
		{Port: "80", Action: "block"},
		{Port: "8080", Action: "accept"},
		{Port: "443", Action: "block"},
	}
	cmds := firewallIPTablesCommands(rules)
	if len(cmds) != 1+1+2*4 {
		t.Fatalf("命令条数 = %d, want %d", len(cmds), 1+1+2*4)
	}
	if !reflect.DeepEqual(cmds[0], []string{"-F", firewallChainName}) {
		t.Fatalf("首条命令 = %v, want [-F %s]", cmds[0], firewallChainName)
	}
	if want := []string{"-A", firewallChainName, "-p", "tcp", "--dport", "8080", "-j", "ACCEPT"}; !reflect.DeepEqual(cmds[1], want) {
		t.Fatalf("放行命令 = %v, want %v", cmds[1], want)
	}

	lastAcceptIndex, firstBlockIndex := -1, len(cmds)
	for i, args := range cmds {
		joined := " " + strings.Join(args, " ") + " "
		if strings.Contains(joined, " --dport 8080 ") {
			lastAcceptIndex = i
		}
		if (strings.Contains(joined, " --dport 80 ") || strings.Contains(joined, " --dport 443 ")) && i < firstBlockIndex {
			firstBlockIndex = i
		}
	}
	if lastAcceptIndex < 0 || firstBlockIndex >= len(cmds) || lastAcceptIndex >= firstBlockIndex {
		t.Fatalf("放行规则(%d)未整体位于阻止规则(%d)之前", lastAcceptIndex, firstBlockIndex)
	}

	for _, port := range []string{"80", "443"} {
		acceptIfaces := []string{}
		dropIndex := -1
		for i, args := range cmds {
			joined := " " + strings.Join(args, " ") + " "
			if !strings.Contains(joined, " --dport "+port+" ") {
				continue
			}
			switch {
			case strings.Contains(joined, " -j ACCEPT "):
				acceptIfaces = append(acceptIfaces, args[3])
			case strings.Contains(joined, " -j DROP "):
				dropIndex = i
			}
		}
		if !reflect.DeepEqual(acceptIfaces, []string{"bridge0", "eth0", "tailscale0"}) {
			t.Fatalf("阻止端口 %s 的 ACCEPT 接口序列 = %v, want [bridge0 eth0 tailscale0]", port, acceptIfaces)
		}
		if dropIndex < 0 {
			t.Fatalf("阻止端口 %s 缺少 DROP 规则", port)
		}
		for i, args := range cmds {
			joined := " " + strings.Join(args, " ") + " "
			if strings.Contains(joined, " --dport "+port+" ") && strings.Contains(joined, " -j ACCEPT ") && i > dropIndex {
				t.Fatalf("阻止端口 %s 的 ACCEPT(%d) 出现在 DROP(%d) 之后", port, i, dropIndex)
			}
		}
	}
}

func TestFirewallRulesFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := runtimeFirewallPortsFile
	runtimeFirewallPortsFile = filepath.Join(dir, "firewall_ports.conf")
	t.Cleanup(func() { runtimeFirewallPortsFile = old })

	in := []firewallRule{{Port: "80", Action: "block"}, {Port: "8080", Action: "accept"}}
	if err := writeFirewallRulesFile(runtimeFirewallPortsFile, in); err != nil {
		t.Fatalf("写入防火墙配置失败: %v", err)
	}
	data, err := os.ReadFile(runtimeFirewallPortsFile)
	if err != nil {
		t.Fatalf("读取防火墙配置失败: %v", err)
	}
	if string(data) != "block 80\naccept 8080\n" {
		t.Fatalf("配置文件内容 = %q, want %q", string(data), "block 80\naccept 8080\n")
	}
	out, err := readFirewallRulesFile(runtimeFirewallPortsFile)
	if err != nil {
		t.Fatalf("读取防火墙配置失败: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip = %v, want %v", out, in)
	}

	if err := os.WriteFile(runtimeFirewallPortsFile, []byte("80\naccept 8080\n443\nbad line here\nblock 0\n\n"), 0644); err != nil {
		t.Fatalf("写入旧格式配置失败: %v", err)
	}
	out, err = readFirewallRulesFile(runtimeFirewallPortsFile)
	if err != nil {
		t.Fatalf("读取旧格式配置报错: %v", err)
	}
	want := []firewallRule{
		{Port: "80", Action: "block"},
		{Port: "8080", Action: "accept"},
		{Port: "443", Action: "block"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("旧格式兼容读取 = %v, want %v", out, want)
	}

	missing, err := readFirewallRulesFile(filepath.Join(dir, "missing.conf"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("读取不存在文件 = %v, %v, want 空列表无错误", missing, err)
	}
}

func TestParseIPTablesChainDump(t *testing.T) {
	sample := `Chain INPUT (policy ACCEPT 12345 packets, 6789012 bytes)
num   pkts bytes target     prot opt in     out     source               destination
1     9876  4321 SADMIN_FW  all  --  *      *       0.0.0.0/0            0.0.0.0/0

Chain FORWARD (policy ACCEPT 0 packets, 0 bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 100K packets, 5M bytes)
num   pkts bytes target     prot opt in     out     source               destination

Chain SADMIN_FW (1 references)
num   pkts bytes target     prot opt in     out     source               destination
1     100K    5M ACCEPT     tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            tcp dpt:8080
2       20  1024 ACCEPT     tcp  --  bridge0 *      0.0.0.0/0            0.0.0.0/0            tcp dpt:80
3       2G     0 DROP       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            tcp dpt:80
`
	chains := parseIPTablesChainDump(sample)
	if len(chains) != 4 {
		t.Fatalf("链数量 = %d, want 4", len(chains))
	}

	input := chains[0]
	if input.Name != "INPUT" || input.Policy != "ACCEPT" || input.Pkts != 12345 || input.Bytes != 6789012 {
		t.Fatalf("INPUT 链头 = %+v, want name=INPUT policy=ACCEPT pkts=12345 bytes=6789012", input)
	}
	if len(input.Rules) != 1 {
		t.Fatalf("INPUT 规则数 = %d, want 1", len(input.Rules))
	}
	jump := input.Rules[0]
	if jump.Num != 1 || jump.Pkts != 9876 || jump.Bytes != 4321 || jump.Target != firewallChainName || jump.Proto != "all" || jump.In != "*" || jump.Extra != "" {
		t.Fatalf("INPUT 跳转规则 = %+v, want num=1 pkts=9876 bytes=4321 target=%s extra=空", jump, firewallChainName)
	}

	if chains[1].Name != "FORWARD" || len(chains[1].Rules) != 0 {
		t.Fatalf("FORWARD 链 = %+v, want 空链", chains[1])
	}

	output := chains[2]
	if output.Pkts != 100*1024 || output.Bytes != 5*1024*1024 {
		t.Fatalf("OUTPUT 链头计数 = %d/%d, want %d/%d", output.Pkts, output.Bytes, 100*1024, 5*1024*1024)
	}

	fw := chains[3]
	if fw.Name != firewallChainName || len(fw.Rules) != 3 {
		t.Fatalf("%s 链 = %+v, want 3 条规则", firewallChainName, fw)
	}
	first := fw.Rules[0]
	if first.Num != 1 || first.Pkts != 100*1024 || first.Bytes != 5*1024*1024 || first.Target != "ACCEPT" || first.Extra != "tcp dpt:8080" {
		t.Fatalf("放行规则条目 = %+v, want K/M 计数与 Extra tcp dpt:8080", first)
	}
	if fw.Rules[1].In != "bridge0" {
		t.Fatalf("阻止规则接口 = %q, want bridge0", fw.Rules[1].In)
	}
	if fw.Rules[2].Pkts != int64(2)*1024*1024*1024 || fw.Rules[2].Target != "DROP" {
		t.Fatalf("DROP 规则条目 = %+v, want 2G 计数", fw.Rules[2])
	}
}

func TestMockModeFirewallSaveAndStatusOverWS(t *testing.T) {
	ts, client := e2eTestServer(t)
	old := runtimeFirewallPortsFile
	runtimeFirewallPortsFile = filepath.Join(t.TempDir(), "firewall_ports.conf")
	t.Cleanup(func() { runtimeFirewallPortsFile = old })

	saveResp := e2eWebSocketCall(t, ts, client, "fw1", "POST", "/api/firewall_data", "action=save&block_ports=80,443&accept_ports=8080")
	if status := fmt.Sprint(saveResp["status"]); status != "200" {
		t.Fatalf("firewall save status = %v, want 200 (error=%v)", saveResp["status"], saveResp["error"])
	}
	var saveData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(saveResp["body"])), &saveData); err != nil {
		t.Fatalf("firewall save body 非 JSON: %v", err)
	}
	if saveData["ok"] != true {
		t.Fatalf("firewall save ok = %v, want true", saveData["ok"])
	}
	savedRules, _ := saveData["rules"].([]any)
	if len(savedRules) != 3 {
		t.Fatalf("firewall save rules = %v, want 3 条", saveData["rules"])
	}

	statusResp := e2eWebSocketCall(t, ts, client, "fw2", "POST", "/api/firewall_data", "")
	if status := fmt.Sprint(statusResp["status"]); status != "200" {
		t.Fatalf("firewall status = %v, want 200 (error=%v)", statusResp["status"], statusResp["error"])
	}
	var statusData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(statusResp["body"])), &statusData); err != nil {
		t.Fatalf("firewall status body 非 JSON: %v", err)
	}
	statusRules, _ := statusData["rules"].([]any)
	if len(statusRules) != 3 {
		t.Fatalf("firewall rules = %v, want 3 条", statusData["rules"])
	}
	if got := fmt.Sprint(statusData["ruleCount"]); got != "9" {
		t.Fatalf("firewall ruleCount = %s, want 9 (1 放行×1 + 2 阻止×4)", got)
	}
	if statusData["jumpInstalled"] != true {
		t.Fatalf("firewall jumpInstalled = %v, want true", statusData["jumpInstalled"])
	}

	chains, _ := statusData["chains"].([]any)
	var inputChain, fwChain map[string]any
	for _, item := range chains {
		chain, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch chain["name"] {
		case "INPUT":
			inputChain = chain
		case firewallChainName:
			fwChain = chain
		}
	}
	if inputChain == nil || inputChain["policy"] != "ACCEPT" {
		t.Fatalf("chains 缺少 policy ACCEPT 的 INPUT 链: %v", statusData["chains"])
	}
	if fwChain == nil {
		t.Fatalf("chains 缺少 %s 链: %v", firewallChainName, statusData["chains"])
	}
	fwRules, _ := fwChain["rules"].([]any)
	if len(fwRules) != 9 {
		t.Fatalf("%s 链规则条目 = %d, want 9", firewallChainName, len(fwRules))
	}

	clearResp := e2eWebSocketCall(t, ts, client, "fw3", "POST", "/api/firewall_data", "action=save")
	if status := fmt.Sprint(clearResp["status"]); status != "200" {
		t.Fatalf("firewall clear status = %v, want 200 (error=%v)", clearResp["status"], clearResp["error"])
	}
	var clearData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(clearResp["body"])), &clearData); err != nil {
		t.Fatalf("firewall clear body 非 JSON: %v", err)
	}
	if clearData["ok"] != true {
		t.Fatalf("firewall clear ok = %v, want true", clearData["ok"])
	}

	afterResp := e2eWebSocketCall(t, ts, client, "fw4", "POST", "/api/firewall_data", "")
	var afterData map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(afterResp["body"])), &afterData); err != nil {
		t.Fatalf("firewall after body 非 JSON: %v", err)
	}
	afterRules, _ := afterData["rules"].([]any)
	if len(afterRules) != 0 {
		t.Fatalf("清空后 rules = %v, want 空列表", afterData["rules"])
	}
	if got := fmt.Sprint(afterData["ruleCount"]); got != "0" {
		t.Fatalf("清空后 ruleCount = %s, want 0", got)
	}
	if afterData["jumpInstalled"] != false {
		t.Fatalf("清空后 jumpInstalled = %v, want false", afterData["jumpInstalled"])
	}
}

func TestFirewallSaveRejectsConflictingPortsOverWS(t *testing.T) {
	ts, client := e2eTestServer(t)
	old := runtimeFirewallPortsFile
	runtimeFirewallPortsFile = filepath.Join(t.TempDir(), "firewall_ports.conf")
	t.Cleanup(func() { runtimeFirewallPortsFile = old })

	resp := e2eWebSocketCall(t, ts, client, "fw5", "POST", "/api/firewall_data", "action=save&block_ports=80&accept_ports=80")
	if status := fmt.Sprint(resp["status"]); status != "400" {
		t.Fatalf("端口冲突 save status = %v, want 400", resp["status"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("端口冲突 save body 非 JSON: %v", err)
	}
	if data["ok"] != false {
		t.Fatalf("端口冲突 save ok = %v, want false", data["ok"])
	}
	if msg := fmt.Sprint(data["error"]); !strings.Contains(msg, "端口冲突") {
		t.Fatalf("端口冲突 save error = %q, want 包含「端口冲突」", msg)
	}
}

// TestApplyFirewallRulesTransactionalRestoresOldRulesOnFailure 执行桩在第 N 条失败:
// 应用立即停止,完整重放旧规则命令序列(旧规则重建),并返回原错误。
func TestApplyFirewallRulesTransactionalRestoresOldRulesOnFailure(t *testing.T) {
	newRules := []firewallRule{
		{Port: "8080", Action: firewallActionAccept},
		{Port: "80", Action: firewallActionBlock},
	}
	oldRules := []firewallRule{
		{Port: "443", Action: firewallActionAccept},
		{Port: "22", Action: firewallActionBlock},
	}
	newCmds := firewallIPTablesCommands(newRules)
	oldCmds := firewallIPTablesCommands(oldRules)

	const failAt = 2 // 第 3 条(索引 2)新规则命令失败,模拟 xtables 锁争用超时
	stubErr := errors.New("iptables 模拟执行失败: 资源暂时不可用")
	executed := [][]string{}
	executor := func(args []string) error {
		executed = append(executed, args)
		if len(executed) == failAt+1 {
			return stubErr
		}
		return nil
	}

	err := applyFirewallRulesTransactional(executor, newRules, oldRules)
	if err != stubErr {
		t.Fatalf("返回错误 = %v, want 原错误 %v", err, stubErr)
	}
	want := append(append([][]string{}, newCmds[:failAt+1]...), oldCmds...)
	if !reflect.DeepEqual(executed, want) {
		t.Fatalf("执行序列(%d 条) = %v, want 前 %d 条新规则命令 + 完整旧规则恢复序列(%d 条) %v",
			len(executed), executed, failAt+1, len(oldCmds), want)
	}
}

// TestApplyFirewallRulesTransactionalSuccessSkipsRestore 全部成功:只执行新规则命令序列,无恢复。
func TestApplyFirewallRulesTransactionalSuccessSkipsRestore(t *testing.T) {
	newRules := []firewallRule{
		{Port: "8080", Action: firewallActionAccept},
		{Port: "80", Action: firewallActionBlock},
	}
	oldRules := []firewallRule{{Port: "22", Action: firewallActionBlock}}

	executed := [][]string{}
	executor := func(args []string) error {
		executed = append(executed, args)
		return nil
	}
	if err := applyFirewallRulesTransactional(executor, newRules, oldRules); err != nil {
		t.Fatalf("全部成功时不应返回错误: %v", err)
	}
	want := firewallIPTablesCommands(newRules)
	if !reflect.DeepEqual(executed, want) {
		t.Fatalf("执行序列(%d 条) = %v, want 仅新规则命令序列(%d 条, 无恢复) %v",
			len(executed), executed, len(want), want)
	}
}

// TestApplyFirewallRulesTransactionalRestoreFailureKeepsOriginalError 恢复阶段本身也失败
// (如锁持续被占):仍尽力重放到序列末尾,返回的仍是原错误,不吞错。
func TestApplyFirewallRulesTransactionalRestoreFailureKeepsOriginalError(t *testing.T) {
	newRules := []firewallRule{{Port: "80", Action: firewallActionBlock}}
	oldRules := []firewallRule{
		{Port: "443", Action: firewallActionAccept},
		{Port: "22", Action: firewallActionBlock},
	}
	newCmds := firewallIPTablesCommands(newRules)
	oldCmds := firewallIPTablesCommands(oldRules)

	const newPhaseCalls = 2 // 第 2 条新规则命令失败
	stubErr := errors.New("原始应用失败")
	executed := [][]string{}
	executor := func(args []string) error {
		executed = append(executed, args)
		switch len(executed) {
		case newPhaseCalls: // 新规则阶段失败
			return stubErr
		case newPhaseCalls + 2: // 恢复阶段第 2 条也失败,模拟锁持续被占
			return errors.New("恢复命令失败")
		}
		return nil
	}

	err := applyFirewallRulesTransactional(executor, newRules, oldRules)
	if err != stubErr {
		t.Fatalf("返回错误 = %v, want 原错误 %v", err, stubErr)
	}
	want := append(append([][]string{}, newCmds[:newPhaseCalls]...), oldCmds...)
	if !reflect.DeepEqual(executed, want) {
		t.Fatalf("执行序列(%d 条) = %v, want 恢复序列仍完整尝试 %v", len(executed), executed, want)
	}
}

// TestApplyFirewallRulesTransactionalNilOldRulesRestoresEmptyChain oldRules 为 nil(无已知
// 旧规则,如启动路径):失败时恢复序列仅为 flush,即把链重建为空而非停留在残缺态。
func TestApplyFirewallRulesTransactionalNilOldRulesRestoresEmptyChain(t *testing.T) {
	newRules := []firewallRule{{Port: "80", Action: firewallActionBlock}}
	newCmds := firewallIPTablesCommands(newRules)

	executed := [][]string{}
	executor := func(args []string) error {
		executed = append(executed, args)
		if len(executed) == 2 { // 第 2 条新规则命令失败
			return errors.New("第二条命令失败")
		}
		return nil
	}
	if err := applyFirewallRulesTransactional(executor, newRules, nil); err == nil {
		t.Fatal("失败时应返回错误")
	}
	want := append(append([][]string{}, newCmds[:2]...), []string{"-F", firewallChainName})
	if !reflect.DeepEqual(executed, want) {
		t.Fatalf("执行序列 = %v, want 失败后仅重放 [-F %s] %v", executed, firewallChainName, want)
	}
}
