package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func hasIdentifierNamed(source productionSource, name string) bool {
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func catalogMutationAPI(source productionSource) (string, bool) {
	return catalogMutationAPIWithTypes(source, sourceTypeSpecs(source))
}

func catalogMutationAPIWithTypes(source productionSource, typeSpecs map[string]*ast.TypeSpec) (string, bool) {
	for _, declaration := range source.file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if receiver := receiverBaseName(declaration.Recv); receiver != "" {
				if (isCatalogReceiver(receiver) || receiverHasCompositionContextFrom(declaration, typeSpecs) || functionParameterTypesHaveMutationContext(declaration)) && hasMutationVerbPrefix(declaration.Name.Name) {
					return declaration.Name.Name, true
				}
			} else if isCatalogMutationName(declaration.Name.Name) || hasCatalogMutationContext(declaration) {
				return declaration.Name.Name, true
			}
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				values, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range values.Names {
					if isCatalogMutationName(name.Name) || hasCatalogMutationContextForValue(name.Name, values) {
						return name.Name, true
					}
				}
			}
		}
	}
	return "", false
}

func isCatalogMutationName(name string) bool {
	lower := strings.ToLower(name)
	for _, verb := range []string{"add", "remove", "set", "register", "unregister", "delete", "replace"} {
		if strings.HasPrefix(lower, verb) {
			rest := lower[len(verb):]
			if strings.Contains(rest, "module") || strings.Contains(rest, "command") || strings.Contains(rest, "tool") || strings.Contains(rest, "catalog") {
				return true
			}
		}
		if strings.HasSuffix(lower, verb) {
			start := len(name) - len(verb)
			if start > 0 && name[start] >= 'A' && name[start] <= 'Z' {
				prefix := lower[:start]
				if strings.Contains(prefix, "module") || strings.Contains(prefix, "command") || strings.Contains(prefix, "tool") || strings.Contains(prefix, "catalog") {
					return true
				}
			}
		}
	}
	return false
}

func hasMutationVerbPrefix(name string) bool {
	lower := strings.ToLower(name)
	for _, verb := range []string{"add", "remove", "set", "register", "unregister", "delete", "replace"} {
		if strings.HasPrefix(lower, verb) {
			return true
		}
	}
	return false
}

func hasCatalogMutationContext(function *ast.FuncDecl) bool {
	if !isBareMutationVerb(function.Name.Name) || function.Type == nil || function.Type.Params == nil {
		return false
	}
	for _, field := range function.Type.Params.List {
		if expressionHasMutationContext(field.Type) {
			return true
		}
		for _, name := range field.Names {
			if expressionHasMutationContext(name) {
				return true
			}
		}
	}
	return false
}

func functionParameterTypesHaveMutationContext(function *ast.FuncDecl) bool {
	if function == nil || function.Type == nil || function.Type.Params == nil {
		return false
	}
	for _, field := range function.Type.Params.List {
		if expressionHasMutationContext(field.Type) {
			return true
		}
	}
	return false
}

func hasCatalogMutationContextForValue(name string, values *ast.ValueSpec) bool {
	if !isBareMutationVerb(name) {
		return false
	}
	for _, value := range values.Values {
		if function, ok := value.(*ast.FuncLit); ok && function.Type != nil && function.Type.Params != nil {
			for _, field := range function.Type.Params.List {
				if expressionHasMutationContext(field.Type) {
					return true
				}
				for _, name := range field.Names {
					if expressionHasMutationContext(name) {
						return true
					}
				}
			}
		}
	}
	return false
}

func isBareMutationVerb(name string) bool {
	switch strings.ToLower(name) {
	case "add", "remove", "set", "register", "unregister", "delete", "replace":
		return true
	default:
		return false
	}
}

func expressionHasMutationContext(expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		name := strings.ToLower(ident.Name)
		if strings.Contains(name, "module") || strings.Contains(name, "command") || strings.Contains(name, "tool") || strings.Contains(name, "catalog") || strings.Contains(name, "definition") || strings.Contains(name, "descriptor") {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasRuntimeRegistrationInitWithFacts(source productionSource, globalFacts map[string]packageGlobalFact, moduleIDs map[string]struct{}) bool {
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "init" || function.Body == nil {
			continue
		}
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if found {
				return false
			}
			switch value := node.(type) {
			case *ast.CallExpr:
				if runtimeRegistrationCall(value) || appendTouchesCompositionState(value, globalFacts, moduleIDs) {
					found = true
				}
			case *ast.AssignStmt:
				for index, expression := range value.Lhs {
					var rhs ast.Expr
					if index < len(value.Rhs) {
						rhs = value.Rhs[index]
					}
					if assignmentTouchesCompositionState(expression, rhs, globalFacts, moduleIDs) {
						found = true
						break
					}
				}
			case *ast.IncDecStmt:
				if assignmentTouchesCompositionState(value.X, nil, globalFacts, moduleIDs) {
					found = true
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

type packageGlobalFact struct {
	name        string
	typeExpr    ast.Expr
	initializer ast.Expr
}

// packageGlobalFacts joins package-level declarations across files. Runtime
// registration can be split between a neutral global in one file and init in
// another, so a same-file name check is insufficient for this guard.
func packageGlobalFacts(sources []productionSource) map[string]map[string]packageGlobalFact {
	result := make(map[string]map[string]packageGlobalFact)
	for _, source := range sources {
		facts := result[source.packagePath]
		if facts == nil {
			facts = make(map[string]packageGlobalFact)
			result[source.packagePath] = facts
		}
		for _, declaration := range source.file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				values, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range values.Names {
					var initializer ast.Expr
					if index < len(values.Values) {
						initializer = values.Values[index]
					}
					facts[name.Name] = packageGlobalFact{name: name.Name, typeExpr: values.Type, initializer: initializer}
				}
			}
		}
	}
	return result
}

func runtimeRegistrationCall(call *ast.CallExpr) bool {
	if call == nil {
		return false
	}
	var name string
	var receiver ast.Expr
	switch function := call.Fun.(type) {
	case *ast.Ident:
		name = function.Name
	case *ast.SelectorExpr:
		name = function.Sel.Name
		receiver = function.X
	default:
		return false
	}
	if isCatalogMutationName(name) {
		return true
	}
	if !isBareMutationVerb(name) {
		return false
	}
	if receiver != nil && expressionHasCatalogStateContext(receiver) {
		return true
	}
	for _, argument := range call.Args {
		if expressionHasMutationContext(argument) {
			return true
		}
	}
	return false
}

func appendTouchesCompositionState(call *ast.CallExpr, globalFacts map[string]packageGlobalFact, moduleIDs map[string]struct{}) bool {
	if call == nil || len(call.Args) == 0 {
		return false
	}
	function, ok := call.Fun.(*ast.Ident)
	if !ok || function.Name != "append" {
		return false
	}
	return assignmentTouchesCompositionState(call.Args[0], nil, globalFacts, moduleIDs)
}

func expressionHasCatalogStateContext(expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && isCatalogStateName(ident.Name) {
			found = true
			return false
		}
		return true
	})
	return found
}

func assignmentTouchesCompositionState(expression ast.Expr, rhs ast.Expr, globalFacts map[string]packageGlobalFact, moduleIDs map[string]struct{}) bool {
	if expression == nil {
		return false
	}
	root := expression
	var indexKey ast.Expr
	for {
		switch value := root.(type) {
		case *ast.IndexExpr:
			indexKey = value.Index
			root = value.X
		case *ast.IndexListExpr:
			if len(value.Indices) > 0 {
				indexKey = value.Indices[0]
			}
			root = value.X
		case *ast.SelectorExpr:
			if isCatalogStateName(value.Sel.Name) {
				return true
			}
			root = value.X
		case *ast.StarExpr:
			root = value.X
		case *ast.ParenExpr:
			root = value.X
		case *ast.Ident:
			fact, global := globalFacts[value.Name]
			if !global {
				return false
			}
			if isCatalogStateName(value.Name) || expressionHasCompositionType(fact.typeExpr) {
				return true
			}
			return globalOpenRegistryMutation(fact, indexKey, rhs, moduleIDs)
		default:
			return expressionHasMutationContext(expression)
		}
	}
}

func globalOpenRegistryMutation(fact packageGlobalFact, indexKey, rhs ast.Expr, moduleIDs map[string]struct{}) bool {
	if !globalOpenRegistryShape(fact) || len(moduleIDs) == 0 || !isCanonicalString(indexKey, moduleIDs) {
		return false
	}
	return expressionIsConcreteProbeValue(rhs)
}

func globalOpenRegistryShape(fact packageGlobalFact) bool {
	expression := fact.typeExpr
	if expression == nil {
		expression = fact.initializer
	}
	if !expressionIsMutable(expression) {
		return false
	}
	return expressionContainsAnyOrEmptyInterface(expression)
}

func expressionContainsAnyOrEmptyInterface(expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "any" {
			found = true
			return false
		}
		if interfaceType, ok := node.(*ast.InterfaceType); ok && interfaceType.Methods != nil && len(interfaceType.Methods.List) == 0 {
			found = true
			return false
		}
		return true
	})
	return found
}

