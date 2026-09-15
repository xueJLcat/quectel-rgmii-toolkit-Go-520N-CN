package main

import (
	"strings"
	"time"
)

// maxAgeForATCacheCommand 返回 AT 命令结果的缓存时长。缓存分级是本项目
// AT 读取策略的唯一定义点:
//
//	动作命令(写/重启/删除等)        -> 0,不缓存,每次执行
//	静态身份信息(IMEI/IMSI 等)      -> atCacheStaticMaxAge(长)
//	配置类(设置页状态/频段偏好等)   -> atCacheConfigMaxAge(中)
//	半静态(模式偏好组合查询)         -> atCacheSemiStaticMaxAge
//	短信列表                         -> 5 秒
//	信号详情(页面专属,不参与周期刷新) -> 5 秒
//	实时信号/网络状态                -> atCacheReadMaxAge(短)
//	其余读取命令                     -> atCacheReadMaxAge
func maxAgeForATCacheCommand(command string) time.Duration {
	command = sanitizeATCommand(command)
	upper := strings.ToUpper(command)
	switch {
	case isATActionCommand(command):
		return 0
	case isDeviceStaticInfoCommand(command) || isDeviceIdentityQueryCommand(command):
		return atCacheStaticMaxAge
	case isSettingsStatusConfigCommand(command) || isStandaloneLANIPCommand(command) || isBandPreferenceQueryCommand(command):
		return atCacheConfigMaxAge
	case isNetworkPreferenceStatusCommand(command):
		return atCacheSemiStaticMaxAge
	case isSignalDetailCommand(command):
		return 5 * time.Second
	case isSMSListCommand(command):
		return 5 * time.Second
	case strings.Contains(upper, "+QSIMSTAT") || strings.Contains(upper, "+CPIN") || strings.Contains(upper, `+QMAP="WWAN"`) || strings.Contains(upper, "+QCAINFO") || strings.Contains(upper, "+QENG") || strings.Contains(upper, "+QRSRP") || strings.Contains(upper, "+CSQ") || strings.Contains(upper, "+QTEMP"):
		return atCacheReadMaxAge
	default:
		return atCacheReadMaxAge
	}
}

// isDeviceIdentityQueryCommand 判断是否为设备静态身份查询(单条或组合中
// 包含身份字段)。这类命令结果不随时间变化,使用长缓存。
func isDeviceIdentityQueryCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	for _, token := range []string{"+CIMI", "+ICCID", "+CNUM", "+CGMI", "+CGSN", "+QGMR"} {
		if strings.Contains(upper, token) {
			return true
		}
	}
	return false
}

func isStandaloneLANIPCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(sanitizeATCommand(command)), `AT+QMAP="LANIP"`)
}

func isDeviceStaticInfoCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(sanitizeATCommand(command)), `AT+CGMI;+CGSN;+QGMR;+CIMI;+ICCID;+CNUM`)
}

func isBandPreferenceQueryCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	return strings.Contains(upper, `+QNWPREFCFG="LTE_BAND"`) &&
		strings.Contains(upper, `+QNWPREFCFG= "NSA_NR5G_BAND"`) &&
		strings.Contains(upper, `+QNWPREFCFG= "NR5G_BAND"`)
}

func isNetworkPreferenceStatusCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	return strings.Contains(upper, `+QNWPREFCFG="MODE_PREF"`) &&
		strings.Contains(upper, `+QNWPREFCFG="NR5G_DISABLE_MODE"`) &&
		strings.Contains(upper, "+CGDCONT?") &&
		strings.Contains(upper, "+CGCONTRDP=1") &&
		strings.Contains(upper, `+QNWLOCK="COMMON/4G"`) &&
		strings.Contains(upper, `+QNWLOCK="COMMON/5G"`)
}

func isSettingsStatusConfigCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	return strings.Contains(upper, `+QMAP="MPDN_RULE"`) &&
		strings.Contains(upper, `+QMAP="DHCPV6DNS"`) &&
		strings.Contains(upper, `+QCFG="USBNET"`) &&
		strings.Contains(upper, `+QMAP="DMZ"`) &&
		strings.Contains(upper, `+QMAP="DHCPV4DNS"`)
}

func isSMSListCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	return strings.Contains(upper, "+CMGL=4") || strings.Contains(upper, `+CMGL="ALL"`)
}

// isSignalDetailCommand 判定信号详情页专用组合命令。按净化后的完整命令
// 精确匹配:仪表盘的组合命令同样包含 +QENG/+QRSRP,不能用包含判断,
// 否则会把常驻刷新的仪表盘命令也拖进页面专属策略。
func isSignalDetailCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(sanitizeATCommand(command)), signalDetailATCommand())
}

func isPeriodicRefreshSuppressedATCommand(command string) bool {
	// 短信列表、信号详情与邻区扫描均为页面专属拉取:不参与周期刷新,
	// 仅在对应页面请求时执行,降低模块常态 AT 负载。邻区扫描是手动触发
	// 的一次性测量命令(含 ~3s 超时预算),若按普通读命令参与周期刷新,
	// 单次扫描后会在最近请求窗口(2 分钟)内每 15 秒被后台重跑一次,
	// 持续占用串行 AT 通道,拖慢仪表盘等常驻读取。
	return isSMSListCommand(command) || isSignalDetailCommand(command) || isNeighbourScanCommand(command)
}

