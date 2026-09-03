package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	appImportPath           = "ecs/internal/app"
	configImportPath        = "ecs/internal/config"
	moduleImportPath        = "ecs/internal/module"
	probeImportPath         = "ecs/internal/probe"
	reportImportPath        = "ecs/internal/report"
	runnerImportPath        = "ecs/internal/runner"
	scoreImportPath         = "ecs/internal/score"
	toolImportPath          = "ecs/internal/tool"
	toolsmanifestImportPath = "ecs/internal/toolsmanifest"
)

type productionSource struct {
	path        string
	packagePath string
	file        *ast.File
}

func loadAllProductionSources(t *testing.T, root string) []productionSource {
	t.Helper()
	var result []productionSource
	for _, relativeRoot := range []string{"cmd", "internal"} {
		sourceRoot := filepath.Join(root, relativeRoot)
		err := filepath.WalkDir(sourceRoot, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if excludedProductionDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), filePath, nil, parser.ParseComments)
			if err != nil {
				return fmt.Errorf("parse %s: %w", filePath, err)
			}
			result = append(result, productionSource{path: filePath, packagePath: packageImportPath(root, filePath), file: file})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func excludedProductionDirectory(name string) bool {
	switch name {
	case ".git", ".cache", "cache", "generated", "gen", "testdata", "vendor":
		return true
	default:
		return false
	}
}

func packageImportPath(root, filePath string) string {
	relative, err := filepath.Rel(root, filepath.Dir(filePath))
	if err != nil {
		return ""
	}
	return "ecs/" + filepath.ToSlash(relative)
}

func isBuiltinDefinitionSource(root, filePath string) bool {
	want := filepath.Join(root, "internal", "probe", "definitions.go")
	return filepath.Clean(filePath) == filepath.Clean(want)
}

func isBootstrapSource(root, filePath string) bool {
	want := filepath.Join(root, "internal", "app", "bootstrap.go")
	return filepath.Clean(filePath) == filepath.Clean(want)
}

// resolveImportBindings maps the lexical package name used by a selector to
// its import path. It deliberately rejects dot imports for the packages whose
// symbols this guard tracks: without a lexical qualifier an AST-only source
// proof could not distinguish those symbols from local declarations.
func resolveImportBindings(file *ast.File) (map[string]string, error) {
	bindings := make(map[string]string, len(file.Imports))
	for _, specification := range file.Imports {
		importPath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("unquote import path %q: %w", specification.Path.Value, err)
		}
		localName := path.Base(importPath)
		if specification.Name != nil {
			localName = specification.Name.Name
			if localName == "." {
				switch importPath {
				case moduleImportPath, probeImportPath, configImportPath, toolImportPath, "sync", "reflect":
					return nil, fmt.Errorf("dot import of tracked package %q is ambiguous", importPath)
				default:
					continue
				}
			}
			if localName == "_" {
				continue
			}
		}
		if previous, exists := bindings[localName]; exists && previous != importPath {
			return nil, fmt.Errorf("import name %q resolves to both %q and %q", localName, previous, importPath)
		}
		bindings[localName] = importPath
	}
	return bindings, nil
}

func hasImportedCompositeLiteral(source productionSource, importedPath, typeName string) (bool, error) {
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if containsImportedType(literal.Type, bindings, importedPath, typeName) {
			found = true
		}
		return !found
	})
	return found, nil
}

func hasImportedCall(source productionSource, importedPath, functionName string) (bool, error) {
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch function := call.Fun.(type) {
		case *ast.Ident:
			if source.packagePath == importedPath && function.Name == functionName {
				found = true
			}
		case *ast.SelectorExpr:
			packageIdent, ok := function.X.(*ast.Ident)
			if ok && function.Sel.Name == functionName && bindings[packageIdent.Name] == importedPath {
				found = true
			}
		}
		return !found
	})
	return found, nil
}

// hasImportedSymbol includes local references in the imported package itself
// and qualified references through default or aliased imports elsewhere. It
// intentionally does not match an unrelated unqualified function in another
// package that happens to have the same spelling.
func hasImportedSymbol(source productionSource, importedPath, symbolName string) (bool, error) {
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		switch value := node.(type) {
		case *ast.Ident:
			if source.packagePath == importedPath && value.Name == symbolName {
				found = true
			}
		case *ast.SelectorExpr:
			packageIdent, ok := value.X.(*ast.Ident)
			if ok && value.Sel.Name == symbolName && bindings[packageIdent.Name] == importedPath {
				found = true
			}
		}
		return !found
	})
	return found, nil
}

