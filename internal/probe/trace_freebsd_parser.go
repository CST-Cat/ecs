package probe

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
)

// parseFreeBSDTraceroute parses the numeric, one-probe-per-hop output emitted
// by FreeBSD 15.1's traceroute(8)/traceroute6(8). It intentionally rejects
// hostname tokens: the execution adapter always passes -n, so accepting a
// hostname here would hide an unexpected change in the source contract.
func parseFreeBSDTraceroute(output, family, target string) (traceResult, error) {
	text := strings.ReplaceAll(output, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	wantHeader := "traceroute"
	if family == "6" || family == "ipv6" {
		wantHeader = "traceroute6"
	}
	headerSeen := false
	trace := traceResult{
		Engine:  "freebsd-traceroute",
		Family:  traceFamilyName(family),
		Target:  strings.TrimSpace(target),
		Adapter: traceFreeBSDTracerouteAdapter,
	}
	for lineNumber, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !headerSeen {
			if fields[0] == wantHeader {
				headerSeen = true
			}
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err != nil {
			continue
		}
		hop, err := parseFreeBSDTracerouteHop(fields, family)
		if err != nil {
			return traceResult{}, fmt.Errorf("FreeBSD traceroute line %d: %w", lineNumber+1, err)
		}
		if want := len(trace.Hops) + 1; hop.Hop != want {
			return traceResult{}, fmt.Errorf("FreeBSD traceroute hop sequence = %d at line %d, want %d", hop.Hop, lineNumber+1, want)
		}
		trace.Hops = append(trace.Hops, hop)
	}
	if !headerSeen {
		return traceResult{}, fmt.Errorf("FreeBSD traceroute header not found")
	}
	if len(trace.Hops) == 0 {
		return traceResult{}, fmt.Errorf("FreeBSD traceroute output contains no hop rows")
	}
	return trace, nil
}

func parseFreeBSDTracerouteHop(fields []string, family string) (traceHop, error) {
	if len(fields) < 2 {
		return traceHop{}, fmt.Errorf("hop row is missing probe fields")
	}
	hopNumber, err := strconv.Atoi(fields[0])
	if err != nil || hopNumber <= 0 {
		return traceHop{}, fmt.Errorf("invalid hop number %q", fields[0])
	}
	hop := traceHop{Hop: hopNumber}
	for index := 1; index < len(fields); index++ {
		token := strings.TrimSpace(fields[index])
		if token == "" || token == "*" {
			continue
		}
		if strings.HasPrefix(token, "!") {
			// FreeBSD documents !H, !N, !P and other ! annotations after
			// a response. Keep the annotation as source syntax but do not
			// invent a new canonical status field for it.
			continue
		}
		address := normalizeTraceAddress(token)
		if address != "" {
			parsed := net.ParseIP(address)
			if parsed == nil {
				return traceHop{}, fmt.Errorf("invalid address token %q", token)
			}
			if family == "4" || family == "ipv4" {
				if parsed.To4() == nil {
					return traceHop{}, fmt.Errorf("IPv6 address %q in IPv4 trace", token)
				}
			} else if family == "6" || family == "ipv6" {
				if parsed.To4() != nil {
					return traceHop{}, fmt.Errorf("IPv4 address %q in IPv6 trace", token)
				}
			}
			if !hop.Responded {
				hop.IP = address
				hop.Responded = true
			}
			continue
		}
		if rtt, ok := parseFreeBSDRTTToken(token); ok {
			if hop.RTTMS == nil {
				hop.RTTMS = rtt
			}
			if index+1 < len(fields) && strings.EqualFold(fields[index+1], "ms") {
				index++
			}
			continue
		}
		if strings.EqualFold(token, "ms") {
			return traceHop{}, fmt.Errorf("RTT unit without a numeric value")
		}
		return traceHop{}, fmt.Errorf("unexpected non-numeric hop token %q (hostname or malformed annotation)", token)
	}
	return hop, nil
}

func parseFreeBSDRTTToken(token string) (*float64, bool) {
	trimmed := strings.TrimSpace(token)
	if strings.HasSuffix(strings.ToLower(trimmed), "ms") {
		trimmed = strings.TrimSpace(trimmed[:len(trimmed)-2])
	}
	if trimmed == "" {
		return nil, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, false
	}
	return &value, true
}
