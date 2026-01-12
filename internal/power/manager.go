package power

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PowerManager handles power profile switching
type PowerManager struct {
	mu          sync.RWMutex
	currentMode string

	// State Tracking
	isIdle        bool
	isRemotePlay  bool
	isGameRunning bool
	gamePID       int
	isGamePaused  bool

	// Components
	idleMonitor interface {
		GetTimeUntilIdle() time.Duration
		ResetActivity()
	}
}

// NewPowerManager creates a new PowerManager
func NewPowerManager() *PowerManager {
	return &PowerManager{}
}

// SetIdleMonitor registers the idle monitor for status reporting
func (pm *PowerManager) SetIdleMonitor(im interface {
	GetTimeUntilIdle() time.Duration
	ResetActivity()
}) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.idleMonitor = im
}

// ValidateTools checks if required external tools are available
func (pm *PowerManager) ValidateTools() error {
	required := []string{"powerprofilesctl"}
	optional := []string{"scxctl", "powertop"}

	var missing []string
	for _, tool := range required {
		if !commandExists(tool) {
			missing = append(missing, tool)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %v", missing)
	}

	for _, tool := range optional {
		if !commandExists(tool) {
			log.Printf("Warning: optional tool '%s' not found\n", tool)
		}
	}
	return nil
}

// SetState updates the tracking state for API reporting
func (pm *PowerManager) SetState(idle, remote, game bool, pid int, paused bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.isIdle = idle
	pm.isRemotePlay = remote
	pm.isGameRunning = game
	pm.gamePID = pid
	pm.isGamePaused = paused
}

// Status represents the power manager state
type Status struct {
	Mode             string  `json:"mode"`
	IsIdle           bool    `json:"is_idle"`
	IsRemotePlay     bool    `json:"is_remote_play"`
	IsGameRunning    bool    `json:"is_game_running"`
	GamePID          int     `json:"game_pid"`
	IsGamePaused     bool    `json:"is_game_paused"`
	SecondsUntilIdle float64 `json:"seconds_until_idle"`
}

// GetStatus returns the current status
func (pm *PowerManager) GetStatus() Status {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var secondsUntilIdle float64
	if pm.idleMonitor != nil {
		secondsUntilIdle = pm.idleMonitor.GetTimeUntilIdle().Seconds()
	}

	return Status{
		Mode:             pm.currentMode,
		IsIdle:           pm.isIdle,
		IsRemotePlay:     pm.isRemotePlay,
		IsGameRunning:    pm.isGameRunning,
		GamePID:          pm.gamePID,
		IsGamePaused:     pm.isGamePaused,
		SecondsUntilIdle: secondsUntilIdle,
	}
}

// SetDefaultActive sets the default active mode (Performance) when no specific game or remote play is detected but the system is not idle.
func (pm *PowerManager) SetDefaultActive() error {
	return pm.SetPerformance("Active Usage")
}

// SetPerformance enables performance mode
func (pm *PowerManager) SetPerformance(reason string) error {
	pm.mu.Lock()
	if pm.currentMode == "performance" {
		pm.mu.Unlock()
		log.Printf("[MODE IGNORED] Already in Performance Mode (%s)\n", reason)
		return nil
	}
	pm.currentMode = "performance"
	pm.mu.Unlock()

	log.Printf("[MODE SET] Switching to Performance Mode (%s)\n", reason)

	// 1. Power Profile -> Performance
	if err := runCommand("powerprofilesctl", "set", "performance"); err != nil {
		log.Printf("Error setting power profile: %v\n", err)
	}

	// 2. CPU Preference -> Speed
	setCPUPref("balance_performance")

	// 3. Scheduler -> Gaming
	if commandExists("scxctl") {
		if err := runCommand("scxctl", "switch", "--sched", "scx_lavd", "--mode", "gaming"); err != nil {
			log.Printf("Error setting scheduler: %v\n", err)
		}
	}

	// 4. PCIe ASPM -> Aggressive
	setASPM("performance")

	// 5. Disable Latency (Reverting USB & Audio Latency)
	pm.disableLatency()

	return nil
}

// SetPowersave enables power save mode
func (pm *PowerManager) SetPowersave(reason string) error {
	pm.mu.Lock()
	if pm.currentMode == "powersave" {
		pm.mu.Unlock()
		log.Printf("[MODE IGNORED] Already in Power Saver Mode (%s)\n", reason)
		return nil
	}
	pm.currentMode = "powersave"
	pm.mu.Unlock()

	log.Printf("[MODE SET] Switching to Power Saver (%s)\n", reason)

	// 1. Power Profile -> Power Saver
	if err := runCommand("powerprofilesctl", "set", "power-saver"); err != nil {
		log.Printf("Error setting power profile: %v\n", err)
	}

	// 2. CPU Preference -> Power Saving
	setCPUPref("power")

	// 3. Scheduler -> Powersave
	if commandExists("scxctl") {
		if err := runCommand("scxctl", "switch", "--sched", "scx_lavd", "--mode", "powersave"); err != nil {
			log.Printf("Error setting scheduler: %v\n", err)
		}
	}

	// 4. PCIe ASPM -> Aggressive
	setASPM("powersupersave")

	// 5. Powertop -> Max Savings
	pm.enablePowertopTune()

	return nil
}

// GetCurrentMode returns the currently active mode
func (pm *PowerManager) GetCurrentMode() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentMode
}

// enablePowertopTune runs powertop --auto-tune
func (pm *PowerManager) enablePowertopTune() {
	log.Println("  -> Applying Powertop Auto-Tune...")
	if commandExists("powertop") {
		if err := runCommand("powertop", "--auto-tune"); err != nil {
			log.Printf("Error running powertop: %v\n", err)
		}
	}
}

// disableLatency disables USB autosuspend and audio power save
func (pm *PowerManager) disableLatency() {
	log.Println("  -> Reverting USB & Audio Latency...")

	// 1. Disable USB Autosuspend
	// Iterate over /sys/bus/usb/devices/*/power/control
	matches, _ := filepath.Glob("/sys/bus/usb/devices/*/power/control")
	for _, path := range matches {
		if err := os.WriteFile(path, []byte("on"), 0644); err != nil {
			// Ignore non-writable files
		}
	}

	// 2. Disable Audio Power Save
	audioPath := "/sys/module/snd_hda_intel/parameters/power_save"
	if _, err := os.Stat(audioPath); err == nil {
		os.WriteFile(audioPath, []byte("0"), 0644)
	}
}

// Helper functions

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command '%s %s' failed: %v, output: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func setCPUPref(pref string) {
	// Check if cpu0 exists
	if _, err := os.Stat("/sys/devices/system/cpu/cpu0/cpufreq"); os.IsNotExist(err) {
		return
	}
	// Write to all cpus
	matches, _ := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference")
	for _, path := range matches {
		os.WriteFile(path, []byte(pref), 0644)
	}
}

func setASPM(policy string) {
	path := "/sys/module/pcie_aspm/parameters/policy"
	if _, err := os.Stat(path); err == nil {
		os.WriteFile(path, []byte(policy), 0644)
	}
}

// TriggerActivity manually resets the idle timer
func (pm *PowerManager) TriggerActivity() {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.idleMonitor != nil {
		pm.idleMonitor.ResetActivity()
	}
}
