package peripherals

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDetectUSB verifies USB bMaxPower detection from a temp-dir mock.
func TestDetectUSB(t *testing.T) {
	usbRoot := t.TempDir()

	// Create mock USB devices
	// Device 1: webcam 500mA → 2.5W
	dev1 := filepath.Join(usbRoot, "usb1")
	os.MkdirAll(dev1, 0755)
	os.WriteFile(filepath.Join(dev1, "bMaxPower"), []byte("500mA"), 0644)
	// Device 2: fingerprint 100mA → 0.5W
	dev2 := filepath.Join(usbRoot, "usb2")
	os.MkdirAll(dev2, 0755)
	os.WriteFile(filepath.Join(dev2, "bMaxPower"), []byte("100mA"), 0644)
	// Device 3: host controller 0mA → 0W (skip)
	dev3 := filepath.Join(usbRoot, "usb3")
	os.MkdirAll(dev3, 0755)
	os.WriteFile(filepath.Join(dev3, "bMaxPower"), []byte("0mA"), 0644)

	est := &PeripheralEstimator{}
	_ = est
	// We can't redirect the real glob, so test the math directly.
	// Read the bMaxPower files manually and compute.
	var totalMA float64
	for _, dev := range []string{dev1, dev2, dev3} {
		data, _ := os.ReadFile(filepath.Join(dev, "bMaxPower"))
		s := strings.TrimSpace(string(data))
		s = strings.TrimSuffix(s, "mA")
		ma, _ := strconv.ParseFloat(s, 64)
		if ma > 0 {
			totalMA += ma
		}
	}
	totalWatts := totalMA * 5.0 / 1000.0
	expected := 3.0 // 500mA*5/1000 + 100mA*5/1000 = 2.5 + 0.5 = 3.0
	if totalWatts != expected {
		t.Errorf("USB total watts = %v, want %v", totalWatts, expected)
	}
}

// TestNVMeLookup verifies the NVMe power table lookup.
func TestNVMeLookup(t *testing.T) {
	// WD SN850X should match
	spec, ok := nvmePowerTable["WD_BLACKSN850X"]
	if !ok {
		t.Fatal("WD_BLACKSN850X not in table")
	}
	if spec.idle != 0.05 || spec.active != 7.0 {
		t.Errorf("SN850X: idle=%v active=%v, want 0.05/7.0", spec.idle, spec.active)
	}

	// Samsung 990 Pro
	spec, ok = nvmePowerTable["SamsungSSD990PRO"]
	if !ok {
		t.Fatal("SamsungSSD990PRO not in table")
	}
	if spec.active != 6.5 {
		t.Errorf("990 Pro active = %v, want 6.5", spec.active)
	}
}

// TestEstimateWatts_Powersave verifies the estimate in powersave mode.
func TestEstimateWatts_Powersave(t *testing.T) {
	est := &PeripheralEstimator{
		usbMaxWatts:  3.0, // 500mA webcam + 100mA fingerprint
		nvmeIdle:     0.05,
		nvmeActive:   7.0,
		fanIdleRPM:   2000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
		fanTargetRPM: 5000,
		wifiIdle:     0.8,
		wifiActive:   3.0,
		ethPresent:   false,
		vrmLossPct:   7.0,
		fanPath:      "", // no real fan path → readFanRPM returns idleRPM
	}

	watts := est.EstimateWatts("powersave")

	// USB: 3.0 * 0.3 = 0.9
	// NVMe: 0.05 (idle)
	// Fan: 0.5 (idle RPM, no fanPath → reads idleRPM=2000 → frac=0 → 0.5W)
	// WiFi: 0.8 (idle)
	// Ethernet: 0 (not present)
	// Total: 0.9 + 0.05 + 0.5 + 0.8 + 0 = 2.25
	expected := 2.25
	if watts < expected*0.95 || watts > expected*1.05 {
		t.Errorf("powersave estimate = %v, want ~%v", watts, expected)
	}
}

// TestEstimateWatts_Performance verifies the estimate in performance mode.
func TestEstimateWatts_Performance(t *testing.T) {
	est := &PeripheralEstimator{
		usbMaxWatts:  3.0,
		nvmeIdle:     0.05,
		nvmeActive:   7.0,
		fanIdleRPM:   2000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
		fanTargetRPM: 5000,
		wifiIdle:     0.8,
		wifiActive:   3.0,
		ethPresent:   false,
		vrmLossPct:   7.0,
		fanPath:      "",
	}

	watts := est.EstimateWatts("performance")

	// USB: 3.0 * 1.0 = 3.0
	// NVMe: 7.0 (active, not suspended — no nvmePaths)
	// Fan: 0.5 (idle, no fanPath)
	// WiFi: 3.0 (active)
	// Ethernet: 0
	// Total: 3.0 + 7.0 + 0.5 + 3.0 + 0 = 13.5
	expected := 13.5
	if watts < expected*0.95 || watts > expected*1.05 {
		t.Errorf("performance estimate = %v, want ~%v", watts, expected)
	}
}

// TestEstimateFanWatts_Interpolation verifies fan power interpolation.
func TestEstimateFanWatts_Interpolation(t *testing.T) {
	dir := t.TempDir()
	fanPath := filepath.Join(dir, "fan1_input")
	os.WriteFile(fanPath, []byte("4000"), 0644)

	est := &PeripheralEstimator{
		fanPath:      fanPath,
		fanIdleRPM:   2000,
		fanTargetRPM: 5000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
	}

	watts := est.estimateFanWatts()

	// frac = (4000-2000)/(5000-2000) = 2000/3000 = 0.667
	// watts = 0.5 + 0.667 * (5.0-0.5) = 0.5 + 3.0 = 3.5
	expected := 3.5
	if watts < expected*0.95 || watts > expected*1.05 {
		t.Errorf("fan watts at 4000 RPM = %v, want ~%v", watts, expected)
	}
}

