package netaudit

import (
	"context"
	"strings"
	"testing"
)

type fakeFirewallRunner struct {
	available map[string]bool
	outputs   map[string]string
	err       error
	errors    map[string]error
	calls     []string
}

func (f *fakeFirewallRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err, ok := f.errors[key]; ok {
		return []byte(f.outputs[key]), err
	}
	return []byte(f.outputs[key]), f.err
}

func (f *fakeFirewallRunner) LookPath(name string) bool {
	return f.available[name]
}

func TestParseNFTables(t *testing.T) {
	data := []byte(`{
		"nftables": [
			{"table": {"family": "ip", "name": "filter"}},
			{"chain": {"family": "ip", "table": "filter", "name": "input", "hook": "input", "policy": "drop"}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 22}},
				{"accept": null}
			]}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 23}},
				{"drop": null}
			]}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "==", "left": {"payload": {"protocol": "udp", "field": "dport"}}, "right": {"range": [53, 54]}}},
				{"accept": null}
			]}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": {"set": [80, 443]}}},
				{"accept": null}
			]}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "!=", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 22}},
				{"accept": null}
			]}},
			{"rule": {"table": "filter", "chain": "input", "expr": [
				{"match": {"op": "<", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 1024}},
				{"accept": null}
			]}}
		]
	}`)

	stats, err := parseNFTables(data)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rules != 6 {
		t.Errorf("rules = %d, want 6", stats.Rules)
	}
	if len(stats.Allowed) != 3 {
		t.Fatalf("allowed = %v, want three accepting rules", stats.Allowed)
	}
	if stats.Allowed[0].Protocol != "tcp" || stats.Allowed[0].Ports != "22" || stats.Allowed[0].Chain != "input" {
		t.Errorf("unexpected first allowed rule: %+v", stats.Allowed[0])
	}
	if stats.Allowed[1].Ports != "53-54" {
		t.Errorf("range = %q, want 53-54", stats.Allowed[1].Ports)
	}
	if stats.Allowed[2].Ports != "80,443" {
		t.Errorf("list = %q, want 80,443", stats.Allowed[2].Ports)
	}
}

