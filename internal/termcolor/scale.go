package termcolor

import "math"

// relativeLogThreshold is deliberately high enough to keep ordinary
// comparisons linear.  Once the smallest and largest positive values have a
// wide dynamic range, a linear eight-cell bar rounds all middle values to the
// first cell and stops carrying useful information.
const relativeLogThreshold = 32.0

// relativeRatio maps a value into a group's 0..1 range.  It uses the ordinary
// linear ratio for compact ranges and a logarithmic ratio for wide positive
// ranges, so values such as 4, 1000 and 10000 remain visually distinguishable
// in a short terminal bar.  A non-zero value always receives a positive ratio
// (Bar then gives it its minimum one-cell marker).
//
// Values outside the supplied range are intentionally not clamped here:
// Palette.Bar can add its overflow marker when a caller supplies a value above
// the observed maximum.
func relativeRatio(value, groupMin, groupMax float64) float64 {
	if math.IsNaN(value) || math.IsNaN(groupMin) || math.IsNaN(groupMax) ||
		math.IsInf(value, 0) || math.IsInf(groupMin, 0) || math.IsInf(groupMax, 0) || groupMax <= 0 {
		return 0
	}
	if value <= 0 {
		return 0
	}
	if groupMin > 0 && groupMax > groupMin {
		span := math.Log(groupMax) - math.Log(groupMin)
		if span > 0 && !math.IsInf(span, 0) && groupMax/groupMin >= relativeLogThreshold {
			ratio := (math.Log(value) - math.Log(groupMin)) / span
			if ratio > 0 {
				return ratio
			}
			return math.SmallestNonzeroFloat64
		}
	}
	ratio := value / groupMax
	if ratio > 0 {
		return ratio
	}
	return math.SmallestNonzeroFloat64
}

// BarRelativeRange renders a relative bar with both endpoints available for
// choosing a stable scale.
func (p Palette) BarRelativeRange(value, groupMin, groupMax float64, width int) string {
	// relativeRatio already returns 0 for a non-positive or NaN maximum.
	return p.Bar(relativeRatio(value, groupMin, groupMax), width)
}
