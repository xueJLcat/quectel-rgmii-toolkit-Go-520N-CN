//go:build !linux
// +build !linux

package main

import "errors"

// 本文件为 dnsmasq 原生层在非 Linux 环境(如 Windows 本地测试版)的占位实现,
// 仅保证编译通过;设备上不会运行这些路径。

func reloadDnsmasqHosts() error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func restartDnsmasqUnit() error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func testDnsmasqConfig() error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func ensureDNSIncludeFile(enabled bool, servers []string) error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func ensureDnsmasqMarker() error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func applyDNSUpstream(enabled bool, servers []string) error {
	return errors.New("dnsmasq 管理仅支持 Linux 设备环境")
}

func reconcileDNSUpstreamAtStartup() {}
