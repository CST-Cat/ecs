package model

import "testing"

func TestRedactedCopyMasksAndClonesRepresentativeNestedData(t *testing.T) {
	original := Report{Results: []Result{{
		Fields: []Field{{Key: "ip", Value: "203.0.113.10", Sensitive: true}},
		Tables: []Table{{Columns: []string{"value"}, Rows: [][]string{{"original"}}}},
	}}}
	redacted := RedactedCopy(original, false)
	if redacted.Results[0].Fields[0].Value != "203.0.x.x" || !redacted.Run.Redacted {
		t.Fatalf("redacted report = %+v", redacted.Results[0].Fields[0])
	}
	redacted.Results[0].Fields[0].Value = "changed"
	redacted.Results[0].Tables[0].Rows[0][0] = "changed"
	if original.Results[0].Fields[0].Value != "203.0.113.10" || original.Results[0].Tables[0].Rows[0][0] != "original" {
		t.Fatal("RedactedCopy shared or mutated source data")
	}
}

func TestEvidenceNormalizeDerivesGrade(t *testing.T) {
	evidence := Evidence{Valid: 8, Expected: 4, Grade: EvidenceInsufficient}
	evidence.Normalize()
	if evidence.Valid != 4 || evidence.Expected != 4 || evidence.Grade != EvidenceComplete || evidence.EvidenceRatio() != 1 {
		t.Fatalf("normalized evidence = %+v ratio=%f", evidence, evidence.EvidenceRatio())
	}
	partial := NewEvidence(1, 2, "run")
	if partial.Grade != EvidencePartial || partial.EvidenceRatio() != 0.5 {
		t.Fatalf("partial evidence = %+v ratio=%f", partial, partial.EvidenceRatio())
	}
}
