package report

import (
	"reflect"
	"strings"
)

// terminalSafeCopy returns a deep copy whose strings cannot carry terminal
// control sequences. Report fields can contain command output, remote labels,
// URLs and file names, so none of them are trusted at the text-rendering
// boundary. Map keys are sanitized as well because some renderers display
// parameter names.
func terminalSafeCopy[T any](value T) T {
	cloned := cloneTerminalValue(reflect.ValueOf(value))
	if !cloned.IsValid() {
		var zero T
		return zero
	}
	return cloned.Interface().(T)
}

func cloneTerminalValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}

	switch value.Kind() {
	case reflect.String:
		out := reflect.New(value.Type()).Elem()
		out.SetString(sanitizeTerminalText(value.String()))
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneTerminalValue(value.Elem()))
		return out
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneTerminalValue(value.Elem()))
		return out
	case reflect.Struct:
		// Copy the complete value first so opaque standard-library structs such
		// as time.Time retain their private representation. Exported fields are
		// then recursively replaced with sanitized deep copies.
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath != "" {
				continue
			}
			out.Field(index).Set(cloneTerminalValue(value.Field(index)))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			out.Index(index).Set(cloneTerminalValue(value.Index(index)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			out.Index(index).Set(cloneTerminalValue(value.Index(index)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			out.SetMapIndex(cloneTerminalValue(iterator.Key()), cloneTerminalValue(iterator.Value()))
		}
		return out
	default:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		return out
	}
}

// sanitizeTerminalText replaces every C0, DEL and C1 control character with
// a plain space. Removing ESC makes CSI/OSC payloads inert; removing CR, LF,
// TAB, backspace and their C1 counterparts also prevents layout spoofing.
// Consecutive controls collapse to one space to keep diagnostics readable.
func sanitizeTerminalText(value string) string {
	if !strings.ContainsFunc(value, terminalControlRune) {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	lastWasControl := false
	for _, character := range value {
		if terminalControlRune(character) {
			if !lastWasControl {
				out.WriteByte(' ')
			}
			lastWasControl = true
			continue
		}
		out.WriteRune(character)
		lastWasControl = false
	}
	return out.String()
}

func terminalControlRune(character rune) bool {
	return character <= 0x1f || (character >= 0x7f && character <= 0x9f)
}
