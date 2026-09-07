// page_network_config.go 提供网络设置页的 /api/network_config_data 端点。
package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// macBindMu 保护静态绑定的"读-算-写"整体互斥(固件查询、槽位分配、固件写入、
// 状态文件写入),防止并发请求竞争同一槽位;开机补齐与处理器共用同一把锁。
var macBindMu sync.Mutex

// ipPassthroughDisableMu 保护"禁用 IP 透传"后台流程的在途标记:处理器立即
// 返回响应后由后台协程分步执行配置与重启,无去重时并发/重复提交(前端重试、
// 双击、多标签)会派生多个协程重复下发命令并广播互相矛盾的结果事件。
var (
	ipPassthroughDisableMu       sync.Mutex
	ipPassthroughDisableInFlight bool
)

// beginIPPassthroughDisable 尝试占用"禁用 IP 透传"流程,已在途时返回 false。
func beginIPPassthroughDisable() bool {
	ipPassthroughDisableMu.Lock()
	defer ipPassthroughDisableMu.Unlock()
	if ipPassthroughDisableInFlight {
		return false
	}
	ipPassthroughDisableInFlight = true
	return true
}

func endIPPassthroughDisable() {
	ipPassthroughDisableMu.Lock()
	ipPassthroughDisableInFlight = false
	ipPassthroughDisableMu.Unlock()
}

