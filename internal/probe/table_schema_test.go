package probe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"ecs/internal/model"
)

// TestProductionTableLiteralsDeclareMachineSchema is deliberately source
// based: most probes build their table in a path that needs an external
// command or network service, so invoking every Run method would make this
// coverage check flaky. It still visits every production model.Table literal
// and makes adding a display-only table without a machine schema impossible.
func TestProductionTableLiteralsDeclareMachineSchema(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	directory := filepath.Dir(filename)
	files, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seenKeys := make(map[string]token.Position)
	literalCount := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isModelTableType(literal.Type) {
				return true
			}
			literalCount++
			fields := tableLiteralFields(literal)
			for _, required := range []string{"Key", "Columns", "ColumnKeys"} {
				if _, ok := fields[required]; !ok {
					t.Errorf("%s: production model.Table is missing %s", fset.Position(literal.Pos()), required)
				}
			}
			if key, ok := stringLiteral(fields["Key"]); ok {
				if strings.TrimSpace(key) == "" {
					t.Errorf("%s: production model.Table has empty Key", fset.Position(literal.Pos()))
				} else if previous, exists := seenKeys[key]; exists {
					t.Errorf("%s: duplicate production table Key %q (already at %s)", fset.Position(literal.Pos()), key, previous)
				} else {
					seenKeys[key] = fset.Position(literal.Pos())
				}
			}
			if columns, columnsOK := stringSliceLiteral(fields["Columns"]); columnsOK {
				if keys, keysOK := stringSliceLiteral(fields["ColumnKeys"]); keysOK {
					if len(columns) != len(keys) {
						t.Errorf("%s: Columns/ColumnKeys lengths = %d/%d", fset.Position(literal.Pos()), len(columns), len(keys))
					}
					seenColumns := make(map[string]bool, len(keys))
					for _, columnKey := range keys {
						if strings.TrimSpace(columnKey) == "" || seenColumns[columnKey] {
							t.Errorf("%s: empty or duplicate static ColumnKey %q", fset.Position(literal.Pos()), columnKey)
						}
						seenColumns[columnKey] = true
					}
				}
			}
			if identity, ok := stringLiteral(fields["RowIdentity"]); ok {
				if keys, ok := stringSliceLiteral(fields["ColumnKeys"]); ok && !containsSchemaString(keys, identity) {
					t.Errorf("%s: RowIdentity %q is not present in ColumnKeys", fset.Position(literal.Pos()), identity)
				}
			}
			return true
		})
	}
	if literalCount == 0 {
		t.Fatal("no production model.Table literals found")
	}
	const wantProductionTableLiteralCount = 34
	if literalCount != wantProductionTableLiteralCount {
		t.Fatalf("visited %d production model.Table literals, want all %d construction points", literalCount, wantProductionTableLiteralCount)
	}
}

func TestDynamicProductionTableSchemas(t *testing.T) {
	findings := make(map[string]qualityFinding, len(qualitySourceOrder))
	for _, id := range qualitySourceOrder {
		findings[id] = qualityFinding{ID: id, Name: qualitySourceLabels[id]}
	}
	bundle := ipQualityBundle{Version: "4", Findings: findings}
	tables := []model.Table{
		streamMemoryTable(nil),
		streamStabilityTable(nil),
		npbResultsTable(nil, nil, 1),
		openSSLResultsTable(nil, nil, 1),
		zstdThroughputTable(nil, 1),
		bundle.typeTable(),
		bundle.scoreTable(),
		bundle.factorTable(),
		bundle.statusTable(),
	}
	bundle6 := ipQualityBundle{Version: "6", Findings: findings}
	tables = append(tables,
		bundle6.typeTable(), bundle6.scoreTable(), bundle6.factorTable(), bundle6.statusTable(),
	)
	for _, table := range tables {
		assertTableSchema(t, table)
	}
	assertUniqueTableKeys(t, tables)

	assertAppCategoryDescriptors(t)
	assertMediaCategoryDescriptors(t)
}

