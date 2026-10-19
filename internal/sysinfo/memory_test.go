package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

const meminfoFixture = `MemTotal:        1000000 kB
MemFree:          200000 kB
MemAvailable:     400000 kB
Buffers:           50000 kB
Cached:           100000 kB
SReclaimable:      10000 kB
Shmem:              5000 kB
`

func TestCollectMemoryFrom_Full(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(meminfoFixture), 0644); err != nil {
		t.Fatal(err)
	}

	got := collectMemoryFrom(path)

	if got.TotalBytes != 1000000*1024 {
		t.Errorf("TotalBytes = %d, want %d", got.TotalBytes, 1000000*1024)
	}
	if got.FreeBytes != 200000*1024 {
		t.Errorf("FreeBytes = %d, want %d", got.FreeBytes, 200000*1024)
	}
	if got.AvailableBytes != 400000*1024 {
		t.Errorf("AvailableBytes = %d, want %d", got.AvailableBytes, 400000*1024)
	}
	if got.UsedBytes != 600000*1024 {
		t.Errorf("UsedBytes = %d, want %d", got.UsedBytes, 600000*1024)
	}
	if got.UsedPercent < 59.9 || got.UsedPercent > 60.1 {
		t.Errorf("UsedPercent = %v, want ~60", got.UsedPercent)
	}
}

func TestCollectMemoryFrom_FallbackWithoutMemAvailable(t *testing.T) {
	fixture := `MemTotal:        1000000 kB
MemFree:          200000 kB
Buffers:           50000 kB
Cached:           100000 kB
SReclaimable:      10000 kB
Shmem:              5000 kB
`
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}

	got := collectMemoryFrom(path)

	wantAvailable := int64(200000+50000+100000+10000-5000) * 1024
	if got.AvailableBytes != wantAvailable {
		t.Errorf("AvailableBytes = %d, want %d", got.AvailableBytes, wantAvailable)
	}
	wantUsed := int64(1000000)*1024 - wantAvailable
	if got.UsedBytes != wantUsed {
		t.Errorf("UsedBytes = %d, want %d", got.UsedBytes, wantUsed)
	}
}

func TestCollectMemoryFrom_MissingFile(t *testing.T) {
	got := collectMemoryFrom(filepath.Join(t.TempDir(), "does-not-exist"))
	if got != (MemoryInfo{}) {
		t.Errorf("missing file should yield zero values, got %+v", got)
	}
}

func TestCollectMemoryFrom_MalformedLines(t *testing.T) {
	fixture := `MemTotal:        1000000 kB
garbage line
MemFree: not-a-number
MemAvailable: 500000 kB
`
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}

	got := collectMemoryFrom(path)
	if got.TotalBytes != 1000000*1024 {
		t.Errorf("TotalBytes = %d, want %d", got.TotalBytes, 1000000*1024)
	}
	if got.AvailableBytes != 500000*1024 {
		t.Errorf("AvailableBytes = %d, want %d", got.AvailableBytes, 500000*1024)
	}
}
