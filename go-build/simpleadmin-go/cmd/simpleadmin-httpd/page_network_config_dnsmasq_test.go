package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseMacBindAT(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []macBindLive
	}{
		{
			name: "空输入",
			raw:  "",
			want: []macBindLive{},
		},
		{
			name: "仅 OK 表示无绑定",
			raw:  "AT+QMAP=\"MAC_bind\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "单条绑定",
			raw:  "+QMAP: \"MAC_bind\",1,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}},
		},
		{
			name: "多条绑定按序解析且小写 MAC 归一为大写",
			raw: strings.Join([]string{
				`+QMAP: "MAC_bind",1,"52:54:00:12:34:56","192.168.225.50"`,
				`+QMAP: "MAC_bind",2,"aa:bb:cc:dd:ee:01","192.168.225.51"`,
				"OK",
			}, "\r\n"),
			want: []macBindLive{
				{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"},
				{Index: 2, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
			},
		},
		{
			name: "索引越界跳过",
			raw:  "+QMAP: \"MAC_bind\",11,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "索引为零跳过",
			raw:  "+QMAP: \"MAC_bind\",0,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "索引非数字跳过",
			raw:  "+QMAP: \"MAC_bind\",x,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "非法 MAC 跳过",
			raw:  "+QMAP: \"MAC_bind\",1,\"not-a-mac\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "IPv6 地址行跳过",
			raw:  "+QMAP: \"MAC_bind\",1,\"52:54:00:12:34:56\",\"2408:4001:f00::1\"\r\nOK\r\n",
			want: []macBindLive{},
		},
		{
			name: "记录名大小写不敏感",
			raw:  "+QMAP: \"mac_bind\",3,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{{Index: 3, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}},
		},
		{
			name: "横线分隔 MAC 归一为冒号大写",
			raw:  "+QMAP: \"MAC_bind\",1,\"52-54-00-12-34-56\",\"192.168.225.50\"\r\nOK\r\n",
			want: []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}},
		},
		{
			name: "无分隔 12 位 MAC 归一为冒号大写",
			raw:  "+QMAP: \"MAC_bind\",2,\"aabbccddee01\",\"192.168.225.51\"\r\nOK\r\n",
			want: []macBindLive{{Index: 2, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}},
		},
		{
			name: "混合大小写 MAC 归一为大写",
			raw:  "+QMAP: \"MAC_bind\",3,\"52:54:00:1a:3B:56\",\"192.168.225.52\"\r\nOK\r\n",
			want: []macBindLive{{Index: 3, MAC: "52:54:00:1A:3B:56", IP: "192.168.225.52"}},
		},
		{
			name: "混杂噪声行只取合法绑定",
			raw: strings.Join([]string{
				"AT+QMAP=\"MAC_bind\"",
				"+CME ERROR: 100",
				`+QMAP: "MAC_bind",1,"52:54:00:12:34:56","192.168.225.50"`,
				`+QMAP: "MAC_bind",2,"zz:zz:zz:zz:zz:zz","192.168.225.51"`,
				`+QMAP: "MAC_bind",9,"aa:bb:cc:dd:ee:09"`,
				`+QMAP: "DMZ",0,4`,
				"OK",
			}, "\r\n"),
			want: []macBindLive{{Index: 1, MAC: "52:54:00:12:34:56", IP: "192.168.225.50"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMacBindAT(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseMacBindAT(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeMACAddress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "小写转大写", in: "aa:bb:cc:dd:ee:01", want: "AA:BB:CC:DD:EE:01"},
		{name: "已是大写原样", in: "52:54:00:12:34:56", want: "52:54:00:12:34:56"},
		{name: "混合大小写转大写", in: "Aa:bB:cC:dD:eE:01", want: "AA:BB:CC:DD:EE:01"},
		{name: "连字符分隔归一为冒号", in: "aa-bb-cc-dd-ee-01", want: "AA:BB:CC:DD:EE:01"},
		{name: "连字符混合大小写", in: "AA-BB-CC-dd-ee-01", want: "AA:BB:CC:DD:EE:01"},
		{name: "无分隔 12 位归一为冒号", in: "aabbccddee01", want: "AA:BB:CC:DD:EE:01"},
		{name: "无分隔 12 位大写", in: "AABBCCDDEE01", want: "AA:BB:CC:DD:EE:01"},
		{name: "前后空白先去除", in: "  aa:bb:cc:dd:ee:01  ", want: "AA:BB:CC:DD:EE:01"},
		{name: "少一段", in: "aa:bb:cc:dd:ee", want: ""},
		{name: "多一段", in: "aa:bb:cc:dd:ee:01:02", want: ""},
		{name: "非十六进制字符", in: "gg:bb:cc:dd:ee:01", want: ""},
		{name: "空串", in: "", want: ""},
		{name: "含空格", in: "aa:bb:cc dd:ee:01", want: ""},
		{name: "无分隔 11 位拒绝", in: "aabbccddee0", want: ""},
		{name: "无分隔 12 位含非法字符拒绝", in: "aabbccddee0g", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMACAddress(tt.in); got != tt.want {
				t.Fatalf("normalizeMACAddress(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAllocateMacBindIndex(t *testing.T) {
	t.Run("MAC 已存在复用其槽位", func(t *testing.T) {
		existing := []macBindLive{
			{Index: 5, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
		}
		idx, err := allocateMacBindIndex(existing, "aa:bb:cc:dd:ee:01")
		if err != nil || idx != 5 {
			t.Fatalf("allocateMacBindIndex = %d, %v, want 5 无错误", idx, err)
		}
	})

	t.Run("取最小空闲号", func(t *testing.T) {
		existing := []macBindLive{
			{Index: 1, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
			{Index: 3, MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"},
		}
		idx, err := allocateMacBindIndex(existing, "AA:BB:CC:DD:EE:09")
		if err != nil || idx != 2 {
			t.Fatalf("allocateMacBindIndex = %d, %v, want 2 无错误", idx, err)
		}
	})

	t.Run("空列表取 1", func(t *testing.T) {
		idx, err := allocateMacBindIndex(nil, "AA:BB:CC:DD:EE:09")
		if err != nil || idx != 1 {
			t.Fatalf("allocateMacBindIndex = %d, %v, want 1 无错误", idx, err)
		}
	})

	t.Run("满 10 条报错", func(t *testing.T) {
		existing := make([]macBindLive, 0, 10)
		for i := 1; i <= 10; i++ {
			existing = append(existing, macBindLive{
				Index: i,
				MAC:   fmt.Sprintf("AA:BB:CC:DD:EE:%02X", i),
				IP:    "192.168.225.50",
			})
		}
		idx, err := allocateMacBindIndex(existing, "FF:FF:FF:FF:FF:01")
		if err == nil {
			t.Fatalf("槽位已满未报错,得到槽位 %d", idx)
		}
		if !strings.Contains(err.Error(), "最多 10 条") {
			t.Fatalf("错误信息未包含上限说明: %v", err)
		}
	})
}

func TestValidateDNSServerList(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{
			name: "逗号空白换行混合分隔",
			raw:  "223.5.5.5, 119.29.29.29\n1.1.1.1\t8.8.4.4",
			want: []string{"223.5.5.5", "119.29.29.29", "1.1.1.1", "8.8.4.4"},
		},
		{
			name: "重复地址按出现顺序去重",
			raw:  "1.1.1.1,223.5.5.5,1.1.1.1",
			want: []string{"1.1.1.1", "223.5.5.5"},
		},
		{
			name: "IPv6 大小写不敏感去重",
			raw:  "2001:DB8::1, 2001:db8::1",
			want: []string{"2001:DB8::1"},
		},
		{
			name: "IPv6 地址合法",
			raw:  "2001:4860:4860::8888",
			want: []string{"2001:4860:4860::8888"},
		},
		{
			name:    "空输入报错",
			raw:     "",
			wantErr: "请至少填写一个 DNS 服务器",
		},
		{
			name:    "只有分隔符报错",
			raw:     " ,\n\t,",
			wantErr: "请至少填写一个 DNS 服务器",
		},
		{
			name:    "超过 4 个报错",
			raw:     "1.1.1.1 2.2.2.2,3.3.3.3,4.4.4.4,5.5.5.5",
			wantErr: "最多",
		},
		{
			name:    "非法地址报错",
			raw:     "223.5.5.5,not.an.ip.addr",
			wantErr: "无效的 DNS 服务器地址",
		},
		{
			name:    "超范围 IPv4 报错",
			raw:     "999.1.1.1",
			wantErr: "无效的 DNS 服务器地址",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDNSServerList(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("validateDNSServerList(%q) 未报错,得到 %v", tt.raw, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("错误 %q 未包含 %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateDNSServerList(%q) 报错: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("validateDNSServerList(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestUpsertMacBindState(t *testing.T) {
	t.Run("新 MAC 追加", func(t *testing.T) {
		got := upsertMacBindState([]macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}, "AA:BB:CC:DD:EE:02", "192.168.225.52")
		want := []macBindEntry{
			{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
			{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("upsertMacBindState = %+v, want %+v", got, want)
		}
	})

	t.Run("已有 MAC 更新 IP 且大小写不敏感", func(t *testing.T) {
		got := upsertMacBindState([]macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}, "aa:bb:cc:dd:ee:01", "192.168.225.99")
		want := []macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.99"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("upsertMacBindState = %+v, want %+v", got, want)
		}
	})

	t.Run("空列表追加", func(t *testing.T) {
		got := upsertMacBindState(nil, "AA:BB:CC:DD:EE:01", "192.168.225.51")
		want := []macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("upsertMacBindState = %+v, want %+v", got, want)
		}
	})
}

func TestRemoveMacBindState(t *testing.T) {
	entries := []macBindEntry{
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
		{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"},
	}

	t.Run("按 MAC 大小写不敏感移除", func(t *testing.T) {
		got := removeMacBindState(entries, "aa:bb:cc:dd:ee:01")
		want := []macBindEntry{{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("removeMacBindState = %+v, want %+v", got, want)
		}
	})

	t.Run("MAC 不存在原样保留", func(t *testing.T) {
		got := removeMacBindState(entries, "FF:FF:FF:FF:FF:FF")
		if !reflect.DeepEqual(got, entries) {
			t.Fatalf("removeMacBindState = %+v, want %+v", got, entries)
		}
	})

	t.Run("移除最后一条得到非 nil 空列表", func(t *testing.T) {
		got := removeMacBindState([]macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}, "AA:BB:CC:DD:EE:01")
		if got == nil || len(got) != 0 {
			t.Fatalf("removeMacBindState = %+v, want 非 nil 空列表", got)
		}
	})

	t.Run("幂等删除:固件已无时仅清状态残留", func(t *testing.T) {
		got := removeMacBindState([]macBindEntry{{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}, "aa:bb:cc:dd:ee:01")
		if got == nil || len(got) != 0 {
			t.Fatalf("removeMacBindState = %+v, want 非 nil 空列表", got)
		}
	})
}

func TestMacBindQueryReady(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "收到 OK 且有绑定行",
			raw:  "+QMAP: \"MAC_bind\",1,\"52:54:00:12:34:56\",\"192.168.225.50\"\r\nOK\r\n",
			want: true,
		},
		{
			name: "仅 OK 表示无绑定也就绪",
			raw:  "AT+QMAP=\"MAC_bind\"\r\nOK\r\n",
			want: true,
		},
		{
			name: "空响应未就绪",
			raw:  "",
			want: false,
		},
		{
			name: "CME ERROR 未就绪",
			raw:  "+CME ERROR: 100",
			want: false,
		},
		{
			name: "运行器错误文本未就绪",
			raw:  "timeout waiting for response",
			want: false,
		},
		{
			name: "后台未就绪提示即使含 OK 也不可信",
			raw:  atCachePendingText + "\r\nOK\r\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := macBindQueryReady(tt.raw); got != tt.want {
				t.Fatalf("macBindQueryReady(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMacBindIPConflict(t *testing.T) {
	live := []macBindLive{
		{Index: 1, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
	}

	t.Run("其他 MAC 占用同一 IP 判冲突", func(t *testing.T) {
		if !macBindIPConflict(live, "AA:BB:CC:DD:EE:02", "192.168.225.51") {
			t.Fatal("macBindIPConflict = false, want true")
		}
	})

	t.Run("MAC 大小写不一致仍判冲突", func(t *testing.T) {
		if !macBindIPConflict(live, "aa:bb:cc:dd:ee:99", "192.168.225.51") {
			t.Fatal("macBindIPConflict = false, want true")
		}
	})

	t.Run("同一 MAC 更新自身 IP 不算冲突", func(t *testing.T) {
		if macBindIPConflict(live, "aa:bb:cc:dd:ee:01", "192.168.225.51") {
			t.Fatal("macBindIPConflict = true, want false")
		}
	})

	t.Run("不同 IP 不冲突", func(t *testing.T) {
		if macBindIPConflict(live, "AA:BB:CC:DD:EE:02", "192.168.225.52") {
			t.Fatal("macBindIPConflict = true, want false")
		}
	})

	t.Run("空列表不冲突", func(t *testing.T) {
		if macBindIPConflict(nil, "AA:BB:CC:DD:EE:02", "192.168.225.51") {
			t.Fatal("macBindIPConflict = true, want false")
		}
	})
}

func TestMissingMacBindEntries(t *testing.T) {
	entries := []macBindEntry{
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"},
		{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"},
	}

	t.Run("状态有而固件无的条目判为缺失", func(t *testing.T) {
		live := []macBindLive{{Index: 1, MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.225.51"}}
		got := missingMacBindEntries(entries, live)
		want := []macBindEntry{{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.225.52"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("missingMacBindEntries = %+v, want %+v", got, want)
		}
	})

	t.Run("MAC 大小写不一致不算缺失", func(t *testing.T) {
		live := []macBindLive{
			{Index: 1, MAC: "aa:bb:cc:dd:ee:01", IP: "192.168.225.51"},
			{Index: 2, MAC: "aa:bb:cc:dd:ee:02", IP: "192.168.225.52"},
		}
		got := missingMacBindEntries(entries, live)
		if len(got) != 0 {
			t.Fatalf("missingMacBindEntries = %+v, want 空", got)
		}
	})

	t.Run("固件无绑定时全部缺失", func(t *testing.T) {
		got := missingMacBindEntries(entries, nil)
		if !reflect.DeepEqual(got, entries) {
			t.Fatalf("missingMacBindEntries = %+v, want %+v", got, entries)
		}
	})
}

// ---------- mac_bind_del 幂等路径:状态持久化失败不得吞掉 ----------

// macBindDeleteIdempotentPOST 经真实 /api/ws 通道触发幂等删除分支并返回解析后
// 的响应体。mac 必须是固件侧查询结果中不存在的地址(查询就绪且 target==nil)。
func macBindDeleteIdempotentPOST(t *testing.T, ts *httptest.Server, client *http.Client, id, mac string) map[string]any {
	t.Helper()
	resp := e2eWebSocketCall(t, ts, client, id, "POST", "/api/network_config_data", "action=mac_bind_del&mac="+mac)
	if status := fmt.Sprint(resp["status"]); status != "200" {
		t.Fatalf("mac_bind_del status = %v, want 200 (error=%v)", resp["status"], resp["error"])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(resp["body"])), &data); err != nil {
		t.Fatalf("mac_bind_del body not json: %v", err)
	}
	return data
}

func TestMacBindDeleteIdempotentStatePersistence(t *testing.T) {
	const absentMAC = "aa:bb:cc:dd:ee:99" // 不在 mock 固件绑定表中,必走幂等分支
	old := runtimeMacBindStateFile
	t.Cleanup(func() { runtimeMacBindStateFile = old })

	t.Run("移除状态残留按成功处理", func(t *testing.T) {
		ts, client := e2eTestServer(t)
		runtimeMacBindStateFile = filepath.Join(t.TempDir(), "mac_bind.conf")
		if err := writeMacBindState([]macBindEntry{{MAC: "AA:BB:CC:DD:EE:99", IP: "192.168.225.99"}}); err != nil {
			t.Fatalf("预置状态失败: %v", err)
		}
		data := macBindDeleteIdempotentPOST(t, ts, client, "mbdel1", absentMAC)
		if data["ok"] != true {
			t.Fatalf("ok = %v, want true (error=%v)", data["ok"], data["error"])
		}
		entries, err := readMacBindState()
		if err != nil || len(entries) != 0 {
			t.Fatalf("删除后状态 = %+v, %v, want 空(残留已清)", entries, err)
		}
	})

	t.Run("状态读失败如实报错", func(t *testing.T) {
		ts, client := e2eTestServer(t)
		runtimeMacBindStateFile = filepath.Join(t.TempDir(), "mac_bind.conf")
		if err := os.WriteFile(runtimeMacBindStateFile, []byte("[broken"), 0600); err != nil {
			t.Fatalf("写入损坏状态文件失败: %v", err)
		}
		data := macBindDeleteIdempotentPOST(t, ts, client, "mbdel2", absentMAC)
		if data["ok"] != false {
			t.Fatalf("ok = %v, want false (状态读取失败不得吞掉)", data["ok"])
		}
		if got := stringValue(data["error"]); got != "状态持久化失败,请重试" {
			t.Fatalf("error = %q, want 状态持久化失败,请重试", got)
		}
	})

	t.Run("状态写失败如实报错", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("写失败注入依赖 /proc 不可创建子目录,仅 linux 有效")
		}
		ts, client := e2eTestServer(t)
		// 状态文件不存在(读取按空列表成功);父目录 /proc 无法创建子目录,
		// 原子写入的 MkdirAll 失败 → 命中纯写失败分支。
		runtimeMacBindStateFile = "/proc/simpleadmin-test/mac_bind.conf"
		data := macBindDeleteIdempotentPOST(t, ts, client, "mbdel3", absentMAC)
		if data["ok"] != false {
			t.Fatalf("ok = %v, want false (状态写入失败不得吞掉)", data["ok"])
		}
		if got := stringValue(data["error"]); got != "状态持久化失败,请重试" {
			t.Fatalf("error = %q, want 状态持久化失败,请重试", got)
		}
	})
}
