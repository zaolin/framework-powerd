package ollama

import (
	"testing"

	"github.com/zaolin/framework-powerd/internal/config"
)

// TestMatchGroup_CIDRMatch verifies T3: MatchGroup correctly matches an IP
// within a CIDR range.
func TestMatchGroup_CIDRMatch(t *testing.T) {
	groups := []config.IPGroup{
		{Name: "lan", CIDRs: []string{"192.168.0.0/16"}},
		{Name: "tailscale", CIDRs: []string{"100.64.0.0/10"}},
	}

	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.100", "lan"},
		{"192.168.0.1", "lan"},
		{"100.64.0.1", "tailscale"},
		{"100.127.255.255", "tailscale"},
		{"10.0.0.1", ""}, // no match
		{"172.16.0.1", ""}, // no match
	}

	for _, tc := range tests {
		got := MatchGroup(tc.ip, groups)
		if got != tc.want {
			t.Errorf("MatchGroup(%q) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

// TestMatchGroup_SingleIPFallback verifies the single-IP fallback when a CIDR
// string is actually a plain IP.
func TestMatchGroup_SingleIPFallback(t *testing.T) {
	groups := []config.IPGroup{
		{Name: "specific", CIDRs: []string{"10.0.0.5"}},
	}

	got := MatchGroup("10.0.0.5", groups)
	if got != "specific" {
		t.Errorf("MatchGroup(\"10.0.0.5\") = %q, want %q", got, "specific")
	}

	// Non-matching IP should return ""
	got = MatchGroup("10.0.0.6", groups)
	if got != "" {
		t.Errorf("MatchGroup(\"10.0.0.6\") = %q, want %q", got, "")
	}
}

// TestMatchGroup_InvalidIP verifies MatchGroup returns "" for malformed IPs.
func TestMatchGroup_InvalidIP(t *testing.T) {
	groups := []config.IPGroup{
		{Name: "lan", CIDRs: []string{"192.168.0.0/16"}},
	}

	got := MatchGroup("not-an-ip", groups)
	if got != "" {
		t.Errorf("MatchGroup(\"not-an-ip\") = %q, want %q", got, "")
	}
}

// TestMatchGroup_EmptyGroups verifies MatchGroup returns "" when no groups
// are configured.
func TestMatchGroup_EmptyGroups(t *testing.T) {
	got := MatchGroup("192.168.1.1", nil)
	if got != "" {
		t.Errorf("MatchGroup with nil groups = %q, want %q", got, "")
	}
}

// TestMatchGroup_OverlappingCIDRs verifies the first match wins when CIDRs
// overlap.
func TestMatchGroup_OverlappingCIDRs(t *testing.T) {
	groups := []config.IPGroup{
		{Name: "narrow", CIDRs: []string{"192.168.1.0/24"}},
		{Name: "wide", CIDRs: []string{"192.168.0.0/16"}},
	}

	got := MatchGroup("192.168.1.50", groups)
	if got != "narrow" {
		t.Errorf("MatchGroup with overlapping CIDRs = %q, want %q (first match)", got, "narrow")
	}
}