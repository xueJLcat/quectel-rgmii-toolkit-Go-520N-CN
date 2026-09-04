package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// mock 设备原型:移远 RG520N-CN(项目目标模块,中国电信 46011,5G SA 驻留)。
const (
	mockModuleModel     = "RG520N-CN"
	mockModuleRevision  = "RG520NCNAAR02A02M4G_XM"
	mockFirmwareVersion = "RG520NCNAAR02A02M4G_XM_01.001.01.001"
	mockIMEI            = "869987060012345"
	mockIMSI            = "460115177700001"
	mockICCID           = "89861124055700001234"
	mockWWANIPv4        = "100.104.47.236"
	mockWWANIPv6        = "240e:440:e116:e60:8c9f:9edd:8135:1963"
	mockLANIP           = `+QMAP: "LANIP",192.168.225.20,192.168.225.170,192.168.225.1`
	mockCGCONTRDP       = `+CGCONTRDP: 1,0,"ctnet","100.104.47.236","36.14.4.64.225.22.14.96.24.208.58.78.32.17.224.84", "254.128.0.0.0.0.0.0.0.0.0.0.0.0.0.1","222.222.222.222" "36.14.0.76.64.8.0.0.0.0.0.0.0.0.0.1","222.222.202.202" "36.14.0.76.72.8.0.0.0.0.0.0.0.0.0.1"`
	mockQSPNLine        = `+QSPN: "00430048004E002D00430054","00430054","4E2D56FD75354FE1",1,"46011"`
)

func mockQTEMPSensorLines() []string {
	return []string{
		`+QTEMP:"modem-lte-sub6-pa1","40"`,
		`+QTEMP:"modem-sdr0-pa0","0"`,
		`+QTEMP:"modem-sdr0-pa1","0"`,
		`+QTEMP:"modem-sdr0-pa2","0"`,
		`+QTEMP:"modem-sdr1-pa0","0"`,
		`+QTEMP:"modem-sdr1-pa1","0"`,
		`+QTEMP:"modem-sdr1-pa2","0"`,
		`+QTEMP:"modem-mmw0","-273"`,
		`+QTEMP:"aoss-0-usr","43"`,
		`+QTEMP:"cpuss-0-usr","41"`,
		`+QTEMP:"mdmq6-0-usr","42"`,
		`+QTEMP:"mdmss-0-usr","43"`,
		`+QTEMP:"mdmss-1-usr","41"`,
		`+QTEMP:"mdmss-2-usr","42"`,
		`+QTEMP:"mdmss-3-usr","41"`,
		`+QTEMP:"modem-lte-sub6-pa2","40"`,
		`+QTEMP:"modem-ambient-usr","41"`,
	}
}

// mockQSCANLines 返回小区扫描结果,新固件 13 字段格式:
// 0=制式,1/2=MCC/MNC,3=频点,4=PCI,5=RSRP,…,12=频段。
// NR5G 行带 "NR5G" 字样,LTE 行不带;MCC/MNC 用中国电信 46011,与原型一致。
// 注意:QSCAN 已从扫描接口移除(本固件不返回小区),此函数仅供
// mockATResponse 的 +QSCAN 兜底分支(联调终端透传)使用。
func mockQSCANLines() []string {
	return []string{
		`+QSCAN: "NR5G",460,11,627264,301,-85,-11,18,30,0,0,0,78`,
		`+QSCAN: "NR5G",460,11,627264,198,-97,-13,11,30,0,0,0,78`,
		`+QSCAN: "LTE",460,11,1650,445,-88,-7,20,15,0,0,0,3`,
		`+QSCAN: "LTE",460,11,100,123,-102,-14,5,15,0,0,0,1`,
	}
}

