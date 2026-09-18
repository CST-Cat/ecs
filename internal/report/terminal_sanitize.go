package report

import (
	"reflect"
	"strings"

	comparison "ecs/internal/compare"
	"ecs/internal/model"
	"ecs/internal/score"
)

// These typed entry points are the complete supported surface for terminal
// sanitization. The shared reflection walker below is deliberately kept
// internal: renderers accept these report DTOs, not arbitrary object graphs.
func sanitizedReportCopy(value model.Report) model.Report {
	return cloneSupportedReportValue(reflect.ValueOf(value)).Interface().(model.Report)
}

func sanitizedComparisonCopy(value comparison.Report) comparison.Report {
	return cloneSupportedReportValue(reflect.ValueOf(value)).Interface().(comparison.Report)
}

func sanitizedScoreCopy(value *score.Report) *score.Report {
	if value == nil {
		return nil
	}
	return cloneSupportedReportValue(reflect.ValueOf(value)).Interface().(*score.Report)
}

// cloneSupportedReportValue returns a deep copy whose strings cannot carry terminal
// control sequences. Report fields can contain command output, remote labels,
// URLs and file names, so none of them are trusted at any rendering boundary.
// Map keys are copied verbatim: they are canonical identity, not presentation
// text. A renderer that emits a map key must sanitize it at that output
// boundary, after the copy has preserved the original identity.
//
// Every renderer runs this, not just the terminal one: a Markdown or HTML
// report is routinely read with cat or grep, where a surviving escape sequence
// executes just as it would in the text report. html/template escapes HTML
// metacharacters and markdownEscape escapes Markdown ones, but neither removes
// ESC or the remaining C0/C1 controls.
func cloneSupportedReportValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	if value.Type() == reflect.TypeOf(model.Value{}) {
		fieldValue := value.Interface().(model.Value)
		if raw, ok := fieldValue.Raw(); ok {
			return reflect.ValueOf(model.RawValue(sanitizeTerminalText(raw)))
		}
		if key, ok := fieldValue.Key(); ok {
			return reflect.ValueOf(model.KeyValue(sanitizeTerminalText(key)))
		}
		return value
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
		out.Elem().Set(cloneSupportedReportValue(value.Elem()))
		return out
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneSupportedReportValue(value.Elem()))
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
			out.Field(index).Set(cloneSupportedReportValue(value.Field(index)))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			out.Index(index).Set(cloneSupportedReportValue(value.Index(index)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			out.Index(index).Set(cloneSupportedReportValue(value.Index(index)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			// Never sanitize or deep-clone a map key. Changing a key while
			// copying can merge two canonical members and silently drop one.
			out.SetMapIndex(iterator.Key(), cloneSupportedReportValue(iterator.Value()))
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
