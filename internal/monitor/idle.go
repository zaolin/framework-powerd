package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// IdleMonitor watches for system idle state by monitoring raw input devices
type IdleMonitor struct {
	Timeout        time.Duration
	Debug          bool
	lastActivity   int64 // Atomic Unix timestamp
	watchedDevices map[string]bool
	mu             sync.Mutex
}

// NewIdleMonitor creates a new monitor with the specified timeout
func NewIdleMonitor(timeout time.Duration, debug bool) *IdleMonitor {
	return &IdleMonitor{
		Timeout:        timeout,
		Debug:          debug,
		lastActivity:   time.Now().Unix(),
		watchedDevices: make(map[string]bool),
	}
}

// Start begins monitoring input devices and checking for idle state
func (m *IdleMonitor) Start(ctx context.Context, onIdle func(), onActive func()) error {
	// 1. Initial Scan: Start watchers for existing input devices
	if err := m.scanAndWatch(ctx); err != nil {
		log.Printf("[IdleMonitor] Warning: Initial scan failed: %v\n", err)
	}

	// 2. Hotplug: Watch /dev/input for new devices
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[IdleMonitor] Warning: Failed to create fsnotify watcher: %v. Hotplug disabled.\n", err)
	} else {
		// Watch /dev/input directory
		if err := watcher.Add("/dev/input"); err != nil {
			log.Printf("[IdleMonitor] Warning: Failed to watch /dev/input: %v\n", err)
			watcher.Close()
		} else {
			log.Println("[IdleMonitor] Hotplug detection enabled on /dev/input")
			go m.handleHotplug(ctx, watcher)
		}
	}

	// 3. Start the main ticker loop to check for idle timeout
	go m.runTicker(ctx, onIdle, onActive)

	return nil
}

// scanAndWatch finds all current input devices and starts watching them
func (m *IdleMonitor) scanAndWatch(ctx context.Context) error {
	inputs, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return err
	}
	joysticks, err := filepath.Glob("/dev/input/js*")
	if err == nil {
		inputs = append(inputs, joysticks...)
	}

	log.Printf("[IdleMonitor] Found %d input devices. Starting watchers...\n", len(inputs))
	for _, devPath := range inputs {
		go m.watchDevice(ctx, devPath)
	}
	return nil
}

// handleHotplug listens for file creation events in /dev/input
func (m *IdleMonitor) handleHotplug(ctx context.Context, watcher *fsnotify.Watcher) {
	defer watcher.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Look for Create events
			if event.Op&fsnotify.Create == fsnotify.Create {
				filename := filepath.Base(event.Name)
				// Check if it's an event device or joystick
				if match, _ := filepath.Match("event*", filename); match {
					log.Printf("[IdleMonitor] Hotplug: New input device detected: %s\n", event.Name)
					// Give the OS a split second to finalize the device node creation?
					time.Sleep(100 * time.Millisecond)
					go m.watchDevice(ctx, event.Name)
				} else if match, _ := filepath.Match("js*", filename); match {
					log.Printf("[IdleMonitor] Hotplug: New joystick detected: %s\n", event.Name)
					time.Sleep(100 * time.Millisecond)
					go m.watchDevice(ctx, event.Name)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[IdleMonitor] Hotplug watcher error: %v\n", err)
		}
	}
}

// AddDevice manually registers a device for watching
func (m *IdleMonitor) AddDevice(ctx context.Context, path string) {
	log.Printf("[IdleMonitor] Manually adding device: %s\n", path)
	go m.watchDevice(ctx, path)
}

