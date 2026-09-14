//go:build windows

package probe

import (
	"encoding/binary"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	windowsRawSMBIOSProvider = 0x52534d42 // provider signature "RSMB"
	windowsUser32DLL         = "user32.dll"
	windowsDisplayMirroring  = 0x00000008
	windowsAdaptersBuffer    = 15 * 1024
	windowsAdaptersMax       = 4 << 20
	windowsErrorBuffer       = 111
)

var (
	windowsGetSystemFirmwareTable = windowsSystemKernel32.NewProc("GetSystemFirmwareTable")
	windowsDisplayUser32          = syscall.NewLazyDLL(windowsUser32DLL)
	windowsEnumDisplayDevices     = windowsDisplayUser32.NewProc("EnumDisplayDevicesW")
	windowsGetAdaptersAddresses   = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetAdaptersAddresses")
)

func collectHardwareInventory() hardwareInventory {
	return collectWindowsHardwareInventory()
}

func collectWindowsHardwareInventory() hardwareInventory {
	hardware := parseWindowsSMBIOSInventory(readWindowsSMBIOS())
	hardware.GPUs = collectWindowsGPUs()
	hardware.NICs = collectWindowsNICs()
	hardware.BlockDevices = collectWindowsBlockDevices()
	return hardware
}

func unknownWindowsHardwareInventory() hardwareInventory {
	return hardwareInventory{
		SystemVendor:   "unknown",
		ProductName:    "unknown",
		ProductVersion: "unknown",
		BoardVendor:    "unknown",
		BoardName:      "unknown",
		BoardVersion:   "unknown",
		BIOSVendor:     "unknown",
		BIOSVersion:    "unknown",
		BIOSDate:       "unknown",
	}
}

// readWindowsSMBIOS obtains the raw table through GetSystemFirmwareTable. It
// is consumed immediately by the parser below; no raw firmware bytes enter a
// report.
func readWindowsSMBIOS() []byte {
	if err := windowsGetSystemFirmwareTable.Find(); err != nil {
		return nil
	}
	size, _, _ := windowsGetSystemFirmwareTable.Call(uintptr(windowsRawSMBIOSProvider), 0, 0, 0)
	if size < 8 || size > windowsAdaptersMax {
		return nil
	}
	data := make([]byte, size)
	got, _, _ := windowsGetSystemFirmwareTable.Call(
		uintptr(windowsRawSMBIOSProvider), 0, uintptr(unsafe.Pointer(&data[0])), size,
	)
	if got < 8 {
		return nil
	}
	if got > uintptr(len(data)) {
		return nil
	}
	if got < uintptr(len(data)) {
		data = data[:got]
	}
	return data
}

// parseWindowsSMBIOSInventory reads only the non-identifying product fields
// from SMBIOS types 0, 1 and 2. The string indexes for other fields are never
// followed, so the parser cannot accidentally add a persistent machine fact.
func parseWindowsSMBIOSInventory(data []byte) hardwareInventory {
	result := unknownWindowsHardwareInventory()
	if len(data) < 8 {
		return result
	}
	length := binary.LittleEndian.Uint32(data[4:8])
	if length == 0 || length > uint32(len(data)-8) {
		return result
	}
	table := data[8 : 8+length]
	for offset := 0; offset+4 <= len(table); {
		recordLength := int(table[offset+1])
		if recordLength < 4 || offset+recordLength > len(table) {
			break
		}
		end := offset + recordLength
		for end+1 < len(table) && (table[end] != 0 || table[end+1] != 0) {
			end++
		}
		if end+1 >= len(table) {
			break
		}
		record := table[offset : end+2]
		switch table[offset] {
		case 0:
			result.BIOSVendor = windowsSMBIOSValue(record, 4)
			result.BIOSVersion = windowsSMBIOSValue(record, 5)
			result.BIOSDate = windowsSMBIOSValue(record, 8)
		case 1:
			result.SystemVendor = windowsSMBIOSValue(record, 4)
			result.ProductName = windowsSMBIOSValue(record, 5)
			result.ProductVersion = windowsSMBIOSValue(record, 6)
		case 2:
			result.BoardVendor = windowsSMBIOSValue(record, 4)
			result.BoardName = windowsSMBIOSValue(record, 5)
			result.BoardVersion = windowsSMBIOSValue(record, 6)
		case 127:
			return result
		}
		offset = end + 2
	}
	return result
}

