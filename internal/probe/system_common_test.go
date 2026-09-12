package probe

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// These facts and rendering helpers do not depend on procfs or any other
// operating-system interface, so keep them exercised by FreeBSD as well as
// Linux builds.
func TestPortableSystemParsersAndPlaceholders(t *testing.T) {
	for _, test := range []struct {
		input string
		want  uint64
		valid bool
	}{
		{input: "12345.75 0.00\n", want: 12345, valid: true},
		{input: "", valid: false},
		{input: "not-a-number", valid: false},
		{input: "-1.0 0.0", valid: false},
		{input: "NaN 0.0", valid: false},
	} {
		if got, valid := parseUptimeSeconds([]byte(test.input)); got != test.want || valid != test.valid {
			t.Fatalf("parseUptimeSeconds(%q) = %d/%v, want %d/%v", test.input, got, valid, test.want, test.valid)
		}
	}
	for _, value := range []string{"", "unknown", "unavailable", "unlimited_or_unavailable", "n/a", "none found", "—"} {
		if !isUnavailableSystemValue(value) {
			t.Fatalf("placeholder %q was not recognized", value)
		}
	}
	if isUnavailableSystemValue("real value") {
		t.Fatal("known system value was recognized as unavailable")
	}
}

func TestParseUptimeSecondsIsMachineFactParser(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  uint64
		valid bool
	}{
		{name: "fractional", input: "12345.75 0.00\n", want: 12345, valid: true},
		{name: "empty", input: "", valid: false},
		{name: "malformed", input: "not-a-number", valid: false},
		{name: "negative", input: "-1.0 0.0", valid: false},
		{name: "nan", input: "NaN 0.0", valid: false},
		{name: "positive infinity", input: "+Inf 0.0", valid: false},
		{name: "negative infinity", input: "-Inf 0.0", valid: false},
		{name: "uint64 overflow", input: "18446744073709551616.0 0.0", valid: false},
		{name: "max uint64 fractional", input: "18446744073709551615.9 0.0", want: ^uint64(0), valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := parseUptimeSeconds([]byte(test.input))
			if got != test.want || valid != test.valid {
				t.Fatalf("parseUptimeSeconds(%q) = %d/%v, want %d/%v", test.input, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestSystemEvidenceExcludesUnavailablePlaceholders(t *testing.T) {
	result := model.NewResult("system", "system")
	result.Fields = []model.Field{
		systemField("available", "fixture"),
		systemField("unavailable", "unavailable"),
		systemField("known_unlimited", "unlimited"),
		systemField("unlimited_or_unavailable", "unlimited_or_unavailable"),
		systemField("not_applicable", "n/a"),
		systemField("unknown", "unknown"),
	}
	finalizeSystemResult(&result, systemSnapshot{})
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 6 || result.Status != model.StatusWarning {
		t.Fatalf("placeholder evidence/status = %+v/%s", result.Evidence, result.Status)
	}
}

func TestSystemBuiltinUsesDirectProbeAndLiveResultHasNoDuplicateFacts(t *testing.T) {
	systemCount := 0
	for _, definition := range BuiltinDefinitions() {
		if _, ok := definition.Probe.(systemProbe); ok {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Fatalf("systemProbe builtin count = %d", systemCount)
	}
	result := (systemProbe{}).Run(context.Background(), Environment{})
	if result.Title != "module.system.title" || len(result.SummaryMessages) != 1 {
		t.Fatalf("live direct system result = %+v", result)
	}
	fields := make(map[string]bool, len(result.Fields))
	for _, field := range result.Fields {
		if fields[field.Key] {
			t.Fatalf("live duplicate field %q", field.Key)
		}
		fields[field.Key] = true
		if strings.HasPrefix(field.Label, "probe.kernel.") && (!i18n.Has(i18n.LangZH, field.Label) || !i18n.Has(i18n.LangEN, field.Label)) {
			t.Fatalf("live kernel field label is not bilingual: %+v", field)
		}
	}
	if fields["memory"] || fields["disk"] || fields["uptime"] || !fields["uptime_seconds"] {
		t.Fatalf("live legacy/uptime fields = %v", fields)
	}
	measurements := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurements[measurement.Key] {
			t.Fatalf("live duplicate measurement %q", measurement.Key)
		}
		measurements[measurement.Key] = true
		if strings.HasPrefix(measurement.Label, "probe.kernel.") && (!i18n.Has(i18n.LangZH, measurement.Label) || !i18n.Has(i18n.LangEN, measurement.Label)) {
			t.Fatalf("live kernel measurement label is not bilingual: %+v", measurement)
		}
	}
	tables := make(map[string]bool, len(result.Tables))
	for _, table := range result.Tables {
		if tables[table.Key] {
			t.Fatalf("live duplicate table %q", table.Key)
		}
		tables[table.Key] = true
		if strings.HasPrefix(table.Key, "system.kernel.") {
			if !i18n.Has(i18n.LangZH, table.Title) || !i18n.Has(i18n.LangEN, table.Title) {
				t.Fatalf("live kernel table title is not bilingual: %+v", table)
			}
			for _, column := range table.Columns {
				if !i18n.Has(i18n.LangZH, column.Label) || !i18n.Has(i18n.LangEN, column.Label) {
					t.Fatalf("live kernel table column is not bilingual: %+v", table)
				}
			}
		}
	}
}

func TestSystemProductionHasSingleCollectionOwner(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	probeDir := filepath.Dir(testFile)
	if _, err := os.Stat(filepath.Join(probeDir, "system.go")); err != nil {
		// Cross-compiled tests may execute on a host without the build machine's
		// source path. The integration harness places the package sources in the
		// working directory so the same ownership check remains deterministic.
		if workingDir, err := os.Getwd(); err == nil {
			probeDir = workingDir
		}
	}
	files, err := filepath.Glob(filepath.Join(probeDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{
		"collectSystem":              true,
		"CaptureEnvironmentSnapshot": true,
		"discoverLocalCloudIdentity": true,
		"appendKernelNetworkParams":  true,
	}
	forbidden := map[string]bool{
		"systemSemanticProbe":   true,
		"stabilizeSystemResult": true,
		"systemUptimeSeconds":   true,
	}
	productionCalls := make(map[string][]string, len(targets))
	systemRunCalls := make(map[string]int, len(targets))
	forbiddenUses := make(map[string][]string, len(forbidden))
	systemRunCount := 0
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CallExpr:
				var name string
				switch function := value.Fun.(type) {
				case *ast.Ident:
					name = function.Name
				case *ast.SelectorExpr:
					name = function.Sel.Name
				}
				if targets[name] {
					productionCalls[name] = append(productionCalls[name], filepath.Base(filename))
				}
			case *ast.Ident:
				if forbidden[value.Name] {
					forbiddenUses[value.Name] = append(forbiddenUses[value.Name], filepath.Base(filename))
				}
			}
			return true
		})
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "Run" || function.Recv == nil || function.Body == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver, ok := function.Recv.List[0].Type.(*ast.Ident)
			if !ok || receiver.Name != "systemProbe" {
				continue
			}
			systemRunCount++
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch function := call.Fun.(type) {
				case *ast.Ident:
					name = function.Name
				case *ast.SelectorExpr:
					name = function.Sel.Name
				}
				if targets[name] {
					systemRunCalls[name]++
				}
				return true
			})
		}
	}
	if systemRunCount != 1 {
		t.Fatalf("systemProbe.Run count = %d", systemRunCount)
	}
	for name := range targets {
		if len(productionCalls[name]) != 1 || systemRunCalls[name] != 1 {
			t.Fatalf("system production call sites for %s = %v, systemProbe.Run calls = %d", name, productionCalls[name], systemRunCalls[name])
		}
	}
	for name, uses := range forbiddenUses {
		if len(uses) != 0 {
			t.Fatalf("forbidden system bridge identifier %s remains in %v", name, uses)
		}
	}
}
