package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Validate checks the structural contract shared by table producers and
// consumers. A row cell is addressed by its position in Columns, so a width
// mismatch is invalid rather than an invitation to silently shift cells.
func (table Table) Validate() error {
	seenKeys := make(map[string]int, len(table.Columns))
	for index, column := range table.Columns {
		if column.Key == "" {
			return fmt.Errorf("table column key at index %d must not be empty", index)
		}
		if previous, ok := seenKeys[column.Key]; ok {
			return fmt.Errorf("table column key %q is duplicated at indexes %d and %d", column.Key, previous, index)
		}
		seenKeys[column.Key] = index
	}
	if table.RowIdentity != "" {
		if _, ok := seenKeys[table.RowIdentity]; !ok {
			return fmt.Errorf("table row identity %q does not name a column", table.RowIdentity)
		}
	}
	for index, row := range table.Rows {
		if len(row) != len(table.Columns) {
			return fmt.Errorf("table row %d has %d cells, want %d", index, len(row), len(table.Columns))
		}
	}
	return nil
}

func (table Table) MarshalJSON() ([]byte, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}
	type tableJSON Table
	return json.Marshal(tableJSON(table))
}

func (table *Table) UnmarshalJSON(data []byte) error {
	if table == nil {
		return fmt.Errorf("model table: nil receiver")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("model table must be a JSON object")
	}

	type tableJSON Table
	var decoded tableJSON
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("model table: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("model table must contain one JSON object")
		}
		return fmt.Errorf("model table trailing data: %w", err)
	}
	validated := Table(decoded)
	if err := validated.Validate(); err != nil {
		return err
	}
	*table = validated
	return nil
}
