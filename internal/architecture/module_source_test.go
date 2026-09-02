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
	moduleImportPath = "ecs/internal/module"
	probeImportPath  = "ecs/internal/probe"
	configImportPath = "ecs/internal/config"
	runnerImportPath = "ecs/internal/runner"
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
				case moduleImportPath, probeImportPath, configImportPath:
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
		selector, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != typeName {
			return true
		}
		packageIdent, ok := selector.X.(*ast.Ident)
		if ok && bindings[packageIdent.Name] == importedPath {
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

func hasLocalIdentifier(source productionSource, packagePath, symbolName string) bool {
	if source.packagePath != packagePath {
		return false
	}
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == symbolName {
			found = true
			return false
		}
		return !found
	})
	return found
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
	for _, importPath := range []string{moduleImportPath, probeImportPath} {
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

func TestProductionSourceHasNoRemovedModuleAdaptersOrRunnerBindings(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	if _, err := os.Stat(filepath.Join(root, "internal", "config", "modules.go")); !os.IsNotExist(err) {
		t.Fatalf("removed config/modules.go still exists: %v", err)
	}
	for _, source := range sources {
		for _, name := range []string{"ModuleDescriptors", "ModuleDescriptorFor", "ModuleIDs", "moduleDescriptors"} {
			uses, err := hasImportedSymbol(source, configImportPath, name)
			if err != nil {
				t.Fatalf("resolve config symbol %q in %s: %v", name, source.path, err)
			}
			if uses {
				t.Fatalf("production source %s still declares or references removed adapter %q", source.path, name)
			}
		}
		if source.packagePath == runnerImportPath {
			for _, name := range []string{
				"bindModuleProbes", "bindBuiltinModules", "moduleBinding", "selectBindings", "runBinding", "bindingTitle",
			} {
				if hasLocalIdentifier(source, runnerImportPath, name) {
					t.Fatalf("runner source %s still contains removed binding path %q", source.path, name)
				}
			}
		}
		if uses, err := hasImportedSymbol(source, probeImportPath, "BuiltinCatalog"); err != nil {
			t.Fatalf("resolve BuiltinCatalog references in %s: %v", source.path, err)
		} else if uses {
			t.Fatalf("production source %s declares or references removed BuiltinCatalog", source.path)
		}
	}
}

func TestProductionConfigAndProbeHaveNoRuntimeRegistration(t *testing.T) {
	root := repositoryRoot(t)
	for _, source := range loadAllProductionSources(t, root) {
		if source.packagePath != configImportPath && source.packagePath != probeImportPath {
			continue
		}
		if hasLocalFunction(source, source.packagePath, "init") {
			t.Fatalf("%s has runtime init registration in %s", source.packagePath, source.path)
		}
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if strings.HasPrefix(function.Name.Name, "Register") || strings.HasPrefix(function.Name.Name, "registerModule") {
				t.Fatalf("%s has registration-style API %q in %s", source.packagePath, function.Name.Name, source.path)
			}
		}
	}
}
