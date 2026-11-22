package netaudit

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestQueryRuntime(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"Id": "` + id + `",
				"Names": ["/llama"],
				"Ports": [
					{"IP": "127.0.0.1", "PrivatePort": 18080, "PublicPort": 18081, "Type": "tcp"},
					{"IP": "0.0.0.0", "PrivatePort": 9090, "PublicPort": 9090, "Type": "tcp"}
				]
			}
		]`))
	}))
	defer server.Close()

	inventory := newRuntimeInventory()
	endpoint := runtimeEndpoint{
		runtime: "docker",
		baseURL: server.URL,
		path:    "/containers/json?all=1",
	}
	if err := queryRuntime(context.Background(), server.Client(), endpoint, inventory); err != nil {
		t.Fatal(err)
	}

	identity, ok := inventory.lookupID(id[:12])
	if !ok || identity.Name != "llama" || identity.Runtime != "docker" {
		t.Fatalf("container lookup = %+v, %v", identity, ok)
	}
	identity, ok = inventory.lookupPort("tcp", "127.0.0.1", 18081)
	if !ok || identity.ID != id {
		t.Errorf("published-port lookup = %+v, %v", identity, ok)
	}
	if _, ok := inventory.lookupPort("tcp", "0.0.0.0", 18081); ok {
		t.Error("an IP-specific published port must not become a wildcard match")
	}
	if _, ok := inventory.lookupPort("tcp6", "127.0.0.1", 18081); !ok {
		t.Error("tcp6 lookup must use the runtime's tcp port mapping")
	}
	if _, ok := inventory.lookupPort("tcp", "192.0.2.1", 9090); !ok {
		t.Error("wildcard published-port lookup failed")
	}
	if _, ok := inventory.lookupPort("tcp6", "2001:db8::1", 9090); ok {
		t.Error("an IPv4 wildcard publication must not match an IPv6 socket")
	}
	if _, ok := inventory.lookupPort("tcp", "127.0.0.1", 22); ok {
		t.Error("unpublished port must not resolve")
	}
}

func TestQueryRuntimeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	inventory := newRuntimeInventory()
	endpoint := runtimeEndpoint{runtime: "docker", baseURL: server.URL, path: "/containers/json"}
	if err := queryRuntime(context.Background(), server.Client(), endpoint, inventory); err == nil {
		t.Fatal("expected an HTTP error")
	}
}

func TestQueryRuntimePodmanPorts(t *testing.T) {
	const id = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4.0.0/libpod/containers/json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"Id":"` + id + `","Names":["/llama"],"Ports":[{"host_ip":"127.0.0.1","host_port":18081,"container_port":18080,"protocol":"tcp"}]}]`))
	}))
	defer server.Close()

	inventory := newRuntimeInventory()
	endpoint := runtimeEndpoint{
		runtime: "podman",
		baseURL: server.URL,
		path:    "/v4.0.0/libpod/containers/json",
	}
	if err := queryRuntime(context.Background(), server.Client(), endpoint, inventory); err != nil {
		t.Fatal(err)
	}

	identity, ok := inventory.lookupPort("tcp", "127.0.0.1", 18081)
	if !ok || identity.Name != "llama" || identity.Runtime != "podman" {
		t.Errorf("podman published-port lookup = %+v, %v", identity, ok)
	}
	if _, ok := inventory.lookupID(id[:12]); !ok {
		t.Error("podman short container ID did not resolve")
	}
}

func TestDiscoverRuntimesReportsPresentSocketError(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})}
	go server.Serve(listener)
	defer server.Close()

	orig := runtimeEndpoints
	runtimeEndpoints = []runtimeEndpoint{{
		runtime: "docker",
		socket:  socket,
		baseURL: "http://docker",
		path:    "/containers/json",
	}}
	t.Cleanup(func() { runtimeEndpoints = orig })

	inventory, err := discoverRuntimes(context.Background(), func(string) error { return nil })
	if err == nil {
		t.Fatal("a present but failing Docker socket must be reported")
	}
	if len(inventory.byID) != 0 {
		t.Errorf("inventory should stay empty: %v", inventory.byID)
	}
}

func TestDiscoverRuntimesReportsSocketPermissionError(t *testing.T) {
	_, err := discoverRuntimes(context.Background(), func(string) error {
		return os.ErrPermission
	})
	if err == nil {
		t.Fatal("a socket permission error must not be treated as an absent runtime")
	}
}