func isCanonicalString(expression ast.Expr, canonical map[string]struct{}) bool {
	for {
		switch value := expression.(type) {
		case *ast.ParenExpr:
			expression = value.X
			continue
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				return false
			}
			decoded, err := strconv.Unquote(value.Value)
			if err != nil {
				return false
			}
			_, ok := canonical[decoded]
			return ok
		default:
			return false
		}
	}
}

func expressionIsConcreteProbeValue(expression ast.Expr) bool {
	for {
		switch value := expression.(type) {
		case *ast.ParenExpr:
			expression = value.X
			continue
		case *ast.StarExpr:
			expression = value.X
			continue
		case *ast.CompositeLit:
			name := strings.ToLower(expressionTypeName(value.Type))
			return strings.HasSuffix(name, "probe")
		case *ast.CallExpr:
			function, ok := value.Fun.(*ast.Ident)
			if !ok || function.Name != "new" || len(value.Args) != 1 {
				return false
			}
			name := strings.ToLower(expressionTypeName(value.Args[0]))
			return strings.HasSuffix(name, "probe")
		default:
			return false
		}
	}
}

func isCatalogReceiver(name string) bool {
	return name == "Catalog" || name == "moduleCatalog" || name == "commandCatalog" || name == "toolCatalog"
}

func receiverBaseName(receiver *ast.FieldList) string {
	if receiver == nil || len(receiver.List) == 0 {
		return ""
	}
	return receiverTypeBaseName(receiver.List[0].Type)
}

func receiverTypeBaseName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.StarExpr:
		return receiverTypeBaseName(value.X)
	case *ast.ParenExpr:
		return receiverTypeBaseName(value.X)
	case *ast.IndexExpr:
		return receiverTypeBaseName(value.X)
	case *ast.IndexListExpr:
		return receiverTypeBaseName(value.X)
	default:
		return ""
	}
}

func sourceTypeSpecs(source productionSource) map[string]*ast.TypeSpec {
	result := make(map[string]*ast.TypeSpec)
	for _, declaration := range source.file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if ok {
				result[typeSpec.Name.Name] = typeSpec
			}
		}
	}
	return result
}

func packageTypeSpecs(sources []productionSource) map[string]map[string]*ast.TypeSpec {
	result := make(map[string]map[string]*ast.TypeSpec)
	for _, source := range sources {
		typeSpecs := result[source.packagePath]
		if typeSpecs == nil {
			typeSpecs = make(map[string]*ast.TypeSpec)
			result[source.packagePath] = typeSpecs
		}
		for name, typeSpec := range sourceTypeSpecs(source) {
			typeSpecs[name] = typeSpec
		}
	}
	return result
}

func receiverHasCompositionContextFrom(function *ast.FuncDecl, typeSpecs map[string]*ast.TypeSpec) bool {
	if function == nil {
		return false
	}
	return sourceTypeHasCompositionContextFrom(typeSpecs, receiverBaseName(function.Recv), make(map[string]struct{}))
}

func sourceTypeHasCompositionContext(source productionSource, name string) bool {
	return sourceTypeHasCompositionContextFrom(sourceTypeSpecs(source), name, make(map[string]struct{}))
}

func sourceTypeHasCompositionContextFrom(typeSpecs map[string]*ast.TypeSpec, name string, seen map[string]struct{}) bool {
	if name == "" {
		return false
	}
	if _, alreadySeen := seen[name]; alreadySeen {
		return false
	}
	seen[name] = struct{}{}
	typeSpec := typeSpecs[name]
	if typeSpec == nil {
		return false
	}
	if typeSpecHasCompositionContext(typeSpec) {
		return true
	}
	return sourceTypeHasCompositionAlias(typeSpecs, typeSpec.Type, seen)
}

func sourceTypeHasCompositionAlias(typeSpecs map[string]*ast.TypeSpec, expression ast.Expr, seen map[string]struct{}) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return sourceTypeHasCompositionContextFrom(typeSpecs, value.Name, seen)
	case *ast.ParenExpr:
		return sourceTypeHasCompositionAlias(typeSpecs, value.X, seen)
	case *ast.StarExpr:
		return sourceTypeHasCompositionAlias(typeSpecs, value.X, seen)
	default:
		return false
	}
}

func typeSpecHasCompositionContext(typeSpec *ast.TypeSpec) bool {
	if typeSpec == nil {
		return false
	}
	if isCompositionTypeName(typeSpec.Name.Name) {
		return true
	}
	structType, ok := typeSpec.Type.(*ast.StructType)
	return ok && structHasCompositionField(structType)
}

func structHasCompositionField(structType *ast.StructType) bool {
	if structType == nil || structType.Fields == nil {
		return false
	}
	for _, field := range structType.Fields.List {
		if expressionHasCompositionType(field.Type) {
			return true
		}
		for _, name := range field.Names {
			if isCatalogStateName(name.Name) {
				return true
			}
		}
	}
	return false
}

func isCompositionPlumbingSource(source productionSource) bool {
	// The app package is the composition boundary: command handlers and their
	// helpers all receive the per-invocation application value. The module,
	// runner, and tool packages contain the typed catalog plumbing. Match
	// package descendants too, so moving a helper to a new file or subpackage
	// cannot evade the guard.
	for _, root := range []string{appImportPath, moduleImportPath, runnerImportPath, toolImportPath} {
		if source.packagePath == root || strings.HasPrefix(source.packagePath, root+"/") {
			return true
		}
	}

	// Probe and config also contain legitimate domain tables and open JSON
	// values. Include only sources that carry a composition signal there (or
	// in a newly introduced internal helper), leaving unrelated domain state
	// such as regexp tables and error sentinels outside this narrow audit.
	if source.packagePath == probeImportPath || strings.HasPrefix(source.packagePath, probeImportPath+"/") {
		return hasProbeCompositionSignal(source)
	}
	if source.packagePath == configImportPath || strings.HasPrefix(source.packagePath, configImportPath+"/") {
		return hasModuleCompositionSignal(source)
	}
	if strings.HasPrefix(source.packagePath, "ecs/internal/") || strings.HasPrefix(source.packagePath, "ecs/cmd/") {
		return hasModuleCompositionSignal(source)
	}
	return false
}

func hasProbeCompositionSignal(source productionSource) bool {
	if hasCompositionContextType(source) {
		return true
	}
	return hasIdentifierNamed(source, "Definition") ||
		hasIdentifierNamed(source, "Descriptor") ||
		hasIdentifierNamed(source, "BuiltinDefinitions") ||
		hasIdentifierNamed(source, "CatalogFromDefinitions")
}

func hasModuleCompositionSignal(source productionSource) bool {
	if hasRegistryShapedPackageVariable(source) || hasCompositionContextType(source) {
		return true
	}
	for _, declaration := range source.file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			name := strings.ToLower(declaration.Name.Name)
			if (strings.Contains(name, "factory") || strings.Contains(name, "registry")) &&
				(strings.Contains(name, "module") || strings.Contains(name, "command") || strings.Contains(name, "tool") || strings.Contains(name, "catalog")) {
				return true
			}
			if isCompositionFunction(declaration) {
				return true
			}
			if functionReturnsCompositionType(declaration) {
				return true
			}
		case *ast.GenDecl:
			if declaration.Tok != token.TYPE {
				continue
			}
			for _, specification := range declaration.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				name := strings.ToLower(typeSpec.Name.Name)
				if strings.Contains(name, "definition") || strings.Contains(name, "descriptor") || strings.Contains(name, "catalog") || strings.Contains(name, "commandhandler") || strings.Contains(name, "doctortool") {
					return true
				}
			}
		}
	}
	return false
}

