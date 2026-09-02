package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/probe"
	"ecs/internal/textwidth"
)

func doctorCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "%s %s\n", i18n.T("help.extraArgs"), strings.Join(args, " "))
		return 1
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 2
	}
	fmt.Fprintln(stdout, i18n.T("doctor.header"))
	tools := doctorTools()
	missingRequired := false
	requiredFailure := false
	for _, tool := range tools {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return 130
			}
			return 2
		}
		path, err := lookupDoctorTool(tool)
		if err != nil {
			if !errors.Is(err, exec.ErrNotFound) {
				if tool.required {
					requiredFailure = true
				}
				fmt.Fprintf(stdout, "  %-11s %s %s · %v\n", tool.name, textwidth.Pad(i18n.T("doctor.failed"), 8), tool.purpose, err)
				continue
			}
			label := i18n.T("doctor.optional")
			if tool.required {
				label = i18n.T("doctor.missing")
				missingRequired = true
				requiredFailure = true
			}
			fmt.Fprintf(stdout, "  %-11s %s %s\n", tool.name, textwidth.Pad(label, 8), tool.purpose)
			continue
		}
		versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var version string
		var runErr error
		if tool.check != nil {
			version, runErr = tool.check(versionCtx, path)
		} else {
			var output []byte
			output, runErr = exec.CommandContext(versionCtx, path, tool.args...).CombinedOutput()
			version = strings.TrimSpace(string(output))
		}
		versionContextErr := versionCtx.Err()
		cancel()
		if newline := strings.IndexByte(version, '\n'); newline >= 0 {
			version = version[:newline]
		}
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return 130
			}
			return 2
		}
		if errors.Is(versionContextErr, context.DeadlineExceeded) || errors.Is(runErr, context.DeadlineExceeded) {
			if tool.required {
				requiredFailure = true
			}
			fmt.Fprintf(stdout, "  %-11s %s %s · %v\n", tool.name, textwidth.Pad(i18n.T("doctor.timeout"), 8), tool.purpose, runErr)
			continue
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(versionContextErr, context.Canceled) {
				return 130
			}
			if tool.required {
				requiredFailure = true
			}
			fmt.Fprintf(stdout, "  %-11s %s %s · %v\n", tool.name, textwidth.Pad(i18n.T("doctor.failed"), 8), tool.purpose, runErr)
			continue
		}
		if version == "" {
			if tool.required {
				requiredFailure = true
			}
			label := i18n.T("doctor.optional")
			if tool.required {
				label = i18n.T("doctor.unknownVersion")
			}
			fmt.Fprintf(stdout, "  %-11s %s %s · %s\n", tool.name, textwidth.Pad(label, 8), tool.purpose, i18n.T("doctor.unknownVersion"))
			continue
		}
		fmt.Fprintf(stdout, "  %-11s %s %s · %s\n", tool.name, textwidth.Pad(i18n.T("doctor.ready"), 8), tool.purpose, version)
	}
	if missingRequired {
		fmt.Fprintln(stdout, "\n"+i18n.T("doctor.installHint"))
		fmt.Fprintln(stdout, i18n.T("doctor.noSubstitute"))
		return 2
	}
	if requiredFailure {
		return 2
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("doctor.allReady"))
	return 0
}

type doctorTool struct {
	name     string
	required bool
	purpose  string
	args     []string
	lookup   func() (string, error)
	check    func(context.Context, string) (string, error)
}

func lookupDoctorTool(tool doctorTool) (string, error) {
	if tool.lookup != nil {
		return tool.lookup()
	}
	return exec.LookPath(tool.name)
}

func lookupNextTrace() (string, error) {
	path, err := exec.LookPath("nexttrace-tiny")
	if err != nil {
		return "", err
	}
	return path, nil
}

