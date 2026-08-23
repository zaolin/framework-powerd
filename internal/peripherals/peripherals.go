// Package peripherals auto-detects USB, NVMe, fan, WiFi, and Ethernet
// devices at daemon startup and estimates their power consumption based on
// the current power mode and real-time fan RPM.
package peripherals

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PeripheralEstimator holds auto-detected device info and estimates power.
type PeripheralEstimator struct {
	usbMaxWatts  float64 // sum of all USB bMaxPower converted to watts
	nvmeIdle     float64
	nvmeActive   float64
	nvmePaths    []string // /sys/class/nvme/nvmeN for runtime_status check
	fanPath      string  // /sys/class/hwmon/hwmonN/fan1_input
	fanTargetRPM int
	fanIdleRPM   int
	fanIdleWatts float64
	fanMaxWatts  float64
	wifiIdle     float64
	wifiActive   float64
	ethPresent   bool
	ethIface     string
	vrmLossPct   float64 // VRM loss percentage (e.g. 7.0 = 7%)
}

// nvmePowerTable holds known NVMe SSD power specs (idle watts, active watts).
// Matched by checking if the detected model string contains the key.
var nvmePowerTable = map[string]struct{ idle, active float64 }{
	"WD_BLACKSN850X":   {0.05, 7.0},
	"SamsungSSD990PRO": {0.05, 6.5},
	"SamsungSSD980":    {0.05, 5.5},
	"SamsungSSD970":    {0.05, 5.5},
	"CrucialCT500P3":   {0.05, 5.0},
	"CrucialCT2000P3":  {0.05, 5.5},
	"KingstonKC3000":   {0.05, 6.0},
	"SeagateFireCuda":  {0.05, 6.5},
}

const defaultNVMeIdle = 0.05
const defaultNVMeActive = 7.0
const defaultFanIdleRPM = 2000
const defaultFanIdleWatts = 0.5
const defaultFanMaxWatts = 5.0
const defaultWiFiIdle = 0.8
const defaultWiFiActive = 3.0
const defaultEthIdle = 0.5
const defaultVRMLoss = 7.0

// Config holds optional overrides for the estimator. If a field is zero,
// the auto-detected/default value is used.
type Config struct {
	VRMLossPercent float64 `json:"vrm_loss_percent"`
	FanIdleRPM     int     `json:"fan_idle_rpm"`
	NVMeIdleWatts  float64 `json:"nvme_idle_watts"`
	NVMeActiveWatts float64 `json:"nvme_active_watts"`
	WiFiIdleWatts  float64 `json:"wifi_idle_watts"`
	WiFiActiveWatts float64 `json:"wifi_active_watts"`
}

// Detect scans sysfs for peripherals and returns an estimator.
func Detect(cfg Config) *PeripheralEstimator {
	e := &PeripheralEstimator{
		vrmLossPct:   defaultVRMLoss,
		fanIdleRPM:   defaultFanIdleRPM,
		fanIdleWatts: defaultFanIdleWatts,
		fanMaxWatts:  defaultFanMaxWatts,
		wifiIdle:     defaultWiFiIdle,
		wifiActive:   defaultWiFiActive,
		nvmeIdle:     defaultNVMeIdle,
		nvmeActive:   defaultNVMeActive,
	}

	e.detectUSB()
	e.detectNVMe()
	e.detectFan()
	e.detectWiFi()
	e.detectEthernet()

	// Apply config overrides
	if cfg.VRMLossPercent > 0 {
		e.vrmLossPct = cfg.VRMLossPercent
	}
	if cfg.FanIdleRPM > 0 {
		e.fanIdleRPM = cfg.FanIdleRPM
	}
	if cfg.NVMeIdleWatts > 0 {
		e.nvmeIdle = cfg.NVMeIdleWatts
	}
	if cfg.NVMeActiveWatts > 0 {
		e.nvmeActive = cfg.NVMeActiveWatts
	}
	if cfg.WiFiIdleWatts > 0 {
		e.wifiIdle = cfg.WiFiIdleWatts
	}
	if cfg.WiFiActiveWatts > 0 {
		e.wifiActive = cfg.WiFiActiveWatts
	}

	return e
}

