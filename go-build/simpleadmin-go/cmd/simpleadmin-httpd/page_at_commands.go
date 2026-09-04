package main

import (
	"strings"
	"time"
)

const (
	atKeyDashboard           = "dashboard"
	atKeyDeviceInfo          = "device_info"
	atKeyNetworkBands        = "network_bands"
	atKeyNetworkSettings     = "network_settings"
	atKeyNetworkConfigStatus = "network_config_status"
	atKeySystemStatus        = "system_status"
	atKeySMSList             = "sms_list"
	atKeySMSListSM           = "sms_list_sm"
	atKeySignalDetail        = "signal_detail"
	atKeySIMStatus           = "sim_status"
	atKeyIMSI                = "imsi"
)

func pageATCommands(key string) []string {
	switch key {
	case atKeyDashboard:
		return []string{`AT+QSIMSTAT?;+CSQ;+QTEMP;+QUIMSLOT?;+QSPN;+QMAP="WWAN";+QENG="servingcell";+QCAINFO;+QGDNRCNT?;+QGDCNT?;+CGCONTRDP=1;+QRSRP`}
	case atKeyDeviceInfo:
		return []string{
			`AT+CGMI;+CGSN;+QGMR;+CIMI;+ICCID;+CNUM`,
			`AT+QSIMSTAT?;+CPIN?;+QMAP="WWAN"`,
			`AT+QMAP="LANIP"`,
		}
	case atKeyNetworkBands:
		return []string{`AT+QNWPREFCFG="lte_band";+QNWPREFCFG= "nsa_nr5g_band";+QNWPREFCFG= "nr5g_band"`}
	case atKeyNetworkSettings:
		return []string{
			`AT+QUIMSLOT?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+CGDCONT?;+CGCONTRDP=1;+QNWLOCK="common/4g";+QNWLOCK="common/5g"`,
			`AT+QCAINFO`,
		}
	case atKeyNetworkConfigStatus:
		// 命令字符串与原 settings_status 保持一致,缓存分级/超时策略按字符串匹配,不得改动。
		return []string{
			`AT+QMAP="MPDN_RULE";+QMAP="DHCPV6DNS";+QCFG="usbnet";+QMAP="DMZ";+QMAP="DHCPV4DNS"`,
			`AT+QMAP="LANIP"`,
		}
	case atKeySystemStatus:
		return []string{`AT+CGSN`}
	case atKeySIMStatus:
		return []string{`AT+CPIN?`}
	case atKeySMSList:
		return []string{smsListATCommand()}
	case atKeySMSListSM:
		return []string{smsListSMATCommand()}
	case atKeySignalDetail:
		return []string{signalDetailATCommand()}
	case atKeyIMSI:
		return []string{`AT+CIMI`}
	default:
		return nil
	}
}

func pageATCommand(key string) string {
	return strings.Join(pageATCommands(key), "\n")
}

func (s *simpleAdminServer) fetchPageAT(key string, force bool, wait bool) string {
	commands := pageATCommands(key)
	if len(commands) == 0 {
		return ""
	}
	responses := make([]string, 0, len(commands))
	for _, command := range commands {
		resp := s.fetchATCommand(command, force, wait)
		if strings.TrimSpace(resp) != "" {
			responses = append(responses, resp)
		}
	}
	return strings.Join(responses, "\n")
}

func (s *simpleAdminServer) fetchATCommand(command string, force bool, wait bool) string {
	if command == "" {
		return ""
	}
	atCommandCache.Start(s.cfg.mockMode)
	return atCommandCache.Fetch(command, force, &wait)
}

func (s *simpleAdminServer) runPageAction(command string) string {
	atCommandCache.Start(s.cfg.mockMode)
	wait := true
	return atCommandCache.Fetch(command, true, &wait)
}

func (s *simpleAdminServer) runPageActionsOK(commands []string, delay time.Duration) (string, bool) {
	responses := make([]string, 0, len(commands))
	for i, command := range commands {
		resp := s.runPageAction(command)
		responses = append(responses, resp)
		if !atResponseOK(resp) {
			return strings.Join(responses, "\n"), false
		}
		if delay > 0 && i < len(commands)-1 {
			time.Sleep(delay)
		}
	}
	return strings.Join(responses, "\n"), true
}

func (s *simpleAdminServer) runDelayedPageAction(command string, delay time.Duration) {
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		s.runPageAction(command)
	}()
}

func (s *simpleAdminServer) runDelayedPageActionsOK(commands []string, startDelay time.Duration, stepDelay time.Duration) {
	go func() {
		if startDelay > 0 {
			time.Sleep(startDelay)
		}
		s.runPageActionsOK(commands, stepDelay)
	}()
}

func settingsRebootNoticeResponse(response string, rebootAfter time.Duration) map[string]any {
	rebootAfterSeconds := int(rebootAfter / time.Second)
	return map[string]any{
		"ok":                     true,
		"response":               response,
		"reboot":                 true,
		"rebooting":              true,
		"rebootAfterSeconds":     rebootAfterSeconds,
		"rebootCountdownSeconds": 40,
		"message":                "设备即将重启，请等待前端倒计时。",
	}
}
