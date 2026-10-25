// Package llm accounts LLM API requests by client IP using a reverse proxy
// access log (Caddy JSON format) in front of llama-server.
package llm

import (
	"net"

	"github.com/zaolin/framework-powerd/internal/config"
)

// MatchGroup returns the group name for an IP, or an empty string when the IP
// does not match any configured group. The first matching group wins.
func MatchGroup(ip string, groups []config.IPGroup) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}

	for _, group := range groups {
		for _, cidrStr := range group.CIDRs {
			_, cidr, err := net.ParseCIDR(cidrStr)
			if err != nil {
				// Not a CIDR: allow a plain single IP address.
				if single := net.ParseIP(cidrStr); single != nil && parsed.Equal(single) {
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

// IsExcluded reports whether an IP is excluded from accounting, either
// because it is listed explicitly or because it is a loopback address and
// local traffic is ignored.
func IsExcluded(ip string, excludeIPs []string, ignoreLocal bool) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}

	if ignoreLocal && parsed.IsLoopback() {
		return true
	}

	for _, excluded := range excludeIPs {
		if parsed.Equal(net.ParseIP(excluded)) {
			return true
		}
	}
	return false
}
