package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoctorToolFixture(t *testing.T, directory, name string, broken bool) {
	t.Helper()
	body := doctorToolFixtureBody(name, broken)
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func doctorToolFixtureBody(name string, broken bool) string {
	switch name {
	case "zstd":
		version := "1.5.7"
		if broken {
			version = "1.5.6"
		}
		return "#!/bin/sh\nprintf '%s\\n' 'zstd command line interface 64-bits v" + version + "'\n"
	case "openssl":
		version := "3.5.7"
		if broken {
			version = "3.5.6"
		}
		return "#!/bin/sh\nprintf '%s\\n' 'OpenSSL " + version + " fixture'\n"
	case "npb-ep", "npb-ft":
		if broken {
			return "#!/bin/sh\n# NAS Parallel Benchmarks (NPB3.4-OMP) - EP Benchmark\n# Benchmark Completed.\n"
		}
		benchmark := "EP"
		if name == "npb-ft" {
			benchmark = "FT"
		}
		return "#!/bin/sh\n# NAS Parallel Benchmarks (NPB3.4-OMP) - " + benchmark + " Benchmark\n# Benchmark Completed.\n# Mop/s total\n# Verification\n# 3.4.4\n"
	case "stream":
		if broken {
			return "#!/bin/sh\n# STREAM version 5.10\n# Number of Threads requested\n# Best Rate\n"
		}
		return "#!/bin/sh\n# STREAM version 5.10\n# Number of Threads requested\n# Best Rate\n# Function\n"
	default:
		if broken {
			return "#!/bin/sh\nexit 1\n"
		}
		return "#!/bin/sh\nprintf '%s\\n' 'fixture tool 1.0'\n"
	}
}

func installDoctorFixtures(t *testing.T, broken string, optional ...string) {
	t.Helper()
	directory := t.TempDir()
	optionalName := ""
	if len(optional) > 0 {
		optionalName = optional[0]
	}
	for _, tool := range doctorTools() {
		if tool.required || tool.name == optionalName {
			writeDoctorToolFixture(t, directory, tool.name, tool.name == broken)
		}
	}
	t.Setenv("PATH", directory)
}

func TestDoctorReportsMissingRequiredTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var output strings.Builder
	status := Main(context.Background(), []string{"doctor", "--lang", "en"}, &output, &strings.Builder{})
	if status != 2 || !strings.Contains(output.String(), "ecs standard benchmark dependencies") ||
		!strings.Contains(output.String(), "missing") {
		t.Fatalf("doctor missing-required status=%d output=%q", status, output.String())
	}
}

func TestDoctorReportsReadyWithLocalFixtures(t *testing.T) {
	installDoctorFixtures(t, "")
	var output strings.Builder
	status := Main(context.Background(), []string{"doctor", "--lang", "en"}, &output, &strings.Builder{})
	text := output.String()
	if status != 0 || !strings.Contains(text, "Standard performance tools are ready.") ||
		!strings.Contains(text, "optional") {
		t.Fatalf("doctor fixture status=%d output=%q", status, text)
	}
}

func TestDoctorReportsToolVersionFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		broken string
	}{
		{name: "generic version command", broken: "sysbench"},
		{name: "zstd pin", broken: "zstd"},
		{name: "openssl pin", broken: "openssl"},
		{name: "NPB marker", broken: "npb-ep"},
		{name: "STREAM marker", broken: "stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			installDoctorFixtures(t, test.broken)
			var output strings.Builder
			status := Main(context.Background(), []string{"doctor", "--lang", "en"}, &output, &strings.Builder{})
			text := output.String()
			if status != 2 || !strings.Contains(text, test.broken) || !strings.Contains(text, "version unknown") {
				t.Fatalf("doctor %s failure status=%d output=%q", test.broken, status, text)
			}
		})
	}
}

func TestDoctorReportsOptionalToolVersionFailureWithoutBlocking(t *testing.T) {
	installDoctorFixtures(t, "ping", "ping")
	var output strings.Builder
	status := Main(context.Background(), []string{"doctor", "--lang", "en"}, &output, &strings.Builder{})
	text := output.String()
	if status != 0 || !strings.Contains(text, "ping") || !strings.Contains(text, "optional") || !strings.Contains(text, "version unknown") {
		t.Fatalf("doctor optional failure status=%d output=%q", status, text)
	}
}