// handleNetworkConfigData 处理网络设置页 /api/network_config_data 的各动作。
//
// 响应契约:
//   - 输入校验失败(非法 MAC/IP/模式等)→ 400 + ok:false;
//   - AT/运行时执行失败(AT 通道忙、固件写入失败、重载失败、状态持久化失败等)
//     → 200 + ok:false,并附 error 说明;
//   - 查询类失败(列表/状态未就绪)绝不返回 ok:true,
//     避免前端把失败误当成"空结果"渲染。
func (s *simpleAdminServer) handleNetworkConfigData(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(requestValue(r, "action"))
	switch action {
	case "", "status":
		raw := s.fetchPageAT(atKeyNetworkConfigStatus, boolQuery(r, "force", false), true)
		data := parseNetworkConfigStatusAT(raw)
		// 开机保护期/后台未就绪时返回默认值会误导用户,标记 pending 供前端重试。
		data["pending"] = strings.Contains(raw, atCachePendingText)
		// 完全读不到数据才判定为读取失败,前端按重试处理;尾部 ERROR 的部分成功不算。
		if networkConfigStatusReadFailed(raw) {
			data["error"] = "AT 通道读取失败"
		}
		writeJSON(w, http.StatusOK, data)
	case "ip_passthrough":
		mode := strings.ToUpper(strings.TrimSpace(requestValue(r, "mode")))
		enabled := boolQuery(r, "enabled", true)
		if !enabled {
			startDelay := time.Second
			stepDelay := time.Second
			configCommands := []string{
				`AT+QMAP="MPDN_RULE",0`,
				`AT+QMAPWAC=1`,
			}
			// 在途去重:流程结束(成功重启前进程消亡,或配置失败提前终止)
			// 之前,重复提交直接按"进行中"响应,不再派生第二个后台协程。
			if !beginIPPassthroughDisable() {
				writeJSON(w, http.StatusOK, settingsRebootNoticeResponse("禁用 IP 透传流程已在进行中，请等待设备重启。", startDelay))
				return
			}
			writeJSON(w, http.StatusOK, settingsRebootNoticeResponse("后台将开始禁用 IP 透传并重启设备。", startDelay))
			s.runDelayedIPPassthroughDisable(configCommands, `AT+CFUN=1,1`, startDelay, stepDelay)
			return
		}

		var command string
		switch mode {
		case "ETH":
			command = `AT+QMAP="MPDN_RULE",0,1,0,1,1,"FF:FF:FF:FF:FF:FF"`
		case "USB":
			command = `AT+QMAP="MPDN_RULE",0,1,0,3,1,"FF:FF:FF:FF:FF:FF"`
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid passthrough mode"})
			return
		}
		resp := s.runPageAction(command)
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "dns_proxy":
		family := strings.TrimSpace(requestValue(r, "family"))
		if family != "4" && family != "6" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid dns family"})
			return
		}
		state := "disable"
		if boolQuery(r, "enabled", false) {
			state = "enable"
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QMAP="DHCPV%sDNS","%s"`, family, state))
		result := map[string]any{"ok": atResponseOK(resp), "response": resp}
		if !atResponseOK(resp) {
			// 本固件(电信定制版)已移除 DHCPV4DNS,查询/设置恒返回 ERROR;
			// 返回明确原因,前端据此提示而不是笼统的"操作失败"。
			result["error"] = "AT 命令执行失败(此固件可能不支持该命令)"
		}
		writeJSON(w, http.StatusOK, result)
	case "usbnet":
		mode := strings.ToUpper(strings.TrimSpace(requestValue(r, "mode")))
		code := map[string]string{"RMNET": "0", "ECM": "1", "MBIM": "2", "RNDIS": "3"}[mode]
		if code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid usbnet mode"})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QCFG="usbnet",%s`, code))
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "dmz":
		enabled := boolQuery(r, "enabled", false)
		command := `AT+QMAP="DMZ",0`
		if enabled {
			ip := cleanIP(requestValue(r, "ip"))
			if ip == "" || !isUnicastIPv4(ip) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid dmz ip"})
				return
			}
			command = fmt.Sprintf(`AT+QMAP="DMZ",1,4,%s`, ip)
		}
		resp := s.runPageAction(command)
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "lanip":
		start := cleanIP(requestValue(r, "start"))
		end := cleanIP(requestValue(r, "end"))
		gw := cleanIP(requestValue(r, "gateway"))
		if start == "" || end == "" || gw == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid lan ip"})
			return
		}
		// 语义校验:三个地址必须是单播 IPv4 且池起点不大于终点。旧实现只做
		// 点分四段格式校验,start>end、0.0.0.0、组播/广播地址会原样写入固件;
		// 固件一旦接受,DHCP 客户端拿不到正确地址,管理员可能被断在管理界面
		// 之外,只能物理/AT 复位。
		if !isUnicastIPv4(start) || !isUnicastIPv4(end) || !isUnicastIPv4(gw) || !lanIPRangeOrdered(start, end) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid lan ip"})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QMAP="LANIP",%s,%s,%s`, start, end, gw))
		writeJSON(w, http.StatusOK, map[string]any{"ok": atResponseOK(resp), "response": resp})
	case "mac_bind_list":
		raw := s.runPageAction(`AT+QMAP="MAC_bind"`)
		ready := macBindQueryReady(raw)
		if !ready {
			// 查询失败绝不返回 ok:true,否则前端会把失败渲染成"无绑定"。
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "AT 通道忙,请稍后重试", "bindings": []macBindLive{}, "atReady": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bindings": parseMacBindAT(raw), "atReady": true})
	case "mac_bind_set":
		mac := normalizeMACAddress(requestValue(r, "mac"))
		ip := cleanIP(requestValue(r, "ip"))
		// 与 dmz/lanip 同口径的语义校验:cleanIP 只做点分四段+每段 ≤255,
		// 前导零形态(192.168.010.010)会被固件回读时的 net.ParseIP 拒绝,
		// 绑定在列表里不可见、删除走幂等路径清状态后固件侧永久残留僵尸租约;
		// 字符串比较的冲突检测也会被前导零绕过(同 IP 两种写法不判冲突),
		// 0.0.0.0/组播/广播写入 dhcp_hosts 产生垃圾条目。
		if mac == "" || !isUnicastIPv4(ip) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid mac bind"})
			return
		}
		// 从查询到状态写入整体加锁,防止并发请求竞争同一槽位。
		macBindMu.Lock()
		defer macBindMu.Unlock()
		raw := s.runPageAction(`AT+QMAP="MAC_bind"`)
		// 查询失败若按空列表继续,会覆盖固件已有绑定的槽位,必须直接拒绝。
		if !macBindQueryReady(raw) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "AT 通道忙,请稍后重试"})
			return
		}
		live := parseMacBindAT(raw)
		// 同一 IP 已被其他 MAC 绑定时会产生重复租约,提前拦截。
		if macBindIPConflict(live, mac, ip) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "ip already bound to another mac"})
			return
		}
		idx, err := allocateMacBindIndex(live, mac)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QMAP="MAC_bind",%d,"%s","%s"`, idx, mac, ip))
		if !atResponseOK(resp) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "static bind failed", "response": resp})
			return
		}
		// 状态文件供开机补齐用,持久化失败必须如实报错(与 mac_bind_del
		// 已修复的口径对齐):厂商状态在重启后丢失时开机补齐是唯一的恢复
		// 机制,未进入状态文件的绑定会静默消失,而用户看到的是"保存成功"。
		entries, err := readMacBindState()
		if err != nil {
			log.Printf("读取静态绑定状态失败: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已写入,但状态持久化失败,请重试保存"})
			return
		}
		if err := writeMacBindState(upsertMacBindState(entries, mac, ip)); err != nil {
			log.Printf("保存静态绑定状态失败: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已写入,但状态持久化失败,请重试保存"})
			return
		}
		// 固件只写 /etc/data/dhcp_hosts 不重载 dnsmasq,静态租约需 SIGHUP 生效;
		// 固件不自动重载,重载失败时本次设置实际未生效,必须如实报错。
		if !s.cfg.mockMode {
			if err := reloadDnsmasqHosts(); err != nil {
				log.Printf("重载 dnsmasq 静态租约失败: %v", err)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已写入,但 dnsmasq 重载失败,请重试"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "mac_bind_del":
		mac := normalizeMACAddress(requestValue(r, "mac"))
		if mac == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid mac bind"})
			return
		}
		// 从查询到状态写入整体加锁,防止并发请求竞争同一槽位。
		macBindMu.Lock()
		defer macBindMu.Unlock()
		raw := s.runPageAction(`AT+QMAP="MAC_bind"`)
		// 查询未就绪时无法确认绑定是否存在,返回"未找到"会误导调用方。
		if !macBindQueryReady(raw) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "AT 通道忙,请稍后重试"})
			return
		}
		live := parseMacBindAT(raw)
		var target *macBindLive
		for i := range live {
			if strings.EqualFold(live[i].MAC, mac) {
				target = &live[i]
				break
			}
		}
		if target == nil {
			// 幂等路径:固件已无该绑定(查询就绪),若状态文件仍有残留则移除,
			// 防止开机补齐复活已删条目;重复删除按成功处理。
			// 状态读/写失败必须与正常删除路径一致如实报错:
			// 残留状态会让开机补齐复活已删条目。
			entries, err := readMacBindState()
			if err != nil {
				log.Printf("读取静态绑定状态失败: %v", err)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "状态持久化失败,请重试"})
				return
			}
			if err := writeMacBindState(removeMacBindState(entries, mac)); err != nil {
				log.Printf("保存静态绑定状态失败: %v", err)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "状态持久化失败,请重试"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QMAP="MAC_bind",%d,"",""`, target.Index))
		if !atResponseOK(resp) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "static bind delete failed", "response": resp})
			return
		}
		// 固件侧已删除,状态持久化失败必须如实报错:
		// 否则开机补齐会按旧状态复活已删条目。
		entries, err := readMacBindState()
		if err != nil {
			log.Printf("读取静态绑定状态失败: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已删除,但状态持久化失败,请重试"})
			return
		}
		if err := writeMacBindState(removeMacBindState(entries, mac)); err != nil {
			log.Printf("保存静态绑定状态失败: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已删除,但状态持久化失败,请重试"})
			return
		}
		// 固件只写 /etc/data/dhcp_hosts 不重载 dnsmasq,静态租约需 SIGHUP 生效;
		// 固件不自动重载,重载失败时本次删除实际未生效,必须如实报错。
		if !s.cfg.mockMode {
			if err := reloadDnsmasqHosts(); err != nil {
				log.Printf("重载 dnsmasq 静态租约失败: %v", err)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "固件已写入,但 dnsmasq 重载失败,请重试"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "dns_upstream":
		state, err := readDNSUpstreamState()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "读取上游 DNS 状态失败: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": state.Enabled, "servers": state.Servers})
	case "dns_upstream_set":
		// 保存会重启设备 dnsmasq(约 1 秒),前端已有确认框。
		// "应用配置→写状态文件"整体持有 dnsUpstreamMu(见其注释):锁只盖
		// apply 时并发保存交错会让状态文件与实际生效配置背离,开机自愈再按
		// 陈旧状态静默回滚用户最后一次保存。
		dnsUpstreamMu.Lock()
		defer dnsUpstreamMu.Unlock()
		enabled := boolQuery(r, "enabled", false)
		if !enabled {
			if !s.cfg.mockMode {
				if err := applyDNSUpstreamLocked(false, nil); err != nil {
					writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "应用上游 DNS 失败: " + err.Error()})
					return
				}
			}
			// 配置已生效,状态持久化失败必须如实报错:否则开机自愈会按旧状态回滚本次设置。
			if err := writeDNSUpstreamState(dnsUpstreamState{Enabled: false, Servers: []string{}}); err != nil {
				log.Printf("保存上游 DNS 状态失败: %v", err)
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "配置已生效,但状态持久化失败,请重试保存"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		servers, err := validateDNSServerList(requestValue(r, "servers"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if !s.cfg.mockMode {
			if err := applyDNSUpstreamLocked(true, servers); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "应用上游 DNS 失败: " + err.Error()})
				return
			}
		}
		// 配置已生效,状态持久化失败必须如实报错:否则开机自愈会按旧状态回滚本次设置。
		if err := writeDNSUpstreamState(dnsUpstreamState{Enabled: true, Servers: servers}); err != nil {
			log.Printf("保存上游 DNS 状态失败: %v", err)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "配置已生效,但状态持久化失败,请重试保存"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported action"})
	}
}

