//go:build linux

package probe

import (
	"reflect"
	"testing"
	"time"
)

func TestLinuxPingArgumentsKeepIputilsContract(t *testing.T) {
	for _, test := range []struct {
		name    string
		family  string
		timeout time.Duration
		want    []string
	}{
		{name: "IPv4", family: "4", timeout: 500 * time.Millisecond, want: []string{"-n", "-q", "-c", "3", "-W", "1", "-4", "host"}},
		{name: "IPv6", family: "6", timeout: time.Second, want: []string{"-n", "-q", "-c", "3", "-W", "1", "-6", "host"}},
		{name: "auto", timeout: 0, want: []string{"-n", "-q", "-c", "3", "-W", "1", "host"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pingArgumentsForFamily("host", 3, test.timeout, test.family); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Linux ping args = %#v, want %#v", got, test.want)
			}
		})
	}
	if pingCommand != "ping" {
		t.Fatalf("Linux ping command = %q, want frozen tool ID ping", pingCommand)
	}
}
