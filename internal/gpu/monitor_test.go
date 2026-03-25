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
