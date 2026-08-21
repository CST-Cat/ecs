package compare

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredNoticeJSONContract(t *testing.T) {
	notice := canonicalNotice("compare.notice.schemaMixed", "ecs.report/v1, ecs.report/v2")
	content, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, `"key":"compare.notice.schemaMixed"`) || !strings.Contains(got, `"args":["ecs.report/v1, ecs.report/v2"]`) {
		t.Fatalf("structured notice JSON = %s", got)
	}
	if strings.Contains(got, "::") {
		t.Fatalf("structured notice unexpectedly contains legacy string encoding: %s", got)
	}
	var decoded Notice
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Key != notice.Key || len(decoded.Args) != 1 || decoded.Args[0] != notice.Args[0] {
		t.Fatalf("structured notice round trip = %#v, want %#v", decoded, notice)
	}
}

func TestComparisonSchemaVersionStaysV1DuringBeta(t *testing.T) {
	if SchemaVersion != "ecs.compare/v1" {
		t.Fatalf("comparison schema version = %q, want ecs.compare/v1", SchemaVersion)
	}
}
