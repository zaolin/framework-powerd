package netaudit

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner executes local firewall tools and reports availability.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(name string) bool
}

type realRunner struct {
	timeout time.Duration
}

func (r realRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.timeout <= 0 {
		r.timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (realRunner) LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// collectFirewall tries backends in order and returns the first readable
// ruleset. An absent firewall tool is reported as unsupported, not as an error.
func collectFirewall(ctx context.Context, runner CommandRunner) FirewallStats {
	var lastErr error
	var emptyNFTables *FirewallStats
	for _, backend := range []string{"nft", "iptables", "firewall-cmd"} {
		if !runner.LookPath(backend) {
			continue
		}
		var stats FirewallStats
		var err error
		switch backend {
		case "nft":
			stats.Backend = "nftables"
			var output []byte
			output, err = runner.Output(ctx, "nft", "--json", "list", "ruleset")
			if err == nil {
				stats, err = parseNFTables(output)
				stats.Backend = "nftables"
			}
		case "iptables":
			stats.Backend = "iptables"
			var output []byte
			output, err = runner.Output(ctx, "iptables", "-S")
			if err == nil {
				stats, err = parseIPTables(output, "")
				stats.Backend = "iptables"
			}
			if err == nil && runner.LookPath("ip6tables") {
				output, v6err := runner.Output(ctx, "ip6tables", "-S")
				if v6err == nil {
					v6, parseErr := parseIPTables(output, "ip6")
					if parseErr == nil {
						stats.Rules += v6.Rules
						stats.Allowed = append(stats.Allowed, v6.Allowed...)
					} else {
						stats.Error = parseErr.Error()
					}
				} else {
					stats.Error = v6err.Error()
				}
			}
		case "firewall-cmd":
			stats.Backend = "firewalld"
			var output []byte
			output, err = runner.Output(ctx, "firewall-cmd", "--list-all")
			if err == nil {
				stats, err = parseFirewalld(output)
				stats.Backend = "firewalld"
			}
		}
		if err == nil {
			if stats.Allowed == nil {
				stats.Allowed = []AllowedPort{}
			}
			// An empty nftables ruleset does not prove that iptables-legacy is
			// empty too. Keep it as a fallback and check iptables before
			// declaring that the host has no firewall rules.
			if backend == "nft" && stats.Rules == 0 && runner.LookPath("iptables") {
				fallback := stats
				emptyNFTables = &fallback
				continue
			}
			return stats
		}
		lastErr = err
	}
	if lastErr != nil {
		if emptyNFTables != nil {
			emptyNFTables.Error = lastErr.Error()
			return *emptyNFTables
		}
		return FirewallStats{Backend: "error", Allowed: []AllowedPort{}, Error: lastErr.Error()}
	}
	if emptyNFTables != nil {
		return *emptyNFTables
	}
	return FirewallStats{Backend: "unsupported", Allowed: []AllowedPort{}}
}

type nftOutput struct {
	Nftables []map[string]json.RawMessage `json:"nftables"`
}

type nftRule struct {
	Family string                       `json:"family"`
	Table  string                       `json:"table"`
	Chain  string                       `json:"chain"`
	Expr   []map[string]json.RawMessage `json:"expr"`
}

// parseNFTables extracts accepting dport rules from nft's JSON ruleset.
func parseNFTables(data []byte) (FirewallStats, error) {
	var output nftOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return FirewallStats{}, err
	}
	stats := FirewallStats{Allowed: []AllowedPort{}}
	namedSets := map[string]string{}
	for _, item := range output.Nftables {
		raw, ok := item["set"]
		if !ok {
			continue
		}
		var set struct {
			Name   string          `json:"name"`
			Family string          `json:"family"`
			Table  string          `json:"table"`
			Elem   json.RawMessage `json:"elem"`
		}
		if err := json.Unmarshal(raw, &set); err != nil {
			return FirewallStats{}, err
		}
		if set.Name != "" {
			namedSets[set.Family+"/"+set.Table+"/"+set.Name] = nftPorts(set.Elem)
		}
	}
	for _, item := range output.Nftables {
		raw, ok := item["rule"]
		if !ok {
			continue
		}
		var rule nftRule
		if err := json.Unmarshal(raw, &rule); err != nil {
			return FirewallStats{}, err
		}
		stats.Rules++

		accepted := false
		negated := false
		protocol, ports := "", ""
		for _, expression := range rule.Expr {
			if _, ok := expression["accept"]; ok {
				accepted = true
			}
			match, ok := expression["match"]
			if !ok {
				continue
			}
			var m struct {
				Op   string `json:"op"`
				Left struct {
					Payload struct {
						Protocol string `json:"protocol"`
						Field    string `json:"field"`
					} `json:"payload"`
				} `json:"left"`
				Right json.RawMessage `json:"right"`
			}
			if err := json.Unmarshal(match, &m); err != nil {
				continue
			}
			if m.Left.Payload.Field != "dport" {
				continue
			}
			if m.Op != "==" {
				// Negated and relational matches accept a set of ports, not
				// the single boundary port. Do not serialize them as exact.
				negated = true
				continue
			}
			protocol = strings.ToLower(m.Left.Payload.Protocol)
			ports = nftPorts(m.Right)
			if strings.HasPrefix(ports, "@") {
				ports = namedSets[rule.Family+"/"+rule.Table+"/"+strings.TrimPrefix(ports, "@")]
			}
		}
		if accepted && !negated && protocol != "" && ports != "" {
			stats.Allowed = appendUniqueAllowed(stats.Allowed, AllowedPort{
				Protocol: protocol,
				Ports:    ports,
				Table:    rule.Table,
				Chain:    rule.Chain,
			})
		}
	}
	return stats, nil
}

func nftPorts(raw json.RawMessage) string {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			if value := nftPorts(item); value != "" {
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, ",")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		if value, ok := object["range"]; ok {
			var endpoints []json.RawMessage
			if err := json.Unmarshal(value, &endpoints); err == nil && len(endpoints) == 2 {
				return nftPorts(endpoints[0]) + "-" + nftPorts(endpoints[1])
			}
		}
		if value, ok := object["set"]; ok {
			return nftPorts(value)
		}
	}
	return ""
}

