package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// summaryContractViolations checks only fields declared by the model types
// whose names are part of the report contract. It deliberately does not
// inspect arbitrary Summary: composite-literal keys: Report.Summary and
// comparison summaries are valid structured objects, while the removed
// fields were specifically Summary.Headline and Result.Summary.
func summaryContractViolations(source string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "summary_contract.go", source, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok || (typeSpec.Name.Name != "Summary" && typeSpec.Name.Name != "Result") {
				continue
			}
			for _, field := range structure.Fields.List {
				jsonName := jsonFieldName(field)
				for _, name := range field.Names {
					if typeSpec.Name.Name == "Summary" && (name.Name == "Headline" || jsonName == "headline") {
						violations = append(violations, typeSpec.Name.Name+"."+name.Name)
					}
					if typeSpec.Name.Name == "Result" && (name.Name == "Summary" || jsonName == "summary") {
						violations = append(violations, typeSpec.Name.Name+"."+name.Name)
					}
				}
			}
		}
	}
	return violations, nil
}

func jsonFieldName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	return strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
}

func TestSummaryContractAnalyzerScopesFieldsToDeclaringType(t *testing.T) {
	legal := "package fixture\n" +
		"type Summary struct { Messages []string `json:\"messages,omitempty\"` }\n" +
		"type Result struct { SummaryMessages []string `json:\"summary_messages,omitempty\"` }\n" +
		"type Report struct { Summary Summary `json:\"summary\"` }\n" +
		"type Comparison struct { Summary string `json:\"summary\"` }\n"
	violations, err := summaryContractViolations(legal)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("legal Report.Summary was rejected: %v", violations)
	}

	legacy := "package fixture\n" +
		"type Summary struct { Headline string `json:\"headline,omitempty\"` }\n" +
		"type Result struct { Summary string `json:\"summary,omitempty\"` }\n" +
		"type Report struct { Summary Summary `json:\"summary\"` }\n"
	violations, err = summaryContractViolations(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 || !containsString(violations, "Summary.Headline") || !containsString(violations, "Result.Summary") {
		t.Fatalf("legacy declarations were not precisely detected: %v", violations)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestProductionUsesOnlyStructuredSummaryFields(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	modelDir := filepath.Dir(sourceFile)
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(modelDir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fileViolations, err := summaryContractViolations(string(source))
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, violation := range fileViolations {
			violations = append(violations, entry.Name()+": "+violation)
		}
	}
	if len(violations) != 0 {
		t.Fatalf("legacy summary declarations remain: %s", strings.Join(violations, "; "))
	}
}