func hasCompositionContextType(source productionSource) bool {
	for name := range sourceTypeSpecs(source) {
		if sourceTypeHasCompositionContext(source, name) {
			return true
		}
	}
	return false
}

func functionReturnsCompositionType(function *ast.FuncDecl) bool {
	if function.Type == nil || function.Type.Results == nil {
		return false
	}
	for _, field := range function.Type.Results.List {
		if expressionHasCompositionType(field.Type) {
			return true
		}
	}
	return false
}

func expressionHasCompositionType(expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			name := strings.ToLower(ident.Name)
			if strings.Contains(name, "definition") || strings.Contains(name, "descriptor") || strings.Contains(name, "catalog") || strings.Contains(name, "commandhandler") || strings.Contains(name, "doctortool") {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func weakCompositionPattern(source productionSource) string {
	return weakCompositionPatternWithTypes(source, sourceTypeSpecs(source))
}

func weakCompositionPatternWithTypes(source productionSource, typeSpecs map[string]*ast.TypeSpec) string {
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return err.Error()
	}
	if name, ok := packageLevelVariable(source); ok {
		return "package-level mutable variable " + name
	}
	if hasWeakAnyUsageWithTypes(source, typeSpecs) {
		return "any type"
	}
	if hasWeakEmptyInterfaceUsageWithTypes(source, typeSpecs) {
		return "empty interface type"
	}
	if hasWeakReflectionUsageWithTypes(source, bindings, typeSpecs) {
		return "reflection import"
	}
	if hasRegistryMutexUsageWithTypes(source, bindings, typeSpecs) {
		return "registry mutex type"
	}
	finding := ""
	ast.Inspect(source.file, func(node ast.Node) bool {
		if finding != "" {
			return false
		}
		switch value := node.(type) {
		case *ast.FuncDecl:
			name := strings.ToLower(value.Name.Name)
			if isWeakFactoryRegistryName(name) {
				finding = "weak factory or registry function"
				return false
			}
		case *ast.TypeSpec:
			name := strings.ToLower(value.Name.Name)
			if isWeakFactoryRegistryName(name) {
				finding = "weak factory or registry type"
				return false
			}
		case *ast.ValueSpec:
			for _, name := range value.Names {
				if isWeakFactoryRegistryName(name.Name) {
					finding = "weak factory or registry value"
					return false
				}
			}
		}
		return finding == ""
	})
	return finding
}

func isWeakFactoryRegistryName(name string) bool {
	lower := strings.ToLower(name)
	if lower == "factory" || lower == "registry" {
		return true
	}
	return (strings.Contains(lower, "factory") || strings.Contains(lower, "registry")) &&
		(strings.Contains(lower, "module") || strings.Contains(lower, "command") || strings.Contains(lower, "tool") || strings.Contains(lower, "catalog") || strings.Contains(lower, "definition") || strings.HasSuffix(lower, "factories") || strings.HasSuffix(lower, "registries"))
}

func astParentMap(file *ast.File) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	stack := make([]ast.Node, 0, 16)
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func enclosingFuncDecl(node ast.Node, parents map[ast.Node]ast.Node) *ast.FuncDecl {
	for current := node; current != nil; current = parents[current] {
		if function, ok := current.(*ast.FuncDecl); ok {
			return function
		}
	}
	return nil
}

func enclosingTypeSpec(node ast.Node, parents map[ast.Node]ast.Node) *ast.TypeSpec {
	for current := node; current != nil; current = parents[current] {
		if typeSpec, ok := current.(*ast.TypeSpec); ok {
			return typeSpec
		}
	}
	return nil
}

func isCompositionSemanticName(name string) bool {
	lower := strings.ToLower(name)
	for _, term := range []string{
		"module", "command", "tool", "catalog", "definition", "descriptor",
		"registry", "handler", "binding", "factory", "composition",
		"application",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func isCatalogStateName(name string) bool {
	lower := strings.ToLower(name)
	for _, term := range []string{
		"registry", "catalog", "definition", "descriptor", "handler", "binding", "factory",
		"moduleids", "moduleorder", "moduleprofile", "modules", "commands", "tools",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func isCompositionTypeName(name string) bool {
	lower := strings.ToLower(name)
	for _, term := range []string{
		"registry", "catalog", "definition", "descriptor", "handler", "binding", "factory",
		"application", "composition",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	if lower == "module" || lower == "command" || lower == "tool" {
		return true
	}
	for _, feature := range []string{"module", "command", "tool"} {
		if strings.Contains(lower, feature) && strings.Contains(lower, "option") {
			return true
		}
	}
	return false
}

func isCompositionFunction(function *ast.FuncDecl) bool {
	if function == nil {
		return false
	}
	if isCatalogReceiver(receiverBaseName(function.Recv)) {
		return true
	}
	name := strings.ToLower(function.Name.Name)
	if isWeakFactoryRegistryName(name) || isCompositionSemanticName(name) {
		return isWeakFactoryRegistryName(name) || hasCompositionVerb(name)
	}
	switch name {
	case "build", "compose", "construct", "dispatch", "register", "unregister":
		return true
	default:
		return functionReturnsCompositionType(function)
	}
}

func hasCompositionVerb(name string) bool {
	for _, verb := range []string{
		"build", "compose", "construct", "create", "make", "new", "dispatch", "register", "unregister", "bind",
	} {
		if strings.HasPrefix(name, verb) {
			return true
		}
	}
	return false
}

func nodeIsCompositionContextWithTypes(node ast.Node, source productionSource, parents map[ast.Node]ast.Node, typeSpecs map[string]*ast.TypeSpec) bool {
	if function := enclosingFuncDecl(node, parents); function != nil && (isCompositionFunction(function) || receiverHasCompositionContextFrom(function, typeSpecs)) {
		return true
	}
	if typeSpec := enclosingTypeSpec(node, parents); typeSpec != nil && sourceTypeHasCompositionContextFrom(typeSpecs, typeSpec.Name.Name, make(map[string]struct{})) {
		return true
	}
	return false
}

func hasWeakAnyUsageWithTypes(source productionSource, typeSpecs map[string]*ast.TypeSpec) bool {
	parents := astParentMap(source.file)
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == "any" && nodeIsCompositionContextWithTypes(ident, source, parents, typeSpecs) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasWeakEmptyInterfaceUsageWithTypes(source productionSource, typeSpecs map[string]*ast.TypeSpec) bool {
	parents := astParentMap(source.file)
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		interfaceType, ok := node.(*ast.InterfaceType)
		if ok && interfaceType.Methods != nil && len(interfaceType.Methods.List) == 0 && nodeIsCompositionContextWithTypes(interfaceType, source, parents, typeSpecs) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasWeakReflectionUsageWithTypes(source productionSource, bindings map[string]string, typeSpecs map[string]*ast.TypeSpec) bool {
	parents := astParentMap(source.file)
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageIdent, ok := selector.X.(*ast.Ident)
		if ok && bindings[packageIdent.Name] == "reflect" && nodeIsCompositionContextWithTypes(selector, source, parents, typeSpecs) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isSyncMutexType(expression ast.Expr, bindings map[string]string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		if parenthesized, ok := expression.(*ast.ParenExpr); ok {
			return isSyncMutexType(parenthesized.X, bindings)
		}
		return false
	}
	packageIdent, ok := selector.X.(*ast.Ident)
	return ok && bindings[packageIdent.Name] == "sync" && (selector.Sel.Name == "Mutex" || selector.Sel.Name == "RWMutex")
}

func hasRegistryMutexUsageWithTypes(source productionSource, bindings map[string]string, typeSpecs map[string]*ast.TypeSpec) bool {
	parents := astParentMap(source.file)
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || !isSyncMutexType(selector, bindings) {
			return true
		}
		for current := ast.Node(selector); current != nil; current = parents[current] {
			switch value := current.(type) {
			case *ast.TypeSpec:
				if sourceTypeHasCompositionContextFrom(typeSpecs, value.Name.Name, make(map[string]struct{})) {
					found = true
				}
			case *ast.Field:
				for _, name := range value.Names {
					if isCatalogStateName(name.Name) {
						found = true
					}
				}
			case *ast.ValueSpec:
				for _, name := range value.Names {
					if isCatalogStateName(name.Name) {
						found = true
					}
				}
			}
			if found {
				return false
			}
		}
		return true
	})
	return found
}

func packageLevelVariable(source productionSource) (string, bool) {
	for _, declaration := range source.file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			values, ok := specification.(*ast.ValueSpec)
			if !ok || len(values.Names) == 0 {
				continue
			}
			for index, name := range values.Names {
				if !isRegistryShapedVariable(name.Name, values, index) {
					continue
				}
				return name.Name, true
			}
		}
	}
	return "", false
}

func hasRegistryShapedPackageVariable(source productionSource) bool {
	_, ok := packageLevelVariable(source)
	return ok
}

func isRegistryShapedVariable(name string, values *ast.ValueSpec, index int) bool {
	lower := strings.ToLower(name)
	for _, term := range []string{
		"registry", "handler", "binding", "catalog", "definition", "estimate",
		"moduleids", "moduleorder", "moduleprofile", "modules", "commands", "tools",
	} {
		if strings.Contains(lower, term) {
			// A semantic composition-state name is forbidden even when its
			// initializer is a constructor call or another indirect value. Such
			// an initializer can still retain shared mutable state, while ordinary
			// immutable scalar sentinels are not classified as catalog state.
			return !isBasicLiteral(valueSpecExpression(values, index))
		}
	}
	if strings.Contains(lower, "factory") && (lower == "factory" || strings.Contains(lower, "module") || strings.Contains(lower, "command") || strings.Contains(lower, "tool") || strings.Contains(lower, "catalog") || strings.Contains(lower, "definition") || strings.HasSuffix(lower, "factories")) {
		return !isBasicLiteral(valueSpecExpression(values, index))
	}
	if !isMutableValueSpec(values, index) {
		return false
	}
	return expressionHasCompositionType(valueSpecExpression(values, index))
}

func isBasicLiteral(expression ast.Expr) bool {
	_, ok := expression.(*ast.BasicLit)
	return ok
}

func isMutableValueSpec(values *ast.ValueSpec, index int) bool {
	expression := valueSpecExpression(values, index)
	if values.Type != nil {
		return expressionIsMutable(values.Type)
	}
	return expressionIsMutable(expression)
}

func valueSpecExpression(values *ast.ValueSpec, index int) ast.Expr {
	if values.Type != nil {
		return values.Type
	}
	if index >= 0 && index < len(values.Values) {
		return values.Values[index]
	}
	return nil
}

func expressionIsMutable(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.ArrayType:
		// A nil length denotes a slice. Fixed-size arrays are still mutable when
		// held globally, so both forms are considered mutable state here.
		return value != nil
	case *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.StarExpr:
		return true
	case *ast.ParenExpr:
		return expressionIsMutable(value.X)
	case *ast.UnaryExpr:
		return value.Op == token.AND || expressionIsMutable(value.X)
	case *ast.CompositeLit:
		return expressionIsMutable(value.Type)
	case *ast.CallExpr:
		// Calls such as errors.New and regexp.MustCompile yield immutable
		// domain values for this guard. A call returning a tracked type is
		// handled by the semantic variable name/type checks instead.
		if function, ok := value.Fun.(*ast.Ident); ok && function.Name == "make" && len(value.Args) > 0 {
			return expressionIsMutable(value.Args[0])
		}
		return false
	default:
		return false
	}
}

func hasLocalCompositeType(source productionSource, typeName string) bool {
	if source.packagePath != toolImportPath {
		return false
	}
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		literal, ok := node.(*ast.CompositeLit)
		if ok && len(literal.Elts) > 0 && containsLocalType(literal.Type, typeName) {
			found = true
		}
		return !found
	})
	return found
}

func containsLocalType(expression ast.Expr, typeName string) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == typeName
	case *ast.ArrayType:
		return containsLocalType(value.Elt, typeName)
	case *ast.Ellipsis:
		return containsLocalType(value.Elt, typeName)
	case *ast.StarExpr:
		return containsLocalType(value.X, typeName)
	case *ast.ParenExpr:
		return containsLocalType(value.X, typeName)
	case *ast.MapType:
		return containsLocalType(value.Key, typeName) || containsLocalType(value.Value, typeName)
	case *ast.ChanType:
		return containsLocalType(value.Value, typeName)
	default:
		return false
	}
}

