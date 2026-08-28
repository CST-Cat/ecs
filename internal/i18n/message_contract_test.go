package i18n

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Dynamic keys cannot be recovered from a string literal, so each one must
// be registered with its complete finite key set and argument contract. The
// AST audit below rejects any new dynamic call until it is explicitly added.
var dynamicMessageContracts = map[string]dynamicMessageContract{
	"internal/probe/nat.go|natSummaryKey(natCategoryKey(categoryCode))": {
		keys: []string{
			"probe.nat.summary.unknown",
			"probe.nat.summary.public",
			"probe.nat.summary.symmetric",
			"probe.nat.summary.full_cone",
			"probe.nat.summary.restricted_cone",
			"probe.nat.summary.port_restricted",
			"probe.nat.summary.cone_unknown_filtering",
		},
		argCount: 0,
		calls:    1,
	},
	"internal/runner/runner.go|descriptor.PrivacyNoticeKey": {
		keys:     []string{"message.notice.ooklaPrivacy"},
		argCount: 0,
		calls:    1,
	},
}

// TestProductionMessageContracts audits only keys that are actually reachable
// through production Message construction. Ordinary probe presentation keys
// are intentionally not treated as fmt formats: many are rendered directly
// with i18n.T and may legitimately contain a literal percent sign.
func TestProductionMessageContracts(t *testing.T) {
	audit, err := productionMessageAudit()
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.calls) == 0 {
		t.Fatal("no production NewMessage callsites found")
	}

	seenDynamic := make(map[string]int)
	for _, call := range audit.calls {
		if call.literal {
			validateMessageFormat(t, call.file, call.line, call.key, call.argCount)
			continue
		}

		contractID := call.contractID()
		contract, ok := dynamicMessageContracts[contractID]
		if !ok {
			t.Errorf("%s:%d: dynamic NewMessage key %s is not registered; add its finite key set and argument contract", call.file, call.line, call.keyExpr)
			continue
		}
		seenDynamic[contractID]++
		if call.argCount != contract.argCount {
			t.Errorf("%s:%d: dynamic NewMessage key %s has %d args, contract requires %d", call.file, call.line, call.keyExpr, call.argCount, contract.argCount)
		}
		for _, key := range contract.keys {
			validateMessageFormat(t, call.file, call.line, key, contract.argCount)
		}
	}

	contractIDs := make([]string, 0, len(dynamicMessageContracts))
	for contractID := range dynamicMessageContracts {
		contractIDs = append(contractIDs, contractID)
	}
	sort.Strings(contractIDs)
	for _, contractID := range contractIDs {
		contract := dynamicMessageContracts[contractID]
		if seenDynamic[contractID] != contract.calls {
			t.Errorf("dynamic Message contract %s saw %d call(s), want %d", contractID, seenDynamic[contractID], contract.calls)
		}
	}

	for _, literal := range audit.directLiterals {
		t.Errorf("%s: production Message literal bypasses NewMessage; use NewMessage so string argument serialization is audited", literal)
	}
}

func validateMessageFormat(t *testing.T, file string, line int, key string, argCount int) {
	t.Helper()
	for _, lang := range Supported() {
		format, ok := lookup(lang, key)
		if !ok {
			t.Errorf("%s:%d: %s Message key %q is missing from its catalog", file, line, lang, key)
			continue
		}
		verbCount := formatVerbCount(format)
		if verbCount != argCount {
			t.Errorf("%s:%d: %s Message %q has %d call argument(s), but format %q has %d verb(s)", file, line, lang, key, argCount, format, verbCount)
		}
		if rendered := fmt.Sprintf(format, stringFormatArgs(argCount)...); strings.Contains(rendered, "%!") {
			t.Errorf("%s:%d: %s Message %q rendered with fmt diagnostics: %q", file, line, lang, key, rendered)
		}
	}
}

func stringFormatArgs(count int) []any {
	args := make([]any, count)
	for index := range args {
		args[index] = fmt.Sprintf("message-arg-%d", index)
	}
	return args
}

type dynamicMessageContract struct {
	keys     []string
	argCount int
	calls    int
}

type productionMessageCall struct {
	file     string
	line     int
	key      string
	keyExpr  string
	argCount int
	literal  bool
}

func (call productionMessageCall) contractID() string {
	return call.file + "|" + call.keyExpr
}

type productionMessageAuditResult struct {
	calls          []productionMessageCall
	directLiterals []string
}

func productionMessageAudit() (productionMessageAuditResult, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return productionMessageAuditResult{}, fmt.Errorf("locate message contract test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	var result productionMessageAuditResult
	for _, relativeDir := range []string{"internal", "cmd"} {
		directory := filepath.Join(root, relativeDir)
		if err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == ".git" || info.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr != nil {
				relative = path
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					if !isNewMessageCall(node) || len(node.Args) == 0 {
						return true
					}
					position := fileSet.Position(node.Pos())
					key, literal := stringLiteral(node.Args[0])
					keyExpr := key
					if !literal {
						var buffer bytes.Buffer
						if formatErr := format.Node(&buffer, fileSet, node.Args[0]); formatErr != nil {
							keyExpr = "<unprintable expression>"
						} else {
							keyExpr = buffer.String()
						}
					}
					result.calls = append(result.calls, productionMessageCall{
						file: relative, line: position.Line, key: key, keyExpr: keyExpr,
						argCount: len(node.Args) - 1, literal: literal,
					})
				case *ast.CompositeLit:
					if isMessageCompositeLiteral(node) && relative != "internal/model/message.go" {
						position := fileSet.Position(node.Pos())
						result.directLiterals = append(result.directLiterals, fmt.Sprintf("%s:%d", relative, position.Line))
					}
				}
				return true
			})
			return nil
		}); err != nil {
			return productionMessageAuditResult{}, err
		}
	}
	return result, nil
}

func isNewMessageCall(call *ast.CallExpr) bool {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name == "NewMessage"
	case *ast.SelectorExpr:
		return function.Sel.Name == "NewMessage"
	default:
		return false
	}
}

func isMessageCompositeLiteral(literal *ast.CompositeLit) bool {
	switch typeExpression := literal.Type.(type) {
	case *ast.Ident:
		return typeExpression.Name == "Message"
	case *ast.SelectorExpr:
		return typeExpression.Sel.Name == "Message"
	default:
		return false
	}
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}