func hasLocalFunction(source productionSource, packagePath, functionName string) bool {
	if source.packagePath != packagePath {
		return false
	}
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == functionName {
			return true
		}
	}
	return false
}

func hasLocalType(source productionSource, packagePath, typeName string) bool {
	if source.packagePath != packagePath {
		return false
	}
	for _, declaration := range source.file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == typeName {
				return true
			}
		}
	}
	return false
}

func hasLocalFunctionPrefix(source productionSource, packagePath, prefix string) bool {
	if source.packagePath != packagePath {
		return false
	}
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && strings.HasPrefix(function.Name.Name, prefix) {
			return true
		}
	}
	return false
}

func containsImportedType(expression ast.Expr, bindings map[string]string, importedPath, typeName string) bool {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		packageIdent, ok := value.X.(*ast.Ident)
		return ok && value.Sel.Name == typeName && bindings[packageIdent.Name] == importedPath
	case *ast.ArrayType:
		return containsImportedType(value.Elt, bindings, importedPath, typeName)
	case *ast.Ellipsis:
		return containsImportedType(value.Elt, bindings, importedPath, typeName)
	case *ast.StarExpr:
		return containsImportedType(value.X, bindings, importedPath, typeName)
	case *ast.ParenExpr:
		return containsImportedType(value.X, bindings, importedPath, typeName)
	case *ast.MapType:
		return containsImportedType(value.Key, bindings, importedPath, typeName) || containsImportedType(value.Value, bindings, importedPath, typeName)
	case *ast.ChanType:
		return containsImportedType(value.Value, bindings, importedPath, typeName)
	default:
		return false
	}
}

func hasDescriptorConstructor(source productionSource) (bool, error) {
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return false, err
	}
	if source.packagePath == probeImportPath && hasLocalFunctionPrefix(source, probeImportPath, "moduleDescriptor") {
		return true, nil
	}
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Type.Results == nil {
			continue
		}
		for _, result := range function.Type.Results.List {
			if containsImportedType(result.Type, bindings, moduleImportPath, "Descriptor") {
				return true, nil
			}
		}
	}
	return false, nil
}

func builtinModuleIDs(root string, sources []productionSource) ([]string, error) {
	for _, source := range sources {
		if !isBuiltinDefinitionSource(root, source.path) {
			continue
		}
		var ids []string
		ast.Inspect(source.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || !strings.HasPrefix(function.Name, "moduleDescriptor") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			id, err := strconv.Unquote(literal.Value)
			if err == nil && strings.TrimSpace(id) != "" {
				ids = append(ids, id)
			}
			return true
		})
		if len(ids) < 2 {
			return nil, fmt.Errorf("builtin module source yielded %d IDs", len(ids))
		}
		return ids, nil
	}
	return nil, fmt.Errorf("builtin module definitions source was not found")
}

func hasStaticModuleIDTable(file *ast.File, canonical map[string]struct{}) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		literal, ok := node.(*ast.CompositeLit)
		if !ok || len(literal.Elts) < 2 || !isCollectionCompositeType(literal.Type) {
			return true
		}
		entriesWithIDs := 0
		for _, element := range literal.Elts {
			if hasCanonicalString(element, canonical) {
				entriesWithIDs++
			}
		}
		if entriesWithIDs >= 2 && entriesWithIDs == len(literal.Elts) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isScorePackage(packagePath string) bool {
	return packagePath == scoreImportPath || strings.HasPrefix(packagePath, scoreImportPath+"/")
}

func parseDetectorSnippet(t *testing.T, source, packagePath string) productionSource {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "snippet.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse detector snippet: %v", err)
	}
	return productionSource{path: "snippet.go", packagePath: packagePath, file: file}
}