// runDelayedIPPassthroughDisable 后台分步执行禁用 IP 透传流程并广播结果。
//
// 判定语义(不同于"三步全部 OK 才算成功"):
//   - 重启前配置命令(configCommands:MPDN_RULE,0 与 QMAPWAC=1)逐条执行,
//     任一条未收到 OK 即终止流程并广播 ok="false",此时不会执行重启;
//   - 配置命令全部成功后执行重启命令 AT+CFUN=1,1;模块先重启、后应答,
//     收不到应答/超时是常态,因此无论 CFUN 是否收到应答都广播 ok="true"。
//
// 广播契约:事件名 ip_passthrough_result(前端按
// simpleadmin:ip_passthrough_result 监听),负载为 map[string]string,
// 字段值一律是字符串——ok 取值 "true"/"false"(不是 JSON 布尔),
// 前端按 String(detail.ok)==='false' 消费,失败时停止重启倒计时。
//
// 延迟/分步节奏与 runDelayedPageActionsOK 一致,但判定规则必须分步区分,
// 所以不复用整体判定。
func (s *simpleAdminServer) runDelayedIPPassthroughDisable(configCommands []string, rebootCommand string, startDelay, stepDelay time.Duration) {
	go func() {
		// 流程终止(配置失败未重启,或重启命令已下发)后释放在途标记:
		// 失败场景用户可立即重试;成功场景设备随即重启,标记随进程消亡。
		defer endIPPassthroughDisable()
		if startDelay > 0 {
			time.Sleep(startDelay)
		}
		ok := ipPassthroughDisableOutcome(s.runPageAction, configCommands, rebootCommand, stepDelay)
		broadcastAPIWebSocketEvent("ip_passthrough_result", map[string]string{"ok": ok})
	}()
}

