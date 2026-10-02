package main

import (
	"strings"
	"time"
)

const (
	networkServiceDHCPWait = 20 * time.Second
)

// parseWWANProfiles extracts profile names from `netsh mbn show profiles`.
// The heading is localized; names are the indented lines below the rule.
func parseWWANProfiles(output string) []string {
	var profiles []string
	afterRule := false
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "---") {
			afterRule = true
			continue
		}
		if !afterRule || trimmed == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			profiles = append(profiles, trimmed)
		}
	}
	return profiles
}
