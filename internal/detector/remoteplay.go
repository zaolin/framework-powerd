package detector

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

// RemotePlayDetector detects Steam Remote Play sessions by identifying
// virtual input devices created by Steam (which usually have empty "Phys" addresses).
type RemotePlayDetector struct {
	PollInterval time.Duration
	isActive     bool
}

// NewRemotePlayDetector creates a new detector
func NewRemotePlayDetector() *RemotePlayDetector {
	return &RemotePlayDetector{
		PollInterval: 5 * time.Second,
	}
}

// Start begins polling /proc/bus/input/devices
func (d *RemotePlayDetector) Start(ctx context.Context, onStart func([]string), onStop func()) error {
	ticker := time.NewTicker(d.PollInterval)
	defer ticker.Stop()

	// Initial check
	d.check(onStart, onStop)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.check(onStart, onStop)
		}
	}
}

func (d *RemotePlayDetector) check(onStart func([]string), onStop func()) {
	devices, found := d.scanForVirtualDevices()

	if found {
		if !d.isActive {
			d.isActive = true
			log.Printf("[RemotePlay] Detected Virtual Input Device (Steam Remote Play). Active. Devices: %v\n", devices)
			onStart(devices)
		} else {
			// Already active. We could check if device list changed, but for now assuming stability.
			// Ideally we might want to re-emit if new devices appear?
			// Let's stick to simple state transition for now, or maybe always emit on check?
			// No, "onStart" implies transition.
			// Wait, if a device is added late?
			// Simplest: only trigger on transition from not-active to active.
		}
	} else {
		if d.isActive {
			d.isActive = false
			log.Println("[RemotePlay] Virtual Input Device disappeared. Stopped.")
			onStop()
		}
	}
}

// scanForVirtualDevices reads /proc/bus/input/devices and looks for
// devices with empty Phys address acting as Joysticks/Gamepads.
func (d *RemotePlayDetector) scanForVirtualDevices() ([]string, bool) {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		log.Printf("[RemotePlay] Error reading /proc/bus/input/devices: %v\n", err)
		return nil, false
	}

	blocks := strings.Split(string(data), "\n\n")
	var foundPaths []string
	found := false

	for _, block := range blocks {
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		var hasName bool
		var isVirtualPhys bool
		var isJoystick bool
		var hasUniq bool
		var jsPath string

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "N: Name=") {
				rawName := strings.ToLower(line)
				if strings.Contains(rawName, "xbox") ||
					strings.Contains(rawName, "controller") ||
					strings.Contains(rawName, "gamepad") ||
					strings.Contains(rawName, "microsoft") {
					hasName = true
				}
			}

			if strings.HasPrefix(line, "P: Phys=") {
				val := strings.TrimPrefix(line, "P: Phys=")
				if val == "" {
					isVirtualPhys = true
				}
			}

			if strings.HasPrefix(line, "U: Uniq=") {
				val := strings.TrimPrefix(line, "U: Uniq=")
				if val != "" {
					hasUniq = true
				}
			}

			if strings.HasPrefix(line, "H: Handlers=") {
				if strings.Contains(line, "js") {
					isJoystick = true
				}
				// Extract handlers
				parts := strings.Fields(strings.TrimPrefix(line, "H: Handlers="))
				for _, part := range parts {
					if strings.HasPrefix(part, "js") {
						jsPath = "/dev/input/" + part
					}
				}
			}
		}

		if isVirtualPhys && !hasUniq && (isJoystick || hasName) {
			found = true
			if jsPath != "" {
				foundPaths = append(foundPaths, jsPath)
			}
		}
	}

	return foundPaths, found
}