func isToolBuiltinSource(root, filePath string) bool {
	want := filepath.Join(root, "internal", "tool", "builtins.go")
	return filepath.Clean(filePath) == filepath.Clean(want)
}

func commandDefinitionSet(source productionSource) ([]string, int, int, error) {
	if source.packagePath != appImportPath {
		return nil, 0, 0, nil
	}
	var names []string
	callCount := 0
	setCount := 0
	var detectorErr error
	ast.Inspect(source.file, func(node ast.Node) bool {
		if detectorErr != nil {
			return false
		}
		switch node := node.(type) {
		case *ast.CallExpr:
			function, ok := node.Fun.(*ast.Ident)
			if !ok || function.Name != "newCommandCatalog" {
				return true
			}
			callCount++
			if len(node.Args) != 1 {
				detectorErr = fmt.Errorf("newCommandCatalog call has %d arguments", len(node.Args))
				return false
			}
			if literal, ok := node.Args[0].(*ast.CompositeLit); !ok || !isCommandDefinitionSet(literal) {
				detectorErr = fmt.Errorf("newCommandCatalog input is not []commandDefinition")
				return false
			}
		case *ast.CompositeLit:
			if !isCommandDefinitionSet(node) {
				return true
			}
			setCount++
			if setCount > 1 {
				detectorErr = fmt.Errorf("multiple command definition sets")
				return false
			}
			for index, item := range node.Elts {
				definition, ok := item.(*ast.CompositeLit)
				if !ok {
					detectorErr = fmt.Errorf("command definition %d is not a composite literal", index)
					return false
				}
				name, ok := stringCompositeField(definition, "Name")
				if !ok || strings.TrimSpace(name) == "" {
					detectorErr = fmt.Errorf("command definition %d has no string Name", index)
					return false
				}
				names = append(names, name)
			}
		}
		return true
	})
	if detectorErr != nil {
		return nil, callCount, setCount, detectorErr
	}
	return names, callCount, setCount, nil
}

func isCommandDefinitionSet(literal *ast.CompositeLit) bool {
	array, ok := literal.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	element, ok := array.Elt.(*ast.Ident)
	return ok && element.Name == "commandDefinition"
}

func stringCompositeField(literal *ast.CompositeLit, fieldName string) (string, bool) {
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok || key.Name != fieldName {
			continue
		}
		value, ok := field.Value.(*ast.BasicLit)
		if !ok || value.Kind != token.STRING {
			return "", false
		}
		decoded, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", false
		}
		return decoded, true
	}
	return "", false
}

func hasCommandSwitchWithMultipleCanonicalNames(file *ast.File, canonical map[string]struct{}) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		switchStatement, ok := node.(*ast.SwitchStmt)
		if !ok || switchStatement.Body == nil {
			return true
		}
		seen := make(map[string]struct{})
		for _, statement := range switchStatement.Body.List {
			clause, ok := statement.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if _, ok := canonical[name]; ok {
					seen[name] = struct{}{}
				}
			}
		}
		if len(seen) > 1 {
			found = true
			return false
		}
		return true
	})
	return found
}

func builtinToolIDs(root string, sources []productionSource) ([]string, error) {
	for _, source := range sources {
		if !isToolBuiltinSource(root, source.path) {
			continue
		}
		var ids []string
		var found bool
		var detectorErr error
		ast.Inspect(source.file, func(node ast.Node) bool {
			if detectorErr != nil || found {
				return false
			}
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Name.Name != "BuiltinDefinitions" {
				return true
			}
			found = true
			if function.Body == nil {
				detectorErr = fmt.Errorf("BuiltinDefinitions has no body")
				return false
			}
			for _, statement := range function.Body.List {
				returnStatement, ok := statement.(*ast.ReturnStmt)
				if !ok || len(returnStatement.Results) != 1 {
					continue
				}
				literal, ok := returnStatement.Results[0].(*ast.CompositeLit)
				if !ok || !isToolDefinitionSet(literal) {
					continue
				}
				for index, item := range literal.Elts {
					definition, ok := item.(*ast.CompositeLit)
					if !ok {
						detectorErr = fmt.Errorf("tool definition %d is not a composite literal", index)
						return false
					}
					id, ok := stringCompositeField(definition, "ID")
					if !ok || strings.TrimSpace(id) == "" {
						detectorErr = fmt.Errorf("tool definition %d has no string ID", index)
						return false
					}
					ids = append(ids, id)
				}
				return false
			}
			detectorErr = fmt.Errorf("BuiltinDefinitions has no []Definition return")
			return false
		})
		if detectorErr != nil {
			return nil, detectorErr
		}
		if !found {
			return nil, fmt.Errorf("BuiltinDefinitions was not found in %s", source.path)
		}
		return ids, nil
	}
	return nil, fmt.Errorf("tool builtins source was not found")
}