func TestSourceDetectorsResolveImportAliases(t *testing.T) {
	source := parseDetectorSnippet(t, `package consumer

import (
	mod "ecs/internal/module"
	p "ecs/internal/probe"
)

func use() {
	_ = mod.Descriptor{}
	_ = p.Definition{}
	_ = p.BuiltinDefinitions()
}

func descriptor() mod.Descriptor { return mod.Descriptor{} }
`, "ecs/internal/consumer")

	if found, err := hasImportedCompositeLiteral(source, moduleImportPath, "Descriptor"); err != nil || !found {
		t.Fatalf("aliased module.Descriptor detection = %v, %v; want true, nil", found, err)
	}
	if found, err := hasImportedCompositeLiteral(source, probeImportPath, "Definition"); err != nil || !found {
		t.Fatalf("aliased probe.Definition detection = %v, %v; want true, nil", found, err)
	}
	if found, err := hasImportedCall(source, probeImportPath, "BuiltinDefinitions"); err != nil || !found {
		t.Fatalf("aliased probe.BuiltinDefinitions detection = %v, %v; want true, nil", found, err)
	}
	if found, err := hasDescriptorConstructor(source); err != nil || !found {
		t.Fatalf("aliased module.Descriptor constructor detection = %v, %v; want true, nil", found, err)
	}

	unrelated := parseDetectorSnippet(t, `package consumer

import other "example.test/module"

func use() {
	_ = other.Descriptor{}
	_ = other.BuiltinDefinitions()
}
`, "ecs/internal/consumer")
	if found, err := hasImportedCompositeLiteral(unrelated, moduleImportPath, "Descriptor"); err != nil || found {
		t.Fatalf("unrelated Descriptor detection = %v, %v; want false, nil", found, err)
	}
	if found, err := hasImportedCall(unrelated, probeImportPath, "BuiltinDefinitions"); err != nil || found {
		t.Fatalf("unrelated BuiltinDefinitions detection = %v, %v; want false, nil", found, err)
	}
}

func TestSourceDetectorsRejectTrackedDotImports(t *testing.T) {
	for _, importPath := range []string{moduleImportPath, probeImportPath, configImportPath, toolImportPath, "sync", "reflect"} {
		source := parseDetectorSnippet(t, "package consumer\n\nimport . \""+importPath+"\"\n", "ecs/internal/consumer")
		if _, err := resolveImportBindings(source.file); err == nil {
			t.Fatalf("dot import %q was accepted by source detector", importPath)
		}
	}
}

func TestLoadAllProductionSourcesIncludesCommandPackages(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	packages := make(map[string]bool)
	for _, source := range sources {
		packages[source.packagePath] = true
	}
	for _, packagePath := range []string{"ecs/cmd/ecs", "ecs/cmd/tools-manifest-check", "ecs/internal/app", "ecs/internal/probe"} {
		if !packages[packagePath] {
			t.Errorf("production source scan omitted package %q", packagePath)
		}
	}
}

func TestModuleIDTableDetectorDistinguishesCatalogFacts(t *testing.T) {
	canonical := map[string]struct{}{"system": {}, "network": {}, "bgp": {}, "cpu": {}}
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name: "replicated ordered module list",
			source: `package config
var moduleIDs = []string{"system", "network", "bgp"}
	`,
			want: true,
		},
		{
			name: "single incidental module ID",
			source: `package config
var moduleIDs = []string{"system"}
	`,
			want: false,
		},
		{
			name: "unrelated domain list",
			source: `package config
var regions = []string{"systematic", "networking", "bgp-like"}
	`,
			want: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := parseDetectorSnippet(t, test.source, configImportPath)
			if got := hasStaticModuleIDTable(source.file, canonical); got != test.want {
				t.Fatalf("module ID table detector = %t, want %t", got, test.want)
			}
		})
	}
}

