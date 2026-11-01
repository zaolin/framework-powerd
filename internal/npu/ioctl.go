package npu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	iocRead  = 2
	iocWrite = 1

	drmCommandBase = 0x40
	drmGetInfo     = 7
	drmSetState    = 8

	// Query parameters from enum amdxdna_drm_get_param.
	paramClocks    = 3
	paramSensors   = 4
	paramContexts  = 5
	paramFirmware  = 8
	paramPowerMode = 9
	paramResources = 12

	setParamPowerMode = 0

	// Sensor types from enum amdxdna_sensor_type.
	sensorPower      = 0
	sensorColumnUtil = 1

	sensorSize   = 168
	contextSize  = 64
	maxSensors   = 16
	maxContexts  = 64
	clockSize    = 24
	resourceSize = 40
	firmwareSize = 16
	modeSize     = 8
)

// getInfo mirrors struct amdxdna_drm_get_info.
type getInfo struct {
	Param      uint32
	BufferSize uint32
	Buffer     uint64
}

// setState mirrors struct amdxdna_drm_set_state.
type setState struct {
	Param      uint32
	BufferSize uint32
	Buffer     uint64
}

// ioctlRequest builds the _IOWR request number for a DRM command.
func ioctlRequest(nr uintptr, size uintptr) uintptr {
	return uintptr(iocRead|iocWrite)<<30 |
		uintptr('d')<<8 |
		nr |
		size<<16
}

// handle is an open accel device.
type handle struct {
	file *os.File
}

func openHandle(path string) (*handle, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &handle{file: file}, nil
}

func (h *handle) close() {
	h.file.Close()
}

// getInfo runs one DRM_AMDXDNA_GET_INFO query and returns the value the kernel
// wrote back to buffer_size. Its meaning depends on the query: the sensor
// query reports bytes written, the hardware-context query reports bytes left
// unused. Callers interpret it accordingly.
func (h *handle) getInfo(param uint32, buf []byte) (int, error) {
	args := getInfo{Param: param, BufferSize: uint32(len(buf))}
	if len(buf) > 0 {
		args.Buffer = uint64(uintptr(unsafe.Pointer(&buf[0])))
	}
	req := ioctlRequest(drmCommandBase+drmGetInfo, unsafe.Sizeof(args))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, h.file.Fd(), req, uintptr(unsafe.Pointer(&args)))
	runtime.KeepAlive(buf)
	if errno != 0 {
		return 0, errno
	}
	if int(args.BufferSize) > len(buf) {
		return 0, fmt.Errorf("npu: driver reported %d bytes in a %d byte buffer", args.BufferSize, len(buf))
	}
	return int(args.BufferSize), nil
}

// setState runs one DRM_AMDXDNA_SET_STATE command.
func (h *handle) setState(param uint32, buf []byte) error {
	args := setState{Param: param, BufferSize: uint32(len(buf))}
	if len(buf) > 0 {
		args.Buffer = uint64(uintptr(unsafe.Pointer(&buf[0])))
	}
	req := ioctlRequest(drmCommandBase+drmSetState, unsafe.Sizeof(args))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, h.file.Fd(), req, uintptr(unsafe.Pointer(&args)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&args)
	if errno != 0 {
		return errno
	}
	return nil
}

// sensorReading is one decoded sensor record.
type sensorReading struct {
	Label  string
	Units  string
	Input  uint32
	UnitM  int8
	Type   uint8
	Status string
}

// clockMeta holds the two clocks reported by the driver.
type clockMeta struct {
	MPNPU uint32
	H     uint32
}

// resourceInfo mirrors struct amdxdna_drm_get_resource_info.
type resourceInfo struct {
	NPUClkMax uint64
	TOPsMax   uint64
	TaskMax   uint64
	TOPsCurr  uint64
	TaskCurr  uint64
}

// readSensors returns the decoded sensor records.
func (h *handle) readSensors() ([]sensorReading, error) {
	buf := make([]byte, maxSensors*sensorSize)
	// The sensor query reports the number of bytes it wrote, unlike the
	// hardware-context query, which reports the bytes it did not use.
	written, err := h.getInfo(paramSensors, buf)
	if err != nil {
		return nil, err
	}
	return parseSensors(buf, written)
}

// parseSensors decodes the sensor array. used is the number of bytes the kernel
// wrote into buf.
func parseSensors(buf []byte, used int) ([]sensorReading, error) {
	if used < 0 || used > len(buf) || used%sensorSize != 0 {
		return nil, fmt.Errorf("npu: sensor payload of %d bytes is not a multiple of %d", used, sensorSize)
	}
	count := used / sensorSize
	out := make([]sensorReading, 0, count)
	for i := 0; i < count; i++ {
		rec := buf[i*sensorSize : (i+1)*sensorSize]
		out = append(out, sensorReading{
			Label:  cString(rec[0:64]),
			Input:  binary.LittleEndian.Uint32(rec[64:68]),
			Units:  cString(rec[144:160]),
			UnitM:  int8(rec[160]),
			Type:   rec[161],
			Status: cString(rec[80:144]),
		})
	}
	return out, nil
}