func isToolDefinitionSet(literal *ast.CompositeLit) bool {
	array, ok := literal.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	element, ok := array.Elt.(*ast.Ident)
	return ok && element.Name == "Definition"
}

func hasStaticToolIDTableInSource(root string, source productionSource, canonical map[string]struct{}) bool {
	found := false
	ast.Inspect(source.file, func(node ast.Node) bool {
		if found {
			return false
		}
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// Builtin module Definitions necessarily carry RequiredTools. Skip
		// that one module table as a unit, while still inspecting any separate
		// tool metadata literal added to the same source file.
		if isBuiltinDefinitionSource(root, source.path) && isModuleDefinitionSet(literal) {
			return false
		}
		if len(literal.Elts) < 2 || !isCollectionCompositeType(literal.Type) {
			return true
		}
		if hasStaticCanonicalToolIDList(literal, canonical) {
			found = true
			return false
		}
		metadataEntriesWithIDs := 0
		for _, element := range literal.Elts {
			if hasCanonicalString(element, canonical) && hasToolMetadataEntry(element, literal.Type) {
				metadataEntriesWithIDs++
			}
		}
		if metadataEntriesWithIDs > 1 {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasStaticCanonicalToolIDList(literal *ast.CompositeLit, canonical map[string]struct{}) bool {
	if literal == nil || len(literal.Elts) < 2 || !isCollectionCompositeType(literal.Type) {
		return false
	}
	seen := make(map[string]struct{})
	for _, element := range literal.Elts {
		if keyValue, ok := element.(*ast.KeyValueExpr); ok {
			addCanonicalString(keyValue.Key, canonical, seen)
			addCanonicalString(keyValue.Value, canonical, seen)
			continue
		}
		addCanonicalString(element, canonical, seen)
	}
	return len(seen) > 1
}

func addCanonicalString(expression ast.Expr, canonical, seen map[string]struct{}) {
	for {
		switch value := expression.(type) {
		case *ast.ParenExpr:
			expression = value.X
			continue
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				return
			}
			id, err := strconv.Unquote(value.Value)
			if err == nil {
				if _, ok := canonical[id]; ok {
					seen[id] = struct{}{}
				}
			}
			return
		default:
			return
		}
	}
}

func isModuleDefinitionSet(literal *ast.CompositeLit) bool {
	if collectionElementTypeName(literal.Type) != "Definition" || len(literal.Elts) == 0 {
		return false
	}
	for _, element := range literal.Elts {
		if !moduleDefinitionEntry(element) {
			return false
		}
	}
	return true
}

func moduleDefinitionEntry(expression ast.Expr) bool {
	if keyValue, ok := expression.(*ast.KeyValueExpr); ok {
		expression = keyValue.Value
	}
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if ok && key.Name == "Descriptor" {
			return true
		}
	}
	return false
}

func hasToolMetadataEntry(expression ast.Expr, collectionType ast.Expr) bool {
	entry := expression
	if keyValue, ok := entry.(*ast.KeyValueExpr); ok {
		entry = keyValue.Value
	}
	if !isCompositeEntry(entry) {
		return false
	}
	typeName := strings.ToLower(collectionElementTypeName(collectionType))
	if strings.Contains(typeName, "benchmark") || strings.Contains(typeName, "contract") || strings.Contains(typeName, "target") {
		return false
	}
	if strings.Contains(typeName, "doctor") || strings.Contains(typeName, "tool") || strings.Contains(typeName, "metadata") || strings.Contains(typeName, "fact") {
		return true
	}
	composite, ok := entry.(*ast.CompositeLit)
	if !ok {
		return false
	}
	hasDoctorField := false
	hasExecutionField := false
	hasNameField := false
	for _, element := range composite.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch strings.ToLower(key.Name) {
		case "id", "purpose", "purposekey", "verification", "doctor", "staging", "required", "requiredness", "arguments", "expectedversion", "successlabel", "category", "source", "standard":
			hasDoctorField = true
		case "binary", "expectedsize", "expectediters", "kernel", "command":
			hasExecutionField = true
		case "name":
			hasNameField = true
		}
	}
	return hasDoctorField || (hasNameField && !hasExecutionField)
}

func collectionElementTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.ArrayType:
		return expressionTypeName(value.Elt)
	case *ast.MapType:
		return expressionTypeName(value.Value)
	case *ast.ParenExpr:
		return collectionElementTypeName(value.X)
	default:
		return ""
	}
}

func expressionTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.StarExpr:
		return expressionTypeName(value.X)
	case *ast.ParenExpr:
		return expressionTypeName(value.X)
	default:
		return ""
	}
}

func isCollectionCompositeType(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.ArrayType, *ast.MapType:
		return true
	case *ast.ParenExpr:
		return isCollectionCompositeType(value.X)
	default:
		return false
	}
}

func isCompositeEntry(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.CompositeLit:
		return true
	case *ast.KeyValueExpr:
		return isCompositeEntry(value.Value)
	case *ast.ParenExpr:
		return isCompositeEntry(value.X)
	default:
		return false
	}
}

func hasCanonicalString(node ast.Node, canonical map[string]struct{}) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		if found {
			return false
		}
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil {
			_, found = canonical[value]
		}
		return !found
	})
	return found
}

func TestProductionToolSourceHasSingleDefinitionOwnerAndBootstrapReach(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	definitionDeclarations := 0
	catalogDeclarations := 0
	for _, source := range sources {
		if source.packagePath == toolImportPath {
			if hasLocalFunction(source, toolImportPath, "BuiltinDefinitions") {
				definitionDeclarations++
				if !isToolBuiltinSource(root, source.path) {
					t.Fatalf("BuiltinDefinitions is declared outside tool/builtins.go: %s", source.path)
				}
			}
			if hasLocalFunction(source, toolImportPath, "BuiltinCatalog") {
				catalogDeclarations++
				if !isToolBuiltinSource(root, source.path) {
					t.Fatalf("BuiltinCatalog is declared outside tool/builtins.go: %s", source.path)
				}
			}
			if !isToolBuiltinSource(root, source.path) && hasLocalCompositeType(source, "Definition") {
				t.Fatalf("tool source %s constructs a replicated local Definition", source.path)
			}
		}
		if found, err := hasImportedCompositeLiteral(source, toolImportPath, "Definition"); err != nil {
			t.Fatalf("resolve tool Definition literals in %s: %v", source.path, err)
		} else if found && !isToolBuiltinSource(root, source.path) {
			t.Fatalf("production source %s constructs a replicated tool Definition", source.path)
		}
		for _, name := range []string{"BuiltinDefinitions", "BuiltinCatalog"} {
			uses, err := hasImportedSymbol(source, toolImportPath, name)
			if err != nil {
				t.Fatalf("resolve tool symbol %q in %s: %v", name, source.path, err)
			}
			if uses && !isToolBuiltinSource(root, source.path) && !isBootstrapSource(root, source.path) {
				t.Fatalf("production source %s reaches tool %s outside builtins/bootstrap", source.path, name)
			}
		}
	}
	if definitionDeclarations != 1 {
		t.Fatalf("BuiltinDefinitions declarations = %d, want exactly one", definitionDeclarations)
	}
	if catalogDeclarations != 1 {
		t.Fatalf("BuiltinCatalog declarations = %d, want exactly one", catalogDeclarations)
	}
}

func TestProductionToolConsumersHaveNoStaticMetadataOrIDSwitch(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	ids, err := builtinToolIDs(root, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("builtin tool IDs = %v, want multiple canonical tools", ids)
	}
	canonical := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := canonical[id]; exists {
			t.Fatalf("builtin tool source repeats ID %q", id)
		}
		canonical[id] = struct{}{}
	}
	typeSpecs := packageTypeSpecs(sources)
	for _, source := range sources {
		// The canonical tool source, module Definition source, and release
		// manifest are intentionally the only places where their respective
		// tool facts may appear as tables. A metadata table in any other
		// first-party package is a duplicate regardless of its filename.
		if isToolMetadataSource(root, source) {
			continue
		}
		if hasStaticToolIDTableInSource(root, source, canonical) {
			t.Fatalf("production source %s contains a static multi-entry tool metadata table", source.path)
		}
		if hasForbiddenToolIDSwitchWithTypeSpecs(source, canonical, typeSpecs[source.packagePath]) {
			t.Fatalf("production source %s contains a switch with multiple canonical tool IDs", source.path)
		}
	}
}

