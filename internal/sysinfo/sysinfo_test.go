package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadKernelVersion verifies the kernel version is read correctly.
func TestReadKernelVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "osrelease")
	os.WriteFile(path, []byte("6.12.1-cachyos-x86_64\n"), 0644)

	got := readKernelVersion(path)
	if got != "6.12.1-cachyos-x86_64" {
		t.Errorf("readKernelVersion = %q, want %q", got, "6.12.1-cachyos-x86_64")
	}
}

// TestReadKernelVersion_Missing verifies empty string for missing file.
func TestReadKernelVersion_Missing(t *testing.T) {
	got := readKernelVersion("/nonexistent/path")
	if got != "" {
		t.Errorf("readKernelVersion = %q, want empty", got)
	}
}

// TestReadOSField verifies extracting a field from os-release.
func TestReadOSField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "os-release")
	os.WriteFile(path, []byte(`NAME="Gentoo"
ID=gentoo
VERSION="2.18"
PRETTY_NAME="Gentoo Linux"
`), 0644)

	if got := readOSField(path, "NAME"); got != "Gentoo" {
		t.Errorf("NAME = %q, want %q", got, "Gentoo")
	}
	if got := readOSField(path, "VERSION"); got != "2.18" {
		t.Errorf("VERSION = %q, want %q", got, "2.18")
	}
	if got := readOSField(path, "ID"); got != "gentoo" {
		t.Errorf("ID = %q, want %q", got, "gentoo")
	}
}

// TestReadOSField_MissingFile verifies empty string for missing file.
func TestReadOSField_MissingFile(t *testing.T) {
	if got := readOSField("/nonexistent", "NAME"); got != "" {
		t.Errorf("readOSField = %q, want empty", got)
	}
}

// TestReadOSField_MissingKey verifies empty string for a key not in the file.
func TestReadOSField_MissingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "os-release")
	os.WriteFile(path, []byte("NAME=Gentoo\n"), 0644)

	if got := readOSField(path, "VERSION"); got != "" {
		t.Errorf("readOSField(VERSION) = %q, want empty (key absent)", got)
	}
}

// TestParseCPUModel verifies extracting the CPU model name from cpuinfo.
func TestParseCPUModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpuinfo")
	os.WriteFile(path, []byte(`processor	: 0
vendor_id	: AuthenticAMD
cpu family	: 25
model name	: AMD Ryzen AI 7 350 w/ Radeon 860M
processor	: 1
`), 0644)

	got := parseCPUModel(path)
	if got != "AMD Ryzen AI 7 350 w/ Radeon 860M" {
		t.Errorf("parseCPUModel = %q, want %q", got, "AMD Ryzen AI 7 350 w/ Radeon 860M")
	}
}

// TestParseCPUModel_MissingFile verifies empty string for missing file.
func TestParseCPUModel_MissingFile(t *testing.T) {
	if got := parseCPUModel("/nonexistent"); got != "" {
		t.Errorf("parseCPUModel = %q, want empty", got)
	}
}

// TestCountCPUs verifies counting processor entries.
func TestCountCPUs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpuinfo")
	os.WriteFile(path, []byte(`processor	: 0
model name	: AMD Ryzen
processor	: 1
model name	: AMD Ryzen
processor	: 2
model name	: AMD Ryzen
`), 0644)

	got := countCPUs(path)
	if got != 3 {
		t.Errorf("countCPUs = %d, want 3", got)
	}
}

// TestCountCPUs_MissingFile verifies 0 for missing file.
func TestCountCPUs_MissingFile(t *testing.T) {
	if got := countCPUs("/nonexistent"); got != 0 {
		t.Errorf("countCPUs = %d, want 0", got)
	}
}

// TestParseMemTotal verifies parsing MemTotal from meminfo.
func TestParseMemTotal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	os.WriteFile(path, []byte(`MemTotal:       48838284 kB
MemFree:         1234567 kB
`), 0644)

	got := parseMemTotal(path)
	expected := int64(48838284 * 1024)
	if got != expected {
		t.Errorf("parseMemTotal = %d, want %d", got, expected)
	}
}

// TestParseMemTotal_MissingFile verifies 0 for missing file.
func TestParseMemTotal_MissingFile(t *testing.T) {
	if got := parseMemTotal("/nonexistent"); got != 0 {
		t.Errorf("parseMemTotal = %d, want 0", got)
	}
}

// TestCollect verifies the full Collect function works with real /proc files
// (on a Linux system). This is an integration test — it asserts no panic and
// that the struct is populated (kernel version is non-empty on Linux).
func TestCollect_NoPanic(t *testing.T) {
	info := Collect()

	// On Linux, kernel version should be non-empty.
	if info.KernelVersion == "" {
		t.Log("warning: KernelVersion is empty (not running on Linux?)")
	}

	// DaemonVersion should always be set (defaults to "dev").
	if info.DaemonVersion == "" {
		t.Error("DaemonVersion is empty, expected at least 'dev'")
	}
}