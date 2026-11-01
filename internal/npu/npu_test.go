package npu

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/zaolin/framework-powerd/internal/config"
)

// sensorRecord builds a 168-byte sensor record for tests.
func sensorRecord(label string, input uint32, units string, unitm int8, typ uint8) []byte {
	rec := make([]byte, sensorSize)
	copy(rec[0:64], label)
	binary.LittleEndian.PutUint32(rec[64:68], input)
	copy(rec[144:160], units)
	rec[160] = byte(unitm)
	rec[161] = typ
	return rec
}

// contextRecord builds a 64-byte hardware context record for tests.
func contextRecord(id, startCol, cols uint32, pid int64, submitted, completed uint64) []byte {
	rec := make([]byte, contextSize)
	binary.LittleEndian.PutUint32(rec[0:4], id)
	binary.LittleEndian.PutUint32(rec[4:8], startCol)
	binary.LittleEndian.PutUint32(rec[8:12], cols)
	binary.LittleEndian.PutUint64(rec[16:24], uint64(pid))
	binary.LittleEndian.PutUint64(rec[24:32], submitted)
	binary.LittleEndian.PutUint64(rec[32:40], completed)
	return rec
}

func TestIoctlRequest(t *testing.T) {
	// _IOWR('d', 0x40+7, 16 bytes) as produced by the C macros.
	if got := ioctlRequest(drmCommandBase+drmGetInfo, 16); got != 0xC0106447 {
		t.Errorf("GET_INFO request = %#x, want 0xC0106447", got)
	}
	if got := ioctlRequest(drmCommandBase+drmSetState, 16); got != 0xC0106448 {
		t.Errorf("SET_STATE request = %#x, want 0xC0106448", got)
	}
}

func TestParseSensors(t *testing.T) {
	buf := append(
		sensorRecord("Total Power", 1500000, "mW", -3, sensorPower),
		sensorRecord("Column 0 Utilization", 40, "%", 0, sensorColumnUtil)...,
	)
	buf = append(buf, sensorRecord("Column 1 Utilization", 60, "%", 0, sensorColumnUtil)...)

	// The driver reports the bytes it did not use.
	readings, err := parseSensors(buf, len(buf))
	if err != nil {
		t.Fatalf("parseSensors: %v", err)
	}
	if len(readings) != 3 {
		t.Fatalf("readings = %d, want 3", len(readings))
	}
	if readings[0].Label != "Total Power" || readings[0].Units != "mW" || readings[0].UnitM != -3 {
		t.Errorf("power sensor = %+v", readings[0])
	}
	if got := sensorWatts(readings[0]); got != 1.5 {
		t.Errorf("power = %v W, want 1.5", got)
	}
	if got := sensorPercent(readings[1]); got != 40 {
		t.Errorf("column 0 = %v%%, want 40", got)
	}
	if got := sensorPercent(readings[2]); got != 60 {
		t.Errorf("column 1 = %v%%, want 60", got)
	}

	// A valid partial array: one record used, two left unused.
	partial, err := parseSensors(buf, sensorSize)
	if err != nil {
		t.Fatalf("parseSensors partial: %v", err)
	}
	if len(partial) != 1 || partial[0].Label != "Total Power" {
		t.Errorf("partial readings = %+v", partial)
	}

	// A misaligned count and a count larger than the buffer are rejected.
	if _, err := parseSensors(buf, 1); err == nil {
		t.Error("expected a misaligned payload to be rejected")
	}
	if _, err := parseSensors(buf, len(buf)+sensorSize); err == nil {
		t.Error("expected an oversized count to be rejected")
	}
}

func TestParseClocks(t *testing.T) {
	buf := make([]byte, 2*clockSize)
	copy(buf[0:16], "MP-NPU Clock")
	binary.LittleEndian.PutUint32(buf[16:20], 1204)
	copy(buf[clockSize:clockSize+16], "H Clock")
	binary.LittleEndian.PutUint32(buf[clockSize+16:clockSize+20], 1711)

	clocks, err := parseClocks(buf)
	if err != nil {
		t.Fatalf("parseClocks: %v", err)
	}
	if clocks.MPNPU != 1204 || clocks.H != 1711 {
		t.Errorf("clocks = %+v, want 1204/1711", clocks)
	}
}