// mockNeighbourScanLines 返回邻区扫描(模式 "Neighbour Scan")的模拟结果:
// `AT+QNWCFG="nr5g_meas_info";+QENG="neighbourcell"` 的输出。
// NR 行字段:`+QNWCFG: "nr5g_meas_info",<序号>,<NR-ARFCN>,<PCI>,<RSRP>,<RSRQ>`,
// 频点 627264 属 n78,与 QSCAN mock 保持一致;LTE 行字段(13 字段):
// `+QENG: "neighbourcell intra","LTE",<earfcn>,<PCID>,<RSRQ>,<RSRP>,<RSSI>,<SINR>,...`。
func mockNeighbourScanLines() []string {
	return []string{
		`+QNWCFG: "nr5g_meas_info",1,627264,301,-85,-11`,
		`+QNWCFG: "nr5g_meas_info",1,627264,198,-97,-13`,
		`+QENG: "neighbourcell intra","LTE",1650,445,-7,-88,-65,0,37,7,16,6,44`,
	}
}

// mockEarfcnLockResponse 模拟 AT+QNWCFG="nr5g_earfcn_lock"/"lte_earfcn_lock"
// 的查询与写入响应。写形态(分段含参数:设置/清零)只回 OK,与真实固件一致;
// 查询返回禁用态(数量 0)。实机启用形态示例:
// `+QNWCFG: "lte_earfcn_lock",1,1650`,清零后为 `+QNWCFG: "lte_earfcn_lock",0`。
func mockEarfcnLockResponse(command string) string {
	if isMockSettingsWriteCommand(command) {
		return strings.TrimSpace(command) + "\r\nOK\r\n"
	}
	upper := strings.ToUpper(strings.TrimSpace(command))
	lines := []string{strings.TrimSpace(command)}
	if strings.Contains(upper, `"NR5G_EARFCN_LOCK"`) {
		lines = append(lines, "", `+QNWCFG: "nr5g_earfcn_lock",0`)
	}
	if strings.Contains(upper, `"LTE_EARFCN_LOCK"`) {
		lines = append(lines, "", `+QNWCFG: "lte_earfcn_lock",0`)
	}
	lines = append(lines, "", "OK")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func mockATResponse(command string) string {
	upper := strings.ToUpper(strings.TrimSpace(command))
	switch {
	case upper == "AT":
		return "AT\r\nOK\r\n"
	case upper == "ATI":
		return "ATI\r\nQuectel\r\n" + mockModuleModel + "\r\nRevision: " + mockModuleRevision + "\r\nOK\r\n"
	case upper == "AT+CGMM" || upper == "+CGMM" || upper == "CGMM":
		return command + "\r\n" + mockModuleModel + "\r\nOK\r\n"
	case upper == "AT+CGMI" || upper == "+CGMI" || upper == "CGMI":
		return command + "\r\nQuectel\r\nOK\r\n"
	case strings.Contains(upper, "+QSIMSTAT") && strings.Contains(upper, "+QENG"):
		return currentMockDashboardATResponse(command)
	case strings.Contains(upper, "+QSIMSTAT") && strings.Contains(upper, "+CPIN"):
		return mockDeviceSIMStatusResponse(command)
	case strings.Contains(upper, "+CGMI") && strings.Contains(upper, "+ICCID"):
		return mockDeviceIdentityResponse(command)
	case strings.Contains(upper, "+QNWLOCK"):
		return mockNetworkSettingsResponse(command)
	case strings.Contains(upper, `"MAC_BIND"`):
		// 必须先于下面的泛化设置分支匹配,否则 MAC_bind 命令会被其吃掉。
		return mockMacBindResponse(command)
	case strings.Contains(upper, `"MPDN_RULE"`) || strings.Contains(upper, `"DHCPV6DNS"`) ||
		strings.Contains(upper, `"DHCPV4DNS"`) || strings.Contains(upper, `"DMZ"`) || strings.Contains(upper, `"USBNET"`):
		return mockSettingsStatusResponse(command)
	case strings.Contains(upper, "+QNWPREFCFG"):
		return mockQNWPREFCFGResponse(command)
	case upper == strings.ToUpper(signalDetailATCommand()):
		// 信号详情专用组合命令:须先于泛化 QENG 分支,否则只会返回 QENG 部分。
		return command + "\r\n+QRSRP: -85,-86,-86,-83,NR5G\r\n" + defaultMockQENGPayload() +
			"\r\n+QCAINFO: \"PCC\",428910,6,\"NR5G BAND 1\",649\r\nOK\r\n"
	case strings.Contains(upper, `+QNWCFG="NR5G_MEAS_INFO"`):
		// 邻区扫描组合命令含 +QENG 子命令,必须先于下面的泛化 QENG 分支,
		// 否则只会返回 QENG 部分,丢失 +QNWCFG 测量行。
		return command + "\r\n" + strings.Join(mockNeighbourScanLines(), "\r\n") + "\r\nOK\r\n"
	case strings.Contains(upper, `"NR5G_EARFCN_LOCK"`), strings.Contains(upper, `"LTE_EARFCN_LOCK"`):
		return mockEarfcnLockResponse(command)
	case strings.Contains(upper, "QENG"):
		// 泛化 QENG 分支的默认载荷即一条 NR5G-SA servingcell 行,
		// 覆盖 `AT+QENG="servingcell"` 单查(本机驻留 NR5G-SA,scs 字段恒 "-")。
		return currentMockQENGATResponse(command)
	case strings.Contains(upper, "QCAINFO"):
		return currentMockQCAInfoATResponse(command)
	case strings.Contains(upper, "+QSCAN"):
		return command + "\r\n" + strings.Join(mockQSCANLines(), "\r\n") + "\r\nOK\r\n"
	case strings.Contains(upper, `"LANIP"`):
		return command + "\r\n" + mockLANIP + "\r\nOK\r\n"
	case strings.Contains(upper, `"WWAN"`):
		return mockWWANStatusResponse(command)
	case upper == "AT+CGSN" || upper == "+CGSN" || upper == "CGSN":
		return command + "\r\n" + mockIMEI + "\r\nOK\r\n"
	case upper == "AT+QGMR" || upper == "+QGMR" || upper == "QGMR":
		return command + "\r\n" + mockFirmwareVersion + "\r\nOK\r\n"
	case upper == "AT+CIMI" || upper == "+CIMI" || upper == "CIMI":
		return command + "\r\n" + mockIMSI + "\r\nOK\r\n"
	case upper == "AT+ICCID" || upper == "+ICCID" || upper == "ICCID":
		return command + "\r\n+ICCID: " + mockICCID + "\r\nOK\r\n"
	case upper == "AT+CNUM" || upper == "+CNUM" || upper == "CNUM":
		return command + "\r\n+CNUM: ,\"\",255\r\nOK\r\n"
	case upper == "AT+CPIN?" || upper == "+CPIN?" || upper == "CPIN?":
		return command + "\r\n+CPIN: READY\r\nOK\r\n"
	case upper == "AT+QSIMSTAT?" || upper == "+QSIMSTAT?" || upper == "QSIMSTAT?":
		return command + "\r\n+QSIMSTAT: 0,1\r\nOK\r\n"
	case upper == "AT+C5GREG?" || upper == "+C5GREG?" || upper == "C5GREG?":
		// 5G 注册状态查询:mock 驻留原型为已注册(本地网,stat=1)。
		return command + "\r\n+C5GREG: 0,1\r\nOK\r\n"
	case upper == "AT+CSQ" || upper == "+CSQ" || upper == "CSQ":
		return command + "\r\n+CSQ: 99,99\r\nOK\r\n"
	case upper == "AT+QTEMP" || upper == "+QTEMP" || upper == "QTEMP":
		return command + "\r\n" + strings.Join(mockQTEMPSensorLines(), "\r\n") + "\r\nOK\r\n"
	case upper == "AT+QSPN" || upper == "+QSPN" || upper == "QSPN":
		return command + "\r\n" + mockQSPNLine + "\r\nOK\r\n"
	case upper == "AT+QUIMSLOT?" || upper == "+QUIMSLOT?":
		return command + "\r\n+QUIMSLOT: 1\r\nOK\r\n"
	case upper == "AT+QGDNRCNT?" || upper == "+QGDNRCNT?":
		return command + "\r\n+QGDNRCNT: 62817184,45605113\r\nOK\r\n"
	case upper == "AT+QGDCNT?" || upper == "+QGDCNT?":
		return command + "\r\n+QGDCNT: 0,0\r\nOK\r\n"
	case upper == "AT+QRSRP" || upper == "+QRSRP":
		return command + "\r\n+QRSRP: -85,-86,-86,-83,NR5G\r\nOK\r\n"
	case upper == "AT+CGCONTRDP=1" || upper == "+CGCONTRDP=1":
		return command + "\r\n" + mockCGCONTRDP + "\r\nOK\r\n"
	case strings.Contains(upper, "CMGL"):
		// SM(SIM)存储缺省为空:入站短信按 CPMS mem3=ME 路由落在 ME,
		// mock 里 SM 返回空列表,双存储合并后仍是 ME 的单一数据,避免重复。
		if strings.Contains(upper, `+CPMS="SM"`) {
			return command + "\r\nOK\r\n"
		}
		return mockSMSList()
	default:
		return command + "\r\nOK\r\n"
	}
}

func mockDeviceIdentityResponse(command string) string {
	return strings.TrimSpace(command) + "\r\n" + `Quectel
` + mockIMEI + `
` + mockFirmwareVersion + `
` + mockIMSI + `
+ICCID: ` + mockICCID + `
+CNUM: ,"",255

OK` + "\r\n"
}

func mockDeviceSIMStatusResponse(command string) string {
	return strings.TrimSpace(command) + "\r\n" + `+QSIMSTAT: 0,1

+CPIN: READY

+QMAP: "WWAN",1,1,"IPV4","` + mockWWANIPv4 + `"
+QMAP: "WWAN",1,1,"IPV6","` + mockWWANIPv6 + `"

OK` + "\r\n"
}

func mockWWANStatusResponse(command string) string {
	return strings.TrimSpace(command) + "\r\n" + `+QMAP: "WWAN",1,1,"IPV4","` + mockWWANIPv4 + `"
+QMAP: "WWAN",1,1,"IPV6","` + mockWWANIPv6 + `"

OK` + "\r\n"
}

func mockNetworkSettingsResponse(command string) string {
	upper := strings.ToUpper(strings.TrimSpace(command))
	lines := []string{strings.TrimSpace(command)}
	if strings.Contains(upper, "+QUIMSLOT") {
		lines = append(lines, "", "+QUIMSLOT: 1")
	}
	if strings.Contains(upper, `"MODE_PREF"`) {
		lines = append(lines, "", `+QNWPREFCFG: "mode_pref",AUTO`)
	}
	if strings.Contains(upper, `"NR5G_DISABLE_MODE"`) {
		lines = append(lines, "", `+QNWPREFCFG: "nr5g_disable_mode",0`)
	}
	if strings.Contains(upper, "+CGDCONT") {
		lines = append(lines, "",
			`+CGDCONT: 1,"IPV4V6","ctnet","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,0,,,,,,,,,"",,,,0`,
			`+CGDCONT: 2,"IPV4V6","IMS","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,0,,,,,,,,,"",,,,0`,
			`+CGDCONT: 3,"IPV4V6","ctwap","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,0,,,,,,,,,"",,,,0`,
			`+CGDCONT: 4,"IPV4V6","sos","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,1,,,,,,,,,"",,,,0`)
	}
	if strings.Contains(upper, "+CGCONTRDP") {
		lines = append(lines, "", mockCGCONTRDP)
	}
	if strings.Contains(upper, `"COMMON/4G"`) {
		lines = append(lines, "", `+QNWLOCK: "common/4g",0`)
	}
	if strings.Contains(upper, `"COMMON/5G"`) {
		lines = append(lines, "", `+QNWLOCK: "common/5g",0`)
	}
	lines = append(lines, "", "OK")
	return strings.Join(lines, "\r\n") + "\r\n"
}

// isMockSettingsWriteCommand 判断设置类命令是否为写形态(`AT+QMAP="XXX",参数`)。
// 按 `;` 分段统计逗号数:裸查询形如 `AT+QMAP="XXX"`,整段没有逗号;
// 写形态带参数,段内至少 1 个逗号。分号组合读命令
// (如 `+QMAP="MPDN_RULE";+QMAP="DHCPV4DNS"`)每段均无逗号,不受影响。
func isMockSettingsWriteCommand(command string) bool {
	upper := strings.ToUpper(command)
	for _, segment := range strings.Split(upper, ";") {
		if strings.Count(segment, ",") >= 1 {
			return true
		}
	}
	return false
}

func mockSettingsStatusResponse(command string) string {
	upper := strings.ToUpper(strings.TrimSpace(command))
	// 真实固件对写命令只回 OK,不返回状态行;mock 保持一致,
	// 否则写响应里混入查询态状态行会被调用方误读为写后状态。
	if isMockSettingsWriteCommand(command) {
		return strings.TrimSpace(command) + "\r\nOK\r\n"
	}
	lines := []string{strings.TrimSpace(command)}
	if strings.Contains(upper, `"MPDN_RULE"`) {
		lines = append(lines, "",
			`+QMAP: "MPDN_rule",0,0,0,0,0`,
			`+QMAP: "MPDN_rule",1,0,0,0,0`,
			`+QMAP: "MPDN_rule",2,0,0,0,0`,
			`+QMAP: "MPDN_rule",3,0,0,0,0`)
	}
	if strings.Contains(upper, `"DHCPV6DNS"`) {
		lines = append(lines, "", `+QMAP: "DHCPV6DNS","disable"`)
	}
	if strings.Contains(upper, `"USBNET"`) {
		lines = append(lines, "", `+QCFG: "usbnet",0`)
	}
	if strings.Contains(upper, `"DMZ"`) {
		lines = append(lines, "", `+QMAP: "DMZ",0,4`, `+QMAP: "DMZ",0,6`)
	}
	if strings.Contains(upper, `"DHCPV4DNS"`) {
		// 写形态(`"DHCPV4DNS",参数`)已在函数入口按写命令只回 OK;
		// 到这里只剩裸查询,按真实固件行为以 ERROR 收尾(该固件不支持查询)。
		lines = append(lines, "", "ERROR")
		return strings.Join(lines, "\r\n") + "\r\n"
	}
	lines = append(lines, "", "OK")
	return strings.Join(lines, "\r\n") + "\r\n"
}

// mockMacBindMu/mockMacBindEntries 维护 MAC_bind mock 的内存态绑定表,
// 让查询/设置/删除在联调中呈现真实固件的有状态行为。
// 初态固定一条示例绑定;测试可直接增删并自行恢复。
var (
	mockMacBindMu      sync.Mutex
	mockMacBindEntries = []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}}
)

