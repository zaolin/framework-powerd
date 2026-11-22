package netaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type containerIdentity struct {
	ID      string
	Name    string
	Runtime string
}

type runtimeInventory struct {
	byID   map[string]containerIdentity
	byPort map[string]containerIdentity
}

func newRuntimeInventory() runtimeInventory {
	return runtimeInventory{
		byID:   map[string]containerIdentity{},
		byPort: map[string]containerIdentity{},
	}
}

func (r runtimeInventory) lookupID(id string) (containerIdentity, bool) {
	if identity, ok := r.byID[id]; ok {
		return identity, true
	}
	var match containerIdentity
	found := false
	for fullID, identity := range r.byID {
		if strings.HasPrefix(fullID, id) || strings.HasPrefix(id, fullID) {
			if found {
				return containerIdentity{}, false
			}
			match, found = identity, true
		}
	}
	return match, found
}

func (r runtimeInventory) lookupPort(protocol, address string, port int) (containerIdentity, bool) {
	protocol = strings.TrimSuffix(strings.ToLower(protocol), "6")
	wildcard := "0.0.0.0"
	if strings.Contains(address, ":") {
		wildcard = "::"
	}
	keys := []string{
		portKey(protocol, address, port),
		portKey(protocol, wildcard, port),
	}
	for _, key := range keys {
		if identity, ok := r.byPort[key]; ok {
			return identity, true
		}
	}
	return containerIdentity{}, false
}

func portKey(protocol, address string, port int) string {
	return fmt.Sprintf("%s/%s/%d", protocol, address, port)
}

type runtimeEndpoint struct {
	runtime string
	socket  string
	baseURL string
	path    string
}

var runtimeEndpoints = []runtimeEndpoint{
	// Unversioned Docker requests use the daemon's current API version; a pinned
	// old version is rejected by recent Docker Engine releases.
	{runtime: "docker", socket: "/run/docker.sock", baseURL: "http://docker", path: "/containers/json?all=1"},
	{runtime: "podman", socket: "/run/podman/podman.sock", baseURL: "http://podman", path: "/v4.0.0/libpod/containers/json"},
}

// discoverRuntimes reads local Docker/Podman metadata over their Unix sockets.
// It performs no network calls and tolerates either runtime being absent.
func discoverRuntimes(ctx context.Context, stat func(string) error) (runtimeInventory, error) {
	inventory := newRuntimeInventory()
	var runtimeErrors []error
	if stat == nil {
		stat = func(path string) error {
			_, err := os.Stat(path)
			return err
		}
	}
	for _, endpoint := range runtimeEndpoints {
		if err := stat(endpoint.socket); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			runtimeErrors = append(runtimeErrors, fmt.Errorf("%s socket: %w", endpoint.runtime, err))
			continue
		}
		client := unixHTTPClient(endpoint.socket)
		if err := queryRuntime(ctx, client, endpoint, inventory); err != nil {
			runtimeErrors = append(runtimeErrors, fmt.Errorf("%s: %w", endpoint.runtime, err))
		}
	}
	return inventory, errors.Join(runtimeErrors...)
}

func unixHTTPClient(socket string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Second}
}

type runtimeContainer struct {
	ID    string        `json:"Id"`
	IDAlt string        `json:"ID"`
	Names []string      `json:"Names"`
	Name  string        `json:"Name"`
	Ports []runtimePort `json:"Ports"`
}

type runtimePort struct {
	// Docker uses IP/PublicPort/Type; libpod uses host_ip/host_port/protocol.
	IP            string `json:"IP"`
	HostIP        string `json:"host_ip"`
	PublicPort    int    `json:"PublicPort"`
	HostPort      int    `json:"host_port"`
	PrivatePort   int    `json:"PrivatePort"`
	ContainerPort int    `json:"container_port"`
	Type          string `json:"Type"`
	Protocol      string `json:"protocol"`
}

func queryRuntime(ctx context.Context, client *http.Client, endpoint runtimeEndpoint, inventory runtimeInventory) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.baseURL+endpoint.path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", endpoint.runtime, resp.Status)
	}

	var containers []runtimeContainer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&containers); err != nil {
		return err
	}
	for _, container := range containers {
		id := container.ID
		if id == "" {
			id = container.IDAlt
		}
		if id == "" {
			continue
		}
		name := container.Name
		if name == "" && len(container.Names) > 0 {
			name = container.Names[0]
		}
		identity := containerIdentity{
			ID:      id,
			Name:    strings.TrimPrefix(name, "/"),
			Runtime: endpoint.runtime,
		}
		inventory.byID[id] = identity
		if len(id) >= 12 {
			inventory.byID[id[:12]] = identity
		}
		for _, port := range container.Ports {
			protocol := port.Type
			if protocol == "" {
				protocol = port.Protocol
			}
			protocol = strings.ToLower(protocol)
			if protocol == "" {
				protocol = "tcp"
			}
			address := port.IP
			if address == "" {
				address = port.HostIP
			}
			publicPort := port.PublicPort
			if publicPort == 0 {
				publicPort = port.HostPort
			}
			if publicPort <= 0 {
				continue
			}
			if address == "" || address == "*" {
				address = "0.0.0.0"
			}
			// Keep 0.0.0.0 and :: distinct. A wildcard publication must never
			// attribute a socket from the other address family to a container.
			inventory.byPort[portKey(protocol, address, publicPort)] = identity
		}
	}
	return nil
}
