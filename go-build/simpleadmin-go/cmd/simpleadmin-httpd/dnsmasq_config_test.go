package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dnsMarkerBlock 是按常量拼装出的期望标记块文本,供各用例复用。
func dnsMarkerBlock() string {
	return dnsmasqMarkerBegin + "\nconf-file=" + dnsmasqIncludePath + "\n" + dnsmasqMarkerEnd + "\n"
}

func TestDnsmasqRenderDNSIncludeContent(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		servers []string
		want    string
	}{
		{
			name:    "禁用时只保留注释头",
			enabled: false,
			servers: []string{"1.1.1.1"},
			want:    dnsIncludeFileHeader + "\n",
		},
		{
			name:    "启用但服务器为空只保留注释头",
			enabled: true,
			servers: nil,
			want:    dnsIncludeFileHeader + "\n",
		},
		{
			name:    "启用单服务器",
			enabled: true,
			servers: []string{"223.5.5.5"},
			want:    dnsIncludeFileHeader + "\nno-resolv\nserver=223.5.5.5\n",
		},
		{
			name:    "启用多服务器按序输出",
			enabled: true,
			servers: []string{"223.5.5.5", "119.29.29.29", "1.1.1.1"},
			want: dnsIncludeFileHeader + "\nno-resolv\n" +
				"server=223.5.5.5\nserver=119.29.29.29\nserver=1.1.1.1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderDNSIncludeContent(tt.enabled, tt.servers); got != tt.want {
				t.Fatalf("renderDNSIncludeContent(%v, %v) = %q, want %q", tt.enabled, tt.servers, got, tt.want)
			}
		})
	}
}

func TestDnsmasqAppendMarkerBlock(t *testing.T) {
	block := dnsMarkerBlock()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "正常追加并以空行分隔",
			content: "dhcp-hostsfile=/etc/data/dhcp_hosts\n",
			want:    "dhcp-hostsfile=/etc/data/dhcp_hosts\n\n" + block,
		},
		{
			name:    "前文无结尾换行时补齐",
			content: "dhcp-hostsfile=/etc/data/dhcp_hosts",
			want:    "dhcp-hostsfile=/etc/data/dhcp_hosts\n\n" + block,
		},
		{
			name:    "空文件直接输出标记块",
			content: "",
			want:    block,
		},
		{
			name:    "已含标记时幂等原样返回",
			content: "a\n\n" + block,
			want:    "a\n\n" + block,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendDNSMarkerBlock(tt.content); got != tt.want {
				t.Fatalf("appendDNSMarkerBlock = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("二次调用结果不变", func(t *testing.T) {
		once := appendDNSMarkerBlock("vendor-line\n")
		twice := appendDNSMarkerBlock(once)
		if once != twice {
			t.Fatalf("二次追加发生变化: %q != %q", twice, once)
		}
	})
}