func isToolsManifestPackage(packagePath string) bool {
	return packagePath == toolsmanifestImportPath || strings.HasPrefix(packagePath, toolsmanifestImportPath+"/")
}

func isToolMetadataSource(root string, source productionSource) bool {
	return isToolBuiltinSource(root, source.path) || isToolsManifestPackage(source.packagePath)
}

func hasForbiddenToolIDSwitch(source productionSource, canonical map[string]struct{}) bool {
	return hasForbiddenToolIDSwitchWithTypeSpecs(source, canonical, sourceTypeSpecs(source))
}

func hasForbiddenToolIDSwitchWithTypeSpecs(source productionSource, canonical map[string]struct{}, typeSpecs map[string]*ast.TypeSpec) bool {
	parents := astParentMap(source.file)
	for _, node := range switchStatements(source.file) {
		if !hasMultipleCanonicalCases(node, canonical) {
			continue
		}
		if !isToolIDSwitchAllowedWithTypeSpecs(source, node, parents, typeSpecs) {
			return true
		}
	}
	return false
}

func switchStatements(file *ast.File) []*ast.SwitchStmt {
	var result []*ast.SwitchStmt
	ast.Inspect(file, func(node ast.Node) bool {
		if statement, ok := node.(*ast.SwitchStmt); ok {
			result = append(result, statement)
		}
		return true
	})
	return result
}

func hasMultipleCanonicalCases(statement *ast.SwitchStmt, canonical map[string]struct{}) bool {
	if statement == nil || statement.Body == nil {
		return false
	}
	seen := make(map[string]struct{})
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			if basic, ok := expression.(*ast.BasicLit); ok && basic.Kind == token.STRING {
				name, err := strconv.Unquote(basic.Value)
				if err == nil {
					if _, ok := canonical[name]; ok {
						seen[name] = struct{}{}
					}
				}
			}
		}
	}
	return len(seen) > 1
}

func isToolIDSwitchAllowed(source productionSource, statement *ast.SwitchStmt, parents map[ast.Node]ast.Node) bool {
	return isToolIDSwitchAllowedWithTypeSpecs(source, statement, parents, sourceTypeSpecs(source))
}

func isToolIDSwitchAllowedWithTypeSpecs(source productionSource, statement *ast.SwitchStmt, parents map[ast.Node]ast.Node, typeSpecs map[string]*ast.TypeSpec) bool {
	// Score's only permitted concrete-tool switch is the projection into its
	// public ToolSpec. Structural evidence (the ToolSpec result, the
	// RequiredTools traversal, and assignment to score-owned tool fields) keeps
	// the exception narrow without exempting a whole package or filename.
	if !isScorePackage(source.packagePath) || statement == nil {
		return false
	}
	function := enclosingFuncDecl(statement, parents)
	if function == nil || !functionReturnsNamedType(function, "ToolSpec") {
		return false
	}
	if !functionContainsIdentifier(function, "RequiredTools") ||
		!functionContainsSelector(function, "Lookup") ||
		!functionContainsToolSpecField(function) {
		return false
	}
	allowedIDs := toolSpecFieldIDs(typeSpecs)
	if len(allowedIDs) == 0 {
		return false
	}
	for id := range switchStringIDs(statement) {
		if _, ok := allowedIDs[id]; !ok {
			return false
		}
	}
	return true
}

func switchStringIDs(statement *ast.SwitchStmt) map[string]struct{} {
	result := make(map[string]struct{})
	if statement == nil || statement.Body == nil {
		return result
	}
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			basic, ok := expression.(*ast.BasicLit)
			if !ok || basic.Kind != token.STRING {
				continue
			}
			id, err := strconv.Unquote(basic.Value)
			if err == nil {
				result[id] = struct{}{}
			}
		}
	}
	return result
}

func toolSpecFieldIDs(typeSpecs map[string]*ast.TypeSpec) map[string]struct{} {
	result := make(map[string]struct{})
	typeSpec := typeSpecs["ToolSpec"]
	if typeSpec == nil {
		return result
	}
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok || structType.Fields == nil {
		return result
	}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			result[strings.ToLower(name.Name)] = struct{}{}
		}
	}
	return result
}

func functionReturnsNamedType(function *ast.FuncDecl, name string) bool {
	if function == nil || function.Type == nil || function.Type.Results == nil {
		return false
	}
	for _, field := range function.Type.Results.List {
		if expressionTypeName(field.Type) == name {
			return true
		}
	}
	return false
}

func functionContainsIdentifier(function *ast.FuncDecl, name string) bool {
	if function == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func functionContainsToolSpecField(function *ast.FuncDecl) bool {
	if function == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "Sysbench", "Fio", "IPerf3":
			found = true
			return false
		default:
			return true
		}
	})
	return found
}

func functionContainsSelector(function *ast.FuncDecl, name string) bool {
	if function == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func TestProductionCommandCompositionHasSingleCatalogSet(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	callCount := 0
	setSources := 0
	setCount := 0
	var canonicalNames []string
	for _, source := range sources {
		names, calls, sets, err := commandDefinitionSet(source)
		if err != nil {
			t.Fatalf("inspect command catalog in %s: %v", source.path, err)
		}
		callCount += calls
		setCount += sets
		if sets > 0 {
			if !isBootstrapSource(root, source.path) {
				t.Fatalf("command definition set is outside bootstrap: %s", source.path)
			}
			setSources++
			canonicalNames = names
		}
		if calls > 0 && !isBootstrapSource(root, source.path) {
			t.Fatalf("newCommandCatalog is called outside bootstrap: %s", source.path)
		}
	}
	if callCount != 1 || setCount != 1 || setSources != 1 || len(canonicalNames) < 2 {
		t.Fatalf("command catalog composition = calls %d, sets %d, sources %d, names %v; want one bootstrap set", callCount, setCount, setSources, canonicalNames)
	}
	canonical := make(map[string]struct{}, len(canonicalNames))
	for _, name := range canonicalNames {
		if _, exists := canonical[name]; exists {
			t.Fatalf("command catalog repeats canonical name %q", name)
		}
		canonical[name] = struct{}{}
	}
	for _, source := range sources {
		if hasCommandSwitchWithMultipleCanonicalNames(source.file, canonical) {
			t.Fatalf("production source %s contains a switch with multiple canonical command cases", source.path)
		}
	}
}

func TestProductionCompositionPlumbingHasNoWeakPatterns(t *testing.T) {
	root := repositoryRoot(t)
	sources := loadAllProductionSources(t, root)
	typeSpecs := packageTypeSpecs(sources)
	for _, source := range sources {
		// The release manifest intentionally uses open JSON values and owns a
		// separate package-level tool table; it is not application composition.
		if isToolsManifestPackage(source.packagePath) {
			continue
		}
		// Apply the semantic detector to every first-party production source.
		// The detector itself narrows any/interface, reflection, mutex, and
		// mutable-global findings to composition contexts, so moving a helper
		// to a new package or filename cannot evade the guard while ordinary
		// domain state remains valid.
		if finding := weakCompositionPatternWithTypes(source, typeSpecs[source.packagePath]); finding != "" {
			t.Fatalf("composition plumbing %s contains %s", source.path, finding)
		}
	}
}

func TestToolSourceDetectorsResolveImportAliases(t *testing.T) {
	source := parseDetectorSnippet(t, `package consumer

import tools "ecs/internal/tool"

func use() {
	_ = tools.Definition{}
	_ = tools.BuiltinCatalog()
}
`, "ecs/internal/consumer")
	if found, err := hasImportedCompositeLiteral(source, toolImportPath, "Definition"); err != nil || !found {
		t.Fatalf("aliased tool.Definition detection = %v, %v; want true, nil", found, err)
	}
	if found, err := hasImportedCall(source, toolImportPath, "BuiltinCatalog"); err != nil || !found {
		t.Fatalf("aliased tool.BuiltinCatalog detection = %v, %v; want true, nil", found, err)
	}
}

func TestCatalogMutationDetectorUsesReceiverAndContext(t *testing.T) {
	cases := []struct {
		name        string
		packagePath string
		source      string
		want        bool
	}{
		{name: "catalog pointer Add", source: "package module\nfunc (c *Catalog) Add(value Definition) {}\n", want: true},
		{name: "parenthesized catalog Set", source: "package module\nfunc (c (*Catalog)) Set(value Definition) {}\n", want: true},
		{name: "qualified catalog Remove", source: "package module\nfunc (c pkg.Catalog) Remove(value Definition) {}\n", want: true},
		{name: "module catalog Add", source: "package module\nfunc (c *moduleCatalog) Add(value Definition) {}\n", want: true},
		{name: "command catalog Remove", source: "package app\nfunc (c commandCatalog) Remove(name string) {}\n", want: true},
		{name: "tool catalog Set", source: "package tool\nfunc (c *toolCatalog) Set(value Definition) {}\n", want: true},
		{name: "neutral registry receiver Add", source: "package runner\ntype state struct { handlers map[string]commandHandler }\nfunc (s *state) Add(handler commandHandler) {}\n", want: true},
		{name: "neutral registry alias receiver Add", source: "package runner\ntype state struct { handlers map[string]commandHandler }\ntype stateAlias = state\nfunc (s (*stateAlias)) Add(handler commandHandler) {}\n", want: true},
		{name: "free module registration", source: "package app\nfunc registerModule(definition Definition) {}\n", want: true},
		{name: "bare registration with definition", source: "package app\nfunc Register(definition Definition) {}\n", want: true},
		{name: "command entrypoint module registration", packagePath: "ecs/cmd/ecs", source: "package main\nfunc RegisterModuleFixture() {}\n", want: true},
		{name: "free tool mutation value", source: "package app\nvar SetTool = func() {}\n", want: true},
		{name: "unrelated DeleteReport", source: "package report\nfunc DeleteReport(path string) {}\n", want: false},
		{name: "unrelated ReplaceText", source: "package report\nfunc ReplaceText(value string) {}\n", want: false},
		{name: "unqualified Delete", source: "package report\nfunc Delete(value string) {}\n", want: false},
		{name: "bare Add for string", source: "package report\nfunc Add(value string) {}\n", want: false},
		{name: "other receiver Replace", source: "package report\nfunc (r Report) Replace(value string) {}\n", want: false},
		{name: "other receiver Replace with module parameter", source: "package report\nfunc (r Report) Replace(module string) {}\n", want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			packagePath := test.packagePath
			if packagePath == "" {
				packagePath = "ecs/internal/test"
			}
			source := parseDetectorSnippet(t, test.source, packagePath)
			if _, got := catalogMutationAPI(source); got != test.want {
				t.Fatalf("catalog mutation detector = %t, want %t", got, test.want)
			}
		})
	}
}

