package detector

import (
	"testing"
)

// TestParseVirtualDevices_VirtualJoystick verifies T7: parseVirtualDevices
// correctly identifies a virtual joystick with empty Phys, no Uniq, and a
// matching name.
func TestParseVirtualDevices_VirtualJoystick(t *testing.T) {
	data := `I: Bus=0003 Vendor=045e Product=028e Version=0114
N: Name="Microsoft X-Box 360 pad"
P: Phys=
U: Uniq=
H: Handlers=js0 event3

`
	paths, found := parseVirtualDevices(data)
	if !found {
		t.Fatal("expected found=true for virtual joystick")
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0] != "/dev/input/js0" {
		t.Errorf("path = %q, want /dev/input/js0", paths[0])
	}
}

// TestParseVirtualDevices_RealDevice verifies a real device (non-empty Phys)
// is NOT detected as virtual.
func TestParseVirtualDevices_RealDevice(t *testing.T) {
	data := `I: Bus=0003 Vendor=046d Product=c52b Version=0111
N: Name="Logitech USB Receiver"
P: Phys=usb-0000:00:14.0-1/input2
U: Uniq=
H: Handlers=js1 event5

`
	_, found := parseVirtualDevices(data)
	if found {
		t.Error("expected found=false for real device with non-empty Phys")
	}
}

// TestParseVirtualDevices_DeviceWithUniq verifies a device with a Uniq value
// is NOT detected as virtual (Uniq presence excludes it).
func TestParseVirtualDevices_DeviceWithUniq(t *testing.T) {
	data := `I: Bus=0005 Vendor=045e Product=02fd Version=0309
N: Name="Xbox Wireless Controller"
P: Phys=
U: Uniq=aa:bb:cc:dd:ee:ff
H: Handlers=js2 event6

`
	_, found := parseVirtualDevices(data)
	if found {
		t.Error("expected found=false for device with Uniq")
	}
}

// TestParseVirtualDevices_EmptyInput verifies empty input returns no devices.
func TestParseVirtualDevices_EmptyInput(t *testing.T) {
	paths, found := parseVirtualDevices("")
	if found {
		t.Error("expected found=false for empty input")
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

// TestParseVirtualDevices_MultipleDevices verifies the parser handles
// multiple blocks, some virtual, some real.
func TestParseVirtualDevices_MultipleDevices(t *testing.T) {
	data := `I: Bus=0003 Vendor=046d Product=c52b
N: Name="Logitech USB Receiver"
P: Phys=usb-0000:00:14.0-1/input2
U: Uniq=
H: Handlers=js0 event3

I: Bus=0003 Vendor=045e Product=028e
N: Name="Xbox 360 Controller"
P: Phys=
U: Uniq=
H: Handlers=js1 event4

`
	paths, found := parseVirtualDevices(data)
	if !found {
		t.Fatal("expected found=true (one virtual device present)")
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0] != "/dev/input/js1" {
		t.Errorf("path = %q, want /dev/input/js1", paths[0])
	}
}