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

	"ecs/internal/i18n"
	"ecs/internal/module"
	"ecs/internal/probe"
	"ecs/internal/textwidth"
	"ecs/internal/tool"
)

func doctorCommand(app application, ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	tools := doctorTools(app.modules, app.tools)
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
		version, runErr = verifyDoctorTool(versionCtx, tool, path)
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
	definition tool.Definition
	name       string
	required   bool
	purpose    string
}

func lookupDoctorTool(item doctorTool) (string, error) {
	return exec.LookPath(item.name)
}

func doctorTools(moduleCatalog module.Catalog, toolCatalog tool.Catalog) []doctorTool {
	// Tool identity comes from module RequiredTools. The tool catalog then
	// filters those references to its standard-doctor membership and supplies
	// requiredness plus all verification facts. A separate standard-profile set
	// keeps optional full-profile tools visible without
	// making them required.
	referencedIDs := make(map[string]struct{})
	standardIDs := make(map[string]struct{})
	for _, descriptor := range moduleCatalog.Descriptors() {
		for _, id := range descriptor.RequiredTools {
			referencedIDs[id] = struct{}{}
			if descriptor.ProfileStandard {
				standardIDs[id] = struct{}{}
			}
		}
	}
	definitions := toolCatalog.DoctorDefinitions()
	tools := make([]doctorTool, 0, len(definitions))
	for _, definition := range definitions {
		if _, referenced := referencedIDs[definition.ID]; !referenced {
			continue
		}
		_, referencedByStandard := standardIDs[definition.ID]
		tools = append(tools, doctorTool{
			definition: definition,
			name:       definition.ID,
			required:   referencedByStandard && definition.Doctor.Required,
			purpose:    i18n.T(definition.PurposeKey),
		})
	}
	return tools
}

func verifyDoctorTool(ctx context.Context, item doctorTool, path string) (string, error) {
	policy := item.definition.Verification
	switch policy.Kind {
	case tool.VerificationCommand:
		output, err := exec.CommandContext(ctx, path, policy.Arguments...).CombinedOutput()
		return strings.TrimSpace(string(output)), err
	case tool.VerificationPinnedZstd, tool.VerificationPinnedOpenSSL:
		return identifyPinnedVersion(ctx, path, policy)
	case tool.VerificationNPB:
		return identifyNPBBinary(ctx, path, policy)
	case tool.VerificationOfficialStream:
		return identifyOfficialStream(path, policy.SuccessLabel)
	default:
		return "", fmt.Errorf("unsupported verification kind %q", policy.Kind)
	}
}

func identifyOfficialStream(path, successLabel string) (string, error) {
	if !probe.IsOfficialStreamBinary(path) {
		return "", fmt.Errorf("official STREAM markers not found in %s", path)
	}
	return successLabel, nil
}

func identifyPinnedVersion(ctx context.Context, path string, policy tool.VerificationPolicy) (string, error) {
	output, err := exec.CommandContext(ctx, path, policy.Arguments...).CombinedOutput()
	version := strings.TrimSpace(string(output))
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile(`(?i)(^|[^0-9])v` + regexp.QuoteMeta(policy.ExpectedVersion) + `([^0-9]|$)`)
	if policy.Kind == tool.VerificationPinnedOpenSSL {
		pattern = regexp.MustCompile(`(?m)^OpenSSL\s+` + regexp.QuoteMeta(policy.ExpectedVersion) + `(?:\s|$)`)
	}
	if !pattern.MatchString(version) {
		return "", fmt.Errorf("%s required", policy.SuccessLabel)
	}
	return policy.SuccessLabel, nil
}

func identifyNPBBinary(_ context.Context, path string, policy tool.VerificationPolicy) (string, error) {
	benchmark := string(policy.NPBVariant)
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
		policy.ExpectedVersion,
	} {
		if !strings.Contains(text, marker) {
			return "", fmt.Errorf("NPB %s marker %q not found", benchmark, marker)
		}
	}
	return policy.SuccessLabel, nil
}