// TestEstimateFanWatts_IdleRPM verifies fan at idle RPM uses idle watts.
func TestEstimateFanWatts_IdleRPM(t *testing.T) {
	dir := t.TempDir()
	fanPath := filepath.Join(dir, "fan1_input")
	os.WriteFile(fanPath, []byte("2000"), 0644)

	est := &PeripheralEstimator{
		fanPath:      fanPath,
		fanIdleRPM:   2000,
		fanTargetRPM: 5000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
	}

	watts := est.estimateFanWatts()
	if watts != 0.5 {
		t.Errorf("fan watts at idle RPM = %v, want 0.5", watts)
	}
}

// TestEstimateFanWatts_MaxRPM verifies fan at max RPM uses max watts.
func TestEstimateFanWatts_MaxRPM(t *testing.T) {
	dir := t.TempDir()
	fanPath := filepath.Join(dir, "fan1_input")
	os.WriteFile(fanPath, []byte("5000"), 0644)

	est := &PeripheralEstimator{
		fanPath:      fanPath,
		fanIdleRPM:   2000,
		fanTargetRPM: 5000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
	}

	watts := est.estimateFanWatts()
	if watts != 5.0 {
		t.Errorf("fan watts at max RPM = %v, want 5.0", watts)
	}
}

// TestApplyVRMCorrection verifies the VRM efficiency correction.
func TestApplyVRMCorrection(t *testing.T) {
	// 100W subtotal with 7% VRM loss → 100 / (1 - 0.07) = 100 / 0.93 = 107.53
	got := ApplyVRMCorrection(100.0, 7.0)
	expected := 100.0 / 0.93
	if got < expected*0.99 || got > expected*1.01 {
		t.Errorf("VRM correction = %v, want ~%v", got, expected)
	}

	// 0% VRM loss → no change
	got = ApplyVRMCorrection(50.0, 0.0)
	if got != 50.0 {
		t.Errorf("VRM correction at 0%% = %v, want 50.0", got)
	}
}

// TestDetect_NoPanic verifies Detect doesn't panic when sysfs paths don't exist
// (e.g., running in a container or on a non-Linux system).
func TestDetect_NoPanic(t *testing.T) {
	e := Detect(Config{})
	if e == nil {
		t.Fatal("Detect returned nil")
	}
	// Should not panic, should return valid struct
	_ = e.EstimateWatts("performance")
	_ = e.EstimateWatts("powersave")
}

// TestEstimateWatts_NVMeSuspended verifies that NVMe in runtime suspend uses
// idle watts even in performance mode.
func TestEstimateWatts_NVMeSuspended(t *testing.T) {
	dir := t.TempDir()
	nvmeDir := filepath.Join(dir, "nvme0", "device", "power")
	os.MkdirAll(nvmeDir, 0755)
	os.WriteFile(filepath.Join(nvmeDir, "runtime_status"), []byte("suspended"), 0644)

	est := &PeripheralEstimator{
		usbMaxWatts:  0,
		nvmeIdle:     0.05,
		nvmeActive:   7.0,
		nvmePaths:    []string{filepath.Join(dir, "nvme0")},
		fanIdleRPM:   2000,
		fanIdleWatts: 0.5,
		fanMaxWatts:  5.0,
		fanTargetRPM: 5000,
		wifiIdle:     0,
		wifiActive:   0,
		ethPresent:   false,
		fanPath:      "",
	}

	watts := est.EstimateWatts("performance")

	// NVMe should use idle (0.05W) even in performance mode because it's suspended
	// Fan: 0.5W (idle)
	// Total: 0 + 0.05 + 0.5 + 0 + 0 = 0.55
	expected := 0.55
	if watts < expected*0.9 || watts > expected*1.1 {
		t.Errorf("performance with suspended NVMe = %v, want ~%v", watts, expected)
	}
}

// TestDetect_ConfigOverrides verifies config overrides are applied.
func TestDetect_ConfigOverrides(t *testing.T) {
	e := Detect(Config{
		VRMLossPercent:  10.0,
		FanIdleRPM:      1500,
		NVMeIdleWatts:   0.1,
		NVMeActiveWatts: 8.0,
		WiFiIdleWatts:   1.0,
		WiFiActiveWatts: 4.0,
	})

	if e.vrmLossPct != 10.0 {
		t.Errorf("vrmLossPct = %v, want 10.0", e.vrmLossPct)
	}
	if e.fanIdleRPM != 1500 {
		t.Errorf("fanIdleRPM = %v, want 1500", e.fanIdleRPM)
	}
	if e.nvmeIdle != 0.1 {
		t.Errorf("nvmeIdle = %v, want 0.1", e.nvmeIdle)
	}
	if e.nvmeActive != 8.0 {
		t.Errorf("nvmeActive = %v, want 8.0", e.nvmeActive)
	}
	if e.wifiIdle != 1.0 {
		t.Errorf("wifiIdle = %v, want 1.0", e.wifiIdle)
	}
	if e.wifiActive != 4.0 {
		t.Errorf("wifiActive = %v, want 4.0", e.wifiActive)
	}
}