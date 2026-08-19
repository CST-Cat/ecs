package failure

import (
	"context"
	"errors"
	"testing"

	"ecs/internal/model"
)

func TestClassifyKnownTimeout(t *testing.T) {
	got := Classify(context.DeadlineExceeded)
	if got.Category != model.FailureTimeout || !got.Retryable {
		t.Fatalf("Classify(timeout) = %+v, want retryable timeout", got)
	}
}

func TestClassifyOpaqueErrorAsUnknown(t *testing.T) {
	got := Classify(errors.New("something unusual happened"))
	if got.Category != model.FailureUnknown || got.Retryable {
		t.Fatalf("Classify(opaque) = %+v, want non-retryable unknown", got)
	}
}
