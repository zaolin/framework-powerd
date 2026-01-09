package monitor

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// IdleMonitor watches for system idle state by monitoring raw input devices
type IdleMonitor struct {
	Timeout      time.Duration
	lastActivity int64 // Atomic Unix timestamp
}

// NewIdleMonitor creates a new monitor with the specified timeout
func NewIdleMonitor(timeout time.Duration) *IdleMonitor {
	return &IdleMonitor{
		Timeout:      timeout,
		lastActivity: time.Now().Unix(),
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

// watchDevice reads from a device file and updates lastActivity on any data
func (m *IdleMonitor) watchDevice(ctx context.Context, path string) {
	// Attempt to open the device. If it was just created, might need a retry?
	// But simple open usually works if the node exists.
	file, err := os.Open(path)
	if err != nil {
		log.Printf("[IdleMonitor] Failed to open %s: %v\n", path, err)
		return
	}
	defer file.Close()

	// We read 1 byte at a time. The read blocks until input arrives.
	buf := make([]byte, 1)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Blocking read
			_, err := file.Read(buf)
			if err != nil {
				// Device might have been disconnected
				// log.Printf("[IdleMonitor] Device %s stopped/disconnected: %v\n", path, err)
				return
			}
			// Update activity timestamp
			atomic.StoreInt64(&m.lastActivity, time.Now().Unix())
		}
	}
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
