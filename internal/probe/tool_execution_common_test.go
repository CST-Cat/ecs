//go:build linux || freebsd || windows

package probe

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProbeCommandWriterEnforcesExactOutputLimit(t *testing.T) {
	cancelCalls := 0
	writer := newProbeCommandWriter(4, func() { cancelCalls++ })
	if count, err := writer.Write([]byte("1234")); count != 4 || err != nil {
		t.Fatalf("exact-limit write = %d, %v; want 4, nil", count, err)
	}
	if count, err := writer.Write([]byte("5")); count != 0 || !errors.Is(err, errProbeCommandOutputLimit) {
		t.Fatalf("over-limit write = %d, %v; want 0 and output-limit error", count, err)
	}
	if count, err := writer.Write([]byte("6")); count != 0 || !errors.Is(err, errProbeCommandOutputLimit) || cancelCalls != 1 {
		t.Fatalf("write after overflow = %d, %v; cancel calls=%d", count, err, cancelCalls)
	}
	if string(writer.data) != "1234" {
		t.Fatalf("bounded output = %q, want exact prefix", writer.data)
	}
}

func TestProcessProbeCommandOutputPreservesStreamsAndErrors(t *testing.T) {
	t.Run("combined output", func(t *testing.T) {
		output := newProbeCommandWriter(32, func() {})
		if _, err := output.Write([]byte("merged output")); err != nil {
			t.Fatal(err)
		}
		result := processProbeCommandOutput(context.Background(), output, output, true, nil)
		if string(result.Combined) != "merged output" || result.Stdout != nil || result.Stderr != nil || result.Err != nil {
			t.Fatalf("combined result = %+v", result)
		}
	})

	t.Run("separate output wraps sanitized stderr", func(t *testing.T) {
		stdout := newProbeCommandWriter(32, func() {})
		stderr := newProbeCommandWriter(32, func() {})
		if _, err := stdout.Write([]byte("stdout")); err != nil {
			t.Fatal(err)
		}
		stderrBytes := []byte(" \x1b[31mcommand failed\x1b[0m \n")
		if _, err := stderr.Write(stderrBytes); err != nil {
			t.Fatal(err)
		}
		runErr := errors.New("fixture command error")
		result := processProbeCommandOutput(context.Background(), stdout, stderr, false, runErr)
		if string(result.Stdout) != "stdout" || string(result.Stderr) != string(stderrBytes) || result.Combined != nil ||
			!errors.Is(result.Err, runErr) || !strings.Contains(result.Err.Error(), "command stderr: command failed") {
			t.Fatalf("separate result = %+v", result)
		}
	})

	t.Run("overflow keeps caller cause and bounded stderr", func(t *testing.T) {
		cause := errors.New("fixture cancellation cause")
		parentContext, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		stdout := newProbeCommandWriter(4, func() {})
		stderr := newProbeCommandWriter(32, func() {})
		if _, err := stdout.Write([]byte("12345")); !errors.Is(err, errProbeCommandOutputLimit) {
			t.Fatalf("overflow trigger error = %v", err)
		}
		if _, err := stderr.Write([]byte("diagnostic")); err != nil {
			t.Fatal(err)
		}
		result := processProbeCommandOutput(parentContext, stdout, stderr, false, nil)
		if result.Stdout != nil || result.Combined != nil || string(result.Stderr) != "diagnostic" ||
			!errors.Is(result.Err, cause) || !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, errProbeCommandOutputLimit) {
			t.Fatalf("overflow result = %+v", result)
		}
	})

	t.Run("separate stderr overflow keeps only bounded diagnostic", func(t *testing.T) {
		cause := errors.New("fixture cancellation cause")
		parentContext, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		stdout := newProbeCommandWriter(32, func() {})
		stderr := newProbeCommandWriter(5, func() {})
		if _, err := stdout.Write([]byte("stdout payload")); err != nil {
			t.Fatal(err)
		}
		if count, err := stderr.Write([]byte("SAFE!LEAKME")); count != 5 || !errors.Is(err, errProbeCommandOutputLimit) {
			t.Fatalf("stderr overflow write = %d, %v; want 5 and output-limit error", count, err)
		}

		result := processProbeCommandOutput(parentContext, stdout, stderr, false, nil)
		if result.Err == nil || result.Stdout != nil || result.Combined != nil || string(result.Stderr) != "SAFE!" {
			t.Fatalf("separate stderr overflow result streams = %+v", result)
		}
		message := result.Err.Error()
		if !strings.Contains(message, "external command stderr exceeded 5-byte limit") ||
			!strings.Contains(message, "command stderr: SAFE!") ||
			strings.Contains(message, "LEAKME") || strings.Contains(message, "stdout payload") {
			t.Fatalf("separate stderr overflow error = %q; want only the bounded diagnostic", message)
		}
		if !errors.Is(result.Err, cause) || !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, errProbeCommandOutputLimit) {
			t.Fatalf("separate stderr overflow error chain = %v; want caller cause and output-limit error", result.Err)
		}
	})
}