func TestProductionModuleSourceHasSingleDefinitionOwner(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	definitionDeclarations := 0
	typeDeclarations := 0
	for _, source := range sources {
		if source.packagePath == probeImportPath && hasLocalFunction(source, probeImportPath, "BuiltinDefinitions") {
			definitionDeclarations++
			if !isBuiltinDefinitionSource(root, source.path) {
				t.Fatalf("BuiltinDefinitions is declared outside definitions.go: %s", source.path)
			}
		}
		if source.packagePath == probeImportPath && hasLocalType(source, probeImportPath, "Definition") {
			typeDeclarations++
			if !isBuiltinDefinitionSource(root, source.path) {
				t.Fatalf("Definition type is declared outside definitions.go: %s", source.path)
			}
		}

		if found, err := hasImportedCompositeLiteral(source, moduleImportPath, "Descriptor"); err != nil {
			t.Fatalf("resolve imports in %s: %v", source.path, err)
		} else if found && !isBuiltinDefinitionSource(root, source.path) {
			t.Fatalf("production source %s declares a replicated module descriptor literal", source.path)
		}
		if found, err := hasImportedCompositeLiteral(source, probeImportPath, "Definition"); err != nil {
			t.Fatalf("resolve imports in %s: %v", source.path, err)
		} else if found && !isBuiltinDefinitionSource(root, source.path) {
			t.Fatalf("production source %s constructs a replicated probe Definition", source.path)
		}
		if found, err := hasDescriptorConstructor(source); err != nil {
			t.Fatalf("resolve descriptor constructors in %s: %v", source.path, err)
		} else if found && !isBuiltinDefinitionSource(root, source.path) {
			t.Fatalf("production source %s declares a replicated descriptor constructor", source.path)
		}
	}
	if definitionDeclarations != 1 {
		t.Fatalf("BuiltinDefinitions declarations = %d, want exactly one", definitionDeclarations)
	}
	if typeDeclarations != 1 {
		t.Fatalf("Definition type declarations = %d, want exactly one", typeDeclarations)
	}

	for _, source := range sources {
		if isBuiltinDefinitionSource(root, source.path) {
			continue
		}
		uses, err := hasImportedSymbol(source, probeImportPath, "BuiltinDefinitions")
		if err != nil {
			t.Fatalf("resolve BuiltinDefinitions references in %s: %v", source.path, err)
		}
		if uses && !isBootstrapSource(root, source.path) {
			t.Fatalf("production source %s reaches builtin definitions outside bootstrap", source.path)
		}
	}
}

func TestProductionSourcesHaveNoReplicatedModuleIDTables(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	ids, err := builtinModuleIDs(root, sources)
	if err != nil {
		t.Fatal(err)
	}
	canonical := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := canonical[id]; exists {
			t.Fatalf("builtin module source repeats ID %q", id)
		}
		canonical[id] = struct{}{}
	}
	for _, source := range sources {
		// The Definition source is the one canonical module table. Score
		// dimensions intentionally own their independent metric membership;
		// they are not module catalog metadata and remain outside this guard.
		if isBuiltinDefinitionSource(root, source.path) || isScorePackage(source.packagePath) {
			continue
		}
		if hasStaticModuleIDTable(source.file, canonical) {
			t.Fatalf("production source %s contains a replicated module ID table", source.path)
		}
	}
}

func TestProductionSourceHasNoRemovedModuleAdaptersOrRunnerBindings(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	if _, err := os.Stat(filepath.Join(root, "internal", "config", "modules.go")); !os.IsNotExist(err) {
		t.Fatalf("removed config/modules.go still exists: %v", err)
	}
	for _, source := range sources {
		for _, name := range []string{
			"ModuleDescriptors", "ModuleDescriptorFor", "ModuleIDs", "moduleDescriptors",
			"bindModuleProbes", "bindBuiltinModules", "moduleBinding", "selectBindings", "runBinding", "bindingTitle",
			"moduleFactories", "moduleEstimates",
		} {
			if hasIdentifierNamed(source, name) {
				t.Fatalf("production source %s still declares or references removed architecture symbol %q", source.path, name)
			}
		}
		for _, name := range []string{"Builtins", "BuiltinCatalog"} {
			uses, err := hasImportedSymbol(source, probeImportPath, name)
			if err != nil {
				t.Fatalf("resolve removed probe symbol %q in %s: %v", name, source.path, err)
			}
			if uses {
				t.Fatalf("production source %s still declares or references removed probe API %q", source.path, name)
			}
		}
	}
}

