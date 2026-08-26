package model

// DerivedGrade computes the canonical grade from normalized counters instead
// of trusting a stale serialized label.
func (e Evidence) DerivedGrade() EvidenceGrade {
	switch {
	case e.Expected <= 0:
		return EvidenceNotPlanned
	case e.Valid >= e.Expected:
		return EvidenceComplete
	case e.Valid > 0:
		return EvidencePartial
	default:
		return EvidenceInsufficient
	}
}

// EffectiveGrade deliberately ignores a stale serialized grade.
func (e Evidence) EffectiveGrade() EvidenceGrade { return e.DerivedGrade() }

// Normalize clamps malformed counters and refreshes the derived grade.
func (e *Evidence) Normalize() {
	if e == nil {
		return
	}
	if e.Valid < 0 {
		e.Valid = 0
	}
	if e.Expected < 0 {
		e.Expected = 0
	}
	if e.Expected == 0 {
		e.Valid = 0
	} else if e.Valid > e.Expected {
		e.Valid = e.Expected
	}
	e.Grade = e.DerivedGrade()
}

// EvidenceRatio returns a renderer-safe coverage ratio in [0, 1].
func (e Evidence) EvidenceRatio() float64 {
	if e.Expected <= 0 || e.Valid <= 0 {
		return 0
	}
	valid := e.Valid
	if valid > e.Expected {
		valid = e.Expected
	}
	return float64(valid) / float64(e.Expected)
}

// NewEvidence normalizes counters at the probe boundary.
func NewEvidence(valid, expected int, unit string) *Evidence {
	evidence := &Evidence{Valid: valid, Expected: expected, Unit: unit}
	evidence.Normalize()
	return evidence
}
