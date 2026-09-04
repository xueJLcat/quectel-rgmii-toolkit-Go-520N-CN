package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseDnsmasqLeases(t *testing.T) {
	now := time.Unix(1000000, 0)
	content := strings.Join([]string{
		"1003600 aa:bb:cc:dd:ee:01 192.168.5.9 phone 01:aa:bb:cc:dd:ee:01",
		"1007200 aa:bb:cc:dd:ee:02 192.168.5.10 * 01:aa:bb:cc:dd:ee:02",
		"999999 aa:bb:cc:dd:ee:03 192.168.5.11 expired 01:aa:bb:cc:dd:ee:03",
		"1003600 00:04:aa:bb:cc:dd:ee:ff 2408:8256:0:1::1234 00:04:aa:bb:cc:dd:ee:ff",
		"not-a-timestamp aa:bb:cc:dd:ee:04 192.168.5.12 bad x",
		"1003600 aa:bb:cc:dd:ee:05",
		"",
	}, "\n")

	got := parseDnsmasqLeases(content, now)
	if len(got) != 2 {
		t.Fatalf("parseDnsmasqLeases len = %d, want 2 (%v)", len(got), got)
	}
	first := got[0]
	if first.MAC != "AA:BB:CC:DD:EE:01" || first.IP != "192.168.5.9" || first.Hostname != "phone" || first.LeaseSeconds != 3600 {
		t.Fatalf("first lease = %+v, want MAC/IP/hostname/3600", first)
	}
	second := got[1]
	if second.Hostname != "" || second.LeaseSeconds != 7200 {
		t.Fatalf("second lease = %+v, want hostname cleared(*) and 7200s", second)
	}
}

func TestParseArpTable(t *testing.T) {
	content := strings.Join([]string{
		"IP address       HW type     Flags       HW address            Mask     Device",
		"192.168.5.9      0x1         0x2         aa:bb:cc:dd:ee:01     *        bridge0",
		"192.168.5.20     0x1         0x2         aa:11:bb:22:cc:33     *        eth0",
		"192.168.5.30     0x1         0x0         00:00:00:00:00:00     *        bridge0",
		"10.0.0.5         0x1         0x2         99:88:77:66:55:44     *        rmnet_data0",
		"192.168.5.40     0x1         0x2         00:00:00:00:00:00     *        bridge0",
		"192.168.5.50     0x1         0x2",
	}, "\n")

	got := parseArpTable(content)
	if len(got) != 2 {
		t.Fatalf("parseArpTable len = %d, want 2 (%v)", len(got), got)
	}
	if got[0].IP != "192.168.5.9" || got[0].MAC != "AA:BB:CC:DD:EE:01" || got[0].LeaseSeconds != -1 {
		t.Fatalf("arp[0] = %+v, want 192.168.5.9/AA:../-1", got[0])
	}
	if got[1].IP != "192.168.5.20" {
		t.Fatalf("arp[1] = %+v, want 192.168.5.20(eth0 保留)", got[1])
	}
}

