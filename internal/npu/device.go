package npu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	accelClassDir = "/sys/class/accel"
	accelDevDir   = "/dev/accel"
)

// Device describes the static identity of an AMD XDNA NPU.
type Device struct {
	Name       string
	Path       string
	Sysfs      string
	PCIAddress string
	VBNV       string
	DeviceType string
	Firmware   string
	Revision   string
	PowerState string
}

// Detect finds the NPU. An explicit device path wins; otherwise the accel class
// is scanned for an amdxdna device. The second return value reports whether a
// device was found; filesystem errors are treated as "not found".
func Detect(device string) (Device, bool) {
	dev, ok, err := detect(device)
	if err != nil {
		return Device{}, false
	}
	return dev, ok
}

// detect is Detect with the underlying filesystem error preserved, so callers
// can tell a missing device from a broken sysfs.
func detect(device string) (Device, bool, error) {
	if device != "" {
		if _, err := os.Stat(device); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return Device{}, false, nil
			}
			return Device{}, false, err
		}
		name := filepath.Base(device)
		dev := Device{
			Name:  name,
			Path:  device,
			Sysfs: filepath.Join(accelClassDir, name),
		}
		readIdentity(&dev)
		return dev, true, nil
	}

	entries, err := os.ReadDir(accelClassDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Device{}, false, nil
		}
		return Device{}, false, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "accel") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		sysfs := filepath.Join(accelClassDir, name)
		xdna, err := isXDNA(sysfs)
		if err != nil {
			return Device{}, false, err
		}
		if !xdna {
			continue
		}
		dev := Device{
			Name:  name,
			Path:  filepath.Join(accelDevDir, name),
			Sysfs: sysfs,
		}
		readIdentity(&dev)
		return dev, true, nil
	}
	return Device{}, false, nil
}

// isXDNA reports whether the accel device is driven by amdxdna. A filesystem
// error is returned instead of reporting the device as absent, so a transient
// sysfs failure does not make the NPU flap.
func isXDNA(sysfs string) (bool, error) {
	driver, err := filepath.EvalSymlinks(filepath.Join(sysfs, "device", "driver"))
	if err == nil {
		return filepath.Base(driver) == "amdxdna", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	// No driver symlink: fall back to the PCI identifiers.
	vendor, vendorErr := readSysfs(filepath.Join(sysfs, "device", "vendor"))
	device, deviceErr := readSysfs(filepath.Join(sysfs, "device", "device"))

	vendorMissing := errors.Is(vendorErr, os.ErrNotExist)
	deviceMissing := errors.Is(deviceErr, os.ErrNotExist)
	if vendorMissing && deviceMissing {
		// An accel entry without PCI identifiers is not an XDNA device.
		return false, nil
	}
	if vendorErr != nil && !vendorMissing {
		return false, vendorErr
	}
	if deviceErr != nil && !deviceMissing {
		return false, deviceErr
	}
	if vendorMissing || deviceMissing {
		// Half of the identity is missing: sysfs is mid-teardown or mid-probe.
		return false, fmt.Errorf("npu: incomplete sysfs entry %s", sysfs)
	}
	return strings.EqualFold(vendor, "0x1022") && strings.EqualFold(device, "0x17f0"), nil
}

// readIdentity fills the static attributes exposed by the driver.
func readIdentity(dev *Device) {
	dev.VBNV = readOrEmpty(filepath.Join(dev.Sysfs, "device", "vbnv"))
	dev.Firmware = readOrEmpty(filepath.Join(dev.Sysfs, "device", "fw_version"))
	dev.Revision = readOrEmpty(filepath.Join(dev.Sysfs, "device", "revision"))
	dev.PowerState = readOrEmpty(filepath.Join(dev.Sysfs, "device", "power_state"))
	if raw := readOrEmpty(filepath.Join(dev.Sysfs, "device", "device_type")); raw != "" {
		dev.DeviceType = deviceTypeName(raw)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(dev.Sysfs, "device")); err == nil {
		dev.PCIAddress = filepath.Base(resolved)
	}
}

// deviceTypeName maps the numeric device type to the driver's name.
func deviceTypeName(raw string) string {
	switch strings.TrimSpace(raw) {
	case "0":
		return "kmq"
	case "1":
		return "umq"
	case "2":
		return "pf"
	default:
		if _, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			return raw
		}
		return ""
	}
}

// readSysfs returns the trimmed content of a sysfs attribute.
func readSysfs(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// readOrEmpty returns the attribute or an empty string when it is unreadable.
func readOrEmpty(path string) string {
	value, err := readSysfs(path)
	if err != nil {
		return ""
	}
	return value
}
