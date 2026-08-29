package model

// DerivedGrade computes the canonical grade from the current counters.
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

// Normalize clamps malformed counters to the persisted machine-fact boundary.
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