func windowsSMBIOSValue(record []byte, index int) string {
	if len(record) < 2 || index < 4 || index >= int(record[1]) {
		return "unknown"
	}
	stringNumber := int(record[index])
	if stringNumber == 0 {
		return "unknown"
	}
	position := int(record[1])
	for current := 1; position < len(record); current++ {
		end := position
		for end < len(record) && record[end] != 0 {
			end++
		}
		if current == stringNumber {
			value := strings.TrimSpace(string(record[position:end]))
			if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unknown") {
				return "unknown"
			}
			return value
		}
		position = end + 1
		if position < len(record) && record[position] == 0 {
			break
		}
	}
	return "unknown"
}

type windowsDisplayDevice struct {
	Size        uint32
	Name        [32]uint16
	Description [128]uint16
	StateFlags  uint32
	_           [128]uint16
	_           [128]uint16
}

func collectWindowsGPUs() []string {
	if err := windowsEnumDisplayDevices.Find(); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for index := uint32(0); index < 64; index++ {
		device := windowsDisplayDevice{Size: uint32(unsafe.Sizeof(windowsDisplayDevice{}))}
		ok, _, _ := windowsEnumDisplayDevices.Call(0, uintptr(index), uintptr(unsafe.Pointer(&device)), 0)
		if ok == 0 || device.StateFlags&windowsDisplayMirroring != 0 {
			if ok == 0 {
				break
			}
			continue
		}
		name := strings.TrimSpace(syscall.UTF16ToString(device.Description[:]))
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

// windowsIPAdapterAddresses stops before the variable-length tail of the
// native record. The fields retained here are only enough to walk the linked
// list and read its display-safe names.
type windowsIPAdapterAddresses struct {
	Length              uint32
	IfIndex             uint32
	Next                *windowsIPAdapterAddresses
	_                   *byte
	FirstUnicastAddress uintptr
	FirstAnycastAddress uintptr
	FirstMulticast      uintptr
	FirstDNSServer      uintptr
	DNSSuffix           *uint16
	Description         *uint16
	FriendlyName        *uint16
	_                   [8]byte
	AddressLength       uint32
	Flags               uint32
	MTU                 uint32
	IfType              uint32
	OperStatus          uint32
	IPv6IfIndex         uint32
	ZoneIndices         [16]uint32
}

func collectWindowsNICs() []string {
	if err := windowsGetAdaptersAddresses.Find(); err != nil {
		return nil
	}
	size := uint32(windowsAdaptersBuffer)
	for attempt := 0; attempt < 3; attempt++ {
		if size == 0 || size > windowsAdaptersMax {
			return nil
		}
		data := make([]byte, size)
		result, _, _ := windowsGetAdaptersAddresses.Call(
			0, 0, 0, uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)),
		)
		if result == windowsErrorBuffer {
			continue
		}
		if result != 0 {
			return nil
		}
		first := (*windowsIPAdapterAddresses)(unsafe.Pointer(&data[0]))
		seen := make(map[string]bool)
		var names []string
		for current, count := first, 0; current != nil && count < 128; current, count = current.Next, count+1 {
			name := windowsUTF16PtrString(current.FriendlyName)
			if name == "" {
				name = windowsUTF16PtrString(current.Description)
			}
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return names
	}
	return nil
}

func windowsUTF16PtrString(pointer *uint16) string {
	if pointer == nil {
		return ""
	}
	values := make([]uint16, 0, 32)
	for index := 0; index < 1024; index++ {
		value := *(*uint16)(unsafe.Add(unsafe.Pointer(pointer), uintptr(index)*2))
		if value == 0 {
			break
		}
		values = append(values, value)
	}
	return strings.TrimSpace(syscall.UTF16ToString(values))
}

func collectWindowsBlockDevices() []string {
	mounts := windowsFixedDriveMounts()
	result := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		result = append(result, mount.Path)
	}
	return result
}

func windowsVirtualizationFromHardware(hardware hardwareInventory) string {
	values := []string{
		hardware.SystemVendor, hardware.ProductName, hardware.BoardVendor,
		hardware.BoardName, hardware.BIOSVendor,
	}
	known := false
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !strings.EqualFold(value, "unknown") {
			known = true
			break
		}
	}
	if !known {
		return "unknown"
	}
	text := strings.ToLower(strings.Join(values, " | "))
	for _, item := range []struct {
		needle string
		name   string
	}{
		{needle: "microsoft corporation | virtual machine", name: "Hyper-V"},
		{needle: "qemu", name: "QEMU"},
		{needle: "vmware", name: "VMware"},
		{needle: "virtualbox", name: "VirtualBox"},
		{needle: "xen", name: "Xen"},
		{needle: "amazon ec2", name: "Amazon EC2"},
		{needle: "google compute engine", name: "Google Compute Engine"},
	} {
		if strings.Contains(text, item.needle) {
			return item.name
		}
	}
	if text == "" || strings.Contains(text, "unknown") {
		return "unknown"
	}
	return "none/unknown"
}
