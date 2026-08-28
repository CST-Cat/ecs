package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValueJSONRoundTripPreservesVariant(t *testing.T) {
	for _, test := range []struct {
		name  string
		value Value
		json  string
	}{
		{name: "zero raw", value: Value{}, json: `{"raw":""}`},
		{name: "raw", value: RawValue("provider output"), json: `{"raw":"provider output"}`},
		{name: "key", value: KeyValue("probe.status.ok"), json: `{"key":"probe.status.ok"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.json {
				t.Fatalf("JSON = %s, want %s", encoded, test.json)
			}
			var decoded Value
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, test.value) {
				t.Fatalf("decoded = %#v, want %#v", decoded, test.value)
			}
		})
	}
}

func TestValueAccessorsKeepRawAndKeyDistinct(t *testing.T) {
	raw := RawValue("probe.status.ok")
	if text, ok := raw.Raw(); !ok || text != "probe.status.ok" {
		t.Fatalf("raw accessor = %q, %v", text, ok)
	}
	if _, ok := raw.Key(); ok {
		t.Fatal("raw value exposed as key")
	}
	key := KeyValue("probe.status.ok")
	if text, ok := key.Key(); !ok || text != "probe.status.ok" {
		t.Fatalf("key accessor = %q, %v", text, ok)
	}
	if _, ok := key.Raw(); ok {
		t.Fatal("key value exposed as raw")
	}
	if raw.Text() != key.Text() {
		t.Fatal("Text did not preserve literal text")
	}
}

func TestValueRejectsNonCanonicalJSON(t *testing.T) {
	for _, input := range []string{
		`"legacy"`,
		`null`,
		`[]`,
		`{}`,
		`{"raw":"x","key":"y"}`,
		`{"other":"x"}`,
		`{"raw":1}`,
		`{"raw":null}`,
		`{"key":""}`,
		`{"raw":"x","raw":"y"}`,
		`{"raw":"x"} {"raw":"y"}`,
	} {
		var value Value
		if err := json.Unmarshal([]byte(input), &value); err == nil {
			t.Errorf("json.Unmarshal(%s) unexpectedly succeeded: %#v", input, value)
		}
	}
}

func TestValueRejectsEmptyKeyOnMarshal(t *testing.T) {
	if _, err := json.Marshal(KeyValue("")); err == nil {
		t.Fatal("empty key marshaled successfully")
	}
}