// ipPassthroughDisableOutcome 按 runDelayedIPPassthroughDisable 的判定语义
// 逐条执行命令,返回广播负载中 ok 的字符串值("true"/"false")。
// 拆为独立函数便于在包内以可控的执行桩测试各路径。
func ipPassthroughDisableOutcome(run func(command string) string, configCommands []string, rebootCommand string, stepDelay time.Duration) string {
	for i, command := range configCommands {
		if i > 0 && stepDelay > 0 {
			time.Sleep(stepDelay)
		}
		if !atResponseOK(run(command)) {
			return "false"
		}
	}
	if stepDelay > 0 {
		time.Sleep(stepDelay)
	}
	// CFUN 触发模块重启,应答通常丢失:只执行、不判定结果。
	run(rebootCommand)
	return "true"
}

// networkConfigStatusReadFailed 判定状态组合命令的 AT 响应是否为完全读取失败。
// 本固件 `AT+QMAP="DHCPV4DNS"` 查询恒返回 ERROR,组合命令因此以 ERROR 收尾,
// 但其余子命令的数据行完整可用——这种"尾部 ERROR 的部分成功"绝不能判为失败
// (历史回归:按 atResponseOK 判定导致前端无限重试、页面永久卡在"状态获取中")。
// 只有完全拿不到数据行(运行器错误/空响应)才算失败;保护期提示由 pending 单独处理。
func networkConfigStatusReadFailed(raw string) bool {
	return !strings.Contains(raw, "+QMAP:") && !strings.Contains(raw, "+QCFG:") && !strings.Contains(raw, atCachePendingText)
}

