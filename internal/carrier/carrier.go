// Package carrier defines the canonical carrier identities shared by
// configuration parsing and provider probes.
package carrier

import "strings"

type ID string

const (
	Telecom ID = "telecom"
	Unicom  ID = "unicom"
	Mobile  ID = "mobile"
)

// Parse returns the canonical identity for a known provider alias.
func Parse(raw string) (ID, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "电信", "中国电信", "telecom", "ct", "chinatelecom", "china telecom":
		return Telecom, true
	case "联通", "中国联通", "unicom", "cu", "chinaunicom", "china unicom":
		return Unicom, true
	case "移动", "中国移动", "mobile", "cm", "chinamobile", "china mobile":
		return Mobile, true
	default:
		return "", false
	}
}
