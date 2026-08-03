package probe

import "testing"

func TestParseDiskDFFieldsReportsDeviceUsageAndMount(t *testing.T) {
	parsed, ok := parseDiskDFFields([]string{"/dev/vda1", "100000", "29000", "64000", "29%", "/root"})
	if !ok {
		t.Fatal("df fields should parse")
	}
	if parsed.DiskDevice != "/dev/vda1" || parsed.DiskMount != "/root" {
		t.Fatalf("device/mount = %q/%q", parsed.DiskDevice, parsed.DiskMount)
	}
	if parsed.DiskTotal != 100000*1024 || parsed.DiskUsed != 29000*1024 || parsed.DiskFree != 64000*1024 {
		t.Fatalf("disk bytes = %d/%d/%d", parsed.DiskTotal, parsed.DiskUsed, parsed.DiskFree)
	}
	if parsed.DiskUsage < 28.9 || parsed.DiskUsage > 29.1 {
		t.Fatalf("disk usage = %f", parsed.DiskUsage)
	}
}
