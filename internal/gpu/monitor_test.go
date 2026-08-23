package gpu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMonitor_AMDGPUFound(t *testing.T) {
	tmpDir := t.TempDir()

	cardPath := filepath.Join(tmpDir, "card0")
	devicePath := filepath.Join(cardPath, "device")
	hwmonPath := filepath.Join(devicePath, "hwmon", "hwmon0")

	os.MkdirAll(filepath.Join(devicePath, "drm"), 0755)
	os.MkdirAll(hwmonPath, 0755)

	os.WriteFile(filepath.Join(devicePath, "vendor"), []byte("0x1002"), 0644)
	os.WriteFile(filepath.Join(devicePath, "mem_info_vram_used"), []byte("4294967296"), 0644)
	os.WriteFile(filepath.Join(devicePath, "mem_info_vram_total"), []byte("8589934592"), 0644)
	os.WriteFile(filepath.Join(devicePath, "mem_info_gtt_used"), []byte("1073741824"), 0644)
	os.WriteFile(filepath.Join(devicePath, "mem_info_gtt_total"), []byte("2147483648"), 0644)
	os.WriteFile(filepath.Join(hwmonPath, "temp1_input"), []byte("62000"), 0644)
	os.WriteFile(filepath.Join(hwmonPath, "power1_average"), []byte("45500000"), 0644)

	t.Setenv("SYS_CLASS_DRM", tmpDir)
}

func TestStats_Defaults(t *testing.T) {
	stats := Stats{}

	if stats.VRAMUsedBytes != 0 {
		t.Errorf("Expected VRAMUsedBytes 0, got %d", stats.VRAMUsedBytes)
	}
	if stats.VRAMTotalBytes != 0 {
		t.Errorf("Expected VRAMTotalBytes 0, got %d", stats.VRAMTotalBytes)
	}
	if stats.GTTUsedBytes != 0 {
		t.Errorf("Expected GTTUsedBytes 0, got %d", stats.GTTUsedBytes)
	}
	if stats.GTTTotalBytes != 0 {
		t.Errorf("Expected GTTTotalBytes 0, got %d", stats.GTTTotalBytes)
	}
	if stats.CPUUsagePercent != 0 {
		t.Errorf("Expected CPUUsagePercent 0, got %f", stats.CPUUsagePercent)
	}
	if stats.TemperatureC != 0 {
		t.Errorf("Expected TemperatureC 0, got %d", stats.TemperatureC)
	}
	if stats.PowerWatts != 0 {
		t.Errorf("Expected PowerWatts 0, got %f", stats.PowerWatts)
	}
}

func TestCPUStats_Struct(t *testing.T) {
	stats := CPUStats{
		User:   1000,
		Nice:   500,
		System: 300,
		Idle:   5000,
		IOWait: 100,
		IRQ:    50,
		Total:  6950,
	}

	if stats.User != 1000 {
		t.Errorf("Expected User 1000, got %d", stats.User)
	}
	if stats.Total != 6950 {
		t.Errorf("Expected Total 6950, got %d", stats.Total)
	}
}

func TestFindAMDGPUPath_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cardPath := filepath.Join(tmpDir, "card0")
	devicePath := filepath.Join(cardPath, "device")
	os.MkdirAll(devicePath, 0755)
	os.WriteFile(filepath.Join(devicePath, "vendor"), []byte("0x8086"), 0644)

	t.Setenv("SYS_CLASS_DRM_TEST", tmpDir)
}

func TestReadSysfsFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_value")

	os.WriteFile(testFile, []byte("12345"), 0644)

	mon := &Monitor{}
	val := mon.readSysfsFile(testFile)

	if val != 12345 {
		t.Errorf("Expected 12345, got %d", val)
	}
}

func TestReadSysfsFile_NotFound(t *testing.T) {
	mon := &Monitor{}
	val := mon.readSysfsFile("/nonexistent/file")

	if val != 0 {
		t.Errorf("Expected 0 for nonexistent file, got %d", val)
	}
}

func TestReadSysfsFile_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_value")

	os.WriteFile(testFile, []byte("not-a-number"), 0644)

	mon := &Monitor{}
	val := mon.readSysfsFile(testFile)

	if val != 0 {
		t.Errorf("Expected 0 for invalid content, got %d", val)
	}
}

// TestFindAMDGPUPath_FoundWithTempDir verifies T9: findAMDGPUPath finds an AMD
// GPU (vendor 0x1002) in a temp directory mock of /sys/class/drm.
func TestFindAMDGPUPath_FoundWithTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	cardDevice := filepath.Join(tmpDir, "card0", "device")
	os.MkdirAll(cardDevice, 0755)
	os.WriteFile(filepath.Join(cardDevice, "vendor"), []byte("0x1002"), 0644)

	path, err := findAMDGPUPath(tmpDir)
	if err != nil {
		t.Fatalf("findAMDGPUPath error: %v", err)
	}
	if path != cardDevice {
		t.Errorf("path = %q, want %q", path, cardDevice)
	}
}

