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

func isCompositionPackage(packagePath string) bool {
	for _, root := range []string{appImportPath, configImportPath, moduleImportPath, probeImportPath, runnerImportPath, toolImportPath} {
		if packagePath == root || strings.HasPrefix(packagePath, root+"/") {
			return true
		}
	}
	return false
}

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
	for _, declaration := range source.file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if receiver := receiverBaseName(declaration.Recv); receiver != "" {
				if isCatalogReceiver(receiver) && hasMutationVerbPrefix(declaration.Name.Name) {
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

func isCatalogReceiver(name string) bool {
	return name == "Catalog" || name == "commandCatalog" || name == "toolCatalog"
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
	return hasIdentifierNamed(source, "Definition") ||
		hasIdentifierNamed(source, "Descriptor") ||
		hasIdentifierNamed(source, "BuiltinDefinitions") ||
		hasIdentifierNamed(source, "CatalogFromDefinitions")
}

func hasModuleCompositionSignal(source productionSource) bool {
	if hasRegistryShapedPackageVariable(source) {
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
	bindings, err := resolveImportBindings(source.file)
	if err != nil {
		return err.Error()
	}
	for _, specification := range source.file.Imports {
		importPath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			return fmt.Sprintf("invalid import path: %v", err)
		}
		if importPath == "reflect" {
			return "reflection import"
		}
	}
	if name, ok := packageLevelVariable(source); ok {
		return "package-level mutable variable " + name
	}
	allowedAny := allowedVariadicAnyPositions(source.file)
	finding := ""
	ast.Inspect(source.file, func(node ast.Node) bool {
		if finding != "" {
			return false
		}
		switch value := node.(type) {
		case *ast.Ident:
			if value.Name == "any" {
				if _, allowed := allowedAny[value.Pos()]; allowed {
					return true
				}
				finding = "any type"
				return false
			}
		case *ast.InterfaceType:
			if value.Methods != nil && len(value.Methods.List) == 0 {
				finding = "empty interface type"
				return false
			}
		case *ast.SelectorExpr:
			packageIdent, ok := value.X.(*ast.Ident)
			if ok && bindings[packageIdent.Name] == "sync" && (value.Sel.Name == "Mutex" || value.Sel.Name == "RWMutex") {
				finding = "registry mutex type"
				return false
			}
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

func allowedVariadicAnyPositions(file *ast.File) map[token.Pos]struct{} {
	allowed := make(map[token.Pos]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Name.Name != "line" || function.Type == nil || function.Type.Params == nil {
			return true
		}
		if receiverBaseName(function.Recv) != "prompter" {
			return true
		}
		for _, field := range function.Type.Params.List {
			ellipsis, ok := field.Type.(*ast.Ellipsis)
			if !ok {
				continue
			}
			ident, ok := ellipsis.Elt.(*ast.Ident)
			if ok && ident.Name == "any" {
				allowed[ident.Pos()] = struct{}{}
			}
		}
		return true
	})
	return allowed
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
				if allowedPackageFunctionSeam(name.Name, values, index) || !isRegistryShapedVariable(name.Name, values, index) {
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
		return false
	default:
		return false
	}
}

func allowedPackageFunctionSeam(name string, values *ast.ValueSpec, index int) bool {
	if name != "openWizardTTY" {
		return false
	}
	if values.Type != nil {
		_, ok := values.Type.(*ast.FuncType)
		return ok
	}
	if index >= len(values.Values) {
		return false
	}
	_, ok := values.Values[index].(*ast.FuncLit)
	return ok
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
	for _, source := range sources {
		// The canonical tool source, module Definition source, and release
		// manifest are intentionally the only places where their respective
		// tool facts may appear as tables. A metadata table in any other
		// first-party package is a duplicate regardless of its filename.
		if isToolBuiltinSource(root, source.path) || isToolsManifestPackage(source.packagePath) {
			continue
		}
		if hasStaticToolIDTableInSource(root, source, canonical) {
			t.Fatalf("production source %s contains a static multi-entry tool metadata table", source.path)
		}
		if isAppPackage(source.packagePath) && hasCommandSwitchWithMultipleCanonicalNames(source.file, canonical) {
			t.Fatalf("app source %s contains a switch with multiple canonical tool IDs", source.path)
		}
	}
}

func isToolsManifestPackage(packagePath string) bool {
	return packagePath == toolsmanifestImportPath || strings.HasPrefix(packagePath, toolsmanifestImportPath+"/")
}

func isAppPackage(packagePath string) bool {
	return packagePath == appImportPath || strings.HasPrefix(packagePath, appImportPath+"/")
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
	for _, source := range loadAllProductionSources(t, root) {
		if !isCompositionPlumbingSource(source) {
			continue
		}
		if finding := weakCompositionPattern(source); finding != "" {
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
		{name: "command catalog Remove", source: "package app\nfunc (c commandCatalog) Remove(name string) {}\n", want: true},
		{name: "tool catalog Set", source: "package tool\nfunc (c *toolCatalog) Set(value Definition) {}\n", want: true},
		{name: "free module registration", source: "package app\nfunc registerModule(definition Definition) {}\n", want: true},
		{name: "bare registration with definition", source: "package app\nfunc Register(definition Definition) {}\n", want: true},
		{name: "command entrypoint module registration", packagePath: "ecs/cmd/ecs", source: "package main\nfunc RegisterModuleFixture() {}\n", want: true},
		{name: "free tool mutation value", source: "package app\nvar SetTool = func() {}\n", want: true},
		{name: "unrelated DeleteReport", source: "package report\nfunc DeleteReport(path string) {}\n", want: false},
		{name: "unrelated ReplaceText", source: "package report\nfunc ReplaceText(value string) {}\n", want: false},
		{name: "unqualified Delete", source: "package report\nfunc Delete(value string) {}\n", want: false},
		{name: "bare Add for string", source: "package report\nfunc Add(value string) {}\n", want: false},
		{name: "other receiver Replace", source: "package report\nfunc (r Report) Replace(value string) {}\n", want: false},
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
			gotSwitch := hasCommandSwitchWithMultipleCanonicalNames(source.file, canonical)
			got := gotTable || gotSwitch
			if got != test.want {
				t.Fatalf("tool metadata detector = %t (table=%t switch=%t), want %t", got, gotTable, gotSwitch, test.want)
			}
		})
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
		{name: "any", source: "package app\ntype value any\n", want: "any type"},
		{name: "empty interface", source: "package app\ntype value interface{}\n", want: "empty interface type"},
		{name: "reflection alias", source: "package app\nimport refl \"reflect\"\nvar _ = refl.TypeOf\n", want: "reflection import"},
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
		{name: "registry mutex", source: "package app\nimport \"sync\"\nfunc use() { var mu sync.Mutex; _ = mu }\n", want: "registry mutex type"},
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
			source:      "package app\nimport refl \"reflect\"\nvar typeOf = refl.TypeOf\n",
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
