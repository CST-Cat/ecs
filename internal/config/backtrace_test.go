package config

import (
	"reflect"
	"testing"
)

func TestParseBacktraceCities(t *testing.T) {
	defaults, err := ParseBacktraceCities("")
	if err != nil || !reflect.DeepEqual(defaults, defaultBacktraceCities) {
		t.Fatalf("empty city selection = %v, %v", defaults, err)
	}
	selected, err := ParseBacktraceCities("shanghai")
	if err != nil || len(selected) != 1 || selected[0] != "shanghai" {
		t.Fatalf("selected city = %v, %v", selected, err)
	}
}

func TestBacktraceTargetsFor(t *testing.T) {
	targets := BacktraceTargetsFor([]string{"chengdu", "beijing"})
	if len(targets) == 0 {
		t.Fatal("selected cities should produce backtrace targets")
	}
	for _, target := range targets {
		if target.Name == "" || target.Address == "" || target.Kind == "" {
			t.Fatalf("incomplete backtrace target: %+v", target)
		}
	}
}
