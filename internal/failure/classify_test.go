package failure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"syscall"
	"testing"

	"ecs/internal/model"
)

func TestClassifyTypedErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		category  model.FailureCategory
		retryable bool
	}{
		{name: "cancelled", err: context.Canceled, category: model.FailureCanceled},
		{name: "timeout", err: context.DeadlineExceeded, category: model.FailureTimeout, retryable: true},
		{name: "tool", err: exec.ErrNotFound, category: model.FailureToolMissing},
		{name: "permission", err: syscall.EACCES, category: model.FailurePermissionDenied},
		{name: "refused wrapped", err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), category: model.FailureConnectionRefused, retryable: true},
		{name: "unreachable", err: syscall.ENETUNREACH, category: model.FailureNetworkUnreachable, retryable: true},
		{name: "dns", err: &net.DNSError{Err: "no such host", Name: "example.invalid"}, category: model.FailureDNS},
		{name: "json", err: &json.SyntaxError{Offset: 3}, category: model.FailureParse},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := Classify(testCase.err)
			if got.Category != testCase.category || got.Retryable != testCase.retryable {
				t.Fatalf("Classify(%v) = %+v, want %s retryable=%v", testCase.err, got, testCase.category, testCase.retryable)
			}
		})
	}
}

func TestClassifyExternalMessagesKeepsStableCategoriesDistinct(t *testing.T) {
	tests := []struct {
		message   string
		category  model.FailureCategory
		retryable bool
	}{
		{"HTTP status 429: timeout from quota service", model.FailureRateLimited, true},
		{"HTTP 503 Service Unavailable", model.FailureHTTPRejected, true},
		{"HTTP 403 Forbidden", model.FailureHTTPRejected, false},
		{"JSON 解析失败: unexpected EOF", model.FailureParse, false},
		{"DNS 解析失败: no such host", model.FailureDNS, true},
		{"x509 certificate signed by unknown authority", model.FailureTLS, false},
		{"executable file not found in PATH", model.FailureToolMissing, false},
		{"operation not supported on this architecture", model.FailureUnsupported, false},
	}
	for _, testCase := range tests {
		got := FromMessage("fetch", "target", testCase.message)
		if got.Category != testCase.category || got.Retryable != testCase.retryable || got.Stage != "fetch" || got.Target != "target" {
			t.Errorf("FromMessage(%q) = %+v", testCase.message, got)
		}
	}
}

func TestEnsureResultNormalizesEvidenceAndAvoidsInventingFailures(t *testing.T) {
	semanticWarning := model.Result{
		ID: "nat", Status: model.StatusWarning, Summary: "检测到对称 NAT",
		Evidence: &model.Evidence{Valid: 5, Expected: 3, Grade: model.EvidenceInsufficient},
	}
	EnsureResult(&semanticWarning)
	if len(semanticWarning.Failures) != 0 {
		t.Fatalf("semantic warning gained fabricated failure: %+v", semanticWarning.Failures)
	}
	if semanticWarning.Evidence.Valid != 3 || semanticWarning.Evidence.Grade != model.EvidenceComplete {
		t.Fatalf("evidence was not normalized: %+v", semanticWarning.Evidence)
	}

	failed := model.Result{ID: "dns", Status: model.StatusError, Error: "query timed out"}
	EnsureResult(&failed)
	if len(failed.Failures) != 1 || failed.Failures[0].Category != model.FailureTimeout {
		t.Fatalf("module error failure = %+v", failed.Failures)
	}
	EnsureResult(&failed)
	if len(failed.Failures) != 1 {
		t.Fatalf("EnsureResult duplicated failure: %+v", failed.Failures)
	}
}

func TestClassifyNilAndOpaqueErrorsAsUnknown(t *testing.T) {
	for _, err := range []error{nil, errors.New("something unusual happened")} {
		if got := Classify(err); got.Category != model.FailureUnknown || got.Retryable {
			t.Errorf("Classify(%v) = %+v", err, got)
		}
	}
}
