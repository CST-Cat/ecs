package probe

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var latencyPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*ms`)

// parseNextTraceCanonical is the sole adapter for the frozen NextTrace Tiny
// native JSON format. Both route and backtrace consume the resulting ECS
// facts; neither consumer parses native JSON independently.
func parseNextTraceCanonical(output, family, target string) (traceResult, error) {
	var payload struct {
		Hops [][]json.RawMessage `json:"Hops"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil || len(payload.Hops) == 0 {
		return traceResult{}, fmt.Errorf("NextTrace output contains no hop slots")
	}
	trace := traceResult{
		Engine:  traceNextTraceEngineName,
		Family:  traceFamilyName(family),
		Target:  target,
		Adapter: traceNextTraceAdapter,
		Hops:    make([]traceHop, 0, len(payload.Hops)),
	}
	for index, probes := range payload.Hops {
		hop := traceHop{
			Hop: index + 1,
		}
		for _, rawProbe := range probes {
			probe := map[string]json.RawMessage{}
			if json.Unmarshal(rawProbe, &probe) != nil {
				var scalar string
				if json.Unmarshal(rawProbe, &scalar) == nil {
					if address := normalizeTraceAddress(scalar); address != "" {
						hop.IP = address
						hop.Responded = true
						break
					}
				}
				continue
			}
			address := normalizeTraceAddress(jsonRawAddress(jsonMapRawValue(probe, "Address", "IP", "Ip")))
			if address == "" {
				if scalar := jsonRawString(rawProbe); scalar != "" {
					address = normalizeTraceAddress(scalar)
				}
			}
			if address == "" {
				continue
			}
			hop.IP = address
			hop.Responded = true
			hop.ASN = normalizeTraceASN(traceProbeValue(probe, "ASN", "ASNumber", "AS", "ASNO", "Asnumber"))
			hop.Network = strings.TrimSpace(traceNetworkValue(probe))
			hop.Location = traceLocation(probe)
			if rtt, ok := traceRTTMS(jsonMapString(probe, "RTT", "Latency", "Delay", "Time", "AvgRTT")); ok {
				hop.RTTMS = rtt
			}
			break
		}
		trace.Hops = append(trace.Hops, hop)
	}
	return trace, nil
}

// The JSON helpers below belong to the NextTrace adapter. They intentionally
// return canonical trace fields (not backtrace presentation structs), so the
// adapter remains the only native-format parser in production code.
func jsonMapRawValue(values map[string]json.RawMessage, names ...string) json.RawMessage {
	raw, _ := jsonMapRaw(values, names...)
	return raw
}

func jsonRawAddress(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if value := jsonRawString(raw); value != "" {
		return value
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(raw, &nested) != nil {
		return ""
	}
	return jsonRawString(jsonMapRawValue(nested, "IP", "Ip", "Address", "Addr"))
}

func traceProbeValue(probe map[string]json.RawMessage, names ...string) string {
	if value := jsonMapString(probe, names...); value != "" {
		return value
	}
	geoRaw, ok := jsonMapRaw(probe, "Geo")
	if !ok {
		return ""
	}
	var geo map[string]json.RawMessage
	if json.Unmarshal(geoRaw, &geo) != nil {
		return ""
	}
	return jsonMapString(geo, names...)
}

// traceNetworkValue accepts only fields whose source contract describes a
// network or organization. Reverse-DNS PTR/Host/Hostname values are observed
// host labels, not network identity, and must not be placed in Network.
func traceNetworkValue(probe map[string]json.RawMessage) string {
	if value := jsonMapString(probe, "ASName", "Organization", "Org", "ISP", "Isp", "Network"); value != "" {
		return value
	}
	if geoRaw, ok := jsonMapRaw(probe, "Geo"); ok {
		var geo map[string]json.RawMessage
		if json.Unmarshal(geoRaw, &geo) == nil {
			if value := jsonMapString(geo, "ASName", "Organization", "Org", "ISP", "Isp", "Network", "Owner"); value != "" {
				return value
			}
		}
	}
	return jsonMapString(probe, "Owner")
}

func jsonMapString(values map[string]json.RawMessage, names ...string) string {
	raw, ok := jsonMapRaw(values, names...)
	if !ok {
		return ""
	}
	return jsonRawString(raw)
}

func jsonMapRaw(values map[string]json.RawMessage, names ...string) (json.RawMessage, bool) {
	for _, name := range names {
		for key, raw := range values {
			if strings.EqualFold(key, name) {
				return raw, true
			}
		}
	}
	return nil, false
}

func jsonRawString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		for _, item := range values {
			if value := jsonRawString(item); value != "" {
				return value
			}
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for _, key := range []string{"Name", "name", "City", "city", "Location", "location", "Value", "value", "IP", "Ip", "Address", "Addr"} {
			if item, ok := object[key]; ok {
				if value := jsonRawString(item); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func normalizeTraceAddress(value string) string {
	value = strings.TrimSpace(value)
	if zone := strings.LastIndexByte(value, '%'); zone >= 0 {
		value = value[:zone]
	}
	if parsed := net.ParseIP(strings.Trim(value, "[](),<>")); parsed != nil {
		return parsed.String()
	}
	return ""
}

func normalizeTraceLatency(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if match := latencyPattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + " ms"
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		// NextTrace serializes net.Duration RTT values as nanoseconds. Small
		// numeric RTT values are emitted as milliseconds by some Tiny builds.
		if number >= 1000 {
			number /= float64(time.Millisecond)
		}
		return strconv.FormatFloat(number, 'f', -1, 64) + " ms"
	}
	return value
}

func normalizeTraceASN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(value), "AS") {
		return value
	}
	if number, err := strconv.Atoi(value); err == nil && number > 0 {
		return "AS" + strconv.Itoa(number)
	}
	return value
}

func traceLocation(values map[string]json.RawMessage) string {
	parts := traceLocationParts(values)
	if len(parts) > 0 {
		return strings.Join(parts, " / ")
	}
	for _, name := range []string{"Location", "Geo"} {
		raw, ok := jsonMapRaw(values, name)
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			if nestedParts := traceLocationParts(nested); len(nestedParts) > 0 {
				return strings.Join(nestedParts, " / ")
			}
		}
	}
	if value := strings.TrimSpace(jsonMapString(values, "Location", "Geo", "Region")); value != "" {
		return value
	}
	lat := strings.TrimSpace(jsonMapString(values, "lat", "latitude"))
	lng := strings.TrimSpace(jsonMapString(values, "lng", "lon", "longitude"))
	if lat != "" && lng != "" {
		return lat + ", " + lng
	}
	return ""
}

func traceLocationParts(values map[string]json.RawMessage) []string {
	parts := make([]string, 0, 4)
	for _, name := range []string{"Country", "country", "Prov", "prov", "State", "state", "City", "city", "District", "district"} {
		if value := strings.TrimSpace(jsonMapString(values, name)); value != "" {
			duplicate := false
			for _, existing := range parts {
				if strings.EqualFold(existing, value) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				parts = append(parts, value)
			}
		}
	}
	return parts
}
