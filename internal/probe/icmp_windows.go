//go:build windows

package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	windowsAFINET6             = uint16(23)
	windowsIPSuccess           = uint32(0)
	windowsIPRequestTimedOut   = syscall.Errno(11010)
	windowsErrorTimeout        = syscall.Errno(1460)
	windowsICMPEchoReplyV4Size = 12
	windowsICMPEchoReplyV6Size = 36
)

var (
	windowsICMPDLL         = syscall.NewLazyDLL("iphlpapi.dll")
	windowsIcmpCreateFile  = windowsICMPDLL.NewProc("IcmpCreateFile")
	windowsIcmp6CreateFile = windowsICMPDLL.NewProc("Icmp6CreateFile")
	windowsIcmpCloseHandle = windowsICMPDLL.NewProc("IcmpCloseHandle")
	windowsIcmpSendEcho2   = windowsICMPDLL.NewProc("IcmpSendEcho2")
	windowsIcmp6SendEcho2  = windowsICMPDLL.NewProc("Icmp6SendEcho2")
)

// icmpAvailable reports whether the native API entry points are present. It
// performs no network operation; the actual handle and echo call are bounded
// by the context passed to runICMPPingFamily.
func icmpAvailable() bool {
	return windowsIcmpCreateFile.Find() == nil &&
		windowsIcmp6CreateFile.Find() == nil &&
		windowsIcmpCloseHandle.Find() == nil &&
		windowsIcmpSendEcho2.Find() == nil &&
		windowsIcmp6SendEcho2.Find() == nil
}

func runICMPPingFamily(ctx context.Context, host string, count int, timeout time.Duration, family string) icmpStats {
	stats := icmpStats{}
	if count <= 0 {
		return stats
	}
	family = windowsICMPFamily(host, family)
	if family != "4" && family != "6" {
		stats.Err = fmt.Errorf("unsupported native ICMP address family %q", family)
		return stats
	}
	if err := ctx.Err(); err != nil {
		stats.Err = err
		return stats
	}

	createProc, sendProc, err := windowsICMPProcedures(family)
	if err != nil {
		stats.Err = err
		return stats
	}
	handle, _, callErr := createProc.Call()
	if handle == 0 || handle == uintptr(syscall.InvalidHandle) {
		stats.Err = windowsICMPCallError("native ICMP handle creation", callErr)
		return stats
	}
	defer closeWindowsICMPHandle(syscall.Handle(handle))

	samples := make([]icmpSample, 0, count)
	var fatalErr error
	for index := 0; index < count; index++ {
		if err := ctx.Err(); err != nil {
			fatalErr = err
			break
		}
		sample, sampleErr := sendWindowsICMPEcho(ctx, sendProc, syscall.Handle(handle), host, timeout, family)
		samples = append(samples, sample)
		if sampleErr != nil {
			fatalErr = sampleErr
			break
		}
	}
	stats = aggregateICMPSamples(samples)
	if !stats.Available && fatalErr != nil {
		stats.Err = fatalErr
	}
	return stats
}

func windowsICMPFamily(host, family string) string {
	if family == "4" || family == "6" {
		return family
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.To4() != nil {
			return "4"
		}
		if ip.To16() != nil {
			return "6"
		}
	}
	return family
}

func windowsICMPProcedures(family string) (*syscall.LazyProc, *syscall.LazyProc, error) {
	if family == "4" {
		if err := windowsIcmpCreateFile.Find(); err != nil {
			return nil, nil, fmt.Errorf("IcmpCreateFile unavailable: %w", err)
		}
		if err := windowsIcmpSendEcho2.Find(); err != nil {
			return nil, nil, fmt.Errorf("IcmpSendEcho2 unavailable: %w", err)
		}
		return windowsIcmpCreateFile, windowsIcmpSendEcho2, nil
	}
	if err := windowsIcmp6CreateFile.Find(); err != nil {
		return nil, nil, fmt.Errorf("Icmp6CreateFile unavailable: %w", err)
	}
	if err := windowsIcmp6SendEcho2.Find(); err != nil {
		return nil, nil, fmt.Errorf("Icmp6SendEcho2 unavailable: %w", err)
	}
	return windowsIcmp6CreateFile, windowsIcmp6SendEcho2, nil
}