func TestParseResources(t *testing.T) {
	buf := make([]byte, resourceSize)
	values := []uint64{1800, 58, 16, 56, 0}
	for i, value := range values {
		binary.LittleEndian.PutUint64(buf[i*8:i*8+8], value)
	}

	res, err := parseResources(buf)
	if err != nil {
		t.Fatalf("parseResources: %v", err)
	}
	if res.NPUClkMax != 1800 || res.TOPsMax != 58 || res.TaskMax != 16 || res.TOPsCurr != 56 || res.TaskCurr != 0 {
		t.Errorf("resources = %+v", res)
	}
}

func TestParseFirmware(t *testing.T) {
	buf := make([]byte, firmwareSize)
	binary.LittleEndian.PutUint32(buf[0:4], 1)
	binary.LittleEndian.PutUint32(buf[4:8], 0)
	binary.LittleEndian.PutUint32(buf[8:12], 0)
	binary.LittleEndian.PutUint32(buf[12:16], 63)
	if got := parseFirmware(buf); got != "1.0.0.63" {
		t.Errorf("firmware = %q, want 1.0.0.63", got)
	}
}

func TestParseContexts(t *testing.T) {
	buf := contextRecord(7, 4, 8, 4242, 100, 99)
	if len(buf) != contextSize {
		t.Fatalf("record size = %d, want %d", len(buf), contextSize)
	}

	contexts, err := parseContexts(buf, 0)
	if err != nil {
		t.Fatalf("parseContexts: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(contexts))
	}
	ctx := contexts[0]
	if ctx.ContextID != 7 || ctx.StartColumn != 4 || ctx.NumColumns != 8 || ctx.PID != 4242 {
		t.Errorf("context = %+v", ctx)
	}
	if ctx.Submitted != 100 || ctx.Completed != 99 {
		t.Errorf("counters = %d/%d", ctx.Submitted, ctx.Completed)
	}
	if _, err := parseContexts(buf, -contextSize); err == nil {
		t.Error("expected an oversized count to be rejected")
	}
}

func TestPowerModeMapping(t *testing.T) {
	for i, name := range PowerModes() {
		value, ok := powerModeValue(name)
		if !ok || int(value) != i {
			t.Errorf("powerModeValue(%q) = %d/%v, want %d", name, value, ok, i)
		}
		if got := powerModeName(uint8(i)); got != name {
			t.Errorf("powerModeName(%d) = %q, want %q", i, got, name)
		}
	}
	if _, ok := powerModeValue("warp"); ok {
		t.Error("unknown power mode must be rejected")
	}
	if got := powerModeName(200); got != "unknown" {
		t.Errorf("powerModeName(200) = %q, want unknown", got)
	}
}

func TestDetectWithFakeSysfs(t *testing.T) {
	root := t.TempDir()
	classDir := filepath.Join(root, "class")
	devDir := filepath.Join(root, "dev")
	dev := filepath.Join(classDir, "accel0", "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// No driver symlink: detection falls back to the PCI IDs.
	write(filepath.Join(dev, "vendor"), "0x1022\n")
	write(filepath.Join(dev, "device"), "0x17f0\n")
	write(filepath.Join(dev, "vbnv"), "RyzenAI-npu6\n")
	write(filepath.Join(dev, "device_type"), "0\n")
	write(filepath.Join(dev, "fw_version"), "1.0.0.63\n")
	write(filepath.Join(dev, "revision"), "0x20\n")
	write(filepath.Join(dev, "power_state"), "D3hot\n")

	oldClass, oldDev := accelClassDir, accelDevDir
	accelClassDir, accelDevDir = classDir, devDir
	defer func() { accelClassDir, accelDevDir = oldClass, oldDev }()

	found, ok := Detect("")
	if !ok {
		t.Fatal("expected the fake NPU to be detected")
	}
	if found.Name != "accel0" || found.Path != filepath.Join(devDir, "accel0") {
		t.Errorf("device = %+v", found)
	}
	if found.VBNV != "RyzenAI-npu6" || found.DeviceType != "kmq" || found.Firmware != "1.0.0.63" {
		t.Errorf("identity = %+v", found)
	}
	if found.PowerState != "D3hot" || found.Revision != "0x20" {
		t.Errorf("state = %+v", found)
	}
	if found.PCIAddress != "device" {
		t.Errorf("pci address = %q", found.PCIAddress)
	}

	// An empty class directory means no NPU.
	if err := os.RemoveAll(filepath.Join(classDir, "accel0")); err != nil {
		t.Fatal(err)
	}
	if _, ok := Detect(""); ok {
		t.Error("expected no device after removing the fake sysfs entry")
	}
}

func TestDeviceTypeName(t *testing.T) {
	cases := map[string]string{"0": "kmq", "1": "umq", "2": "pf", "7": "7", "": ""}
	for raw, want := range cases {
		if got := deviceTypeName(raw); got != want {
			t.Errorf("deviceTypeName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestMonitorUsesInjectedQuery(t *testing.T) {
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.present = true
	m.device = Device{Path: "/dev/accel/accel0", VBNV: "RyzenAI-npu6"}
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	m.query = func(dev Device) (Stats, error) {
		return Stats{
			Present:           true,
			PowerWatts:        2.5,
			ColumnUtilization: []float64{10, 30},
			MPNPUClockMHz:     1204,
			NPUClockMaxMHz:    1800,
			Contexts:          []Context{{ContextID: 1, PID: 42}},
		}, nil
	}

	m.poll()

	stats := m.GetStats()
	if stats.PowerWatts != 2.5 || stats.MPNPUClockMHz != 1204 || stats.NPUClockMaxMHz != 1800 {
		t.Errorf("stats = %+v", stats)
	}
	if stats.ColumnUtilizationAvg != 20 || stats.ColumnUtilizationMax != 30 {
		t.Errorf("utilization avg/max = %v/%v, want 20/30", stats.ColumnUtilizationAvg, stats.ColumnUtilizationMax)
	}
	if len(stats.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(stats.Contexts))
	}
	if got := m.Health(); got != "ok" {
		t.Errorf("health = %q, want ok", got)
	}
	if stats.Device != "/dev/accel/accel0" {
		t.Errorf("device = %q", stats.Device)
	}

	// The returned snapshot must not alias the cached one.
	stats.ColumnUtilization[0] = 99
	stats.Contexts[0].PID = 99
	fresh := m.GetStats()
	if fresh.ColumnUtilization[0] != 10 || fresh.Contexts[0].PID != 42 {
		t.Error("mutating the snapshot must not change the monitor state")
	}
}

func TestMonitorReportsErrors(t *testing.T) {
	fakeAccel(t, true)
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	if !m.Present() {
		t.Fatal("expected the fake device at construction")
	}
	m.query = func(dev Device) (Stats, error) {
		return Stats{Present: true}, os.ErrPermission
	}

	m.poll()
	if got := m.Health(); got != "error" {
		t.Errorf("health = %q, want error", got)
	}
	if got := m.GetStats().LastError; got == "" {
		t.Error("expected the error to be recorded")
	}
}

func TestMonitorHealthStates(t *testing.T) {
	if got := NewMonitor(config.NPUConfig{}).Health(); got != "disabled" {
		t.Errorf("health = %q, want disabled", got)
	}

	absent := NewMonitor(config.NPUConfig{Enabled: true})
	absent.present = false
	if got := absent.Health(); got != "absent" {
		t.Errorf("health = %q, want absent", got)
	}

	waiting := NewMonitor(config.NPUConfig{Enabled: true})
	waiting.present = true
	if got := waiting.Health(); got != "waiting" {
		t.Errorf("health = %q, want waiting", got)
	}
}

func TestSetPowerModeValidation(t *testing.T) {
	disabled := NewMonitor(config.NPUConfig{})
	if err := disabled.SetPowerMode("high"); err == nil {
		t.Error("expected a disabled monitor to reject the change")
	}

	absent := NewMonitor(config.NPUConfig{Enabled: true})
	absent.present = false
	if err := absent.SetPowerMode("high"); err == nil {
		t.Error("expected an absent device to reject the change")
	}

	present := NewMonitor(config.NPUConfig{Enabled: true})
	present.present = true
	present.device = Device{Path: filepath.Join(t.TempDir(), "missing")}
	if err := present.SetPowerMode("warp"); err == nil {
		t.Error("expected an unknown mode to be rejected")
	}
	// A known mode on a missing device fails, but not because of the name.
	if err := present.SetPowerMode("high"); err == nil {
		t.Error("expected the open of a missing device to fail")
	}
}

func TestQueryDeviceMissingDevice(t *testing.T) {
	if _, err := queryDevice(Device{Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("expected an error for a missing device node")
	}
}

// capturedSensorPayload is the first two records of a real GET_INFO sensor
// payload from a RyzenAI-npu6 device: "Total Power" followed by "Column 0
// Utilization". It pins the byte layout independently of the record builder
// used by the other tests.
const capturedSensorPayload = "" +
	"546f74616c20506f776572000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"000000000000000000000000000000006d570000000000000000000000000000" +
	"fd00000000000000436f6c756d6e2030205574696c697a6174696f6e00000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000002500000000000000" +
	"00000000000000000001000000000000"

func TestParseSensorsCapturedPayload(t *testing.T) {
	buf, err := hex.DecodeString(capturedSensorPayload)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if len(buf) != 2*sensorSize {
		t.Fatalf("fixture size = %d, want %d", len(buf), 2*sensorSize)
	}

	// The capture was taken while the device was idle, so every input value is
	// zero. Inject distinctive values to prove the parser reads the four bytes
	// immediately after the 64-byte label, not a neighbouring field.
	binary.LittleEndian.PutUint32(buf[64:68], 1234)
	binary.LittleEndian.PutUint32(buf[sensorSize+64:sensorSize+68], 56)

	readings, err := parseSensors(buf, len(buf))
	if err != nil {
		t.Fatalf("parseSensors: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("readings = %d, want 2", len(readings))
	}

	power := readings[0]
	if power.Label != "Total Power" || power.Units != "mW" || power.UnitM != -3 || power.Type != sensorPower {
		t.Errorf("power record = %+v", power)
	}
	if power.Status != "" {
		t.Errorf("power status = %q, want empty", power.Status)
	}
	if power.Input != 1234 {
		t.Errorf("power input = %d, want 1234", power.Input)
	}

	column := readings[1]
	if column.Label != "Column 0 Utilization" || column.Units != "%" || column.UnitM != 0 || column.Type != sensorColumnUtil {
		t.Errorf("column record = %+v", column)
	}
	if column.Input != 56 {
		t.Errorf("column input = %d, want 56", column.Input)
	}
	if got := sensorPercent(column); got != 56 {
		t.Errorf("column percent = %v, want 56", got)
	}
}

func TestIoctlArgumentSizes(t *testing.T) {
	// The kernel expects the C struct size; a layout change must not slip by.
	if got := unsafe.Sizeof(getInfo{}); got != 16 {
		t.Errorf("getInfo size = %d, want 16", got)
	}
	if got := unsafe.Sizeof(setState{}); got != 16 {
		t.Errorf("setState size = %d, want 16", got)
	}
}

func TestParseContextsAllCounters(t *testing.T) {
	rec := contextRecord(3, 1, 2, 77, 11, 10)
	binary.LittleEndian.PutUint64(rec[40:48], 4) // migrations
	binary.LittleEndian.PutUint64(rec[48:56], 5) // preemptions
	binary.LittleEndian.PutUint64(rec[56:64], 6) // errors

	contexts, err := parseContexts(rec, 0)
	if err != nil {
		t.Fatalf("parseContexts: %v", err)
	}
	ctx := contexts[0]
	if ctx.Migrations != 4 || ctx.Preemptions != 5 || ctx.Errors != 6 {
		t.Errorf("counters = %d/%d/%d, want 4/5/6", ctx.Migrations, ctx.Preemptions, ctx.Errors)
	}
}

// fakeAccel creates a fake accel class entry and device node.
func fakeAccel(t *testing.T, withNode bool) (classDir, devDir string) {
	t.Helper()
	root := t.TempDir()
	classDir = filepath.Join(root, "class")
	devDir = filepath.Join(root, "dev")
	dev := filepath.Join(classDir, "accel0", "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		if err := os.WriteFile(filepath.Join(dev, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("vendor", "0x1022\n")
	write("device", "0x17f0\n")
	write("vbnv", "RyzenAI-npu6\n")
	write("device_type", "0\n")
	write("fw_version", "1.0.0.63\n")
	write("revision", "0x20\n")
	write("power_state", "D3hot\n")

	if withNode {
		if err := os.WriteFile(filepath.Join(devDir, "accel0"), nil, 0o666); err != nil {
			t.Fatal(err)
		}
	}

	oldClass, oldDev := accelClassDir, accelDevDir
	accelClassDir, accelDevDir = classDir, devDir
	t.Cleanup(func() { accelClassDir, accelDevDir = oldClass, oldDev })
	return classDir, devDir
}

func TestDetectExplicitDevice(t *testing.T) {
	_, devDir := fakeAccel(t, true)
	node := filepath.Join(devDir, "accel0")

	found, ok := Detect(node)
	if !ok {
		t.Fatal("expected an explicit existing device to be detected")
	}
	if found.Path != node || found.Name != "accel0" || found.VBNV != "RyzenAI-npu6" {
		t.Errorf("device = %+v", found)
	}

	if err := os.Remove(node); err != nil {
		t.Fatal(err)
	}
	if _, ok := Detect(node); ok {
		t.Error("expected a missing explicit device to be rejected")
	}
}

func TestGetInfoReportsErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-device")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := openHandle(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()

	if _, err := h.getInfo(paramSensors, make([]byte, sensorSize)); err == nil {
		t.Error("expected the ioctl on a regular file to fail")
	}
}

func TestMonitorDeviceAppearsLater(t *testing.T) {
	classDir, devDir := fakeAccel(t, true)
	// Start with no device visible, then create it.
	if err := os.RemoveAll(filepath.Join(classDir, "accel0")); err != nil {
		t.Fatal(err)
	}

	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	if m.Present() {
		t.Fatal("expected no device at construction")
	}
	if got := m.Health(); got != "absent" {
		t.Errorf("health = %q, want absent", got)
	}
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	m.query = func(dev Device) (Stats, error) {
		return Stats{Present: true, PowerWatts: 1.5}, nil
	}

	// The device shows up after the daemon started.
	if err := os.MkdirAll(filepath.Join(classDir, "accel0", "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"vendor": "0x1022", "device": "0x17f0", "vbnv": "RyzenAI-npu6",
		"device_type": "0", "fw_version": "1.0.0.63", "power_state": "D3hot",
	} {
		if err := os.WriteFile(filepath.Join(classDir, "accel0", "device", name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(devDir, "accel0"), nil, 0o666)

	m.poll()

	if !m.Present() {
		t.Fatal("expected the device to be picked up without a restart")
	}
	if got := m.Health(); got != "ok" {
		t.Errorf("health = %q, want ok", got)
	}
	if got := m.GetStats().PowerWatts; got != 1.5 {
		t.Errorf("power = %v, want 1.5", got)
	}
}

func TestMonitorKeepsSnapshotAcrossErrors(t *testing.T) {
	fakeAccel(t, true)
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }

	fail := false
	m.query = func(dev Device) (Stats, error) {
		if fail {
			return Stats{}, os.ErrPermission
		}
		return Stats{Present: true, PowerWatts: 2.5, Firmware: "1.0.0.63"}, nil
	}

	m.poll()
	if got := m.Health(); got != "ok" {
		t.Fatalf("health = %q, want ok", got)
	}

	fail = true
	m.poll()
	stats := m.GetStats()
	if got := m.Health(); got != "error" {
		t.Errorf("health = %q, want error", got)
	}
	if stats.PowerWatts != 2.5 || stats.Firmware != "1.0.0.63" {
		t.Errorf("last good values were lost: %+v", stats)
	}
	if !m.Present() {
		t.Error("a transient query error must not report the device as gone")
	}

	fail = false
	m.poll()
	if got := m.Health(); got != "ok" {
		t.Errorf("health = %q, want ok after recovery", got)
	}
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want cleared", got)
	}
}

func TestMonitorDetectsRemovedDevice(t *testing.T) {
	classDir, _ := fakeAccel(t, true)
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	m.query = func(dev Device) (Stats, error) {
		return Stats{}, os.ErrPermission
	}

	if !m.Present() {
		t.Fatal("expected the fake device at construction")
	}

	// The driver is unbound: sysfs entry gone, query still failing.
	if err := os.RemoveAll(filepath.Join(classDir, "accel0")); err != nil {
		t.Fatal(err)
	}
	m.poll()

	if m.Present() {
		t.Error("expected the device to be reported as gone")
	}
	if got := m.Health(); got != "absent" {
		t.Errorf("health = %q, want absent", got)
	}
}

func TestParseSensorsEightColumns(t *testing.T) {
	// npu6 exposes eight columns, so the kernel writes nine records
	// (power plus eight columns). The parser must keep all of them.
	buf := sensorRecord("Total Power", 2000000, "mW", -3, sensorPower)
	for i := 0; i < 8; i++ {
		buf = append(buf, sensorRecord("Column Utilization", uint32(i+10), "%", 0, sensorColumnUtil)...)
	}
	if len(buf) != 9*sensorSize {
		t.Fatalf("payload = %d bytes, want %d", len(buf), 9*sensorSize)
	}

	readings, err := parseSensors(buf, len(buf))
	if err != nil {
		t.Fatalf("parseSensors: %v", err)
	}
	if len(readings) != 9 {
		t.Fatalf("readings = %d, want 9", len(readings))
	}
	if got := sensorPercent(readings[8]); got != 17 {
		t.Errorf("last column = %v, want 17", got)
	}
}

func TestMonitorAdoptionFollowedByQueryError(t *testing.T) {
	classDir, devDir := fakeAccel(t, true)
	if err := os.RemoveAll(filepath.Join(classDir, "accel0")); err != nil {
		t.Fatal(err)
	}

	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	m.query = func(dev Device) (Stats, error) {
		return Stats{}, os.ErrPermission
	}

	// The device appears, but the first query fails while it initialises.
	if err := os.MkdirAll(filepath.Join(classDir, "accel0", "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"vendor": "0x1022", "device": "0x17f0", "vbnv": "RyzenAI-npu6",
	} {
		if err := os.WriteFile(filepath.Join(classDir, "accel0", "device", name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(devDir, "accel0"), nil, 0o666)

	m.poll()

	if !m.Present() {
		t.Error("expected the adopted device to stay present")
	}
	stats := m.GetStats()
	if !stats.Present {
		t.Error("expected stats.present to be true after adoption")
	}
	if got := m.Health(); got != "error" {
		t.Errorf("health = %q, want error", got)
	}
}

func TestMonitorDetectErrorKeepsSnapshot(t *testing.T) {
	classDir, _ := fakeAccel(t, true)
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }

	ok := true
	m.query = func(dev Device) (Stats, error) {
		if ok {
			return Stats{Present: true, PowerWatts: 3}, nil
		}
		return Stats{}, os.ErrPermission
	}
	m.poll()
	if got := m.Health(); got != "ok" {
		t.Fatalf("health = %q, want ok", got)
	}

	// Replace the accel class directory with a regular file: detection now
	// fails with ENOTDIR rather than "not found".
	oldClass := accelClassDir
	broken := filepath.Join(t.TempDir(), "broken")
	if err := os.WriteFile(broken, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	accelClassDir = broken
	t.Cleanup(func() { accelClassDir = oldClass })

	ok = false
	m.poll()

	if !m.Present() {
		t.Error("a sysfs read error must not report the device as removed")
	}
	stats := m.GetStats()
	if stats.PowerWatts != 3 {
		t.Errorf("last good snapshot was lost: %+v", stats)
	}
	if got := m.Health(); got != "error" {
		t.Errorf("health = %q, want error", got)
	}
	_ = classDir
}

func TestMonitorFollowsRenumberedDevice(t *testing.T) {
	classDir, devDir := fakeAccel(t, true)
	cfg := config.NPUConfig{Enabled: true, PollInterval: "1s"}
	m := NewMonitor(cfg)
	m.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	m.query = func(dev Device) (Stats, error) {
		return Stats{}, os.ErrPermission
	}

	if !m.Present() {
		t.Fatal("expected accel0 at construction")
	}

	// accel0 is replaced by accel1 between polls.
	if err := os.RemoveAll(filepath.Join(classDir, "accel0")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(classDir, "accel1", "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"vendor": "0x1022", "device": "0x17f0", "vbnv": "RyzenAI-npu6",
	} {
		if err := os.WriteFile(filepath.Join(classDir, "accel1", "device", name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(devDir, "accel1"), nil, 0o666)

	m.poll()

	if got := m.GetStats().Device; got != filepath.Join(devDir, "accel1") {
		t.Errorf("device = %q, want the renumbered node", got)
	}
	if !m.Present() {
		t.Error("expected the device to stay present after renumbering")
	}
}
