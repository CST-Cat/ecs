package model

import (
	"fmt"
	"math"
)

func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func FormatRate(value float64, unit string) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "n/a"
	}
	switch {
	case math.Abs(value) >= 1000:
		return fmt.Sprintf("%.0f %s", value, unit)
	case math.Abs(value) >= 100:
		return fmt.Sprintf("%.1f %s", value, unit)
	default:
		return fmt.Sprintf("%.2f %s", value, unit)
	}
}

func BoolPtr(value bool) *bool {
	return &value
}
