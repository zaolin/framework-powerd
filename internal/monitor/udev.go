package monitor

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

// Monitor watches for Udev events
type Monitor struct {
	fd     int
	stop   chan struct{}
	events chan UdevEvent
}

type UdevEvent struct {
	Action     string
	Subsystem  string
	DevPath    string
	Properties map[string]string
}

// NewMonitor creates a new Udev monitor
func NewMonitor() (*Monitor, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %v", err)
	}

	addr := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: 1, // Multicast group 1 for Udev events
		Pid:    0, // Kernel listens to Pid 0
	}

	if err := unix.Bind(fd, addr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to bind socket: %v", err)
	}

	return &Monitor{
		fd:     fd,
		stop:   make(chan struct{}),
		events: make(chan UdevEvent),
	}, nil
}

// Start begins listening for events
func (m *Monitor) Start() (<-chan UdevEvent, error) {
	go m.listen()
	return m.events, nil
}

// Stop closes the monitor
func (m *Monitor) Stop() {
	close(m.stop)
	unix.Close(m.fd)
	close(m.events)
}

func (m *Monitor) listen() {
	buf := make([]byte, 4096) // Buffer for netlink messages
	for {
		select {
		case <-m.stop:
			return
		default:
			n, _, err := unix.Recvfrom(m.fd, buf, 0)
			if err != nil {
				// If socket is closed manually, this will error, so we handle it gracefully if stopped
				select {
				case <-m.stop:
					return
				default:
					fmt.Printf("Error receiving udev event: %v\n", err)
					continue
				}
			}

			if n > 0 {
				event, err := parseUdevEvent(buf[:n])
				if err == nil {
					m.events <- event
				}
			}
		}
	}
}

func parseUdevEvent(data []byte) (UdevEvent, error) {
	// Udev events are null-terminated strings
	// First string is "ACTION@DEVPATH"
	// Rest are "KEY=VALUE"
	parts := bytes.Split(data, []byte{0x00})
	if len(parts) == 0 {
		return UdevEvent{}, fmt.Errorf("empty event")
	}

	header := string(parts[0])
	headerParts := strings.SplitN(header, "@", 2)
	if len(headerParts) != 2 {
		return UdevEvent{}, fmt.Errorf("invalid header: %s", header)
	}

	event := UdevEvent{
		Action:     headerParts[0],
		DevPath:    headerParts[1],
		Properties: make(map[string]string),
	}

	for _, part := range parts[1:] {
		if len(part) == 0 {
			continue
		}
		kv := strings.SplitN(string(part), "=", 2)
		if len(kv) == 2 {
			event.Properties[kv[0]] = kv[1]
		}
	}

	// Helper to fill top info
	if val, ok := event.Properties["SUBSYSTEM"]; ok {
		event.Subsystem = val
	}

	return event, nil
}
