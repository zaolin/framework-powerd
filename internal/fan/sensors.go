package fan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sensorSource reads one temperature snapshot.
type sensorSource interface {
	Read() (SensorSet, error)
}

// hwmonReader reads temperatures from the kernel hwmon class.
type hwmonReader struct {
	ec   string
	cpu  string
	gpu  string
	nvme string
	acpi string
}

// newHwmonReader locates the hwmon devices by name.
func newHwmonReader() *hwmonReader {
	return &hwmonReader{
		ec:   findHwmon("cros_ec"),
		cpu:  findHwmon("k10temp"),
		gpu:  findHwmon("amdgpu"),
		nvme: findHwmon("nvme"),
		acpi: findHwmon("acpi_fan"),
	}
}

// findHwmon returns the hwmon directory with the given name, or "".
func findHwmon(name string) string {
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		dir := filepath.Join("/sys/class/hwmon", entry.Name())
		label, err := os.ReadFile(filepath.Join(dir, "name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(label)) == name {
			return dir
		}
	}
	return ""
}

// Read returns the current snapshot.
func (r *hwmonReader) Read() (SensorSet, error) {
	var set SensorSet

	if cpu, ok := hwmonMax(r.cpu); ok {
		set.CPU = &cpu
	}
	if gpu, ok := hwmonMax(r.gpu); ok {
		set.GPU = &gpu
	}
	if nvme, ok := hwmonMax(r.nvme); ok {
		set.NVMe = &nvme
	}
	set.CPUEC, set.Board = readEC(r.ec)

	if r.ec != "" {
		if rpm, ok := readInt(filepath.Join(r.ec, "fan1_input")); ok {
			set.FanRPM = &rpm
		}
	}
	if set.FanRPM == nil && r.acpi != "" {
		if rpm, ok := readInt(filepath.Join(r.acpi, "fan1_input")); ok {
			set.FanRPM = &rpm
		}
	}

	var max float64
	found := false
	for _, value := range []*float64{set.CPU, set.CPUEC, set.Board, set.GPU, set.NVMe} {
		if value == nil {
			continue
		}
		if !found || *value > max {
			max = *value
			found = true
		}
	}
	if !found {
		return SensorSet{}, fmt.Errorf("fan: no temperature source readable")
	}
	set.Max = max
	return set, nil
}

// readEC splits the EC sensors into a CPU value and a board value. Sensors
// whose label mentions "cpu" are treated as CPU; the rest are mainboard.
func readEC(dir string) (cpu, board *float64) {
	if dir == "" {
		return nil, nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "temp*_input"))
	if err != nil {
		return nil, nil
	}
	for _, file := range files {
		value, err := readMilliC(file)
		if err != nil {
			continue
		}
		label := strings.ToLower(readLabel(file))
		if strings.Contains(label, "cpu") {
			cpu = maxPtr(cpu, value)
		} else {
			board = maxPtr(board, value)
		}
	}
	return cpu, board
}

// hwmonMax returns the highest temp*_input in a hwmon directory.
func hwmonMax(dir string) (float64, bool) {
	if dir == "" {
		return 0, false
	}
	files, err := filepath.Glob(filepath.Join(dir, "temp*_input"))
	if err != nil {
		return 0, false
	}
	var max float64
	found := false
	for _, file := range files {
		value, err := readMilliC(file)
		if err != nil {
			continue
		}
		if !found || value > max {
			max = value
			found = true
		}
	}
	return max, found
}

// readMilliC reads a millidegree Celsius attribute.
func readMilliC(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return float64(v) / 1000, nil
}

// readLabel returns the label belonging to a temp*_input file.
func readLabel(inputPath string) string {
	data, err := os.ReadFile(strings.Replace(inputPath, "_input", "_label", 1))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readInt reads an integer attribute.
func readInt(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return v, true
}

// maxPtr returns a pointer to the higher of the current value and v.
func maxPtr(current *float64, v float64) *float64 {
	if current != nil && *current >= v {
		return current
	}
	out := v
	return &out
}
