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
func (d *RemotePlayDetector) Start(ctx context.Context, onStart func(), onStop func()) error {
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

func (d *RemotePlayDetector) check(onStart func(), onStop func()) {
	found := d.scanForVirtualDevices()

	if found && !d.isActive {
		d.isActive = true
		log.Println("[RemotePlay] Detected Virtual Input Device (Steam Remote Play). Active.")
		onStart()
	} else if !found && d.isActive {
		d.isActive = false
		log.Println("[RemotePlay] Virtual Input Device disappeared. Stopped.")
		onStop()
	}
}

// scanForVirtualDevices reads /proc/bus/input/devices and looks for
// devices with empty Phys address acting as Joysticks/Gamepads.
func (d *RemotePlayDetector) scanForVirtualDevices() bool {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		log.Printf("[RemotePlay] Error reading /proc/bus/input/devices: %v\n", err)
		return false
	}

	blocks := strings.Split(string(data), "\n\n")

	for _, block := range blocks {
		if block == "" {
			continue
		}

		// Analysis variables
		lines := strings.Split(block, "\n")
		var hasName bool
		var isVirtualPhys bool
		var isJoystick bool

		for _, line := range lines {
			line = strings.TrimSpace(line)

			// 1. Check Name
			if strings.HasPrefix(line, "N: Name=") {
				rawName := strings.ToLower(line)
				// Common names for virtual controllers
				if strings.Contains(rawName, "xbox") ||
					strings.Contains(rawName, "controller") ||
					strings.Contains(rawName, "gamepad") ||
					strings.Contains(rawName, "microsoft") {
					hasName = true
				}
			}

			// 2. Check Phys Address
			// Virtual devices often have empty Phys or specific virtual paths
			if strings.HasPrefix(line, "P: Phys=") {
				// "P: Phys=" (Empty) is common for uinput/virtual devices
				val := strings.TrimPrefix(line, "P: Phys=")
				if val == "" {
					isVirtualPhys = true
				}
			}

			// 3. Check Handlers (Must be a joystick/event device)
			if strings.HasPrefix(line, "H: Handlers=") {
				if strings.Contains(line, "js") { // js0, js1, etc.
					isJoystick = true
				}
			}
		}

		// Criteria: Empty Phys AND (Joystick Handler OR Known Controller Name)
		// We emphasize Joystick handler because a keyboard/mouse might also satisfy other conditions
		// but Steam Remote Play primarily creates a gamepad.
		if isVirtualPhys && (isJoystick || hasName) {
			// Found one!
			return true
		}
	}

	return false
}
