package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func runInformationCommand(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestInformationCommandsSucceed(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "help", args: []string{"help", "--lang", "en"}, marker: "Usage:"},
		{name: "version", args: []string{"version"}, marker: "ecs "},
		{name: "list", args: []string{"list", "--lang", "en"}, marker: "Profiles:"},
		{name: "config example", args: []string{"config", "example"}, marker: `"profile": "standard"`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || stderr != "" || !strings.Contains(stdout, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestInformationCommandsReportDistinctFailures(t *testing.T) {
	// list's extra arguments and unknown flags share one custom parser branch.
	// config's missing and unknown subcommands share the same argument guard.
	cases := []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "unknown command", args: []string{"not-a-command", "--lang", "en"}, marker: "unknown command"},
		{name: "list extra argument", args: []string{"list", "--lang", "en", "--unexpected"}, marker: "error: unexpected arguments"},
		{name: "list unsupported format", args: []string{"list", "--format=xml", "--lang", "en"}, marker: `unsupported list format "xml"`},
		{name: "config subcommand missing", args: []string{"config", "--lang", "en"}, marker: "Usage: ecs config example"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}