// detectUSB scans /sys/bus/usb/devices/*/bMaxPower and sums the wattage.
func (e *PeripheralEstimator) detectUSB() {
	matches, _ := filepath.Glob("/sys/bus/usb/devices/*/bMaxPower")
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		// Format: "100mA" or "500mA"
		maStr := strings.TrimSuffix(s, "mA")
		ma, err := strconv.ParseFloat(maStr, 64)
		if err != nil || ma <= 0 {
			continue
		}
		// Convert mA at 5V → watts: W = mA * 5 / 1000
		e.usbMaxWatts += ma * 5.0 / 1000.0
	}
}

// detectNVMe scans /sys/class/nvme/nvme* and looks up power specs.
func (e *PeripheralEstimator) detectNVMe() {
	matches, _ := filepath.Glob("/sys/class/nvme/nvme*")
	for _, nvmePath := range matches {
		e.nvmePaths = append(e.nvmePaths, nvmePath)

		modelData, err := os.ReadFile(filepath.Join(nvmePath, "model"))
		if err != nil {
			continue
		}
		model := strings.TrimSpace(string(modelData))
		// Remove spaces for matching
		modelKey := strings.ReplaceAll(model, " ", "")

		// Look up in table
		for key, spec := range nvmePowerTable {
			if strings.Contains(modelKey, key) {
				e.nvmeIdle = spec.idle
				e.nvmeActive = spec.active
				break
			}
		}
	}
}

// detectFan finds the first hwmon fan input and reads its target RPM.
func (e *PeripheralEstimator) detectFan() {
	matches, _ := filepath.Glob("/sys/class/hwmon/*/fan*_input")
	for _, fanPath := range matches {
		// Verify the file is readable
		if _, err := os.ReadFile(fanPath); err != nil {
			continue
		}
		e.fanPath = fanPath

		// Try to read the target RPM (max)
		base := strings.TrimSuffix(fanPath, "_input")
		targetData, err := os.ReadFile(base + "_target")
		if err == nil {
			target, _ := strconv.Atoi(strings.TrimSpace(string(targetData)))
			if target > 0 {
				e.fanTargetRPM = target
			}
		}
		break // use the first fan found
	}

	// If no target found, try reading max
	if e.fanTargetRPM == 0 {
		if e.fanPath != "" {
			base := strings.TrimSuffix(e.fanPath, "_input")
			maxData, err := os.ReadFile(base + "_max")
			if err == nil {
				maxRPM, _ := strconv.Atoi(strings.TrimSpace(string(maxData)))
				if maxRPM > 0 {
					e.fanTargetRPM = maxRPM
				}
			}
		}
		if e.fanTargetRPM == 0 {
			e.fanTargetRPM = 5000 // fallback default
		}
	}
}

// detectWiFi checks for a wireless network controller via PCI class 0x028000.
func (e *PeripheralEstimator) detectWiFi() {
	matches, _ := filepath.Glob("/sys/bus/pci/devices/*/class")
	for _, classPath := range matches {
		data, err := os.ReadFile(classPath)
		if err != nil {
			continue
		}
		class := strings.TrimSpace(string(data))
		// 0x028000 = network controller, wireless
		if class == "0x028000" {
			return // WiFi found, keep defaults
		}
	}
	// No WiFi detected → zero it out
	e.wifiIdle = 0
	e.wifiActive = 0
}

// detectEthernet checks for an ethernet controller and its link state.
func (e *PeripheralEstimator) detectEthernet() {
	matches, _ := filepath.Glob("/sys/class/net/*")
	for _, netPath := range matches {
		ifaceName := filepath.Base(netPath)
		if ifaceName == "lo" {
			continue
		}
		// Check if this is a real device (has "device" symlink)
		if _, err := os.Stat(filepath.Join(netPath, "device")); os.IsNotExist(err) {
			continue
		}
		// Check if it's an ethernet interface (not wireless)
		if _, err := os.Stat(filepath.Join(netPath, "wireless")); err == nil {
			continue // skip wireless interfaces
		}
		e.ethPresent = true
		e.ethIface = ifaceName
		return
	}
}

