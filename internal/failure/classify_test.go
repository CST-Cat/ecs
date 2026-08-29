package failure

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os/exec"
	"syscall"
	"testing"

	"ecs/internal/model"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestClassifyCoversTypedAndTextFailureCategories(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		category  model.FailureCategory
		retryable bool
	}{
		{name: "nil", category: model.FailureUnknown},
		{name: "cancelled", err: context.Canceled, category: model.FailureCanceled},
		{name: "deadline", err: context.DeadlineExceeded, category: model.FailureTimeout, retryable: true},
		{name: "network timeout", err: timeoutError{}, category: model.FailureTimeout, retryable: true},
		{name: "url timeout", err: &url.Error{Op: "GET", URL: "https://example.test", Err: timeoutError{}}, category: model.FailureTimeout, retryable: true},
		{name: "dns timeout", err: &net.DNSError{Name: "slow.example", Err: "lookup timeout", IsTimeout: true}, category: model.FailureTimeout, retryable: true},
		{name: "missing tool", err: exec.ErrNotFound, category: model.FailureToolMissing},
		{name: "permission", err: syscall.EACCES, category: model.FailurePermissionDenied},
		{name: "refused", err: syscall.ECONNREFUSED, category: model.FailureConnectionRefused, retryable: true},
		{name: "unreachable", err: syscall.ENETUNREACH, category: model.FailureNetworkUnreachable, retryable: true},
		{name: "dns", err: &net.DNSError{Name: "bad.example", Err: "lookup failed", IsTemporary: true}, category: model.FailureDNS, retryable: true},
		{name: "permanent dns", err: &net.DNSError{Name: "bad.example", Err: "no such host"}, category: model.FailureDNS},
		{name: "tls", err: x509.UnknownAuthorityError{}, category: model.FailureTLS},
		{name: "hostname tls", err: x509.HostnameError{}, category: model.FailureTLS},
		{name: "json", err: &json.SyntaxError{Offset: 1}, category: model.FailureParse},
		{name: "json type", err: &json.UnmarshalTypeError{}, category: model.FailureParse},
		{name: "opaque", err: errors.New("something unusual happened"), category: model.FailureUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := Classify(test.err)
			if got.Category != test.category || got.Retryable != test.retryable {
				t.Fatalf("Classify(%v) = %+v, want %s retryable=%v", test.err, got, test.category, test.retryable)
			}
		})
	}
	wrapped := errors.Join(errors.New("timeout text"), context.Canceled)
	if got := Classify(wrapped); got.Category != model.FailureCanceled || got.Retryable {
		t.Fatalf("typed cancellation should take precedence = %+v", got)
	}
}

func TestClassifyTextCategoriesThroughFromMessage(t *testing.T) {
	for _, test := range []struct {
		name      string
		message   string
		category  model.FailureCategory
		retryable bool
	}{
		{name: "cancelled", message: "context canceled", category: model.FailureCanceled},
		{name: "rate limited", message: "HTTP status 429", category: model.FailureRateLimited, retryable: true},
		{name: "timeout text", message: "request timed out", category: model.FailureTimeout, retryable: true},
		{name: "HTTP timeout", message: "HTTP status 408", category: model.FailureTimeout, retryable: true},
		{name: "HTTP client rejection", message: "HTTP status 401", category: model.FailureHTTPRejected},
		{name: "HTTP server rejection", message: "HTTP status 503", category: model.FailureHTTPRejected, retryable: true},
		{name: "connection refused", message: "connection refused", category: model.FailureConnectionRefused, retryable: true},
		{name: "network unreachable", message: "network is unreachable", category: model.FailureNetworkUnreachable, retryable: true},
		{name: "DNS", message: "no such host", category: model.FailureDNS, retryable: true},
		{name: "TLS", message: "certificate verify failed", category: model.FailureTLS},
		{name: "permission", message: "permission denied", category: model.FailurePermissionDenied},
		{name: "missing tool", message: "command not found", category: model.FailureToolMissing},
		{name: "unsupported", message: "feature is unsupported", category: model.FailureUnsupported},
		{name: "parse", message: "malformed JSON response", category: model.FailureParse},
		{name: "unknown", message: "unclassified operation failed", category: model.FailureUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := FromMessage("stage", "target", test.message)
			if got.Category != test.category || got.Retryable != test.retryable || got.Stage != "stage" || got.Target != "target" || got.Message != test.message || got.Count != 1 {
				t.Fatalf("FromMessage(%q) = %+v", test.message, got)
			}
		})
	}
}

func TestFailureConstructors(t *testing.T) {
	fromError := FromError("download", "node-a", syscall.ECONNREFUSED)
	if fromError.Category != model.FailureConnectionRefused || !fromError.Retryable || fromError.Stage != "download" || fromError.Target != "node-a" || fromError.Message == "" || fromError.Count != 1 {
		t.Fatalf("FromError = %+v", fromError)
	}

	empty := FromMessage("stage", "target", "")
	if empty.Category != model.FailureUnknown || empty.Message != "" {
		t.Fatalf("empty FromMessage = %+v", empty)
	}
}
