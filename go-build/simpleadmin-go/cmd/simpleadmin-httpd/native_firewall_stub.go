//go:build !linux
// +build !linux

package main

import "errors"

func applyFirewallRules(rules, oldRules []firewallRule) error {
	return errors.New("firewall management is only supported on Linux")
}

func runFirewallCommandOutput(command string, args []string) (string, error) {
	return "", errors.New("firewall management is only supported on Linux")
}

func firewallDumpChains() ([]firewallChainInfo, error) {
	return nil, errors.New("firewall chain dump is only supported on Linux")
}

func applySavedFirewallAtStartup() {}
