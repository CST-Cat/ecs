package model

import (
	"encoding/binary"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
)

func RedactedCopy(in Report, reveal bool) Report {
	out := in
	out.Notices = cloneMessages(in.Notices)
	out.SensitiveIPs = append([]string(nil), in.SensitiveIPs...)
	out.Run.Requested = append([]string(nil), in.Run.Requested...)
	out.Run.OutputFormats = append([]string(nil), in.Run.OutputFormats...)
	out.Summary.Messages = cloneMessages(in.Summary.Messages)
	out.Results = make([]Result, len(in.Results))
	for i, result := range in.Results {
		out.Results[i] = result
		out.Results[i].SummaryMessages = cloneMessages(result.SummaryMessages)
		out.Results[i].Methodology.Parameters = cloneStringMap(result.Methodology.Parameters)
		if result.Evidence != nil {
			evidence := *result.Evidence
			out.Results[i].Evidence = &evidence
		}
		out.Results[i].Fields = append([]Field(nil), result.Fields...)
		out.Results[i].Measurements = cloneMeasurements(result.Measurements)
		out.Results[i].Failures = append([]Failure(nil), result.Failures...)
		out.Results[i].Notes = append([]string(nil), result.Notes...)
		out.Results[i].Sources = append([]Source(nil), result.Sources...)
		out.Results[i].TextBlocks = append([]TextBlock(nil), result.TextBlocks...)
		out.Results[i].Tables = make([]Table, len(result.Tables))
		for j, table := range result.Tables {
			out.Results[i].Tables[j] = table
			out.Results[i].Tables[j].Columns = append([]TableColumn(nil), table.Columns...)
			out.Results[i].Tables[j].Rows = make([][]Value, len(table.Rows))
			for k, row := range table.Rows {
				out.Results[i].Tables[j].Rows[k] = append([]Value(nil), row...)
			}
		}
		if !reveal {
			for j := range out.Results[i].Fields {
				if out.Results[i].Fields[j].Sensitive {
					if raw, ok := out.Results[i].Fields[j].Value.Raw(); ok {
						out.Results[i].Fields[j].Value = RawValue(Mask(raw))
					}
				}
			}
		}
	}
	if !reveal {
		redactExactLocalIPs(&out, collectSensitiveIPs(in))
	}
	// The allow-list is an internal redaction aid, not report data.
	out.SensitiveIPs = nil
	out.Run.Redacted = !reveal
	return out
}

func cloneMeasurements(measurements []Measurement) []Measurement {
	if measurements == nil {
		return nil
	}
	out := make([]Measurement, len(measurements))
	for index, measurement := range measurements {
		out[index] = measurement
		if measurement.HigherIsBetter != nil {
			direction := *measurement.HigherIsBetter
			out[index].HigherIsBetter = &direction
		}
	}
	return out
}

var (
	textIPv4Pattern = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,3})?`)
	textIPv6Pattern = regexp.MustCompile(`(?i)[0-9a-f:]{2,}(?:/\d{1,3})?`)
)

func Mask(value string) string {
	value = strings.TrimSpace(value)
	if ip := net.ParseIP(value); ip != nil {
		return maskIP(ip)
	}
	if _, network, err := net.ParseCIDR(value); err == nil {
		return maskCIDR(network)
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return net.JoinHostPort(maskIP(ip), port)
		}
	}
	if value == "" {
		return value
	}
	return "hidden"
}

func maskIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.x.x", v4[0], v4[1])
	}
	if v6 := ip.To16(); v6 != nil {
		return fmt.Sprintf(
			"%x:%x:x:x:x:x:x:x",
			binary.BigEndian.Uint16(v6[0:2]),
			binary.BigEndian.Uint16(v6[2:4]),
		)
	}
	return "hidden"
}

func maskCIDR(network *net.IPNet) string {
	if network == nil {
		return "hidden"
	}
	ones, _ := network.Mask.Size()
	if ones < 0 {
		return "hidden"
	}
	return fmt.Sprintf("%s/%d", maskIP(network.IP), ones)
}

func collectSensitiveIPs(report Report) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range report.SensitiveIPs {
		addIPsFromText(result, value)
	}
	for _, item := range report.Results {
		for _, field := range item.Fields {
			if field.Sensitive {
				addIPsFromText(result, field.Value.Text())
			}
		}
		for _, block := range item.TextBlocks {
			if block.Sensitive {
				addIPsFromText(result, block.Content)
			}
		}
		for _, table := range item.Tables {
			for columnIndex, column := range table.Columns {
				if !column.Sensitive {
					continue
				}
				for _, row := range table.Rows {
					if columnIndex >= 0 && columnIndex < len(row) {
						if raw, ok := row[columnIndex].Raw(); ok {
							addIPsFromText(result, raw)
						}
					}
				}
			}
		}
	}
	return result
}

func addIPsFromText(result map[string]struct{}, value string) {
	add := func(token string) string {
		base := token
		if slash := strings.IndexByte(base, '/'); slash >= 0 {
			base = base[:slash]
		}
		if ip := net.ParseIP(base); ip != nil {
			result[ip.String()] = struct{}{}
		}
		return token
	}
	textIPv4Pattern.ReplaceAllStringFunc(value, add)
	textIPv6Pattern.ReplaceAllStringFunc(value, add)
}

func maskSelectedIPsInText(value string, selected map[string]struct{}) string {
	if value == "" || len(selected) == 0 {
		return value
	}
	maskSelected := func(token string) string {
		base := token
		suffix := ""
		if slash := strings.IndexByte(base, '/'); slash >= 0 {
			base, suffix = base[:slash], base[slash:]
		}
		ip := net.ParseIP(base)
		if ip == nil {
			return token
		}
		if _, ok := selected[ip.String()]; !ok {
			return token
		}
		return maskIP(ip) + suffix
	}
	masked := textIPv4Pattern.ReplaceAllStringFunc(value, maskSelected)
	return textIPv6Pattern.ReplaceAllStringFunc(masked, maskSelected)
}

func redactExactLocalIPs(report *Report, selected map[string]struct{}) {
	if report == nil || len(selected) == 0 {
		return
	}
	redactStringValues(reflect.ValueOf(report).Elem(), selected)
}

// redactStringValues walks every exported string value in the report schema.
// Map keys are stable machine identifiers and are deliberately preserved.
func redactStringValues(value reflect.Value, selected map[string]struct{}) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			redactStringValues(value.Elem(), selected)
		}
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(Value{}) {
			if tagged, ok := value.Interface().(Value); ok {
				if raw, isRaw := tagged.Raw(); isRaw && value.CanSet() {
					value.Set(reflect.ValueOf(RawValue(maskSelectedIPsInText(raw, selected))))
				}
			}
			return
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.CanSet() {
				redactStringValues(field, selected)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			redactStringValues(value.Index(index), selected)
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key()
			item := iterator.Value()
			if item.Kind() == reflect.String {
				masked := reflect.New(item.Type()).Elem()
				masked.SetString(maskSelectedIPsInText(item.String(), selected))
				value.SetMapIndex(key, masked)
			}
		}
	case reflect.String:
		value.SetString(maskSelectedIPsInText(value.String(), selected))
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
