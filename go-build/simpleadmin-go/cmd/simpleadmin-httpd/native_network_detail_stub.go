//go:build !linux
// +build !linux

package main

// collectInterfaceStats 非 Linux 平台(本地/Windows 测试构建)返回固定 mock 接口
// 数据,保证网络详情页在测试环境可渲染;字段与 Linux 实现一致。
func collectInterfaceStats() []map[string]any {
	return []map[string]any{
		{"name": "rmnet_data0", "mac": "aa:bb:cc:00:01:01", "state": "up", "mtu": 1500,
			"rxBytes": int64(1234567890), "txBytes": int64(234567890), "rxRate": int64(102400), "txRate": int64(20480)},
		{"name": "bridge0", "mac": "aa:bb:cc:00:01:02", "state": "up", "mtu": 1500,
			"rxBytes": int64(98765432), "txBytes": int64(87654321), "rxRate": int64(5120), "txRate": int64(2048)},
		{"name": "eth0", "mac": "aa:bb:cc:00:01:03", "state": "up", "mtu": 1500,
			"rxBytes": int64(98765000), "txBytes": int64(87654000), "rxRate": int64(5000), "txRate": int64(2000)},
	}
}

// collectLanClients 非 Linux 平台返回固定 mock 设备列表(含一条无租约的静态设备)。
func collectLanClients() []map[string]any {
	return []map[string]any{
		{"mac": "11:22:33:44:55:66", "ip": "192.168.5.9", "hostname": "phone", "leaseSeconds": int64(3600)},
		{"mac": "66:55:44:33:22:11", "ip": "192.168.5.10", "hostname": "", "leaseSeconds": int64(7200)},
		{"mac": "aa:11:bb:22:cc:33", "ip": "192.168.5.20", "hostname": "static-nas", "leaseSeconds": int64(-1)},
	}
}
