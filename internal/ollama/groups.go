package ollama

import (
	"net"

	"github.com/zaolin/framework-powerd/internal/config"
)

// MatchGroup returns the group name for an IP, or empty string if no match
func MatchGroup(ip string, groups []config.IPGroup) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}

	for _, group := range groups {
		for _, cidrStr := range group.CIDRs {
			_, cidr, err := net.ParseCIDR(cidrStr)
			if err != nil {
				// Try single IP match
				if net.ParseIP(cidrStr) != nil && ip == cidrStr {
					return group.Name
				}
				continue
			}
			if cidr.Contains(parsed) {
				return group.Name
			}
		}
	}
	return ""
}