func TestReceiverContextUsesPackageTypeFacts(t *testing.T) {
	typeSource := parseDetectorSnippet(t, "package runner\ntype state struct { handlers map[string]commandHandler }\n", runnerImportPath)
	methodSource := parseDetectorSnippet(t, "package runner\nimport refl \"reflect\"\nfunc (s *state) Add(handler commandHandler) {}\nfunc (s *state) resolve(value any) { _ = refl.TypeOf(value) }\n", runnerImportPath)
	typeSpecs := packageTypeSpecs([]productionSource{typeSource, methodSource})[runnerImportPath]
	if _, ok := catalogMutationAPIWithTypes(methodSource, typeSpecs); !ok {
		t.Fatal("receiver mutation detector missed a registry type declared in another file")
	}
	if got := weakCompositionPatternWithTypes(methodSource, typeSpecs); got != "any type" {
		t.Fatalf("cross-file receiver weak detector = %q, want %q", got, "any type")
	}
}

func TestToolMetadataDetectorsRejectLegacyTablesAndSwitches(t *testing.T) {
	canonical := map[string]struct{}{"sysbench": {}, "zstd": {}, "nexttrace-tiny": {}, "speedtest": {}}
	cases := []struct {
		name        string
		packagePath string
		source      string
		want        bool
	}{
		{
			name: "legacy doctor table",
			source: `package app
var doctorTools = []doctorTool{
		{name: "sysbench"},
		{name: "zstd"},
}
`,
			want: true,
		},
		{
			name: "equivalent metadata table",
			source: `package app
var tools = []struct{ name string }{
		{name: "sysbench"},
		{name: "zstd"},
}
`,
			want: true,
		},
		{
			name:        "relocated helper metadata table",
			packagePath: "ecs/internal/metadata",
			source: `package app
var relocatedDoctorTools = []doctorTool{
		{name: "sysbench"},
		{name: "zstd"},
}
`,
			want: true,
		},
		{
			name: "single incidental ID",
			source: `package app
var tool = struct{ name string }{name: "sysbench"}
`,
			want: false,
		},
		{
			name: "legacy plan switch",
			source: `package app
func stage(id string) {
	switch id {
	case "zstd":
	case "nexttrace-tiny":
	}
}
`,
			want: true,
		},
		{
			name:        "relocated planner tool switch",
			packagePath: "ecs/internal/planner",
			source: `package planner
func stage(id string) {
	switch id {
	case "zstd":
	case "nexttrace-tiny":
	}
}
`,
			want: true,
		},
		{
			name:        "relocated planner tool ID list",
			packagePath: "ecs/internal/planner",
			source: `package planner
var stagedToolIDs = []string{"sysbench", "zstd", "nexttrace-tiny"}
`,
			want: true,
		},
		{
			name: "tool metadata map keys",
			source: `package planner
var toolLabels = map[string]string{
		"sysbench": "cpu benchmark",
		"zstd": "compression benchmark",
}
`,
			want: true,
		},
		{
			name: "mixed tool ID list",
			source: `package planner
var stages = []string{"prepare", "sysbench", "zstd"}
`,
			want: true,
		},
		{
			name: "policy switch",
			source: `package app
func verify(kind string) {
	switch kind {
	case "command":
	case "pinned_zstd":
	}
}
`,
			want: false,
		},
		{
			name: "single switch ID",
			source: `package app
func stage(id string) {
	switch id {
	case "speedtest":
	}
}
`,
			want: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			packagePath := test.packagePath
			if packagePath == "" {
				packagePath = appImportPath
			}
			source := parseDetectorSnippet(t, test.source, packagePath)
			gotTable := hasStaticToolIDTableInSource("", source, canonical)
			gotSwitch := hasForbiddenToolIDSwitch(source, canonical)
			got := gotTable || gotSwitch
			if got != test.want {
				t.Fatalf("tool metadata detector = %t (table=%t switch=%t), want %t", got, gotTable, gotSwitch, test.want)
			}
		})
	}

	projection := parseDetectorSnippet(t, `package score
type ToolSpec struct { Sysbench string; Fio string }
func project(descriptor struct{ RequiredTools []string }) ToolSpec {
	var spec ToolSpec
	for _, candidate := range descriptor.RequiredTools {
		switch candidate {
		case "sysbench":
		case "fio":
		}
	}
	var catalog struct{}
	_ = catalog.Lookup
	_ = spec.Sysbench
	_ = spec.Fio
	return spec
}
`, scoreImportPath)
	parents := astParentMap(projection.file)
	if hasForbiddenToolIDSwitch(projection, canonical) {
		t.Fatal("score ToolSpec projection was incorrectly rejected")
	}
	if !isToolIDSwitchAllowed(projection, switchStatements(projection.file)[0], parents) {
		t.Fatal("score ToolSpec projection did not satisfy the structural allowance")
	}

	unrelatedScore := parseDetectorSnippet(t, `package score
func unrelated(value string) {
	switch value {
	case "zstd":
	case "nexttrace-tiny":
	}
}
`, scoreImportPath)
	if !hasForbiddenToolIDSwitch(unrelatedScore, canonical) {
		t.Fatal("unrelated score tool switch was incorrectly allowed")
	}
}

