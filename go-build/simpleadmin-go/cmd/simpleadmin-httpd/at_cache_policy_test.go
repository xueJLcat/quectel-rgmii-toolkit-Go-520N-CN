package main

import (
	"testing"
	"time"
)

func TestATCacheTierClassification(t *testing.T) {
	checks := []struct {
		name    string
		command string
		want    time.Duration
	}{
		{"action reboot", "AT+CFUN=1,1", 0},
		{"action delete sms", "AT+CMGD=1", 0},
		{"device static composite", "AT+CGMI;+CGSN;+QGMR;+CIMI;+ICCID;+CNUM", atCacheStaticMaxAge},
		{"standalone imsi", "AT+CIMI", atCacheStaticMaxAge},
		{"standalone imei", "AT+CGSN", atCacheStaticMaxAge},
		{"settings status composite", `AT+QMAP="MPDN_RULE";+QMAP="DHCPV6DNS";+QCFG="usbnet";+QMAP="DMZ";+QMAP="DHCPV4DNS"`, atCacheConfigMaxAge},
		{"standalone lanip", `AT+QMAP="LANIP"`, atCacheConfigMaxAge},
		{"band preference composite", `AT+QNWPREFCFG="lte_band";+QNWPREFCFG= "nsa_nr5g_band";+QNWPREFCFG= "nr5g_band"`, atCacheConfigMaxAge},
		{"network preference composite", `AT+QUIMSLOT?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+CGDCONT?;+CGCONTRDP=1;+QNWLOCK="common/4g";+QNWLOCK="common/5g"`, atCacheSemiStaticMaxAge},
		{"sms list", `AT+CSMS=1;+CSDH=0;+CNMI=2,1,0,0,0;+CMGF=0;+CPMS="ME","ME","ME";+CMGL=4`, 5 * time.Second},
		{"dashboard composite", `AT+QSIMSTAT?;+CSQ;+QTEMP;+QUIMSLOT?;+QSPN;+QMAP="WWAN";+QENG="servingcell";+QCAINFO;+QGDNRCNT?;+QGDCNT?;+CGCONTRDP=1;+QRSRP`, atCacheReadMaxAge},
		{"standalone qeng", `AT+QENG="servingcell"`, atCacheReadMaxAge},
	}
	for _, check := range checks {
		if got := maxAgeForATCacheCommand(check.command); got != check.want {
			t.Fatalf("%s: maxAge = %v, want %v", check.name, got, check.want)
		}
	}
}

func TestIsDeviceIdentityQueryCommand(t *testing.T) {
	positive := []string{"AT+CIMI", "AT+ICCID", "AT+CNUM", "AT+CGMI", "AT+CGSN", "AT+QGMR"}
	for _, command := range positive {
		if !isDeviceIdentityQueryCommand(command) {
			t.Fatalf("isDeviceIdentityQueryCommand(%q) = false, want true", command)
		}
	}
	negative := []string{"AT+CSQ", `AT+QENG="servingcell"`, "AT+CFUN=1,1"}
	for _, command := range negative {
		if isDeviceIdentityQueryCommand(command) {
			t.Fatalf("isDeviceIdentityQueryCommand(%q) = true, want false", command)
		}
	}
}

func TestATTimeoutPolicy(t *testing.T) {
	if got := atSingleCommandTimeoutMS("AT+QSCAN=3,1"); got != 180000 {
		t.Fatalf("QSCAN timeout = %d, want 180000", got)
	}
	if got := atSingleCommandTimeoutMS(`AT+QMAP="MPDN_RULE",0`); got != 10000 {
		t.Fatalf("MPDN_RULE query timeout = %d, want 10000", got)
	}
	if got := atSingleCommandTimeoutMS(`AT+QMBNCFG="List"`); got != 8000 {
		t.Fatalf("QMBNCFG timeout = %d, want 8000", got)
	}
	if got := atSingleCommandTimeoutMS(`AT+QMBNCFG="select","mbn_file.mbn"`); got != 8000 {
		t.Fatalf("QMBNCFG select timeout = %d, want 8000", got)
	}
	if got := atSingleCommandTimeoutMS("ATI"); got != 1000 {
		t.Fatalf("default timeout = %d, want 1000", got)
	}
	// 组合命令超时为子命令之和。
	if got := atGroupedCommandTimeoutMS("AT+CSQ;+QTEMP"); got != 2000 {
		t.Fatalf("grouped timeout = %d, want 2000", got)
	}
	if got := atGroupedCommandTimeoutMS(`AT+CSQ;+QMBNCFG="List"`); got != 9000 {
		t.Fatalf("grouped timeout with QMBNCFG = %d, want 9000", got)
	}
}
