package app

import (
	"context"
	"strings"
	"testing"
)

func TestDoctorReportsMissingRequiredTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var output strings.Builder
	if status := doctorCommand(context.Background(), &output); status != 2 {
		t.Fatalf("doctor status = %d, want missing-required status 2", status)
	}
	if strings.TrimSpace(output.String()) == "" {
		t.Fatal("doctor should emit a diagnostic report")
	}
}
