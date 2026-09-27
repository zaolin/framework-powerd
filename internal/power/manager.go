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
	Debug       bool

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
	optional := []string{"scxctl", "powertop", "iw"}

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
	Mode             string                `json:"mode"`
	IsIdle           bool                  `json:"is_idle"`
	IsRemotePlay     bool                  `json:"is_remote_play"`
	IsGameRunning    bool                  `json:"is_game_running"`
	GamePID          int                   `json:"game_pid"`
	IsGamePaused     bool                  `json:"is_game_paused"`
	SecondsUntilIdle float64               `json:"seconds_until_idle"`
	NetworkDevices   []NetworkDeviceStatus `json:"network_devices"`
}

// NetworkDeviceStatus represents the power status of a network interface
type NetworkDeviceStatus struct {
	Interface    string `json:"interface"`
	PowerControl string `json:"power_control"` // "on" or "auto" (or "unknown")
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
		NetworkDevices:   pm.getNetworkDeviceStatus(),
	}
}

func (pm *PowerManager) getNetworkDeviceStatus() []NetworkDeviceStatus {
	return getNetworkDeviceStatusFromDir("/sys/class/net")
}

// getNetworkDeviceStatusFromDir scans a directory of network interfaces and
// returns their power-control status. Extracted from getNetworkDeviceStatus
// so it is testable with a temp directory (T2).
func getNetworkDeviceStatusFromDir(netDir string) []NetworkDeviceStatus {
	var statuses []NetworkDeviceStatus
	matches, _ := filepath.Glob(filepath.Join(netDir, "*"))
	for _, ifacePath := range matches {
		ifaceName := filepath.Base(ifacePath)
		// Skip loopback & virtual devices (no 'device' symlink)
		if ifaceName == "lo" {
			continue
		}
		if _, err := os.Stat(filepath.Join(ifacePath, "device")); os.IsNotExist(err) {
			continue
		}

		// Check device/power/control
		ctrlPath := filepath.Join(ifacePath, "device", "power", "control")
		content, err := os.ReadFile(ctrlPath)
		state := "unknown"
		if err == nil {
			state = strings.TrimSpace(string(content))
		}

		statuses = append(statuses, NetworkDeviceStatus{
			Interface:    ifaceName,
			PowerControl: state,
		})
	}
	return statuses
}

// setScalingGovernor writes the CPUfreq scaling governor on all online CPUs.
// Best-effort: logs write errors but does not fail the caller.
func setScalingGovernor(governor string) {
	if _, err := os.Stat("/sys/devices/system/cpu/cpu0/cpufreq"); os.IsNotExist(err) {
		return
	}
	matches, _ := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor")
	for _, path := range matches {
		if err := os.WriteFile(path, []byte(governor), 0644); err != nil {
			log.Printf("  -> Failed to set scaling governor %s: %v\n", filepath.Base(path), err)
		}
	}
}

// applyBalancedProfile pins the APU profile trio (platform profile, EPP and
// scaling governor) to the balanced defaults. The powerprofilesctl call is the
// primary, authoritative step: on failure the mode switch is aborted. The sysfs
// writes below are best-effort and re-assert the same values every time, so the
// kernel never ends up fighting power-profiles-daemon over the EPP.
func (pm *PowerManager) applyBalancedProfile() error {
	if err := runCommand("powerprofilesctl", "set", "balanced"); err != nil {
		return fmt.Errorf("set balanced power profile: %w", err)
	}
	setCPUPref("balance_performance")
	setScalingGovernor("powersave")
	return nil
}

// pinScheduler switches scx_lavd to gaming mode. The daemon pins it once
// instead of flipping it with the power mode: the idle delta is negligible and
// gaming mode ramps up fastest when an inference request lands while the
// system is input-idle. Best-effort.
func pinScheduler() {
	if commandExists("scxctl") {
		if err := runCommand("scxctl", "switch", "--sched", "scx_lavd", "--mode", "gaming"); err != nil {
			log.Printf("Error setting scheduler: %v\n", err)
		}
	}
}

// SetDefaultActive sets the default active mode (Performance) when no specific game or remote play is detected but the system is not idle.
func (pm *PowerManager) SetDefaultActive() error {
	return pm.SetPerformance("Active Usage")
}

// SetPerformance applies the active-usage tunables. The APU profile trio is
// pinned to balanced regardless of mode — only the secondary tunables
// (scheduler aside) actually flip with idle/active transitions.
func (pm *PowerManager) SetPerformance(reason string) error {
	pm.mu.Lock()
	if pm.currentMode == "performance" {
		pm.mu.Unlock()
		log.Printf("[MODE IGNORED] Already in Performance Mode (%s)\n", reason)
		return nil
	}
	pm.mu.Unlock()

	log.Printf("[MODE SET] Switching to Performance Mode (%s)\n", reason)

	// 1. Pin APU profile trio -> balanced (primary; its error is authoritative)
	if err := pm.applyBalancedProfile(); err != nil {
		log.Printf("Error setting power profile: %v\n", err)
		return err
	}

	// Primary command succeeded — commit the mode.
	pm.mu.Lock()
	pm.currentMode = "performance"
	pm.mu.Unlock()

	// Best-effort tuning steps below; they log errors but do not fail the call.
	// 2. Scheduler -> Gaming (pinned on every active transition; never downgraded on idle)
	pinScheduler()

	// 3. PCIe ASPM -> Aggressive
	setASPM("performance")

	// 4. Disable Latency (Reverting USB & Audio Latency)
	pm.disableLatency()

	return nil
}