// mockMacBindResponse 是固件私有命令 AT+QMAP="MAC_bind"(官方手册未收录,
// 实机验证)的有状态 mock,供 Windows 预览版联调界面。回显首行保留原
// command 保持用户输入的大小写;渲染格式与真实固件一致:
// `+QMAP: "MAC_bind",idx,"MAC","IP"` 行 + OK,无绑定时只回 OK。
func mockMacBindResponse(command string) string {
	// 命令里没有嵌套引号,直接按逗号切分区分形态:
	// 查询整条不带参数、没有逗号(`AT+QMAP="MAC_bind"`,切分后仅 1 段);
	// 设置/删除为 4 段(`AT+QMAP="MAC_bind",idx,"MAC","IP"`,删除时后两段为空)。
	parts := strings.Split(command, ",")
	switch len(parts) {
	case 1:
		mockMacBindMu.Lock()
		entries := append([]macBindLive(nil), mockMacBindEntries...)
		mockMacBindMu.Unlock()
		if len(entries) == 0 {
			return command + "\r\nOK\r\n"
		}
		lines := []string{command}
		for _, entry := range entries {
			lines = append(lines, fmt.Sprintf(`+QMAP: "MAC_bind",%d,"%s","%s"`, entry.Index, entry.MAC, entry.IP))
		}
		lines = append(lines, "OK")
		return strings.Join(lines, "\r\n") + "\r\n"
	case 4:
		idx, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err == nil {
			mac := strings.Trim(strings.TrimSpace(parts[2]), `"`)
			ip := strings.Trim(strings.TrimSpace(parts[3]), `"`)
			mockMacBindMu.Lock()
			if mac == "" && ip == "" {
				// 删除形态:按槽位号移除。
				kept := make([]macBindLive, 0, len(mockMacBindEntries))
				for _, entry := range mockMacBindEntries {
					if entry.Index != idx {
						kept = append(kept, entry)
					}
				}
				mockMacBindEntries = kept
			} else {
				// 写形态:同槽位覆盖,否则追加。
				upserted := false
				for i := range mockMacBindEntries {
					if mockMacBindEntries[i].Index == idx {
						mockMacBindEntries[i] = macBindLive{Index: idx, MAC: mac, IP: ip}
						upserted = true
						break
					}
				}
				if !upserted {
					mockMacBindEntries = append(mockMacBindEntries, macBindLive{Index: idx, MAC: mac, IP: ip})
				}
			}
			mockMacBindMu.Unlock()
		}
		return command + "\r\nOK\r\n"
	default:
		return command + "\r\nOK\r\n"
	}
}

