//go:build linux || freebsd

package probe

import "testing"

func TestParseDiskDFFields(t *testing.T) {
	for _, test := range []struct {
		name      string
		fields    []string
		wantOK    bool
		wantUsage float64
		wantUsed  uint64
		wantFree  uint64
	}{
		{name: "normal", fields: []string{"/dev/sda", "100", "40", "60", "40%", "/mnt"}, wantOK: true, wantUsage: 40, wantUsed: 40 * 1024, wantFree: 60 * 1024},
		{name: "short", fields: []string{"/dev/sda", "100"}},
		{name: "clamped", fields: []string{"/dev/sda", "100", "150", "200", "200%", "/mnt"}, wantOK: true, wantUsage: 100, wantUsed: 100 * 1024},
		{name: "percentage without total", fields: []string{"/dev/sda", "0", "0", "0", "37.5%", "/mnt"}, wantOK: true, wantUsage: 37.5},
		{name: "invalid blocks", fields: []string{"/dev/sda", "100", "not-a-number", "60", "40%", "/mnt"}},
		{name: "invalid percentage without total", fields: []string{"/dev/sda", "0", "0", "0", "not-a-percent", "/mnt"}},
		{name: "overflow blocks", fields: []string{"/dev/sda", "18446744073709551615", "1", "1", "1%", "/mnt"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseDiskDFFields(test.fields)
			if ok != test.wantOK || (ok && (got.DiskUsage != test.wantUsage || got.DiskUsed != test.wantUsed || got.DiskFree != test.wantFree)) {
				t.Fatalf("df parse = %+v/%v", got, ok)
			}
		})
	}
}