// isNeighbourScanCommand 判定邻区扫描页面专属组合命令(与
// scanModeCommand("Neighbour Scan") 一致)。与信号详情同理,按净化后的
// 完整命令精确匹配:命令含 +QENG 片段,包含判断会误伤仪表盘组合命令。
func isNeighbourScanCommand(command string) bool {
	expected := scanModeCommand("Neighbour Scan")
	return expected != "" && strings.EqualFold(strings.TrimSpace(sanitizeATCommand(command)), expected)
}

// isCommonATCacheCommand 判断命令是否属于页面公共缓存命令集合
// (commonATCacheCommands)。集合身份只决定空闲驱逐豁免与启动预热选取;
// 周期刷新资格一律按最近请求窗口判定(见 isATCacheRefreshEligible),
// 集合内外命令无人查看时都不再后台重跑。
func isCommonATCacheCommand(command string) bool {
	command = sanitizeATCommand(command)
	for _, candidate := range commonATCacheCommands() {
		if sanitizeATCommand(candidate) == command {
			return true
		}
	}
	return false
}

// isATCacheRefreshEligible 判断过期条目是否允许参与周期刷新:所有命令
// (含页面公共命令)一律要求最近请求窗口内被 Fetch 过——页面打开时前端
// 轮询持续续热;关闭页面后后台刷新最多一个窗口内停摆,空闲态只保留
// 短信转发 webhook 与自动化(看门狗/定时重启/时间同步)等允许的常驻
// 服务流量。公共命令的零值条目(启动预热后从未被页面请求)视为无人
// 查看,不参与刷新;非公共零值条目为历史路径或测试直接构造,保持原有
// 刷新语义(生产环境所有条目都会经请求路径设置 lastRequested)。
func isATCacheRefreshEligible(command string, lastRequested time.Time) bool {
	if isCommonATCacheCommand(command) {
		return !lastRequested.IsZero() && time.Since(lastRequested) < atCacheRecentRequestWindow
	}
	if lastRequested.IsZero() {
		return true
	}
	return time.Since(lastRequested) < atCacheRecentRequestWindow
}

// isATActionCommand 判定命令是否为动作类(写/重启/删除)。入参可能是带
// "\r\n" 结尾的 payload(运行器在 at_runner 的重试防护处直接传 payload),
// 必须先净化再匹配:裸 AT&F/AT&W 的精确匹配形态若被尾部 \r\n 击穿,
// 破坏性命令超时后会被运行器盲目重发(恢复出厂被执行两次)。
func isATActionCommand(command string) bool {
	upper := strings.ToUpper(sanitizeATCommand(command))
	if upper == "AT&F" || strings.Contains(upper, ";AT&F") || upper == "AT&W" || strings.Contains(upper, ";AT&W") {
		return true
	}
	// +COPS=<mode>[,...] 手动选网/去注册是写命令;读形态 +COPS=? (查询
	// 支持的模式)豁免,否则查询会被误判动作:不缓存、不重试,执行后还会
	// 全量失效读缓存。
	if strings.Contains(upper, "+COPS=") && !strings.Contains(upper, "+COPS=?") {
		return true
	}
	patterns := []string{
		"+CFUN=",
		"+EGMR=",
		"+CMGD",
		"+CMGS",
		"+CMGW",
		"+QSCAN=",
		"+CGDCONT=",
		"+QMAPWAC=",
		"+QPOWD=",
		// 写形态补全:以下命令可经通用入口(get_atcache/user_atcommand/AT 代理)
		// 到达,漏判为读命令会在超时后被运行器盲目重发(写命令可能已执行,
		// 重发即重复执行),结果还会被按默认档缓存 3 秒。查询形态不受影响:
		// +CPIN?/+CSCA?/+COPS?/+QMBNCFG="List" 均不含下列写模式。
		`+CPIN="`, // 解锁 PIN(查询形态 +CPIN? 不含 =" )
		`+CSCA="`, // 写短信中心号码(查询形态 +CSCA? 不含 =" )
		`+QMBNCFG="SELECT"`,
		`+QMBNCFG="DELETE"`,
		`+QMBNCFG="ACTIVE"`,
		`+QCFG="USBNET",`,
		`+QMAP="MPDN_RULE",0`,
		`+QMAP="DHCPV6DNS",`,
		`+QMAP="DHCPV4DNS",`,
		`+QMAP="DMZ",`,
		`+QMAP="LANIP",`,
		// MAC_BIND 写/删形态带尾逗号;查询形态 `AT+QMAP="MAC_bind"` 无尾逗号,不受影响。
		// 写命令若被当读命令入队,开机保护期内会延迟执行,而调用方已收到失败响应。
		`+QMAP="MAC_BIND",`,
		`+QNWPREFCFG="LTE_BAND",`,
		`+QNWPREFCFG="NSA_NR5G_BAND",`,
		`+QNWPREFCFG="NR5G_BAND",`,
		`+QNWPREFCFG="MODE_PREF",`,
		`+QNWPREFCFG="NR5G_DISABLE_MODE",`,
		`+QNWLOCK="COMMON/4G",`,
		`+QNWLOCK="COMMON/5G",`,
		// 频点锁写形态带尾逗号(参数);查询形态 `AT+QNWCFG="nr5g_earfcn_lock"` 无尾逗号,不受影响。
		`+QNWCFG="NR5G_EARFCN_LOCK",`,
		`+QNWCFG="LTE_EARFCN_LOCK",`,
	}
	for _, pattern := range patterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}
