package architecture

import (
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

// TestLayerDependenciesKeepsLowLevelContractsAcyclic parses the package
// imports directly so this guard remains independent of the shell's current
// working directory and cannot be bypassed by formatting or import aliases.
func TestLayerDependenciesKeepsLowLevelContractsAcyclic(t *testing.T) {
	root := repositoryRoot(t)
	cases := []struct {
		name   string
		dir    string
		forbid []string
	}{
		{name: "module", dir: filepath.Join("internal", "module"), forbid: []string{
			configImportPath, probeImportPath, appImportPath, runnerImportPath, toolImportPath,
		}},
		{name: "config", dir: filepath.Join("internal", "config"), forbid: []string{
			probeImportPath, runnerImportPath, appImportPath,
		}},
		{name: "probe", dir: filepath.Join("internal", "probe"), forbid: []string{
			appImportPath, runnerImportPath,
		}},
		{name: "runner", dir: filepath.Join("internal", "runner"), forbid: []string{
			appImportPath, reportImportPath, scoreImportPath,
		}},
		{name: "score", dir: filepath.Join("internal", "score"), forbid: []string{
			probeImportPath,
		}},
		{name: "report", dir: filepath.Join("internal", "report"), forbid: []string{
			probeImportPath,
		}},
		{name: "tool", dir: filepath.Join("internal", "tool"), forbid: []string{
			appImportPath, configImportPath, moduleImportPath, probeImportPath,
			runnerImportPath, toolsmanifestImportPath,
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			imports, err := packageImports(filepath.Join(root, test.dir))
			if err != nil {
				t.Fatal(err)
			}
			for _, importPath := range imports {
				for _, forbidden := range test.forbid {
					if importPath == forbidden {
						t.Fatalf("%s imports forbidden package %q", test.name, importPath)
					}
				}
			}
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func packageImports(directory string) ([]string, error) {
	imports := make(map[string]struct{})
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if excludedProductionDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, specification := range file.Imports {
			importPath, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				return err
			}
			imports[importPath] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(imports))
	for importPath := range imports {
		result = append(result, importPath)
	}
	sort.Strings(result)
	return result, nil
}

func TestPackageImportsScansProductionDescendants(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("root.go", "package root\nimport \"example/root\"\n")
	if err := os.WriteFile(filepath.Join(nested, "child.go"), []byte("package child\nimport (\n\t\"ecs/internal/config\"\n\t\"example/child\"\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "child_test.go"), []byte("package child\nimport \"example/test-only\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	imports, err := packageImports(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ecs/internal/config", "example/child", "example/root"}
	if strings.Join(imports, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("recursive production imports = %v, want %v", imports, want)
	}
}