func TestMergeLanClients(t *testing.T) {
	leases := []lanClient{
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.5.9", Hostname: "phone", LeaseSeconds: 3600},
		{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.5.10", Hostname: "", LeaseSeconds: 7200},
	}
	arps := []lanClient{
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.5.9", LeaseSeconds: -1},  // 与租约完全重复
		{MAC: "FF:FF:FF:FF:FF:01", IP: "192.168.5.10", LeaseSeconds: -1}, // IP 与租约重复
		{MAC: "FF:FF:FF:FF:FF:02", IP: "192.168.5.20", LeaseSeconds: -1}, // 静态设备
	}

	got := mergeLanClients(leases, arps, networkDetailMaxClients)
	if len(got) != 3 {
		t.Fatalf("merge len = %d, want 3 (%v)", len(got), got)
	}
	wantOrder := []string{"192.168.5.9", "192.168.5.10", "192.168.5.20"}
	for i, want := range wantOrder {
		if got[i].IP != want {
			t.Fatalf("merge[%d].IP = %s, want %s", i, got[i].IP, want)
		}
	}
	if got[0].Hostname != "phone" || got[0].LeaseSeconds != 3600 {
		t.Fatalf("merge[0] = %+v, want 租约条目优先保留", got[0])
	}
	if got[2].LeaseSeconds != -1 {
		t.Fatalf("merge[2].LeaseSeconds = %d, want -1(静态)", got[2].LeaseSeconds)
	}

	limited := mergeLanClients(leases, arps, 2)
	if len(limited) != 2 {
		t.Fatalf("merge with limit 2 len = %d, want 2", len(limited))
	}
}

func TestMergeLanClientsIPv4NumericOrder(t *testing.T) {
	leases := []lanClient{
		{MAC: "AA:BB:CC:DD:EE:02", IP: "192.168.5.10", LeaseSeconds: 100},
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.5.9", LeaseSeconds: 100},
		{MAC: "AA:BB:CC:DD:EE:03", IP: "192.168.5.100", LeaseSeconds: 100},
	}
	got := mergeLanClients(leases, nil, 0)
	wantOrder := []string{"192.168.5.9", "192.168.5.10", "192.168.5.100"}
	for i, want := range wantOrder {
		if got[i].IP != want {
			t.Fatalf("order[%d] = %s, want %s(数值排序而非字典序)", i, got[i].IP, want)
		}
	}
}

func TestDnsmasqLeasePathFromFile(t *testing.T) {
	content := strings.Join([]string{
		"# comment line",
		"interface=bridge0",
		"#dhcp-leasefile=/commented/out.leases",
		"dhcp-leasefile=/var/run/data/dnsmasq.leases",
	}, "\n")
	if got := dnsmasqLeasePathFromFile(content); got != "/var/run/data/dnsmasq.leases" {
		t.Fatalf("lease path = %q, want /var/run/data/dnsmasq.leases", got)
	}
	if got := dnsmasqLeasePathFromFile("interface=bridge0\n"); got != "" {
		t.Fatalf("lease path = %q, want empty", got)
	}
}

func TestUpdateIfaceRateSample(t *testing.T) {
	resetIfaceRateSamplesForTest()
	t.Cleanup(resetIfaceRateSamplesForTest)

	t0 := time.Now()
	rx, tx := updateIfaceRateSample("bridge0", 1000, 500, t0)
	if rx != 0 || tx != 0 {
		t.Fatalf("first sample rate = %d/%d, want 0/0", rx, tx)
	}

	// 3 秒后下行 30000 字节、上行 15000 字节 → 10000/5000 B/s
	rx, tx = updateIfaceRateSample("bridge0", 31000, 15500, t0.Add(3*time.Second))
	if rx != 10000 || tx != 5000 {
		t.Fatalf("second sample rate = %d/%d, want 10000/5000", rx, tx)
	}

	// 间隔不足最小采样间隔 → 沿用上次速率
	rx, tx = updateIfaceRateSample("bridge0", 99999, 99999, t0.Add(3*time.Second+100*time.Millisecond))
	if rx != 10000 || tx != 5000 {
		t.Fatalf("short-interval rate = %d/%d, want 沿用 10000/5000", rx, tx)
	}

	// 计数回绕(差值为负)→ 速率归零
	rx, tx = updateIfaceRateSample("bridge0", 10, 10, t0.Add(6*time.Second))
	if rx != 0 || tx != 0 {
		t.Fatalf("counter-reset rate = %d/%d, want 0/0", rx, tx)
	}
}

func TestSortInterfaceStatsPriority(t *testing.T) {
	stats := []map[string]any{
		{"name": "sit0"}, {"name": "eth0"}, {"name": "rmnet_data2"},
		{"name": "bridge0"}, {"name": "rmnet_data0"},
	}
	sortInterfaceStats(stats)
	want := []string{"rmnet_data0", "bridge0", "eth0", "rmnet_data2", "sit0"}
	for i, name := range want {
		if got, _ := stats[i]["name"].(string); got != name {
			t.Fatalf("sorted[%d] = %s, want %s", i, got, name)
		}
	}
}

func TestIsMACAddress(t *testing.T) {
	valid := []string{"aa:bb:cc:dd:ee:01", "AA:BB:CC:DD:EE:01", "00:11:22:33:44:55"}
	invalid := []string{"", "aa:bb:cc:dd:ee", "aa-bb-cc-dd-ee-01", "gg:bb:cc:dd:ee:01", "00:04:0e:f2:8c:7a:extra", "aa:bb:cc:dd:ee:01:22"}
	for _, s := range valid {
		if !isMACAddress(s) {
			t.Errorf("isMACAddress(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isMACAddress(s) {
			t.Errorf("isMACAddress(%q) = true, want false", s)
		}
	}
}
