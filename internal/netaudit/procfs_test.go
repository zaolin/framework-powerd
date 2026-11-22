package netaudit

import (
	"os"
	"path/filepath"
	"testing"
)

const procNetTCPFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 01 00000000:00000000 00:00000000 00000000     0        0 99999 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCP(t *testing.T) {
	sockets, err := parseProcNet([]byte(procNetTCPFixture), "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 {
		t.Fatalf("got %d sockets, want 1", len(sockets))
	}
	got := sockets[0]
	if got.Address != "0.0.0.0" || got.Port != 22 || got.Inode != "12345" {
		t.Errorf("unexpected socket: %+v", got)
	}
}

func TestParseProcNetTCP6(t *testing.T) {
	fixture := `  sl  local_address                         rem_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4242 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000001000000:1F91 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000     0        0 4243 1 0000000000000000 100 0 0 10 0
`
	sockets, err := parseProcNet([]byte(fixture), "tcp6")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 {
		t.Fatalf("got %d sockets, want 1", len(sockets))
	}
	if sockets[0].Address != "::1" || sockets[0].Port != 8080 || sockets[0].Inode != "4242" {
		t.Errorf("unexpected IPv6 socket: %+v", sockets[0])
	}
}

func TestParseProcNetUDP(t *testing.T) {
	fixture := `   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
  10: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 53 1 0000000000000000 100 0 0 10 0
  11: 0100007F:8000 08080808:0035 01 00000000:00000000 00:00000000 00000000     0        0 54 1 0000000000000000 100 0 0 10 0
  12: 0100007F:8001 08080808:0000 07 00000000:00000000 00:00000000 00000000     0        0 55 1 0000000000000000 100 0 0 10 0
`
	sockets, err := parseProcNet([]byte(fixture), "udp")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 {
		t.Fatalf("got %d UDP sockets, want 1", len(sockets))
	}
	if sockets[0].Address != "0.0.0.0" || sockets[0].Port != 53 {
		t.Errorf("unexpected UDP socket: %+v", sockets[0])
	}
}

func TestDecodeProcIP(t *testing.T) {
	tests := []struct {
		value string
		ipv6  bool
		want  string
	}{
		{"00000000", false, "0.0.0.0"},
		{"0100007F", false, "127.0.0.1"},
		{"00000000000000000000000000000000", true, "::"},
		{"00000000000000000000000001000000", true, "::1"},
	}
	for _, tt := range tests {
		got, err := decodeProcIP(tt.value, tt.ipv6)
		if err != nil {
			t.Errorf("decodeProcIP(%q) error: %v", tt.value, err)
			continue
		}
		if got != tt.want {
			t.Errorf("decodeProcIP(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestParseCgroup(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name      string
		cgroup    string
		unit      string
		container string
		runtime   string
	}{
		{
			name:   "systemd service",
			cgroup: "0::/system.slice/framework-powerd.service\n",
			unit:   "framework-powerd.service",
		},
		{
			name:      "docker systemd scope",
			cgroup:    "0::/system.slice/docker-" + id + ".scope\n",
			container: id,
			runtime:   "docker",
		},
		{
			name:      "podman scope",
			cgroup:    "0::/machine.slice/libpod-" + id + ".scope\n",
			container: id,
			runtime:   "podman",
		},
		{
			name:      "docker cgroupfs path",
			cgroup:    "1:name=systemd:/docker/" + id + "\n",
			container: id,
			runtime:   "docker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit, container, runtime := parseCgroup(tt.cgroup)
			if unit != tt.unit || container != tt.container || runtime != tt.runtime {
				t.Errorf("parseCgroup() = (%q, %q, %q), want (%q, %q, %q)",
					unit, container, runtime, tt.unit, tt.container, tt.runtime)
			}
		})
	}
}

func TestFindProcesses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "4242")
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[12345]", filepath.Join(dir, "fd", "10")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("framework-powerd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte("0::/system.slice/framework-powerd.service\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := findProcesses(root, map[string]bool{"12345": true, "99999": true})
	meta, ok := found["12345"]
	if !ok {
		t.Fatal("socket inode was not resolved")
	}
	if meta.PID != 4242 || meta.Name != "framework-powerd" || meta.Unit != "framework-powerd.service" {
		t.Errorf("unexpected process metadata: %+v", meta)
	}
	if _, ok := found["99999"]; ok {
		t.Error("unrelated socket inode must not be returned")
	}
}

func TestParseProcNetUDP6FiltersConnected(t *testing.T) {
	fixture := `   sl  local_address                         rem_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0035 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 53 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:8000 00000000000000000000000000000000:0035 01 00000000:00000000 00:00000000 00000000     0        0 54 1 0000000000000000 100 0 0 10 0
`
	sockets, err := parseProcNet([]byte(fixture), "udp6")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 || sockets[0].Port != 53 {
		t.Fatalf("unexpected UDP6 sockets: %+v", sockets)
	}
}