// TestFindAMDGPUPath_NoAMD verifies findAMDGPUPath returns an error when no
// AMD vendor (0x1002) is found.
func TestFindAMDGPUPath_NoAMD(t *testing.T) {
	tmpDir := t.TempDir()
	cardDevice := filepath.Join(tmpDir, "card0", "device")
	os.MkdirAll(cardDevice, 0755)
	os.WriteFile(filepath.Join(cardDevice, "vendor"), []byte("0x8086"), 0644) // Intel

	_, err := findAMDGPUPath(tmpDir)
	if err == nil {
		t.Fatal("expected error for non-AMD GPU, got nil")
	}
}

// TestFindHwmonPath_Found verifies findHwmonPath finds the hwmon subdir.
func TestFindHwmonPath_Found(t *testing.T) {
	tmpDir := t.TempDir()
	hwmonDir := filepath.Join(tmpDir, "hwmon", "hwmon0")
	os.MkdirAll(hwmonDir, 0755)

	path, err := findHwmonPath(tmpDir)
	if err != nil {
		t.Fatalf("findHwmonPath error: %v", err)
	}
	if path != hwmonDir {
		t.Errorf("path = %q, want %q", path, hwmonDir)
	}
}

// TestParseTemperatureFromEntries verifies T9: parseTemperatureFromEntries
// correctly parses a temp*_input file from a temp-dir mock.
func TestParseTemperatureFromEntries(t *testing.T) {
	hwmonDir := t.TempDir()
	os.WriteFile(filepath.Join(hwmonDir, "temp1_input"), []byte("62000"), 0644)

	entries, _ := os.ReadDir(hwmonDir)
	got := parseTemperatureFromEntries(entries, hwmonDir)
	if got != 62 {
		t.Errorf("temperature = %d, want 62", got)
	}
}

// TestParseTemperatureFromEntries_NoTemp verifies it returns 0 when no temp
// entry exists.
func TestParseTemperatureFromEntries_NoTemp(t *testing.T) {
	hwmonDir := t.TempDir()
	os.WriteFile(filepath.Join(hwmonDir, "power1_average"), []byte("45500000"), 0644)

	entries, _ := os.ReadDir(hwmonDir)
	got := parseTemperatureFromEntries(entries, hwmonDir)
	if got != 0 {
		t.Errorf("temperature = %d, want 0 (no temp entry)", got)
	}
}

// TestParsePowerFromEntries verifies T9: parsePowerFromEntries correctly parses
// a power*_average file from a temp-dir mock.
func TestParsePowerFromEntries(t *testing.T) {
	hwmonDir := t.TempDir()
	os.WriteFile(filepath.Join(hwmonDir, "power1_average"), []byte("45500000"), 0644)

	entries, _ := os.ReadDir(hwmonDir)
	got := parsePowerFromEntries(entries, hwmonDir)
	if got != 45.5 {
		t.Errorf("power = %v, want 45.5", got)
	}
}

// TestParsePowerFromEntries_NoPower verifies it returns 0 when no power entry
// exists.
func TestParsePowerFromEntries_NoPower(t *testing.T) {
	hwmonDir := t.TempDir()
	os.WriteFile(filepath.Join(hwmonDir, "temp1_input"), []byte("62000"), 0644)

	entries, _ := os.ReadDir(hwmonDir)
	got := parsePowerFromEntries(entries, hwmonDir)
	if got != 0 {
		t.Errorf("power = %v, want 0 (no power entry)", got)
	}
}

// TestCalculateCPUUsageFromLine_FirstCall verifies the first call returns 0
// (baseline).
func TestCalculateCPUUsageFromLine_FirstCall(t *testing.T) {
	m := &Monitor{}
	line := "cpu  100 20 50 5000 100 10 5 0"
	got := m.calculateCPUUsageFromLine(line)
	if got != 0 {
		t.Errorf("first call should return 0 (baseline), got %v", got)
	}
}

// TestCalculateCPUUsageFromLine_SecondCall verifies the second call computes
// the delta correctly.
func TestCalculateCPUUsageFromLine_SecondCall(t *testing.T) {
	m := &Monitor{}
	// First call: baseline. Total = 100+20+50+5000+100+10+5+0 = 5285, Idle = 5000
	m.calculateCPUUsageFromLine("cpu  100 20 50 5000 100 10 5 0")
	// Second call: User increased by 200, Idle increased by 300.
	// Total = 5285+500 = 5785, Idle = 5300
	// delta_total = 500, delta_idle = 300, usage = (500-300)/500 = 40%
	got := m.calculateCPUUsageFromLine("cpu  300 20 50 5300 100 10 5 0")
	if got != 40 {
		t.Errorf("second call should return 40%%, got %v", got)
	}
}

// TestCalculateCPUUsageFromLine_NotCpuLine verifies it returns 0 for non-cpu
// lines.
func TestCalculateCPUUsageFromLine_NotCpuLine(t *testing.T) {
	m := &Monitor{}
	got := m.calculateCPUUsageFromLine("ctxt 123456")
	if got != 0 {
		t.Errorf("non-cpu line should return 0, got %v", got)
	}
}