func sendWindowsICMPEcho(ctx context.Context, sendProc *syscall.LazyProc, handle syscall.Handle, host string, timeout time.Duration, family string) (icmpSample, error) {
	sample := icmpSample{Sent: true}
	timeoutMS, err := windowsICMPTimeout(ctx, timeout)
	if err != nil {
		return icmpSample{}, err
	}
	payload := []byte("ecs-icmp")
	reply := make([]byte, 256)
	var result uintptr
	var callErr error

	switch family {
	case "4":
		ip := net.ParseIP(host).To4()
		if ip == nil {
			return icmpSample{}, fmt.Errorf("native ICMP IPv4 target is invalid: %q", host)
		}
		// IPAddr is an in_addr in network byte order. A little-endian uint32
		// produces the same byte sequence when passed through syscall.Call.
		address := binary.LittleEndian.Uint32(ip)
		result, _, callErr = sendProc.Call(
			uintptr(handle), 0, 0, 0,
			uintptr(address),
			uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
			uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeoutMS),
		)
	case "6":
		parsed := net.ParseIP(host)
		if parsed == nil || parsed.To4() != nil {
			return icmpSample{}, fmt.Errorf("native ICMP IPv6 target is invalid: %q", host)
		}
		ip := parsed.To16()
		source := windowsSockaddrIn6{Family: windowsAFINET6}
		destination := windowsSockaddrIn6{Family: windowsAFINET6}
		copy(destination.Address[:], ip)
		result, _, callErr = sendProc.Call(
			uintptr(handle), 0, 0, 0,
			uintptr(unsafe.Pointer(&source)), uintptr(unsafe.Pointer(&destination)),
			uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
			uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeoutMS),
		)
		runtime.KeepAlive(source)
		runtime.KeepAlive(destination)
	default:
		return icmpSample{}, fmt.Errorf("unsupported native ICMP address family %q", family)
	}
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)

	if result == 0 {
		if callErr != nil && !windowsICMPTimeoutError(callErr) {
			return icmpSample{}, callErr
		}
		return sample, nil
	}
	statusOffset, rttOffset := windowsICMPReplyOffsets(family)
	if len(reply) < rttOffset+4 {
		return icmpSample{}, fmt.Errorf("native ICMP reply buffer is too small")
	}
	status := binary.LittleEndian.Uint32(reply[statusOffset : statusOffset+4])
	if status != windowsIPSuccess {
		// The API completed the request but reported a protocol-level loss.
		// Keep it as a sent/no-reply sample rather than treating localized text
		// or an implementation-specific status as a parser failure.
		return sample, nil
	}
	rtt := binary.LittleEndian.Uint32(reply[rttOffset : rttOffset+4])
	sample.Received = true
	sample.RTTMS = float64(rtt)
	return sample, nil
}

type windowsSockaddrIn6 struct {
	Family   uint16
	Port     uint16
	FlowInfo uint32
	Address  [16]byte
	ScopeID  uint32
}

func windowsICMPReplyOffsets(family string) (status, rtt int) {
	if family == "6" {
		return windowsICMPEchoReplyV6Size - 8, windowsICMPEchoReplyV6Size - 4
	}
	return windowsICMPEchoReplyV4Size - 8, windowsICMPEchoReplyV4Size - 4
}

func windowsICMPTimeout(ctx context.Context, requested time.Duration) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if requested <= 0 {
		requested = time.Millisecond
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}
		if remaining < requested {
			requested = remaining
		}
	}
	milliseconds := uint64(requested / time.Millisecond)
	if requested%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds < 1 {
		milliseconds = 1
	}
	if milliseconds > uint64(^uint32(0)) {
		milliseconds = uint64(^uint32(0))
	}
	return uint32(milliseconds), nil
}

func windowsICMPTimeoutError(err error) bool {
	return err == windowsErrorTimeout || err == windowsIPRequestTimedOut
}

func windowsICMPCallError(operation string, err error) error {
	if err == nil {
		err = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func closeWindowsICMPHandle(handle syscall.Handle) {
	if handle == 0 || handle == syscall.InvalidHandle {
		return
	}
	_, _, _ = windowsIcmpCloseHandle.Call(uintptr(handle))
}
