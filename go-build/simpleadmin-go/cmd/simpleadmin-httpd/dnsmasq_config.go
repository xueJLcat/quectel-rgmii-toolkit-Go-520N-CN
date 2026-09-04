// dnsmasq_config.go 提供 dnsmasq 原生层的平台无关部分:路径常量、状态类型、
// 包含文件/标记块的纯文本渲染函数,以及上游 DNS 与静态租约状态文件的原子读写。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultDNSUpstreamStateFile = "/usrdata/simpleadmin/dns_upstream.conf"
	defaultMacBindStateFile     = "/usrdata/simpleadmin/mac_bind.conf"
	// dnsmasqMainConfPath 是厂商配置文件,出厂后不被 QCMAP 重写,因此可安全追加标记块;
	// 其中 dhcp-hostsfile=/etc/data/dhcp_hosts 指向静态租约文件。
	dnsmasqMainConfPath = "/etc/data/dnsmasq.conf"
	// dnsmasqBridgeConfPath 位于 tmpfs,由 QCMAP 运行时生成,是单元
	// dnsmasq_service@0.service 实际加载的入口,其内嵌套 conf-file=/etc/data/dnsmasq.conf。
	dnsmasqBridgeConfPath = "/var/run/data/dnsmasq.conf.bridge0"
	dnsmasqIncludeDir     = "/usrdata/simpleadmin/dnsmasq.d"
	dnsmasqIncludePath    = "/usrdata/simpleadmin/dnsmasq.d/upstream.conf"
	dnsmasqConfBackupPath = "/usrdata/simpleadmin/backup/dnsmasq.conf.orig"
	dnsmasqUnitName       = "dnsmasq_service@0.service"
	// dnsmasqMarkerBegin/End 包围我们追加进厂商配置文件的标记块,
	// 用于幂等检测与完整移除,避免重复追加或误删厂商内容。
	dnsmasqMarkerBegin        = "# >>> simpleadmin dns-upstream >>>"
	dnsmasqMarkerEnd          = "# <<< simpleadmin dns-upstream <<<"
	dnsmasqMaxUpstreamServers = 4
	// dnsIncludeFileHeader 是包含文件固定注释头:禁用态只保留它,
	// 让 dnsmasq 回退 /etc/resolv.conf 的运营商 DNS。
	dnsIncludeFileHeader = "# simpleadmin upstream dns (managed by simpleadmin-httpd; do not edit)"
)

var (
	runtimeDNSUpstreamStateFile = defaultDNSUpstreamStateFile
	runtimeMacBindStateFile     = defaultMacBindStateFile
)

// dnsUpstreamState 表示上游 DNS 自定义方案的持久化状态。
type dnsUpstreamState struct {
	Enabled bool     `json:"enabled"`
	Servers []string `json:"servers"`
}

// macBindEntry 表示一条静态租约绑定:网卡地址 -> 固定 IPv4。
type macBindEntry struct {
	MAC string `json:"mac"`
	IP  string `json:"ip"`
}

// renderDNSIncludeContent 生成上游 DNS 包含文件内容:禁用或服务器列表为空时
// 只保留注释头(等效回退运营商 DNS);启用时输出 no-resolv 与每个 server=<ip> 行。
func renderDNSIncludeContent(enabled bool, servers []string) string {
	var b strings.Builder
	b.WriteString(dnsIncludeFileHeader)
	b.WriteString("\n")
	if !enabled || len(servers) == 0 {
		return b.String()
	}
	b.WriteString("no-resolv\n")
	for _, server := range servers {
		b.WriteString("server=")
		b.WriteString(server)
		b.WriteString("\n")
	}
	return b.String()
}

// appendDNSMarkerBlock 幂等地在厂商配置末尾追加上游 DNS 标记块;
// 已含起始标记时原样返回,前文结尾换行缺失时补齐并保留一个空行分隔。
func appendDNSMarkerBlock(content string) string {
	if strings.Contains(content, dnsmasqMarkerBegin) {
		return content
	}
	block := dnsmasqMarkerBegin + "\nconf-file=" + dnsmasqIncludePath + "\n" + dnsmasqMarkerEnd + "\n"
	if strings.TrimSpace(content) == "" {
		return block
	}
	return strings.TrimRight(content, "\n") + "\n\n" + block
}