func mockQNWPREFCFGResponse(command string) string {
	lines := []string{command}
	// 频段列表为 RG520N-CN 已确认的硬件能力集。
	// 注意:本机固件 AT+QNWPREFCFG="ue_capability_band" 返回受限子集
	// (LTE 仅 1:3:5:8、NSA 仅 78),不代表硬件能力,勿据此修改本表。
	queries := []struct {
		name  string
		value string
	}{
		{"lte_band", "1:3:5:8:34:38:39:40:41"},
		{"nsa_nr5g_band", "1:8:28:41:78"},
		{"nr5g_band", "1:8:28:41:78"},
		{"mode_pref", "AUTO"},
		{"nr5g_disable_mode", "0"},
	}
	for _, query := range queries {
		pattern := regexp.MustCompile(`(?i)\+QNWPREFCFG\s*=\s*"` + regexp.QuoteMeta(query.name) + `"\s*(?:;|\r|\n|$)`)
		if pattern.MatchString(command) {
			lines = append(lines, fmt.Sprintf(`+QNWPREFCFG: "%s",%s`, query.name, query.value))
		}
	}
	lines = append(lines, "OK")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func mockSMSList() string {
	pduCN := buildMockSMSDeliverPDU("10001", "尊敬的用户：您本月的流量已使用80%，详情可登录中国电信APP查询。")
	pduEN := buildMockSMSDeliverPDU("+10000000000", "SimpleAdmin Windows mock SMS")
	return fmt.Sprintf("+CMGL: 1,0,,%d\r\n%s\r\n+CMGL: 2,0,,%d\r\n%s\r\nOK\r\n", len(pduCN)/2-1, pduCN, len(pduEN)/2-1, pduEN)
}

func mockSMSSendResponse(phoneNumber string) string {
	return fmt.Sprintf("> \r\n+CMGS: 1\r\nOK\r\n# mock sent to %s\r\n", phoneNumber)
}

// mockATProxyTransactionResponse 模拟交互事务(如 AT+CMGS)的完整响应:
// 提示符 + 发送结果 + OK,供 Windows 预览版联调 at-client --payload。
func mockATProxyTransactionResponse() string {
	return "> \r\n+CMGS: 1\r\nOK\r\n"
}

func mockUptimeText() string {
	totalMinutes := int(time.Since(processStartTime).Seconds()) / 60
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	return fmt.Sprintf("up %d day, %d hour, %d min", days, hours, minutes)
}