// readFanRPM reads the current fan speed from hwmon.
func (e *PeripheralEstimator) readFanRPM() int {
	if e.fanPath == "" {
		return e.fanIdleRPM
	}
	data, err := os.ReadFile(e.fanPath)
	if err != nil {
		return e.fanIdleRPM
	}
	rpm, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || rpm <= 0 {
		return e.fanIdleRPM
	}
	return rpm
}

// isNVMeSuspended checks if the NVMe device is in runtime suspend.
func (e *PeripheralEstimator) isNVMeSuspended() bool {
	for _, nvmePath := range e.nvmePaths {
		statusData, err := os.ReadFile(filepath.Join(nvmePath, "device", "power", "runtime_status"))
		if err != nil {
			continue
		}
		status := strings.TrimSpace(string(statusData))
		if status == "suspended" {
			return true
		}
	}
	return false
}

// isEthernetUp checks if the ethernet interface is up and has link.
func (e *PeripheralEstimator) isEthernetUp() bool {
	if !e.ethPresent || e.ethIface == "" {
		return false
	}
	operState, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/operstate", e.ethIface))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(operState)) == "up"
}

// EstimateWatts computes the estimated peripheral power consumption in watts
// based on the current power mode and real-time fan RPM.
func (e *PeripheralEstimator) EstimateWatts(mode string) float64 {
	isPerformance := mode == "performance"

	// USB: 30% of max at idle, 100% at active
	var usbWatts float64
	if isPerformance {
		usbWatts = e.usbMaxWatts
	} else {
		usbWatts = e.usbMaxWatts * 0.3
	}

	// NVMe: active watts in performance, idle in powersave;
	// if runtime-suspended, always use idle even in performance
	var nvmeWatts float64
	if isPerformance && !e.isNVMeSuspended() {
		nvmeWatts = e.nvmeActive
	} else {
		nvmeWatts = e.nvmeIdle
	}

	// Fan: interpolate based on actual RPM
	fanWatts := e.estimateFanWatts()

	// WiFi
	var wifiWatts float64
	if isPerformance {
		wifiWatts = e.wifiActive
	} else {
		wifiWatts = e.wifiIdle
	}

	// Ethernet: 0W if interface is down
	var ethWatts float64
	if e.isEthernetUp() {
		ethWatts = defaultEthIdle
	}

	return usbWatts + nvmeWatts + fanWatts + wifiWatts + ethWatts
}

// estimateFanWatts interpolates fan power based on current RPM.
func (e *PeripheralEstimator) estimateFanWatts() float64 {
	rpm := e.readFanRPM()
	maxRPM := e.fanTargetRPM
	if maxRPM <= e.fanIdleRPM {
		return e.fanIdleWatts
	}

	// Clamp RPM to [idleRPM, maxRPM]
	if rpm < e.fanIdleRPM {
		rpm = e.fanIdleRPM
	}
	if rpm > maxRPM {
		rpm = maxRPM
	}

	// Linear interpolation
	frac := float64(rpm-e.fanIdleRPM) / float64(maxRPM-e.fanIdleRPM)
	return e.fanIdleWatts + frac*(e.fanMaxWatts-e.fanIdleWatts)
}

// ApplyVRMCorrection applies VRM efficiency correction to the subtotal.
// vrmLossPct is the estimated VRM loss percentage (e.g. 7.0 = 7%).
func ApplyVRMCorrection(subtotalWatts, vrmLossPct float64) float64 {
	if vrmLossPct <= 0 {
		return subtotalWatts
	}
	return subtotalWatts / (1.0 - vrmLossPct/100.0)
}

// VRMLossPercent returns the configured VRM loss percentage.
func (e *PeripheralEstimator) VRMLossPercent() float64 {
	return e.vrmLossPct
}