func assertAppCategoryDescriptors(t *testing.T) {
	t.Helper()
	keyToLabel := make(map[string]string)
	labelToKey := make(map[string]string)
	for _, target := range appTargets() {
		category := target.Category
		if strings.TrimSpace(category.Key) == "" || strings.TrimSpace(category.Label) == "" {
			t.Errorf("app target %q has incomplete category descriptor: %+v", target.Name, category)
			continue
		}
		if previous, ok := keyToLabel[category.Key]; ok && previous != category.Label {
			t.Errorf("app machine key %q has inconsistent labels %q and %q", category.Key, previous, category.Label)
		}
		if previous, ok := labelToKey[category.Label]; ok && previous != category.Key {
			t.Errorf("app display category %q maps to multiple machine keys %q and %q", category.Label, previous, category.Key)
		}
		keyToLabel[category.Key] = category.Label
		labelToKey[category.Label] = category.Key
	}
	if len(keyToLabel) == 0 {
		t.Fatal("app target descriptors are empty")
	}
}

func assertMediaCategoryDescriptors(t *testing.T) {
	t.Helper()
	keyToLabel := make(map[string]string)
	labelToKey := make(map[string]string)
	for _, check := range mediaChecks() {
		category := check.Category
		if strings.TrimSpace(category.Key) == "" || strings.TrimSpace(category.Label) == "" {
			t.Errorf("media rule %q has incomplete category descriptor: %+v", check.Name, category)
			continue
		}
		if previous, ok := keyToLabel[category.Key]; ok && previous != category.Label {
			t.Errorf("media machine key %q has inconsistent labels %q and %q", category.Key, previous, category.Label)
		}
		if previous, ok := labelToKey[category.Label]; ok && previous != category.Key {
			t.Errorf("media display category %q maps to multiple machine keys %q and %q", category.Label, previous, category.Key)
		}
		keyToLabel[category.Key] = category.Label
		labelToKey[category.Label] = category.Key
	}
	for region, categories := range mediaRegionCategories {
		for _, category := range categories {
			label, ok := keyToLabel[category.Key]
			if !ok {
				t.Errorf("region %q references undeclared media category key %q", region, category.Key)
				continue
			}
			if label != category.Label {
				t.Errorf("region %q category key %q label = %q, want %q", region, category.Key, category.Label, label)
			}
		}
	}
	if len(keyToLabel) == 0 {
		t.Fatal("media rule descriptors are empty")
	}
}

func assertTableSchema(t *testing.T, table model.Table) {
	t.Helper()
	if strings.TrimSpace(table.Key) == "" {
		t.Errorf("table has empty Key: %+v", table)
	}
	if len(table.Columns) == 0 || len(table.ColumnKeys) != len(table.Columns) {
		t.Errorf("table %q columns/key lengths = %d/%d", table.Key, len(table.Columns), len(table.ColumnKeys))
	}
	seen := make(map[string]bool, len(table.ColumnKeys))
	for _, key := range table.ColumnKeys {
		if strings.TrimSpace(key) == "" || seen[key] {
			t.Errorf("table %q has empty or duplicate column key %q", table.Key, key)
		}
		seen[key] = true
	}
	if table.RowIdentity != "" && !seen[table.RowIdentity] {
		t.Errorf("table %q RowIdentity %q cannot be resolved", table.Key, table.RowIdentity)
	}
	for rowIndex, row := range table.Rows {
		if len(row) != len(table.Columns) {
			t.Errorf("table %q row %d has %d cells, want %d", table.Key, rowIndex, len(row), len(table.Columns))
		}
	}
}

func assertUniqueTableKeys(t *testing.T, tables []model.Table) {
	t.Helper()
	seen := make(map[string]bool, len(tables))
	for _, table := range tables {
		if seen[table.Key] {
			t.Errorf("duplicate table key %q", table.Key)
		}
		seen[table.Key] = true
	}
}

func isModelTableType(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name != "Table" {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "model"
}

func tableLiteralFields(literal *ast.CompositeLit) map[string]ast.Expr {
	fields := make(map[string]ast.Expr, len(literal.Elts))
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if ok {
			fields[name.Name] = field.Value
		}
	}
	return fields
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func stringSliceLiteral(expr ast.Expr) ([]string, bool) {
	literal, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	values := make([]string, 0, len(literal.Elts))
	for _, element := range literal.Elts {
		value, ok := stringLiteral(element)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func containsSchemaString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