// watchDevice reads from a device file and updates lastActivity on any data
func (m *IdleMonitor) watchDevice(ctx context.Context, path string) {
	m.mu.Lock()
	if m.watchedDevices[path] {
		m.mu.Unlock()
		return
	}
	m.watchedDevices[path] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.watchedDevices, path)
		m.mu.Unlock()
	}()

	// Deduplication: If this is an event device, check if it's also a joystick.
	// If so, we skip watching the event device and rely on the js* device to handle deadzones better.
	if strings.Contains(path, "event") && m.isJoystick(path) {
		if m.Debug {
			log.Printf("[IdleMonitor] Debug: Ignoring %s (handled by js interface)\n", path)
		}
		m.mu.Lock()
		delete(m.watchedDevices, path)
		m.mu.Unlock()
		return
	}

	// Attempt to open the device. If it was just created, might need a retry?
	// But simple open usually works if the node exists.
	file, err := os.Open(path)
	if err != nil {
		log.Printf("[IdleMonitor] Failed to open %s: %v\n", path, err)
		return
	}
	defer file.Close()

	if m.Debug {
		log.Printf("[IdleMonitor] Debug: Opened watching device: %s\n", path)
	}

	// We read 1 event at a time.
	// struct input_event is typically 24 bytes on 64-bit.
	// struct js_event is 8 bytes.
	buf := make([]byte, 64)
	var lastLog time.Time

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Blocking read
			n, err := file.Read(buf)
			if err != nil {
				// Device might have been disconnected
				if m.Debug {
					log.Printf("[IdleMonitor] Debug: Device %s stopped/disconnected: %v\n", path, err)
				}
				return
			}

			if n <= 0 {
				continue
			}

			// Handle Javascript Events (deadzone + init filter)
			if strings.Contains(path, "js") && n >= 8 {
				// struct js_event { u32 time; s16 value; u8 type; u8 number; }
				// offsets: time=0-4, value=4-6, type=6, number=7

				// 1. Filter Init Events (0x80)
				typeByte := buf[6]
				if typeByte&0x80 != 0 {
					continue
				}

				// 2. Deadzone Filter for Axis (Type 2)
				if typeByte&0x7F == 2 {
					// parse value (int16 little endian)
					val := int16(uint16(buf[4]) | uint16(buf[5])<<8)
					// Deadzone ~15% (4000 / 32767)
					if val > -4000 && val < 4000 {
						continue
					}
				}
			}

			// Update activity timestamp
			atomic.StoreInt64(&m.lastActivity, time.Now().Unix())

			if m.Debug {
				if time.Since(lastLog) > 5*time.Second {
					log.Printf("[IdleMonitor] Debug: Input detected on %s\n", path)
					lastLog = time.Now()
				}
			}
		}
	}
}

// isJoystick checks if an event device has a corresponding joystick handler
func (m *IdleMonitor) isJoystick(eventPath string) bool {
	// Map /dev/input/eventX -> /sys/class/input/eventX/device/handlers
	base := filepath.Base(eventPath) // eventX
	sysPath := fmt.Sprintf("/sys/class/input/%s/device/handlers", base)

	data, err := os.ReadFile(sysPath)
	if err != nil {
		return false
	}

	// Handlers content: "sysrq kbd event8 js0"
	content := string(data)
	isJs := strings.Contains(content, "js")
	if m.Debug && !isJs && strings.Contains(content, "event") {
		// Log why we think it's NOT a joystick if it's an event device we are checking
		// This might be spammy so maybe only once per device?
		// actually isJoystick is called once on startup/hotplug.
		log.Printf("[IdleMonitor] Debug: Checked %s handlers: '%s' -> isJoystick=%v\n", base, strings.TrimSpace(content), isJs)
	}
	return isJs
}

// runTicker checks every second if the timeout has been exceeded
func (m *IdleMonitor) runTicker(ctx context.Context, onIdle func(), onActive func()) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	isIdle := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := atomic.LoadInt64(&m.lastActivity)
			now := time.Now().Unix()
			diff := now - last

			if diff >= int64(m.Timeout.Seconds()) {
				if !isIdle {
					isIdle = true
					log.Printf("[IdleMonitor] Idle Triggered! No activity for %d seconds.\n", diff)
					onIdle()
				}
			} else {
				if isIdle {
					isIdle = false
					log.Println("[IdleMonitor] Activity Detected! Resuming...")
					onActive()
				}
			}
		}
	}
}

// GetTimeUntilIdle returns the duration remaining until the system enters idle state
func (m *IdleMonitor) GetTimeUntilIdle() time.Duration {
	last := atomic.LoadInt64(&m.lastActivity)
	elapsed := time.Since(time.Unix(last, 0))
	remaining := m.Timeout - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ResetActivity manually resets the idle timer
func (m *IdleMonitor) ResetActivity() {
	atomic.StoreInt64(&m.lastActivity, time.Now().Unix())
	if m.Debug {
		log.Println("[IdleMonitor] Debug: Activity manually reset via API")
	}
}
