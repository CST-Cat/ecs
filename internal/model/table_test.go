package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTableJSONUsesTypedColumnsAndValueRows(t *testing.T) {
	original := Table{
		Key:   "network.state",
		Title: "Network state",
		Columns: []TableColumn{
			{Key: "id", Label: "ID"},
			{Key: "status", Label: "Status", Sensitive: true},
		},
		Rows:        [][]Value{{RawValue("row-1"), KeyValue("probe.network.status.ok")}},
		RowIdentity: "id",
	}
	content, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"key":"network.state","title":"Network state","columns":[{"key":"id","label":"ID"},{"key":"status","label":"Status","sensitive":true}],"rows":[[{"raw":"row-1"},{"key":"probe.network.status.ok"}]],"row_identity":"id"}`
	if string(content) != want {
		t.Fatalf("table JSON = %s, want %s", content, want)
	}
	var decoded Table
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("decoded table = %#v, want %#v", decoded, original)
	}
}

func TestTableRejectsLegacyShapeAndInvalidStructure(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "legacy string cell", input: `{"columns":[{"key":"id","label":"ID"}],"rows":[["legacy"]]}`, want: "tagged object"},
		{name: "legacy columns", input: `{"columns":["id"],"rows":[]}`, want: "cannot unmarshal string"},
		{name: "legacy parallel metadata", input: `{"columns":[{"key":"id","label":"ID"}],"column_keys":["id"],"rows":[[{"raw":"one"}]]}`, want: "unknown field"},
		{name: "unknown column metadata", input: `{"columns":[{"key":"id","label":"ID","old":true}],"rows":[[{"raw":"one"}]]}`, want: "unknown field"},
		{name: "wrong row width", input: `{"columns":[{"key":"id","label":"ID"}],"rows":[[]]}`, want: "row 0"},
		{name: "unknown row value field", input: `{"columns":[{"key":"id","label":"ID"}],"rows":[[{"extra":"x"}]]}`, want: "unknown tag"},
		{name: "row identity missing", input: `{"columns":[{"key":"id","label":"ID"}],"rows":[],"row_identity":"missing"}`, want: "does not name a column"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var table Table
			err := json.Unmarshal([]byte(test.input), &table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("json.Unmarshal error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestTableValidateRejectsDuplicateColumnKeys(t *testing.T) {
	table := Table{Columns: []TableColumn{{Key: "id"}, {Key: "id"}}}
	if err := table.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Validate error = %v", err)
	}
	if _, err := json.Marshal(table); err == nil {
		t.Fatal("invalid table marshaled successfully")
	}
}

func TestTableRejectsEmptyColumnKey(t *testing.T) {
	table := Table{Columns: []TableColumn{{Key: "", Label: "ID"}}}
	if err := table.Validate(); err == nil || !strings.Contains(err.Error(), "column key") {
		t.Fatalf("Validate error = %v", err)
	}
	if _, err := json.Marshal(table); err == nil || !strings.Contains(err.Error(), "column key") {
		t.Fatalf("json.Marshal error = %v", err)
	}
	var decoded Table
	input := `{"columns":[{"key":"","label":"ID"}],"rows":[[{"raw":"one"}]]}`
	if err := json.Unmarshal([]byte(input), &decoded); err == nil || !strings.Contains(err.Error(), "column key") {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
}