func parseNetworkConfigStatusAT(raw string) map[string]any {
	lines := atLines(raw)
	// dnsV4QueryFailed 缺省为 true:本固件 AT+QMAP="DHCPV4DNS" 查询恒返回 ERROR,
	// 响应里不会有 DHCPV4DNS 记录行;只有真解析到该行才置 false。
	// 前端据此把 DNS V4 显示为"未知"而非误导性的"未启用/已启用"。
	out := map[string]any{"ipPassStatus": false, "DNSV6ProxyStatus": false, "DNSV4ProxyStatus": false, "dnsV4QueryFailed": true, "currentUsbNetMode": "未知", "dmzMode": "0", "dmzIP": "", "lanIpStart": "", "lanIpEnd": "", "lanGwIp": ""}
	for _, line := range lines {
		switch {
		case isQMAPRecord(line, "MPDN_rule"):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 2 && parts[2] == "1" {
				out["ipPassStatus"] = true
			}
		case isQMAPRecord(line, "DHCPV6DNS"):
			out["DNSV6ProxyStatus"] = strings.Contains(strings.ToLower(line), `"enable"`)
		case isQMAPRecord(line, "DHCPV4DNS"):
			out["DNSV4ProxyStatus"] = strings.Contains(strings.ToLower(line), `"enable"`)
			out["dnsV4QueryFailed"] = false
		case strings.Contains(strings.ToLower(line), `"usbnet"`):
			// 固件可能返回大写 "USBNET",匹配必须大小写不敏感;
			// 未知编号保持"未知",不得用空值覆盖。
			if m := regexp.MustCompile(`(?i)"usbnet"\s*,\s*(\d)`).FindStringSubmatch(line); len(m) > 1 {
				if mode := map[string]string{"0": "RMNET", "1": "ECM", "2": "MBIM", "3": "RNDIS"}[m[1]]; mode != "" {
					out["currentUsbNetMode"] = mode
				}
			}
		case isQMAPRecord(line, "DMZ"):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			// 固件按地址族各回一行("DMZ",<mode>,4 与 "DMZ",<mode>,6)。
			// 本工具只管理 IPv4 DMZ(写命令 AT+QMAP="DMZ",1,4,<ip>),
			// 不过滤地址族时恒为禁用的 IPv6 行会覆盖 IPv4 的启用状态,
			// 前端据此永远显示 DMZ"未启用"。无地址族字段的旧形态按 IPv4 处理。
			if len(parts) > 2 && parts[2] != "4" {
				continue
			}
			if len(parts) > 1 {
				out["dmzMode"] = parts[1]
			}
			if len(parts) > 3 && parts[1] == "1" {
				out["dmzIP"] = parts[3]
			}
		case isQMAPRecord(line, "LANIP"):
			parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
			if len(parts) > 3 {
				out["lanIpStart"] = lastIPPart(parts[1])
				out["lanIpEnd"] = lastIPPart(parts[2])
				out["lanGwIp"] = parts[3]
			}
		}
	}
	return out
}
func cleanIP(v string) string {
	if !regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`).MatchString(v) {
		return ""
	}
	// 与前端校验对齐:四段格式之外每段数值还必须 ≤255,防止绕过前端直接调用 API。
	for _, octet := range strings.Split(v, ".") {
		n, err := strconv.Atoi(octet)
		if err != nil || n < 0 || n > 255 {
			return ""
		}
	}
	return v
}

// isUnicastIPv4 判定字符串是否为可用作 LAN 配置目标的单播 IPv4 地址:
// net.ParseIP 严格解析(拒绝前导零等非规范形态),并排除未指定(0.0.0.0)、
// 回环、组播、广播与链路本地(169.254/16)地址。
func isUnicastIPv4(v string) bool {
	ip := net.ParseIP(v)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4.IsUnspecified() || ip4.IsLoopback() || ip4.IsMulticast() || ip4.IsLinkLocalUnicast() {
		return false
	}
	return !ip4.Equal(net.IPv4bcast)
}

// lanIPRangeOrdered 判定 DHCP 池起点不大于终点(按 IPv4 数值比较)。
// 调用方须先经 isUnicastIPv4 校验,解析失败按非法处理。
func lanIPRangeOrdered(start, end string) bool {
	s := net.ParseIP(start).To4()
	e := net.ParseIP(end).To4()
	if s == nil || e == nil {
		return false
	}
	return bytes.Compare(s, e) <= 0
}
func lastIPPart(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 4 {
		return parts[3]
	}
	return ""
}
func sanitizeAPN(v string) string {
	return regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(v, "")
}

// macBindLive 表示固件当前生效的一条静态绑定(含槽位号)。
// 来源是固件私有命令 AT+QMAP="MAC_bind"(官方手册未收录,实机验证):
// 固件把绑定写进 /etc/data/dhcp_hosts(dnsmasq dhcp-hostsfile),
// 但不会重载 dnsmasq,需我方 reloadDnsmasqHosts() 才能生效。
type macBindLive struct {
	Index int    `json:"index"`
	MAC   string `json:"mac"`
	IP    string `json:"ip"`
}

// 固件 MAC_bind 槽位号范围(实机验证 1-10,超出会被固件拒绝)。
const (
	macBindIndexMin = 1
	macBindIndexMax = 10
)

// macAddressPattern 校验六段冒号分隔的十六进制 MAC 地址。
var macAddressPattern = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)

// macCompactPattern 校验无分隔符的连续 12 位十六进制 MAC 写法。
var macCompactPattern = regexp.MustCompile(`^[0-9A-Fa-f]{12}$`)

// normalizeMACAddress 规范化 MAC 地址:合法返回大写形式,否则返回空串。
// 固件返回格式固定为冒号大写,归一化仅为防御固件/用户输入变体:
// 先把横线分隔统一为冒号,连续 12 位十六进制(无分隔符)每两位插入冒号,
// 再按六段冒号正则校验并转大写;统一大写便于状态文件与固件查询结果比较。
func normalizeMACAddress(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "-", ":")
	if macCompactPattern.MatchString(v) {
		v = fmt.Sprintf("%s:%s:%s:%s:%s:%s", v[0:2], v[2:4], v[4:6], v[6:8], v[8:10], v[10:12])
	}
	if !macAddressPattern.MatchString(v) {
		return ""
	}
	return strings.ToUpper(v)
}

// macBindQueryReady 判定 MAC_bind 查询原文是否可信:必须收到 OK 且不是
// 后台缓存未就绪提示。无绑定时固件也只回 OK,无法区分"无绑定"与
// "AT 未就绪",查询失败若按空列表继续会覆盖固件已有绑定。抽为纯函数便于测试。
func macBindQueryReady(raw string) bool {
	return atResponseOK(raw) && !strings.Contains(raw, atCachePendingText)
}

// macBindIPConflict 判定 live 中是否已有其他 MAC(大小写不敏感)占用同一 IP。
// 处理器与开机补齐共用同一冲突策略,抽为纯函数便于测试。
func macBindIPConflict(live []macBindLive, mac, ip string) bool {
	for _, entry := range live {
		if entry.IP == ip && !strings.EqualFold(entry.MAC, mac) {
			return true
		}
	}
	return false
}

// parseMacBindAT 解析 `AT+QMAP="MAC_bind"` 查询返回:
// 每条绑定形如 `+QMAP: "MAC_bind",<idx>,"<MAC>","<IP>"`(记录名大小写不敏感),
// 无绑定时固件只回 OK。索引非数字/越界、MAC 非法、IP 非 IPv4 的行跳过,
// 保证返回给前端的数据可直接渲染。
func parseMacBindAT(raw string) []macBindLive {
	out := []macBindLive{}
	for _, line := range atLines(raw) {
		if !isQMAPRecord(line, "MAC_bind") {
			continue
		}
		parts := csvFields(strings.TrimPrefix(line, "+QMAP:"))
		if len(parts) < 4 {
			continue
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil || idx < macBindIndexMin || idx > macBindIndexMax {
			continue
		}
		mac := normalizeMACAddress(parts[2])
		if mac == "" {
			continue
		}
		ip := net.ParseIP(parts[3])
		if ip == nil || ip.To4() == nil {
			// 静态租约只支持 IPv4,IPv6 行视为异常数据跳过。
			continue
		}
		out = append(out, macBindLive{Index: idx, MAC: mac, IP: parts[3]})
	}
	return out
}

// allocateMacBindIndex 为目标 MAC 分配固件槽位:已存在则复用其槽位(幂等更新),
// 否则取 1-10 中最小空闲号;槽位满时返回错误。
func allocateMacBindIndex(existing []macBindLive, mac string) (int, error) {
	used := make(map[int]bool, len(existing))
	for _, entry := range existing {
		if strings.EqualFold(entry.MAC, mac) {
			return entry.Index, nil
		}
		used[entry.Index] = true
	}
	for idx := macBindIndexMin; idx <= macBindIndexMax; idx++ {
		if !used[idx] {
			return idx, nil
		}
	}
	return 0, fmt.Errorf("静态绑定槽位已满,最多 %d 条", macBindIndexMax)
}

// validateDNSServerList 解析并校验用户输入的上游 DNS 列表:
// 逗号/空白(含换行)分隔,去空、按出现顺序去重(大小写不敏感,兼容用户
// 混写 IPv6 大小写),每个地址必须是 net.ParseIP 合法的 IPv4/IPv6。
func validateDNSServerList(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	out := []string{}
	seen := map[string]bool{}
	for _, field := range fields {
		server := strings.TrimSpace(field)
		if server == "" || seen[strings.ToLower(server)] {
			continue
		}
		if net.ParseIP(server) == nil {
			return nil, fmt.Errorf("无效的 DNS 服务器地址: %s", server)
		}
		seen[strings.ToLower(server)] = true
		out = append(out, server)
	}
	if len(out) == 0 {
		return nil, errors.New("请至少填写一个 DNS 服务器")
	}
	if len(out) > dnsmasqMaxUpstreamServers {
		return nil, fmt.Errorf("上游 DNS 服务器最多 %d 个", dnsmasqMaxUpstreamServers)
	}
	return out, nil
}

// upsertMacBindState 在状态列表中按 MAC(大小写不敏感)更新或追加绑定,
// 抽为纯函数便于测试。
func upsertMacBindState(entries []macBindEntry, mac, ip string) []macBindEntry {
	for i := range entries {
		if strings.EqualFold(entries[i].MAC, mac) {
			entries[i].IP = ip
			return entries
		}
	}
	return append(entries, macBindEntry{MAC: mac, IP: ip})
}

// removeMacBindState 从状态列表中移除指定 MAC(大小写不敏感)的绑定,
// 抽为纯函数便于测试;结果保持非 nil,空列表序列化为 [] 而非 null。
func removeMacBindState(entries []macBindEntry, mac string) []macBindEntry {
	out := make([]macBindEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(entry.MAC, mac) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// macBindStateEntryValid 复验状态文件条目是否与 HTTP 保存路径同等合法
// (MAC 可归一化 + IP 为单播 IPv4),供开机补齐使用,抽为纯函数便于测试。
// 状态文件可被手工编辑污染,未复验的条目拼进 AT 命令即构成命令注入
// (见 reconcileMacBindAttempt 的说明)。
func macBindStateEntryValid(entry macBindEntry) bool {
	return normalizeMACAddress(entry.MAC) != "" && isUnicastIPv4(entry.IP)
}

// missingMacBindEntries 计算状态里有、固件侧 live 里缺失的条目(按 MAC
// 大小写不敏感比较),供开机补齐使用。
func missingMacBindEntries(entries []macBindEntry, live []macBindLive) []macBindEntry {
	liveMACs := make(map[string]bool, len(live))
	for _, entry := range live {
		liveMACs[strings.ToLower(entry.MAC)] = true
	}
	out := []macBindEntry{}
	for _, entry := range entries {
		if !liveMACs[strings.ToLower(entry.MAC)] {
			out = append(out, entry)
		}
	}
	return out
}

// reconcileMacBindAtStartup 开机补齐静态绑定:界面保存过的条目若固件侧缺失
// (如重启导致厂商状态丢失)则重新下发;延迟启动避免与开机 AT 初始化竞争,
// 最多重试 3 次,失败仅日志。mock 模式直接返回。
// 每次尝试的读-算-写与处理器共用 macBindMu;重试 sleep 放锁外,
// 锁内不得 sleep,否则会长时间阻塞界面请求。
func (s *simpleAdminServer) reconcileMacBindAtStartup() {
	if s.cfg.mockMode {
		return
	}
	time.Sleep(20 * time.Second)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(30 * time.Second)
		}
		macBindMu.Lock()
		retry := s.reconcileMacBindAttempt()
		macBindMu.Unlock()
		if !retry {
			return
		}
	}
	log.Printf("开机补齐静态绑定重试 3 次仍未完成,放弃")
}

// reconcileMacBindAttempt 执行一次开机补齐,返回是否需要重试。
// 调用方必须持有 macBindMu。
func (s *simpleAdminServer) reconcileMacBindAttempt() bool {
	entries, err := readMacBindState()
	if err != nil {
		log.Printf("读取静态绑定状态失败,跳过开机补齐: %v", err)
		return false
	}
	if len(entries) == 0 {
		return false
	}
	raw := s.runPageAction(`AT+QMAP="MAC_bind"`)
	// 无绑定时固件也只回 OK,无法区分"无绑定"与"AT 未就绪",
	// 必须收到 OK 才能信任查询结果,否则稍后重试。
	if !atResponseOK(raw) {
		log.Printf("开机补齐静态绑定:查询 MAC_bind 未就绪,稍后重试")
		return true
	}
	live := parseMacBindAT(raw)
	missing := missingMacBindEntries(entries, live)
	if len(missing) == 0 {
		return false
	}
	for _, entry := range missing {
		// 状态文件可能损坏/被手工编辑污染(本机 console 页提供 root shell):
		// 补齐路径必须与 HTTP 保存路径同等校验强度(与
		// reconcileDNSUpstreamAtStartup 的复验策略对齐)。MAC/IP 未复验直接
		// 拼进 AT 命令时,JSON 字符串里的 `";+CFUN=1,1;"` 会被模块当复合
		// 命令执行(sanitizeATCommand 不剥分号),开机补齐变成任意 AT 命令
		// 注入入口(重启循环/恢复出厂)。非法条目记日志跳过。
		if !macBindStateEntryValid(entry) {
			log.Printf("开机补齐静态绑定:状态条目非法(MAC=%q IP=%q),跳过", entry.MAC, entry.IP)
			continue
		}
		// 与处理器保持同一冲突策略:该 IP 已被其他 MAC 占用则跳过,
		// 防止开机后产生重复租约。
		if macBindIPConflict(live, entry.MAC, entry.IP) {
			log.Printf("开机补齐静态绑定:IP %s 已被其他 MAC 占用,跳过 %s", entry.IP, entry.MAC)
			continue
		}
		idx, err := allocateMacBindIndex(live, entry.MAC)
		if err != nil {
			log.Printf("开机补齐静态绑定失败: %v", err)
			return true
		}
		resp := s.runPageAction(fmt.Sprintf(`AT+QMAP="MAC_bind",%d,"%s","%s"`, idx, entry.MAC, entry.IP))
		if !atResponseOK(resp) {
			log.Printf("开机补齐静态绑定 %s 失败: %s", entry.MAC, resp)
			return true
		}
		// 下发成功的条目计入 live,后续条目分配槽位时不冲突。
		live = append(live, macBindLive{Index: idx, MAC: strings.ToUpper(entry.MAC), IP: entry.IP})
	}
	// 固件只写 /etc/data/dhcp_hosts 不重载 dnsmasq,补齐后需 SIGHUP 生效。
	if err := reloadDnsmasqHosts(); err != nil {
		log.Printf("开机补齐静态绑定后重载 dnsmasq 失败: %v", err)
	}
	return false
}
