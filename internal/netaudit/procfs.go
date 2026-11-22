package netaudit

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type socketRef struct {
	Protocol string
	Address  string
	Port     int
	Inode    string
}

// parseProcNet parses one /proc/net/{tcp,tcp6,udp,udp6} file. Only listening
// TCP sockets and unconnected UDP sockets are relevant for a port inventory.
func parseProcNet(data []byte, protocol string) ([]socketRef, error) {
	var sockets []socketRef
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for lineNo := 0; scanner.Scan(); lineNo++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || lineNo == 0 {
			continue
		}

		// Fields: sl, local_address, rem_address, st, ..., inode at index 9.
		local, remote, state, inode := fields[1], fields[2], fields[3], fields[9]
		baseProtocol := strings.TrimSuffix(protocol, "6")
		if baseProtocol == "tcp" && state != "0A" {
			continue
		}
		if baseProtocol == "udp" && !zeroRemote(remote) {
			continue
		}

		localIP, localPort, err := splitProcAddress(local, strings.HasSuffix(protocol, "6"))
		if err != nil || localPort == 0 || inode == "0" {
			continue
		}
		sockets = append(sockets, socketRef{
			Protocol: protocol,
			Address:  localIP,
			Port:     localPort,
			Inode:    inode,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sockets, nil
}

func zeroRemote(address string) bool {
	ip, port, err := splitProcAddress(address, strings.Count(address, ":") == 1 && len(address) > 36)
	if err != nil || port != 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsUnspecified()
}

func splitProcAddress(value string, ipv6 bool) (string, int, error) {
	ipHex, portHex, ok := strings.Cut(value, ":")
	if !ok {
		return "", 0, fmt.Errorf("invalid proc address %q", value)
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, err
	}
	ip, err := decodeProcIP(ipHex, ipv6)
	return ip, int(port), err
}

// decodeProcIP decodes the kernel's hexadecimal representation. IPv4 is one
// little-endian word; IPv6 is four little-endian words.
func decodeProcIP(value string, ipv6 bool) (string, error) {
	bytes, err := hex.DecodeString(value)
	if err != nil {
		return "", err
	}
	if ipv6 {
		if len(bytes) != 16 {
			return "", fmt.Errorf("invalid IPv6 address length %d", len(bytes))
		}
		for i := 0; i < len(bytes); i += 4 {
			for j := 0; j < 2; j++ {
				bytes[i+j], bytes[i+3-j] = bytes[i+3-j], bytes[i+j]
			}
		}
	} else {
		if len(bytes) != 4 {
			return "", fmt.Errorf("invalid IPv4 address length %d", len(bytes))
		}
		for i, j := 0, len(bytes)-1; i < j; i, j = i+1, j-1 {
			bytes[i], bytes[j] = bytes[j], bytes[i]
		}
	}
	return net.IP(bytes).String(), nil
}

type processMeta struct {
	PID         int
	Name        string
	Unit        string
	ContainerID string
	Runtime     string
}

// findProcesses resolves socket inodes to process metadata without touching a
// live system beyond the supplied procfs root.
func findProcesses(procRoot string, wanted map[string]bool) map[string]processMeta {
	found := map[string]processMeta{}
	if len(wanted) == 0 {
		return found
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return found
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)

	for _, pid := range pids {
		fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		var meta processMeta
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if !wanted[inode] {
				continue
			}
			if meta.PID == 0 {
				meta = readProcessMeta(procRoot, pid)
			}
			if _, exists := found[inode]; !exists {
				found[inode] = meta
			}
		}
	}
	return found
}

func readProcessMeta(procRoot string, pid int) processMeta {
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	meta := processMeta{PID: pid}
	if data, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
		meta.Name = strings.TrimSpace(string(data))
	}
	if data, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
		meta.Unit, meta.ContainerID, meta.Runtime = parseCgroup(string(data))
	}
	return meta
}

// parseCgroup extracts the systemd service and a Docker/Podman container ID
// from any cgroup v1 or v2 path in the file.
func parseCgroup(data string) (unit, containerID, runtime string) {
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		components := strings.Split(path, "/")
		for i, component := range components {
			if strings.HasSuffix(component, ".service") {
				unit = component
			}
			candidate, candidateRuntime := "", ""
			switch {
			case strings.HasPrefix(component, "docker-"):
				candidate, candidateRuntime = component, "docker"
			case strings.HasPrefix(component, "libpod-"):
				candidate, candidateRuntime = component, "podman"
			case (component == "docker" || component == "libpod") && i+1 < len(components):
				candidate = components[i+1]
				if component == "docker" {
					candidateRuntime = "docker"
				} else {
					candidateRuntime = "podman"
				}
			}
			candidate = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(candidate, "docker-"), "libpod-"), ".scope")
			if isContainerID(candidate) {
				containerID, runtime = candidate, candidateRuntime
			}
		}
	}
	return unit, containerID, runtime
}

func isContainerID(value string) bool {
	if len(value) < 12 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