func doctorTools() []doctorTool {
	catalog := []doctorTool{
		{name: "sysbench", required: true, purpose: "doctor.purpose.sysbench", args: []string{"--version"}},
		{name: "zstd", required: true, purpose: "doctor.purpose.zstd", check: identifyPinnedZstd},
		{name: "npb-ep", required: true, purpose: "doctor.purpose.npbEP", check: identifyNPBBinary("EP")},
		{name: "npb-ft", required: true, purpose: "doctor.purpose.npbFT", check: identifyNPBBinary("FT")},
		{name: "openssl", required: true, purpose: "doctor.purpose.openssl", check: identifyPinnedOpenSSL},
		{name: "fio", required: true, purpose: "doctor.purpose.fio", args: []string{"--version"}},
		{name: "iperf3", required: true, purpose: "doctor.purpose.iperf3", args: []string{"--version"}},
		{name: "stream", required: true, purpose: "doctor.purpose.stream", check: identifyOfficialStream},
		{name: "nexttrace-tiny", purpose: "doctor.purpose.nexttrace", args: []string{"--version"}, lookup: lookupNextTrace},
		{name: "ping", purpose: "doctor.purpose.ping", args: []string{"-V"}},
		{name: "speedtest", purpose: "doctor.purpose.speedtest", args: []string{"--version"}},
	}
	meta := make(map[string]doctorTool, len(catalog))
	for _, item := range catalog {
		meta[item.name] = item
	}
	descriptors := config.ModuleDescriptors(probe.BuiltinCatalog())
	known := make(map[string]bool)
	toolOrder := make([]string, 0)
	for _, descriptor := range descriptors {
		for _, name := range descriptor.RequiredTools {
			name = strings.TrimSpace(name)
			if name == "" || known[name] {
				continue
			}
			known[name] = true
			toolOrder = append(toolOrder, name)
			if _, ok := meta[name]; ok {
				continue
			}
			meta[name] = doctorTool{name: name, purpose: name, args: []string{"--version"}}
		}
	}
	for _, item := range catalog {
		if item.required {
			known[item.name] = true
		}
	}
	tools := make([]doctorTool, 0, len(known))
	for _, item := range catalog {
		if !known[item.name] {
			continue
		}
		item.purpose = i18n.T(item.purpose)
		tools = append(tools, item)
		delete(known, item.name)
	}
	for _, name := range toolOrder {
		if !known[name] {
			continue
		}
		item := meta[name]
		item.purpose = i18n.T(item.purpose)
		tools = append(tools, item)
	}
	return tools
}

func identifyOfficialStream(_ context.Context, path string) (string, error) {
	if !probe.IsOfficialStreamBinary(path) {
		return "", fmt.Errorf("official STREAM markers not found in %s", path)
	}
	return "official STREAM", nil
}

var pinnedZstdVersionPattern = regexp.MustCompile(`(?i)(^|[^0-9])v1\.5\.7([^0-9]|$)`)

func identifyPinnedZstd(ctx context.Context, path string) (string, error) {
	output, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	version := strings.TrimSpace(string(output))
	if err != nil {
		return "", err
	}
	if !pinnedZstdVersionPattern.MatchString(version) {
		return "", fmt.Errorf("zstd 1.5.7 required")
	}
	return "zstd 1.5.7", nil
}

var pinnedOpenSSLVersionPattern = regexp.MustCompile(`(?m)^OpenSSL\s+3\.5\.7(?:\s|$)`)

func identifyPinnedOpenSSL(ctx context.Context, path string) (string, error) {
	output, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	version := strings.TrimSpace(string(output))
	if err != nil {
		return "", err
	}
	if !pinnedOpenSSLVersionPattern.MatchString(version) {
		return "", fmt.Errorf("OpenSSL 3.5.7 required")
	}
	return "OpenSSL 3.5.7", nil
}

func identifyNPBBinary(benchmark string) func(context.Context, string) (string, error) {
	return func(_ context.Context, path string) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		text := string(data)
		for _, marker := range []string{
			"NAS Parallel Benchmarks (NPB3.4-OMP)",
			" - " + benchmark + " Benchmark",
			"Benchmark Completed.",
			"Mop/s total",
			"Verification",
			"3.4.4",
		} {
			if !strings.Contains(text, marker) {
				return "", fmt.Errorf("NPB %s marker %q not found", benchmark, marker)
			}
		}
		return "NPB 3.4.4 " + benchmark + " (Class A verified at run)", nil
	}
}