func TestDnsmasqRemoveMarkerBlock(t *testing.T) {
	block := dnsMarkerBlock()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "有块删除且清理块前空行",
			content: "a\n\n" + block,
			want:    "a\n",
		},
		{
			name:    "无块原样返回",
			content: "a\nb\n",
			want:    "a\nb\n",
		},
		{
			name:    "删除后不残留多余空行",
			content: "a\n\n\n\n" + block,
			want:    "a\n",
		},
		{
			name:    "块后仍有内容时保留原有空行",
			content: "a\n\n" + block + "b\n",
			want:    "a\n\nb\n",
		},
		{
			name:    "只有起始标记无结束标记时保守原样返回",
			content: "a\n" + dnsmasqMarkerBegin + "\nconf-file=x\n",
			want:    "a\n" + dnsmasqMarkerBegin + "\nconf-file=x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removeDNSMarkerBlock(tt.content); got != tt.want {
				t.Fatalf("removeDNSMarkerBlock = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDnsmasqMarkerRoundTrip(t *testing.T) {
	// 追加再删除应还原原文;append 会把结尾换行规整化,故比较前用 TrimRight 去掉尾部换行差异。
	originals := []string{
		"",
		"a",
		"a\n",
		"a\nb\n",
		"a\n\n\n",
		"dhcp-hostsfile=/etc/data/dhcp_hosts\n# vendor comment\n",
	}
	for _, original := range originals {
		got := removeDNSMarkerBlock(appendDNSMarkerBlock(original))
		if strings.TrimRight(got, "\n") != strings.TrimRight(original, "\n") {
			t.Fatalf("往返不一致: 原文 %q, 得到 %q", original, got)
		}
	}
}

func TestDnsmasqMarkerBlockComplete(t *testing.T) {
	block := dnsMarkerBlock()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "完整标记块",
			content: "a\n\n" + block,
			want:    true,
		},
		{
			name:    "缺结束标记",
			content: "a\n" + dnsmasqMarkerBegin + "\nconf-file=" + dnsmasqIncludePath + "\n",
			want:    false,
		},
		{
			name:    "缺 conf-file 行",
			content: "a\n" + dnsmasqMarkerBegin + "\n" + dnsmasqMarkerEnd + "\n",
			want:    false,
		},
		{
			name:    "conf-file 指向其他路径不算完整",
			content: "a\n" + dnsmasqMarkerBegin + "\nconf-file=/tmp/other.conf\n" + dnsmasqMarkerEnd + "\n",
			want:    false,
		},
		{
			name:    "无任何标记",
			content: "a\nb\n",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsMarkerBlockComplete(tt.content); got != tt.want {
				t.Fatalf("dnsMarkerBlockComplete(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestDnsmasqMarkerRepairRoundTrip(t *testing.T) {
	block := dnsMarkerBlock()
	// 修复路径(同 ensureDnsmasqMarker):块不完整时先移除残留再重新追加完整块。
	tests := []struct {
		name      string
		corrupted string
		want      string
	}{
		{
			name:      "块内缺 conf-file 行时移除后重建",
			corrupted: "a\n\n" + dnsmasqMarkerBegin + "\n" + dnsmasqMarkerEnd + "\n",
			want:      "a\n\n" + block,
		},
		{
			name:      "conf-file 指向错误路径时移除后重建",
			corrupted: "a\n\n" + dnsmasqMarkerBegin + "\nconf-file=/tmp/other.conf\n" + dnsmasqMarkerEnd + "\n",
			want:      "a\n\n" + block,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendDNSMarkerBlock(removeDNSMarkerBlock(tt.corrupted))
			if got != tt.want {
				t.Fatalf("修复结果 = %q, want %q", got, tt.want)
			}
			if !dnsMarkerBlockComplete(got) {
				t.Fatalf("修复结果仍不完整: %q", got)
			}
		})
	}

	t.Run("只有起始标记的残留截断后重建", func(t *testing.T) {
		// removeDNSMarkerBlock 对只有起始标记的内容保守保留,需截断配合(同修复顺序)。
		corrupted := "a\n" + dnsmasqMarkerBegin + "\nconf-file=" + dnsmasqIncludePath + "\n"
		got := appendDNSMarkerBlock(truncateDNSMarkerTail(removeDNSMarkerBlock(corrupted)))
		want := "a\n\n" + block
		if got != want {
			t.Fatalf("修复结果 = %q, want %q", got, want)
		}
		if !dnsMarkerBlockComplete(got) {
			t.Fatalf("修复结果仍不完整: %q", got)
		}
	})
}

func TestDnsmasqUpstreamStateFile(t *testing.T) {
	dir := t.TempDir()
	old := runtimeDNSUpstreamStateFile
	runtimeDNSUpstreamStateFile = filepath.Join(dir, "sub", "dns_upstream.conf")
	t.Cleanup(func() { runtimeDNSUpstreamStateFile = old })

	t.Run("文件不存在读默认值", func(t *testing.T) {
		state, err := readDNSUpstreamState()
		if err != nil {
			t.Fatalf("读取不存在的状态文件报错: %v", err)
		}
		want := dnsUpstreamState{Enabled: false, Servers: []string{}}
		if !reflect.DeepEqual(state, want) {
			t.Fatalf("默认状态 = %+v, want %+v", state, want)
		}
	})

	t.Run("写后读一致", func(t *testing.T) {
		in := dnsUpstreamState{Enabled: true, Servers: []string{"223.5.5.5", "1.1.1.1"}}
		if err := writeDNSUpstreamState(in); err != nil {
			t.Fatalf("写入上游 DNS 状态失败: %v", err)
		}
		out, err := readDNSUpstreamState()
		if err != nil {
			t.Fatalf("读取上游 DNS 状态失败: %v", err)
		}
		if !reflect.DeepEqual(out, in) {
			t.Fatalf("round trip = %+v, want %+v", out, in)
		}
		info, err := os.Stat(runtimeDNSUpstreamStateFile)
		if err != nil {
			t.Fatalf("stat 状态文件失败: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("状态文件权限 = %o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("JSON 损坏读报错", func(t *testing.T) {
		if err := os.WriteFile(runtimeDNSUpstreamStateFile, []byte("{not json"), 0600); err != nil {
			t.Fatalf("写入损坏文件失败: %v", err)
		}
		if _, err := readDNSUpstreamState(); err == nil {
			t.Fatalf("读取损坏状态文件未报错, want 错误")
		}
	})

	t.Run("启用但服务器为空归一为禁用", func(t *testing.T) {
		if err := os.Remove(runtimeDNSUpstreamStateFile); err != nil && !os.IsNotExist(err) {
			t.Fatalf("清理状态文件失败: %v", err)
		}
		in := dnsUpstreamState{Enabled: true, Servers: []string{}}
		if err := writeDNSUpstreamState(in); err != nil {
			t.Fatalf("写入上游 DNS 状态失败: %v", err)
		}
		out, err := readDNSUpstreamState()
		if err != nil {
			t.Fatalf("读取上游 DNS 状态失败: %v", err)
		}
		want := dnsUpstreamState{Enabled: false, Servers: []string{}}
		if !reflect.DeepEqual(out, want) {
			t.Fatalf("归一后状态 = %+v, want %+v", out, want)
		}
	})
}

func TestDnsmasqMacBindStateFile(t *testing.T) {
	dir := t.TempDir()
	old := runtimeMacBindStateFile
	runtimeMacBindStateFile = filepath.Join(dir, "sub", "mac_bind.conf")
	t.Cleanup(func() { runtimeMacBindStateFile = old })

	t.Run("文件不存在读空列表", func(t *testing.T) {
		entries, err := readMacBindState()
		if err != nil {
			t.Fatalf("读取不存在的状态文件报错: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("默认列表 = %+v, want 空列表", entries)
		}
	})

	t.Run("写后读一致", func(t *testing.T) {
		in := []macBindEntry{
			{MAC: "aa:bb:cc:dd:ee:01", IP: "192.168.1.10"},
			{MAC: "aa:bb:cc:dd:ee:02", IP: "192.168.1.11"},
		}
		if err := writeMacBindState(in); err != nil {
			t.Fatalf("写入静态租约状态失败: %v", err)
		}
		out, err := readMacBindState()
		if err != nil {
			t.Fatalf("读取静态租约状态失败: %v", err)
		}
		if !reflect.DeepEqual(out, in) {
			t.Fatalf("round trip = %+v, want %+v", out, in)
		}
	})

	t.Run("写空列表读回空列表而非 null", func(t *testing.T) {
		if err := writeMacBindState(nil); err != nil {
			t.Fatalf("写入空列表失败: %v", err)
		}
		out, err := readMacBindState()
		if err != nil || out == nil || len(out) != 0 {
			t.Fatalf("空列表 round trip = %+v, %v, want 空列表无错误", out, err)
		}
	})

	t.Run("JSON 损坏读报错", func(t *testing.T) {
		if err := os.WriteFile(runtimeMacBindStateFile, []byte("[broken"), 0600); err != nil {
			t.Fatalf("写入损坏文件失败: %v", err)
		}
		if _, err := readMacBindState(); err == nil {
			t.Fatalf("读取损坏状态文件未报错, want 错误")
		}
	})
}

// 说明:applyDNSUpstream 的并发串行化不在本文件测试。该函数的文件路径为常量、
// 校验/重启为直接 exec,无可注入桩,单测中无法真实并发调用;串行性由
// native_dnsmasq.go 中的包级锁 dnsUpstreamMu 保证(锁住整个事务含回滚,
// 覆盖页面保存与开机自愈两个调用点),详见其声明处注释。