// parseIPTables parses `iptables -S` output. Only accepting rules with an
// explicit destination port become Allowed entries.
func parseIPTables(data []byte, family string) (FirewallStats, error) {
	stats := FirewallStats{Allowed: []AllowedPort{}}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "-A" {
			continue
		}
		stats.Rules++
		chain := fields[1]
		protocol, ports, verdict := "", "", ""
		negated := false
		for i := 2; i < len(fields); i++ {
			if fields[i] == "!" {
				negated = true
				continue
			}
			switch fields[i] {
			case "-p":
				if i+1 < len(fields) {
					protocol = strings.ToLower(fields[i+1])
					i++
				}
			case "--dport", "--dports":
				if i+1 < len(fields) {
					ports = fields[i+1]
					i++
				}
			case "-j":
				if i+1 < len(fields) {
					verdict = fields[i+1]
					i++
				}
			}
		}
		if verdict == "ACCEPT" && !negated && (protocol == "tcp" || protocol == "udp") && ports != "" {
			chainName := chain
			if family != "" {
				chainName = family + ":" + chain
			}
			stats.Allowed = appendUniqueAllowed(stats.Allowed, AllowedPort{
				Protocol: protocol,
				Ports:    ports,
				Chain:    chainName,
			})
		}
	}
	return stats, nil
}

func parseFirewalld(data []byte) (FirewallStats, error) {
	stats := FirewallStats{Allowed: []AllowedPort{}}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ports:") {
			continue
		}
		for _, entry := range strings.Fields(strings.TrimPrefix(line, "ports:")) {
			port, protocol, ok := strings.Cut(entry, "/")
			if !ok || port == "" || protocol == "" {
				continue
			}
			stats.Allowed = appendUniqueAllowed(stats.Allowed, AllowedPort{
				Protocol: strings.ToLower(protocol),
				Ports:    port,
				Chain:    "firewalld",
			})
		}
	}
	stats.Rules = len(stats.Allowed)
	return stats, nil
}

func appendUniqueAllowed(entries []AllowedPort, entry AllowedPort) []AllowedPort {
	for _, existing := range entries {
		if existing == entry {
			return entries
		}
	}
	return append(entries, entry)
}