// SetPowersave applies the idle tunables. The APU profile trio stays pinned to
// balanced — idle power savings come from ASPM, powertop and network tuning,
// while the balanced EPP/governor keep inference wake-up latency low.
func (pm *PowerManager) SetPowersave(reason string) error {
	pm.mu.Lock()
	if pm.currentMode == "powersave" {
		pm.mu.Unlock()
		log.Printf("[MODE IGNORED] Already in Power Saver Mode (%s)\n", reason)
		return nil
	}
	pm.mu.Unlock()

	log.Printf("[MODE SET] Switching to Power Saver (%s)\n", reason)

	// 1. Pin APU profile trio -> balanced (primary; its error is authoritative)
	if err := pm.applyBalancedProfile(); err != nil {
		log.Printf("Error setting power profile: %v\n", err)
		return err
	}

	// Primary command succeeded — commit the mode.
	pm.mu.Lock()
	pm.currentMode = "powersave"
	pm.mu.Unlock()

	// Best-effort tuning steps below.
	// 2. PCIe ASPM -> Max Savings
	setASPM("powersupersave")

	// 3. Powertop -> Max Savings
	pm.enablePowertopTune()

	// 4. Exclude Network Devices (Stability)
	pm.disableNetworkPowerSave()

	return nil
}

// GetCurrentMode returns the currently active mode. The APU profile trio is
// pinned to balanced, so the externally reported mode is always "balanced";
// the internal mode only gates the secondary tunables.
func (pm *PowerManager) GetCurrentMode() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.currentMode == "" {
		return ""
	}
	return "balanced"
}

// CurrentTunableState returns the internal tunable gate ("performance" or
// "powersave"). Consumers that need the active/idle distinction — such as the
// peripheral power estimator — must use this instead of GetCurrentMode, which
// always reports the pinned "balanced" profile.
func (pm *PowerManager) CurrentTunableState() string {
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

// disableNetworkPowerSave forces 'on' for network devices to prevent connection drops (e.g. mt7925)
func (pm *PowerManager) disableNetworkPowerSave() {
	log.Println("  -> Disabling Power Save for Network Devices...")
	matches, _ := filepath.Glob("/sys/class/net/*")
	for _, ifacePath := range matches {
		if filepath.Base(ifacePath) == "lo" {
			continue
		}
		// Skip virtual devices (check if 'device' symlink exists)
		if _, err := os.Stat(filepath.Join(ifacePath, "device")); os.IsNotExist(err) {
			continue
		}

		// Write "on" to device/power/control
		ctrlPath := filepath.Join(ifacePath, "device", "power", "control")
		if err := os.WriteFile(ctrlPath, []byte("on"), 0644); err != nil {
			log.Printf("  -> Failed to set power on for %s: %v\n", filepath.Base(ifacePath), err)
		}

		// Use iw for wireless interfaces
		if commandExists("iw") {
			if err := exec.Command("iw", "dev", filepath.Base(ifacePath), "set", "power_save", "off").Run(); err != nil {
				// Expected for ethernet interfaces — only log in debug mode.
				if pm.Debug {
					log.Printf("  -> iw power_save off failed for %s: %v\n", filepath.Base(ifacePath), err)
				}
			}
		}
	}
}

// disableLatency disables USB autosuspend and audio power save
func (pm *PowerManager) disableLatency() {
	log.Println("  -> Reverting USB & Audio Latency...")

	// 1. Disable USB Autosuspend
	matches, _ := filepath.Glob("/sys/bus/usb/devices/*/power/control")
	for _, path := range matches {
		if err := os.WriteFile(path, []byte("on"), 0644); err != nil {
			if pm.Debug {
				log.Printf("  -> Failed to write %s: %v\n", path, err)
			}
		}
	}

	// 2. Disable Audio Power Save
	audioPath := "/sys/module/snd_hda_intel/parameters/power_save"
	if _, err := os.Stat(audioPath); err == nil {
		if err := os.WriteFile(audioPath, []byte("0"), 0644); err != nil {
			log.Printf("  -> Failed to disable audio power save: %v\n", err)
		}
	}
}

// Helper functions

// commandRunner is the seam used to execute external commands. It is a
// package-level variable so tests can inject a fake without touching the real
// process table. Production code uses the default (real exec.Command).
var commandRunner execRunner = realExecRunner{}

// ExecRunner is the seam exposed for tests in other packages (e.g. api tests).
type ExecRunner = execRunner

// CommandRunner returns the active command runner.
func CommandRunner() ExecRunner { return commandRunner }

// SetCommandRunner swaps the command runner. Intended for tests; the caller is
// responsible for restoring the previous value (use the helpers in test code).
func SetCommandRunner(r ExecRunner) { commandRunner = r }

type execRunner interface {
	Run(name string, args ...string) error
	LookPath(name string) bool
}

type realExecRunner struct{}

func (realExecRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command '%s %s' failed: %v, output: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (realExecRunner) LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCommand(name string, args ...string) error {
	return commandRunner.Run(name, args...)
}

func commandExists(cmd string) bool {
	return commandRunner.LookPath(cmd)
}

func setCPUPref(pref string) {
	if _, err := os.Stat("/sys/devices/system/cpu/cpu0/cpufreq"); os.IsNotExist(err) {
		return
	}
	matches, _ := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference")
	for _, path := range matches {
		if err := os.WriteFile(path, []byte(pref), 0644); err != nil {
			log.Printf("  -> Failed to set CPU pref %s: %v\n", filepath.Base(path), err)
		}
	}
}

func setASPM(policy string) {
	path := "/sys/module/pcie_aspm/parameters/policy"
	if _, err := os.Stat(path); err == nil {
		if err := os.WriteFile(path, []byte(policy), 0644); err != nil {
			log.Printf("  -> Failed to set ASPM policy %s: %v\n", policy, err)
		}
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
