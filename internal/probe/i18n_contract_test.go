package probe

// 报告文本的语言边界回归测试。
//
// 探针产出的每一条面向用户的文本都必须走稳定 key：渲染层的 displayValue 对
// RawValue 一律原样输出（见 report/presentation.go），所以生产端一旦把该用
// KeyValue 的地方写成 RawValue，那段中文就会原样出现在英文报告里。
//
// 这里用两层检查覆盖两种传递方式：
//
//   - 直接字面量（model.RawValue("中文")、Unit: "中文"）由 AST 扫描抓住，
//     新增探针自动被覆盖，不需要谁记得来这里登记；
//   - 经由静态数据表间接传入 RawValue 的展示字段（目标清单的用途说明等）
//     由下面的表驱动检查抓住，新增一张表时需要在这里加一行。
//
// 之所以扫源码而不是渲染真实报告：跑完 21 个探针需要网络与外部工具，而这个
// 契约与探针能否运行无关，只与源码里写了什么有关。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// measurementUnitAllowlist 是 Measurement.Unit 允许的取值。
//
// Unit 不是展示文本：report/compare.go 与 report/text_score.go 会把它直接拼进
// 显示值，compare/build.go 还把它算进可比性签名。它必须是与语言无关的机器标识。
var measurementUnitAllowlist = map[string]bool{
	"": true, "%": true, "x": true, "s": true, "ms": true,
	"count": true, "events": true, "bytes": true, "cores": true,
	"hops": true, "boolean": true, "score": true, "/100": true,
	"Mbps": true, "MiB/s": true, "MB/s": true, "IOPS": true, "iops": true,
	"events/s": true, "ops/s": true, "Mop/s": true, "retransmits": true,
	"MiB": true, "GiB": true, "KiB": true,
	"packets": true, "queries": true, "targets": true, "runs": true,
}

func containsCJK(value string) bool {
	for _, character := range value {
		if unicode.Is(unicode.Han, character) ||
			unicode.Is(unicode.Hiragana, character) ||
			unicode.Is(unicode.Katakana, character) ||
			unicode.Is(unicode.Hangul, character) {
			return true
		}
	}
	return false
}

// TestProbeSourceKeepsUserFacingTextOutOfRawValues scans every non-test file in
// this package for report text that would bypass the i18n catalog.
func TestProbeSourceKeepsUserFacingTextOutOfRawValues(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob probe sources: %v", err)
	}
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CallExpr:
				checkRawValueCall(t, fileSet, value)
			case *ast.KeyValueExpr:
				checkMeasurementUnit(t, fileSet, value)
			}
			return true
		})
	}
}

// checkRawValueCall rejects model.RawValue("<CJK>"). Raw values are provider
// output or diagnostics and are never translated, so ECS-authored prose must
// use model.KeyValue with a catalog key instead.
func checkRawValueCall(t *testing.T, fileSet *token.FileSet, call *ast.CallExpr) {
	t.Helper()
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "RawValue" {
		return
	}
	if identifier, ok := selector.X.(*ast.Ident); !ok || identifier.Name != "model" {
		return
	}
	if len(call.Args) != 1 {
		return
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil || !containsCJK(text) {
		return
	}
	t.Errorf("%s: model.RawValue(%q) puts untranslated text in the report; use model.KeyValue with a catalog key",
		fileSet.Position(literal.Pos()), text)
}

// checkMeasurementUnit rejects a Measurement.Unit outside the machine allowlist.
func checkMeasurementUnit(t *testing.T, fileSet *token.FileSet, pair *ast.KeyValueExpr) {
	t.Helper()
	key, ok := pair.Key.(*ast.Ident)
	if !ok || key.Name != "Unit" {
		return
	}
	literal, ok := pair.Value.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil {
		return
	}
	if measurementUnitAllowlist[text] {
		return
	}
	t.Errorf("%s: Measurement.Unit = %q is not a machine unit; it reaches compare displays and the comparability signature",
		fileSet.Position(literal.Pos()), text)
}

// TestProbeTargetCatalogsUseStableKeys covers the display fields that reach a
// report through a static catalog rather than a literal RawValue argument.
func TestProbeTargetCatalogsUseStableKeys(t *testing.T) {
	for _, zone := range dnsblZones() {
		if containsCJK(zone.Purpose) {
			t.Errorf("dnsblZone %q purpose %q is untranslated report text", zone.Zone, zone.Purpose)
		}
		if !strings.HasPrefix(zone.Purpose, "probe.blacklist.") {
			t.Errorf("dnsblZone %q purpose %q must be a stable presentation key", zone.Zone, zone.Purpose)
		}
	}
	for _, target := range appTargets() {
		if containsCJK(target.Note) {
			t.Errorf("appTarget %q note %q is untranslated report text", target.Name, target.Note)
		}
		if !strings.HasPrefix(target.Note, "probe.apps.") {
			t.Errorf("appTarget %q note %q must be a stable presentation key", target.Name, target.Note)
		}
	}
}

// TestUnavailablePlaceholdersAreRecognised checks the placeholders that reach a
// system field, because finalizeSystemResult counts any field it does not
// recognise as unavailable towards valid evidence. Only systemField call sites
// matter: fallback is a package-wide helper and its other callers feed error
// messages, which are diagnostics rather than counted observations.
func TestUnavailablePlaceholdersAreRecognised(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob probe sources: %v", err)
	}
	fileSet := token.NewFileSet()
	seen := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			outer, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if identifier, ok := outer.Fun.(*ast.Ident); !ok || identifier.Name != "systemField" {
				return true
			}
			for _, argument := range outer.Args {
				inner, ok := argument.(*ast.CallExpr)
				if !ok {
					continue
				}
				identifier, ok := inner.Fun.(*ast.Ident)
				if !ok || identifier.Name != "fallback" || len(inner.Args) != 2 {
					continue
				}
				literal, ok := inner.Args[1].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				seen++
				if !isUnavailableSystemValue(text) {
					t.Errorf("%s: system field placeholder %q is not recognised as unavailable; finalizeSystemResult would count it as a real observation",
						fileSet.Position(literal.Pos()), text)
				}
			}
			return true
		})
	}
	if seen == 0 {
		t.Fatal("found no system-field placeholders to check; the scan is no longer effective")
	}
	t.Logf("checked %d system-field placeholders", seen)
}