func TestCommandSwitchDetectorDistinguishesDomainSwitches(t *testing.T) {
	canonical := map[string]struct{}{"run": {}, "plan": {}, "list": {}}
	cases := []struct {
		name        string
		packagePath string
		source      string
		want        bool
	}{
		{
			name: "single command case",
			source: `package app
func use(value string) {
	switch value {
	case "run":
	}
}
`,
			want: false,
		},
		{
			name: "multiple command cases",
			source: `package app
func use(value string) {
	switch value {
	case "run", "plan":
	}
}
`,
			want: true,
		},
		{
			name:        "relocated command entrypoint switch",
			packagePath: "ecs/cmd/ecs",
			source: `package main
func dispatch(value string) {
	switch value {
	case "run":
	case "plan":
	}
}
`,
			want: true,
		},
		{
			name: "domain policy cases",
			source: `package app
func use(value string) {
	switch value {
	case "auto", "always":
	}
}
`,
			want: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			packagePath := test.packagePath
			if packagePath == "" {
				packagePath = appImportPath
			}
			source := parseDetectorSnippet(t, test.source, packagePath)
			if got := hasCommandSwitchWithMultipleCanonicalNames(source.file, canonical); got != test.want {
				t.Fatalf("command switch detector = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWeakCompositionDetectorFindsTrackedPatterns(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "any in composition type", source: "package app\ntype commandOptions any\n", want: "any type"},
		{name: "empty interface in composition type", source: "package app\ntype commandOptions interface{}\n", want: "empty interface type"},
		{name: "reflection in composition helper", source: "package app\nimport refl \"reflect\"\nfunc buildCatalog() { _ = refl.TypeOf }\n", want: "reflection import"},
		{name: "factory function", source: "package app\nfunc factory() {}\n", want: "weak factory or registry function"},
		{name: "global map", source: "package app\nvar handlers = map[string]commandHandler{}\n", want: "package-level mutable variable handlers"},
		{name: "global slice", source: "package app\nvar definitions = []Definition{}\n", want: "package-level mutable variable definitions"},
		{name: "indirect global catalog", source: "package app\nvar definitions = buildDefinitions()\n", want: "package-level mutable variable definitions"},
		{name: "config module registry", source: "package config\nvar moduleRegistryFixture = map[string]any{}\n", want: "package-level mutable variable moduleRegistryFixture"},
		{name: "runner typed registry", source: "package runner\nvar moduleHandlers = map[string]handler{}\n", want: "package-level mutable variable moduleHandlers"},
		{name: "ordinary error sentinel", source: "package app\nvar errInvalidFixture = errors.New(\"invalid\")\n", want: ""},
		{name: "ordinary error factory", source: "package app\nvar errorFactory = errors.New(\"invalid\")\n", want: ""},
		{name: "ordinary regexp table", source: "package app\nvar patterns = []*regexp.Regexp{}\n", want: ""},
		{name: "local map", source: "package app\nfunc use() { handlers := map[string]commandHandler{}; _ = handlers }\n", want: ""},
		{name: "registry mutex", source: "package app\nimport \"sync\"\nfunc use() { var catalogMu sync.Mutex; _ = catalogMu }\n", want: "registry mutex type"},
		{name: "ordinary mutex field", source: "package app\nimport \"sync\"\ntype domainState struct { mu sync.Mutex }\n", want: ""},
		{name: "ordinary tool cache mutex", source: "package app\nimport \"sync\"\ntype toolCache struct { mu sync.Mutex }\n", want: ""},
		{name: "catalog state mutex", source: "package app\nimport \"sync\"\ntype catalogRegistry struct { mu sync.Mutex }\n", want: "registry mutex type"},
		{name: "ordinary mutex local", source: "package app\nimport \"sync\"\nfunc use() { var mu sync.Mutex; _ = mu }\n", want: ""},
		{name: "ordinary any value", source: "package app\nfunc preservePanicValue(value any) any { return value }\n", want: ""},
		{name: "neutral registry map any field", source: "package runner\ntype state struct { handlers map[string]any }\n", want: "any type"},
		{name: "neutral registry factory any field", source: "package runner\ntype state struct { factory func() any }\n", want: "any type"},
		{name: "neutral registry empty interface field", source: "package runner\ntype state struct { handlers map[string]interface{} }\n", want: "empty interface type"},
		{name: "neutral registry factory empty interface field", source: "package runner\ntype state struct { factory func() interface{} }\n", want: "empty interface type"},
		{name: "neutral registry mutex sibling field", source: "package runner\nimport \"sync\"\ntype state struct { mu sync.Mutex; handlers map[string]commandHandler }\n", want: "registry mutex type"},
		{name: "neutral registry receiver reflection", source: "package runner\nimport refl \"reflect\"\ntype state struct { handlers map[string]commandHandler }\nfunc (s *state) resolve(value any) { _ = refl.TypeOf(value) }\n", want: "any type"},
		{name: "ordinary tool payload", source: "package app\ntype toolPayload any\n", want: ""},
		{name: "ordinary reflection value", source: "package app\nimport refl \"reflect\"\nfunc preserveType(value refl.Type) refl.Type { return value }\n", want: ""},
		{name: "ordinary tool reflection type", source: "package app\nimport refl \"reflect\"\ntype toolCacheType refl.Type\n", want: ""},
		{name: "ordinary mutex sibling-free state", source: "package runner\nimport \"sync\"\ntype workQueue struct { mu sync.Mutex }\n", want: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := parseDetectorSnippet(t, test.source, appImportPath)
			if got := weakCompositionPattern(source); got != test.want {
				t.Fatalf("weak pattern detector = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompositionScopeCoversRelocatedHelpersAndLegalDomainSeams(t *testing.T) {
	cases := []struct {
		name        string
		packagePath string
		source      string
		inScope     bool
		finding     string
	}{
		{
			name:        "relocated app catalog helper",
			packagePath: appImportPath,
			source:      "package app\nvar handlers = map[string]commandHandler{}\n",
			inScope:     true,
			finding:     "package-level mutable variable handlers",
		},
		{
			name:        "relocated app reflection helper",
			packagePath: appImportPath,
			source:      "package app\nimport refl \"reflect\"\nfunc buildCatalog() { _ = refl.TypeOf }\n",
			inScope:     true,
			finding:     "reflection import",
		},
		{
			name:        "local app catalog construction",
			packagePath: appImportPath,
			source:      "package app\nfunc build() { handlers := map[string]commandHandler{}; _ = handlers }\n",
			inScope:     true,
		},
		{
			name:        "interactive function seam",
			packagePath: appImportPath,
			source: `package app
var openWizardTTY = func() (int, error) { return 0, nil }
type prompter struct{}
func (p *prompter) line(format string, values ...any) {}
`,
			inScope: true,
		},
		{
			name:        "config domain table",
			packagePath: configImportPath,
			source:      "package config\nvar values = map[string]any{}\n",
		},
		{
			name:        "probe domain table",
			packagePath: probeImportPath,
			source:      "package probe\nvar values = map[string]any{}\n",
		},
		{
			name:        "relocated probe definition helper",
			packagePath: probeImportPath,
			source:      "package probe\nvar definitions = []Definition{}\n",
			inScope:     true,
			finding:     "package-level mutable variable definitions",
		},
		{
			name:        "config registry helper",
			packagePath: configImportPath,
			source:      "package config\nvar moduleRegistryFixture = map[string]any{}\n",
			inScope:     true,
			finding:     "package-level mutable variable moduleRegistryFixture",
		},
		{
			name:        "runner registry helper",
			packagePath: runnerImportPath,
			source:      "package runner\nvar moduleHandlers = map[string]handler{}\n",
			inScope:     true,
			finding:     "package-level mutable variable moduleHandlers",
		},
		{
			name:        "relocated internal registry helper",
			packagePath: "ecs/internal/compositionhelper",
			source:      "package compositionhelper\nvar definitions = []Definition{}\n",
			inScope:     true,
			finding:     "package-level mutable variable definitions",
		},
		{
			name:        "relocated internal composition reflection helper",
			packagePath: "ecs/internal/planner",
			source:      "package planner\nimport refl \"reflect\"\nfunc buildCatalog() { _ = refl.TypeOf }\n",
			inScope:     true,
			finding:     "reflection import",
		},
		{
			name:        "relocated internal composition global",
			packagePath: "ecs/internal/planner",
			source:      "package planner\nvar handlers = map[string]commandHandler{}\n",
			inScope:     true,
			finding:     "package-level mutable variable handlers",
		},
		{
			name:        "relocated internal ordinary any helper",
			packagePath: "ecs/internal/planner",
			source:      "package planner\nfunc preservePanicValue(value any) any { return value }\n",
			inScope:     false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := parseDetectorSnippet(t, test.source, test.packagePath)
			if got := isCompositionPlumbingSource(source); got != test.inScope {
				t.Fatalf("composition scope = %t, want %t", got, test.inScope)
			}
			if !test.inScope {
				return
			}
			if got := weakCompositionPattern(source); got != test.finding {
				t.Fatalf("weak composition finding = %q, want %q", got, test.finding)
			}
		})
	}
}