func TestParseIPTables(t *testing.T) {
	data := []byte(`-P INPUT DROP
-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT
-A INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT
-A INPUT -p tcp -m tcp --dport 23 -j REJECT
-A FORWARD -p udp -m udp --dport 53 -j DROP
-A INPUT -p tcp ! --dport 22 -j ACCEPT
`)

	stats, err := parseIPTables(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rules != 5 {
		t.Errorf("rules = %d, want 5", stats.Rules)
	}
	if len(stats.Allowed) != 2 {
		t.Fatalf("allowed = %v, want 2", stats.Allowed)
	}
	if stats.Allowed[0].Ports != "22" || stats.Allowed[1].Ports != "80,443" {
		t.Errorf("unexpected allowed ports: %+v", stats.Allowed)
	}
}

func TestParseFirewalld(t *testing.T) {
	data := []byte(`public (active)
  target: default
  ports: 80/tcp 443/tcp 8000-8100/udp
  services: ssh
`)
	stats, err := parseFirewalld(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Allowed) != 3 || stats.Rules != 3 {
		t.Fatalf("unexpected firewalld stats: %+v", stats)
	}
	if stats.Allowed[2].Ports != "8000-8100" || stats.Allowed[2].Protocol != "udp" {
		t.Errorf("unexpected range: %+v", stats.Allowed[2])
	}
}

func TestCollectFirewallUnsupported(t *testing.T) {
	stats := collectFirewall(context.Background(), &fakeFirewallRunner{available: map[string]bool{}})
	if stats.Backend != "unsupported" {
		t.Errorf("backend = %q, want unsupported", stats.Backend)
	}
	if stats.Error != "" {
		t.Errorf("error = %q, want empty", stats.Error)
	}
}

func TestCollectFirewallNFTables(t *testing.T) {
	runner := &fakeFirewallRunner{
		available: map[string]bool{"nft": true},
		outputs: map[string]string{
			"nft --json list ruleset": `{"nftables":[{"rule":{"table":"filter","chain":"input","expr":[{"accept":null}]}}]}`,
		},
	}
	stats := collectFirewall(context.Background(), runner)
	if stats.Backend != "nftables" || stats.Rules != 1 || stats.Error != "" {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestCollectFirewallFallsBackToIPTables(t *testing.T) {
	runner := &fakeFirewallRunner{
		available: map[string]bool{"nft": true, "iptables": true},
		err:       nil,
		outputs: map[string]string{
			"nft --json list ruleset": "not json",
			"iptables -S":             "-A INPUT -p tcp --dport 22 -j ACCEPT",
		},
	}
	stats := collectFirewall(context.Background(), runner)
	if stats.Backend != "iptables" || len(stats.Allowed) != 1 {
		t.Errorf("unexpected fallback stats: %+v", stats)
	}
}

func TestCollectFirewallReportsIP6Error(t *testing.T) {
	runner := &fakeFirewallRunner{
		available: map[string]bool{"iptables": true, "ip6tables": true},
		outputs: map[string]string{
			"iptables -S":  "-A INPUT -p tcp --dport 22 -j ACCEPT",
			"ip6tables -S": "permission denied",
		},
		errors: map[string]error{
			"ip6tables -S": context.DeadlineExceeded,
		},
	}
	stats := collectFirewall(context.Background(), runner)
	if stats.Error == "" {
		t.Error("an ip6tables failure must be reported")
	}
	if len(stats.Allowed) != 1 || stats.Allowed[0].Ports != "22" {
		t.Errorf("IPv4 rules should be retained: %+v", stats.Allowed)
	}
}

func TestCollectFirewallEmptyNFTablesChecksIPTables(t *testing.T) {
	runner := &fakeFirewallRunner{
		available: map[string]bool{"nft": true, "iptables": true},
		outputs: map[string]string{
			"nft --json list ruleset": `{"nftables":[]}`,
			"iptables -S":             "-A INPUT -p tcp --dport 22 -j ACCEPT",
		},
	}
	stats := collectFirewall(context.Background(), runner)
	if stats.Backend != "iptables" || len(stats.Allowed) != 1 {
		t.Errorf("an empty nftables ruleset must not mask iptables: %+v", stats)
	}
	if len(runner.calls) != 2 {
		t.Errorf("calls = %v, want both backends", runner.calls)
	}
}

func TestParseNFTablesNamedSet(t *testing.T) {
	data := []byte(`{"nftables":[
		{"set":{"table":"filter","name":"web_ports","elem":[80,443]}},
		{"rule":{"table":"filter","chain":"input","expr":[
			{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":"@web_ports"}},
			{"accept":null}
		]}},
		{"rule":{"table":"filter","chain":"input","expr":[
			{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":"@missing"}},
			{"accept":null}
		]}}
	]}`)
	stats, err := parseNFTables(data)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rules != 2 {
		t.Errorf("rules = %d, want 2", stats.Rules)
	}
	if len(stats.Allowed) != 1 || stats.Allowed[0].Ports != "80,443" {
		t.Fatalf("named set must resolve to its elements: %+v", stats.Allowed)
	}
}

func TestParseNFTablesNamedSetFamilyScope(t *testing.T) {
	data := []byte(`{"nftables":[
		{"set":{"family":"ip","table":"filter","name":"web_ports","elem":[80,443]}},
		{"set":{"family":"ip6","table":"filter","name":"web_ports","elem":[8080]}},
		{"rule":{"family":"ip","table":"filter","chain":"input","expr":[
			{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":"@web_ports"}},
			{"accept":null}
		]}},
		{"rule":{"family":"ip6","table":"filter","chain":"input","expr":[
			{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":"@web_ports"}},
			{"accept":null}
		]}}
	]}`)
	stats, err := parseNFTables(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Allowed) != 2 {
		t.Fatalf("allowed = %v, want one rule per family", stats.Allowed)
	}
	if stats.Allowed[0].Ports != "80,443" || stats.Allowed[1].Ports != "8080" {
		t.Errorf("families were mixed: %+v", stats.Allowed)
	}
}
