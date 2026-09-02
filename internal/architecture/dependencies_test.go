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
			"ecs/internal/config", "ecs/internal/probe", "ecs/internal/app", "ecs/internal/runner",
		}},
		{name: "config", dir: filepath.Join("internal", "config"), forbid: []string{
			"ecs/internal/probe", "ecs/internal/runner", "ecs/internal/app",
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
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	imports := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, specification := range file.Imports {
			importPath, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				return nil, err
			}
			imports[importPath] = struct{}{}
		}
	}
	result := make([]string, 0, len(imports))
	for importPath := range imports {
		result = append(result, importPath)
	}
	sort.Strings(result)
	return result, nil
}