func TestCompositionPackagesHaveNoRuntimeRegistration(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	moduleIDs, err := builtinModuleIDs(root, sources)
	if err != nil {
		t.Fatal(err)
	}
	canonicalModuleIDs := make(map[string]struct{}, len(moduleIDs))
	for _, id := range moduleIDs {
		canonicalModuleIDs[id] = struct{}{}
	}
	globalFacts := packageGlobalFacts(sources)
	typeSpecs := packageTypeSpecs(sources)
	for _, source := range sources {
		// Mutation and registration APIs are forbidden wherever they appear,
		// including command entry points and relocated helper packages. The
		// detector itself is receiver/context aware so unrelated domain APIs
		// such as DeleteReport remain valid.
		if name, ok := catalogMutationAPIWithTypes(source, typeSpecs[source.packagePath]); ok {
			t.Fatalf("%s has registration/mutation API %q in %s", source.packagePath, name, source.path)
		}
		if hasRuntimeRegistrationInitWithFacts(source, globalFacts[source.packagePath], canonicalModuleIDs) {
			t.Fatalf("%s has runtime init registration in %s", source.packagePath, source.path)
		}
	}
}

func TestRuntimeRegistrationInitDetectorIsSemantic(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		moduleIDs []string
		want      bool
	}{
		{
			name:   "harmless config init",
			source: "package config\nfunc init() { _ = 1 }\n",
			want:   false,
		},
		{
			name:   "harmless domain call",
			source: "package config\nimport \"runtime\"\nfunc init() { runtime.GOMAXPROCS(1) }\n",
			want:   false,
		},
		{
			name:   "harmless module state initialization",
			source: "package config\nvar moduleCache int\nfunc init() { moduleCache = 1 }\n",
			want:   false,
		},
		{
			name:   "module registration call",
			source: "package config\nfunc init() { registerModule(Definition{}) }\n",
			want:   true,
		},
		{
			name:   "registry state assignment",
			source: "package config\nvar moduleRegistry map[string]Definition\nfunc init() { moduleRegistry[\"fixture\"] = Definition{} }\n",
			want:   true,
		},
		{
			name:   "registry state append",
			source: "package config\nvar moduleDefinitions []Definition\nfunc init() { _ = append(moduleDefinitions, Definition{}) }\n",
			want:   true,
		},
		{
			name:   "catalog mutation call",
			source: "package config\nfunc init() { catalog.Add(Definition{}) }\n",
			want:   true,
		},
		{
			name:   "unrelated registration name",
			source: "package config\nfunc init() { registerMetrics() }\n",
			want:   false,
		},
		{
			name:      "neutral open registry with canonical probe",
			source:    "package probe\nvar state = map[string]any{}\ntype cpuProbe struct{}\nfunc init() { state[\"cpu\"] = cpuProbe{} }\n",
			moduleIDs: []string{"cpu"},
			want:      true,
		},
		{
			name:      "neutral open state without canonical probe",
			source:    "package probe\nvar state = map[string]any{}\nfunc init() { state[\"other\"] = 1 }\n",
			moduleIDs: []string{"cpu"},
			want:      false,
		},
		{
			name:      "neutral open registry with allocated probe",
			source:    "package probe\nvar state = map[string]any{}\ntype cpuProbe struct{}\nfunc init() { state[\"cpu\"] = new(cpuProbe) }\n",
			moduleIDs: []string{"cpu"},
			want:      true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := parseDetectorSnippet(t, test.source, configImportPath)
			if test.moduleIDs != nil {
				source.packagePath = probeImportPath
			}
			moduleIDs := make(map[string]struct{}, len(test.moduleIDs))
			for _, id := range test.moduleIDs {
				moduleIDs[id] = struct{}{}
			}
			facts := packageGlobalFacts([]productionSource{source})[source.packagePath]
			if got := hasRuntimeRegistrationInitWithFacts(source, facts, moduleIDs); got != test.want {
				t.Fatalf("runtime init detector = %t, want %t", got, test.want)
			}
		})
	}

	global := parseDetectorSnippet(t, "package probe\nvar state = map[string]any{}\n", probeImportPath)
	initializer := parseDetectorSnippet(t, "package probe\ntype cpuProbe struct{}\nfunc init() { state[\"cpu\"] = cpuProbe{} }\n", probeImportPath)
	facts := packageGlobalFacts([]productionSource{global, initializer})[probeImportPath]
	if !hasRuntimeRegistrationInitWithFacts(initializer, facts, map[string]struct{}{"cpu": {}}) {
		t.Fatal("cross-file neutral registry init was not detected")
	}
}
