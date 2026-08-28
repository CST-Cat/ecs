package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type valueVariant uint8

const (
	valueRaw valueVariant = iota
	valueKey
)

// Value is a canonical value with an explicit raw or stable-key variant.
// The representation is private so callers cannot manufacture another
// variant or infer semantics from the stored text.
type Value struct {
	variant valueVariant
	text    string
}

// RawValue stores literal provider output, formatted text, or diagnostics.
func RawValue(text string) Value {
	return Value{variant: valueRaw, text: text}
}

// KeyValue stores a stable ECS machine or presentation key without resolving
// it to a language.
func KeyValue(key string) Value {
	return Value{variant: valueKey, text: key}
}

// Raw returns the stored text when this value has the raw variant.
func (value Value) Raw() (string, bool) {
	return value.text, value.variant == valueRaw
}

// Key returns the stored key when this value has the key variant.
func (value Value) Key() (string, bool) {
	return value.text, value.variant == valueKey
}

// Text returns the stored text without translating or interpreting it.
func (value Value) Text() string {
	return value.text
}

func (value Value) MarshalJSON() ([]byte, error) {
	switch value.variant {
	case valueRaw:
		return json.Marshal(struct {
			Raw string `json:"raw"`
		}{Raw: value.text})
	case valueKey:
		if value.text == "" {
			return nil, fmt.Errorf("model value key must not be empty")
		}
		return json.Marshal(struct {
			Key string `json:"key"`
		}{Key: value.text})
	default:
		return nil, fmt.Errorf("model value has unknown variant %d", value.variant)
	}
}

func (value *Value) UnmarshalJSON(data []byte) error {
	if value == nil {
		return fmt.Errorf("model value: nil receiver")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("model value must be a tagged object: %w", err)
	}
	object, ok := first.(json.Delim)
	if !ok || object != '{' {
		return fmt.Errorf("model value must be a tagged object")
	}

	var (
		variant valueVariant
		text    string
		seen    bool
	)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("model value tag: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("model value tag must be a string")
		}
		if seen {
			return fmt.Errorf("model value must contain exactly one tag")
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return fmt.Errorf("model value %q: %w", key, err)
		}
		if key != "raw" && key != "key" {
			return fmt.Errorf("model value has unknown tag %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("model value %q must be a string", key)
		}
		if err := json.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("model value %q must be a string: %w", key, err)
		}
		if key == "key" {
			if text == "" {
				return fmt.Errorf("model value key must not be empty")
			}
			variant = valueKey
		} else {
			variant = valueRaw
		}
		seen = true
	}

	last, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("model value object: %w", err)
	}
	if object, ok := last.(json.Delim); !ok || object != '}' {
		return fmt.Errorf("model value must be a tagged object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("model value must contain one object")
		}
		return fmt.Errorf("model value trailing data: %w", err)
	}
	if !seen {
		return fmt.Errorf("model value must contain exactly one tag")
	}
	*value = Value{variant: variant, text: text}
	return nil
}