// readClocks returns the MP-NPU and H clock frequencies.
func (h *handle) readClocks() (clockMeta, error) {
	// The driver copies min(buffer_size, struct size) and leaves buffer_size
	// untouched, so there is no unused-byte count to verify here.
	buf := make([]byte, 2*clockSize)
	if _, err := h.getInfo(paramClocks, buf); err != nil {
		return clockMeta{}, err
	}
	return parseClocks(buf)
}

func parseClocks(buf []byte) (clockMeta, error) {
	if len(buf) < 2*clockSize {
		return clockMeta{}, fmt.Errorf("npu: clock payload of %d bytes is too short", len(buf))
	}
	return clockMeta{
		MPNPU: binary.LittleEndian.Uint32(buf[16:20]),
		H:     binary.LittleEndian.Uint32(buf[clockSize+16 : clockSize+20]),
	}, nil
}

// readResources returns the TOPs and task limits.
func (h *handle) readResources() (resourceInfo, error) {
	buf := make([]byte, resourceSize)
	if _, err := h.getInfo(paramResources, buf); err != nil {
		return resourceInfo{}, err
	}
	return parseResources(buf)
}

func parseResources(buf []byte) (resourceInfo, error) {
	if len(buf) < resourceSize {
		return resourceInfo{}, fmt.Errorf("npu: resource payload of %d bytes is too short", len(buf))
	}
	field := func(i int) uint64 { return binary.LittleEndian.Uint64(buf[i*8 : i*8+8]) }
	return resourceInfo{
		NPUClkMax: field(0),
		TOPsMax:   field(1),
		TaskMax:   field(2),
		TOPsCurr:  field(3),
		TaskCurr:  field(4),
	}, nil
}

// readFirmware returns the firmware version formatted as major.minor.patch.
func (h *handle) readFirmware() (string, error) {
	buf := make([]byte, firmwareSize)
	if _, err := h.getInfo(paramFirmware, buf); err != nil {
		return "", err
	}
	return parseFirmware(buf), nil
}

func parseFirmware(buf []byte) string {
	if len(buf) < firmwareSize {
		return ""
	}
	major := binary.LittleEndian.Uint32(buf[0:4])
	minor := binary.LittleEndian.Uint32(buf[4:8])
	patch := binary.LittleEndian.Uint32(buf[8:12])
	build := binary.LittleEndian.Uint32(buf[12:16])
	if build != 0 {
		return fmt.Sprintf("%d.%d.%d.%d", major, minor, patch, build)
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// readPowerMode returns the configured power mode name.
func (h *handle) readPowerMode() (string, error) {
	buf := make([]byte, modeSize)
	if _, err := h.getInfo(paramPowerMode, buf); err != nil {
		return "", err
	}
	return powerModeName(buf[0]), nil
}

// setPowerMode asks the driver to switch the NPU power mode. The kernel
// restricts this ioctl to root.
func (h *handle) setPowerMode(name string) error {
	value, ok := powerModeValue(name)
	if !ok {
		return fmt.Errorf("npu: unknown power mode %q", name)
	}
	buf := make([]byte, modeSize)
	buf[0] = value
	return h.setState(setParamPowerMode, buf)
}

// readContexts returns the hardware contexts currently allocated.
func (h *handle) readContexts() ([]Context, error) {
	buf := make([]byte, maxContexts*contextSize)
	remaining, err := h.getInfo(paramContexts, buf)
	if err != nil {
		return nil, err
	}
	return parseContexts(buf, remaining)
}

func parseContexts(buf []byte, remaining int) ([]Context, error) {
	used := len(buf) - remaining
	if used < 0 || used > len(buf) || used%contextSize != 0 {
		return nil, fmt.Errorf("npu: context payload of %d bytes is not a multiple of %d", used, contextSize)
	}
	count := used / contextSize
	out := make([]Context, 0, count)
	for i := 0; i < count; i++ {
		rec := buf[i*contextSize : (i+1)*contextSize]
		out = append(out, Context{
			ContextID:   binary.LittleEndian.Uint32(rec[0:4]),
			StartColumn: binary.LittleEndian.Uint32(rec[4:8]),
			NumColumns:  binary.LittleEndian.Uint32(rec[8:12]),
			PID:         int64(binary.LittleEndian.Uint64(rec[16:24])),
			Submitted:   binary.LittleEndian.Uint64(rec[24:32]),
			Completed:   binary.LittleEndian.Uint64(rec[32:40]),
			Migrations:  binary.LittleEndian.Uint64(rec[40:48]),
			Preemptions: binary.LittleEndian.Uint64(rec[48:56]),
			Errors:      binary.LittleEndian.Uint64(rec[56:64]),
		})
	}
	return out, nil
}

// cString returns a NUL-terminated byte slice as a string.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// isUnsupported reports whether the driver does not implement a query.
func isUnsupported(err error) bool {
	return errors.Is(err, syscall.EOPNOTSUPP)
}

// sensorWatts converts a power sensor reading to watts.
func sensorWatts(s sensorReading) float64 {
	value := float64(s.Input) * math.Pow10(int(s.UnitM))
	switch s.Units {
	case "W":
		return value
	case "mW":
		return value / 1000
	case "uW", "µW":
		return value / 1e6
	default:
		return value / 1000
	}
}

// sensorPercent converts a utilization sensor reading to percent.
func sensorPercent(s sensorReading) float64 {
	return float64(s.Input) * math.Pow10(int(s.UnitM))
}