// removeDNSMarkerBlock 删除从含起始标记的行到含结束标记的行(含两行);
// 无标记时原样返回;块位于文件末尾时顺带清理块前紧邻的多余空行,使结尾自然。
func removeDNSMarkerBlock(content string) string {
	if !strings.Contains(content, dnsmasqMarkerBegin) {
		return content
	}
	lines := strings.Split(content, "\n")
	begin, end := -1, -1
	for i, line := range lines {
		if strings.Contains(line, dnsmasqMarkerBegin) {
			begin = i
			break
		}
	}
	if begin < 0 {
		return content
	}
	for i := begin + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], dnsmasqMarkerEnd) {
			end = i
			break
		}
	}
	if end < 0 {
		// 只有起始标记没有结束标记属于异常内容,保守起见原样返回,避免误删厂商配置。
		return content
	}
	blockAtEnd := true
	for _, line := range lines[end+1:] {
		if strings.TrimSpace(line) != "" {
			blockAtEnd = false
			break
		}
	}
	kept := lines[:begin]
	if blockAtEnd {
		for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
			kept = kept[:len(kept)-1]
		}
	}
	result := strings.Join(append(append([]string{}, kept...), lines[end+1:]...), "\n")
	if blockAtEnd {
		result = strings.TrimRight(result, "\n")
		if result != "" {
			result += "\n"
		}
	}
	return result
}

// dnsMarkerBlockComplete 判断厂商配置内容中的标记块是否完整:必须同时含
// 起始标记、conf-file=包含文件路径 行与结束标记。完整性定义供开机自愈的一致性
// 判断与损坏修复共用;只有起始标记的残留通常来自上次写入中断,不能视为已注入。
func dnsMarkerBlockComplete(content string) bool {
	if !strings.Contains(content, dnsmasqMarkerBegin) || !strings.Contains(content, dnsmasqMarkerEnd) {
		return false
	}
	directive := "conf-file=" + dnsmasqIncludePath
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == directive {
			return true
		}
	}
	return false
}

// truncateDNSMarkerTail 处理只有起始标记、没有结束标记的残留:从含起始标记的行
// 截断到文件末尾。标记块总是由本服务追加在厂商配置末尾,起始标记之后的内容都
// 属于我们(可能中断的)写入,截断不会误删厂商内容;无起始标记时原样返回。
func truncateDNSMarkerTail(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, dnsmasqMarkerBegin) {
			kept := strings.TrimRight(strings.Join(lines[:i], "\n"), "\n")
			if kept != "" {
				kept += "\n"
			}
			return kept
		}
	}
	return content
}

// atomicWriteFile 原子写入:同目录临时文件写入成功后 rename 到位,
// 避免掉电或重启时被 dnsmasq/本程序读到半截内容。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".simpleadmin.tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	// rename 前先落盘:设备掉电常见,避免 rename 后元数据已更新而数据仍在页缓存,
	// 掉电留下空/损坏的状态文件,导致开机自愈与列表接口异常。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// readDNSUpstreamState 读取上游 DNS 状态;文件不存在视为未启用的初始状态(不算错误)。
func readDNSUpstreamState() (dnsUpstreamState, error) {
	state := dnsUpstreamState{Enabled: false, Servers: []string{}}
	data, err := os.ReadFile(runtimeDNSUpstreamStateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return dnsUpstreamState{}, fmt.Errorf("解析上游 DNS 状态文件 %s 失败: %v", runtimeDNSUpstreamStateFile, err)
	}
	if state.Servers == nil {
		state.Servers = []string{}
	}
	// 非法状态归一:启用但服务器列表为空时,包含文件渲染结果只有注释头,
	// dnsmasq 实际回退运营商 DNS,继续显示"已启用"是误导,统一归一为禁用。
	if state.Enabled && len(state.Servers) == 0 {
		state.Enabled = false
	}
	return state, nil
}

// writeDNSUpstreamState 原子写入上游 DNS 状态(权限 0600)。
func writeDNSUpstreamState(state dnsUpstreamState) error {
	if state.Servers == nil {
		state.Servers = []string{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(runtimeDNSUpstreamStateFile, append(data, '\n'), 0600)
}

// readMacBindState 读取静态租约列表;文件不存在视为空列表(不算错误)。
func readMacBindState() ([]macBindEntry, error) {
	data, err := os.ReadFile(runtimeMacBindStateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []macBindEntry{}, nil
		}
		return nil, err
	}
	var entries []macBindEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("解析静态租约状态文件 %s 失败: %v", runtimeMacBindStateFile, err)
	}
	if entries == nil {
		entries = []macBindEntry{}
	}
	return entries, nil
}

// writeMacBindState 原子写入静态租约列表(权限 0600);空列表序列化为 [] 而非 null。
func writeMacBindState(entries []macBindEntry) error {
	if entries == nil {
		entries = []macBindEntry{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(runtimeMacBindStateFile, append(data, '\n'), 0600)
}
