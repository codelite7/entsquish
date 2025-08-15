package entsquish

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileMerger handles the merging of Go files within a package.
type FileMerger struct {
	verboseLogging bool
	dryRun         bool
	config         SquishingConfig
}

// NewFileMerger creates a new file merger.
func NewFileMerger(verboseLogging, dryRun bool, maxFileSize int64) *FileMerger {
	config := DefaultSquishingConfig()
	config.MaxFileSize = maxFileSize
	return &FileMerger{
		verboseLogging: verboseLogging,
		dryRun:         dryRun,
		config:         config,
	}
}

// MergePackage merges all files in the given package.
func (fm *FileMerger) MergePackage(pkg SquishablePackage) error {
	if fm.verboseLogging {
		log.Printf("entsquish: merging package %s with %d files", pkg.Path, len(pkg.Files))
	}

	// Create a shared FileSet for all files in this package
	sharedFileSet := token.NewFileSet()

	// Parse all files in the package using the shared FileSet
	fileInfos, err := fm.parseFiles(pkg, sharedFileSet)
	if err != nil {
		return fmt.Errorf("failed to parse files in package %s: %w", pkg.Path, err)
	}

	// For entity packages, expect exactly 2 files
	// For root packages, expect at least 2 files
	isRootPackage := pkg.EntityName == "gen"
	if !isRootPackage && len(fileInfos) != 2 {
		return fmt.Errorf("expected 2 files in entity package %s, got %d", pkg.Path, len(fileInfos))
	}
	if isRootPackage && len(fileInfos) < 2 {
		return fmt.Errorf("expected at least 2 files in root package %s, got %d", pkg.Path, len(fileInfos))
	}

	// Merge the files
	mergedAST, err := fm.MergeASTs(fileInfos)
	if err != nil {
		return fmt.Errorf("failed to merge ASTs for package %s: %w", pkg.Path, err)
	}

	// Generate output file path
	var outputPath string
	if isRootPackage {
		// For root package, use gen.go directly in the directory
		outputPath = filepath.Join(pkg.Path, "gen.go")
	} else {
		// For entity packages, use entity name
		outputPath = filepath.Join(pkg.Path, pkg.EntityName+".go")
	}

	if fm.dryRun {
		if fm.verboseLogging {
			log.Printf("entsquish: DRY RUN would merge %s -> %s",
				strings.Join(pkg.Files, ", "), outputPath)
		}
		return nil
	}

	// Write merged file using the shared FileSet
	err = fm.writeMergedFile(outputPath, mergedAST, sharedFileSet)
	if err != nil {
		return fmt.Errorf("failed to write merged file for package %s: %w", pkg.Path, err)
	}

	// Remove original files (except if they're the same as output)
	err = fm.removeOriginalFiles(pkg, outputPath)
	if err != nil {
		log.Printf("entsquish: warning: failed to remove original files for package %s: %v", pkg.Path, err)
		// Don't fail the operation for this
	}

	if fm.verboseLogging {
		log.Printf("entsquish: successfully merged package %s", pkg.Path)
	}

	return nil
}

// parseFiles parses all Go files in the package.
func (fm *FileMerger) parseFiles(pkg SquishablePackage, sharedFileSet *token.FileSet) ([]FileInfo, error) {
	var fileInfos []FileInfo

	for _, fileName := range pkg.Files {
		filePath := filepath.Join(pkg.Path, fileName)

		// Get file stats
		stat, err := os.Stat(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat file %s: %w", filePath, err)
		}

		// Safety check for file size
		if stat.Size() > fm.config.MaxFileSize {
			return nil, fmt.Errorf("file %s exceeds size limit (%d bytes > %d bytes). To increase the limit, add entsquish.WithMaxFileSize(%d) when configuring the extension",
				filePath, stat.Size(), fm.config.MaxFileSize, stat.Size()+10*1024*1024)
		}

		// Parse the file using the shared FileSet
		astFile, err := parser.ParseFile(sharedFileSet, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
		}

		fileInfo := FileInfo{
			Path:        filePath,
			PackageName: astFile.Name.Name,
			AST:         astFile,
			FileSet:     sharedFileSet,
			Size:        stat.Size(),
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}

// MergeASTs merges multiple AST files into a single AST.
func (fm *FileMerger) MergeASTs(fileInfos []FileInfo) (*ast.File, error) {
	if len(fileInfos) == 0 {
		return nil, fmt.Errorf("no files to merge")
	}

	// Use the first file as the base
	base := fileInfos[0].AST
	packageName := base.Name.Name

	// Verify all files have the same package name
	for _, fileInfo := range fileInfos {
		if fileInfo.AST.Name.Name != packageName {
			return nil, fmt.Errorf("package name mismatch: %s vs %s",
				packageName, fileInfo.AST.Name.Name)
		}
	}

	// Build a mapping from each file to its import path->alias mapping
	fileImportMappings := make(map[int]map[string]string) // fileIndex -> importPath -> aliasName

	for i, fileInfo := range fileInfos {
		fileImportMappings[i] = make(map[string]string)
		for _, imp := range fileInfo.AST.Imports {
			path := imp.Path.Value

			if imp.Name != nil {
				// Has explicit alias
				fileImportMappings[i][path] = imp.Name.Name
			} else {
				// No alias, use package name
				pathParts := strings.Split(strings.Trim(path, `"`), "/")
				packageName := pathParts[len(pathParts)-1]
				fileImportMappings[i][path] = packageName
			}
		}
	}

	// Create merged file
	merged := &ast.File{
		Name: &ast.Ident{Name: packageName},
	}

	// Resolve import conflicts and get deduplicated imports
	importMapping := fm.ResolveImportConflicts(fileInfos)
	pathToImport := importMapping.PathToImport

	// Convert resolved imports to sorted slice
	var imports []*ast.ImportSpec
	var importPaths []string
	for path := range pathToImport {
		importPaths = append(importPaths, path)
	}
	sort.Strings(importPaths)

	for _, path := range importPaths {
		imports = append(imports, pathToImport[path])
	}

	// Merge declarations
	var allDecls []ast.Decl

	// Add import declarations if we have imports
	if len(imports) > 0 {
		genDecl := &ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: make([]ast.Spec, len(imports)),
		}
		for i, imp := range imports {
			genDecl.Specs[i] = imp
		}
		allDecls = append(allDecls, genDecl)
	}

	// Track seen declarations to avoid duplicates
	seenDecls := make(map[string]bool)

	// Add all other declarations, updating identifiers for each file's context
	for i, fileInfo := range fileInfos {
		for _, decl := range fileInfo.AST.Decls {
			// Skip import declarations as we've already handled them
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				continue
			}

			// Generate a unique signature for this declaration to check for duplicates
			declSignature := fm.generateDeclarationSignature(decl)
			if seenDecls[declSignature] {
				// Skip this duplicate declaration
				continue
			}
			seenDecls[declSignature] = true

			// Update identifiers for this declaration based on this file's import context
			if len(fileImportMappings[i]) > 0 {
				fm.updateIdentifiersForDeclaration(decl, fileImportMappings[i], importMapping.PathToImport)
			}

			allDecls = append(allDecls, decl)
		}
	}

	merged.Decls = allDecls

	return merged, nil
}

// getImportKey generates a unique key for an import spec.
func (fm *FileMerger) getImportKey(imp *ast.ImportSpec) string {
	path := imp.Path.Value
	if imp.Name != nil {
		return imp.Name.Name + " " + path
	}
	return path
}

// ImportAliasMapping holds the mapping from original package names to their new aliases
type ImportAliasMapping struct {
	PathToImport       map[string]*ast.ImportSpec // import path to import spec
	packageNameToAlias map[string]string          // package name to its final alias in merged file
}

// resolveImportConflicts creates a consistent import mapping to avoid naming conflicts.
func (fm *FileMerger) ResolveImportConflicts(fileInfos []FileInfo) ImportAliasMapping {
	// First pass: collect all unique import paths with their preferred aliases
	pathToImport := make(map[string]*ast.ImportSpec)
	pathToAlias := make(map[string]string)
	aliasToPath := make(map[string]string)
	packageNameToAlias := make(map[string]string) // package name to its final alias

	// Define standard aliases for common conflicting packages
	standardAliases := map[string]string{
		`"database/sql"`:             "stdsql",
		`"entgo.io/ent/dialect/sql"`: "entsql",
		`"log"`:                      "stdlog",
		`"entgo.io/ent"`:             "entpkg",
	}

	// Collect ALL identifiers across all files to detect conflicts
	usedIdentifiers := fm.CollectAllIdentifiers(fileInfos)

	// Now process imports and resolve conflicts
	for _, fileInfo := range fileInfos {
		for _, imp := range fileInfo.AST.Imports {
			path := imp.Path.Value

			// Skip if we've already processed this path
			if _, exists := pathToImport[path]; exists {
				continue
			}

			var preferredAlias string
			var hasExplicitAlias bool

			// Determine the preferred alias for this import
			if imp.Name != nil {
				// Import has an explicit alias - preserve it if possible
				preferredAlias = imp.Name.Name
				hasExplicitAlias = true
			} else {
				// No explicit alias - use package name or standard alias
				if standardAlias, hasStandard := standardAliases[path]; hasStandard {
					preferredAlias = standardAlias
				} else {
					// Extract package name from path
					pathParts := strings.Split(strings.Trim(path, `"`), "/")
					preferredAlias = pathParts[len(pathParts)-1]
				}
			}

			// Extract package name for conflict checking
			pathParts := strings.Split(strings.Trim(path, `"`), "/")
			packageName := pathParts[len(pathParts)-1]

			// Check for conflicts with any identifier and generate unique alias if needed
			// Only generate new alias if there's actually a conflict and it's not an explicit alias
			if !hasExplicitAlias && (usedIdentifiers[preferredAlias] || aliasToPath[preferredAlias] != "") {
				preferredAlias = fm.GenerateUniqueAlias(packageName, usedIdentifiers, aliasToPath)
			} else if hasExplicitAlias && (aliasToPath[preferredAlias] != "" && aliasToPath[preferredAlias] != path) {
				// Explicit alias conflicts with another import's alias
				preferredAlias = fm.GenerateUniqueAlias(preferredAlias, usedIdentifiers, aliasToPath)
			}

			// Create the import spec with the resolved alias
			resolvedImport := &ast.ImportSpec{
				Path: imp.Path,
			}

			// Add alias if it's different from the package name or if it's a standard alias or explicit alias
			if preferredAlias != packageName || standardAliases[path] != "" || hasExplicitAlias {
				resolvedImport.Name = &ast.Ident{Name: preferredAlias}
			}

			// Track the mapping from package name to alias for this specific import
			packageNameToAlias[packageName] = preferredAlias

			pathToImport[path] = resolvedImport
			pathToAlias[path] = preferredAlias
			aliasToPath[preferredAlias] = path
		}
	}

	return ImportAliasMapping{
		PathToImport:       pathToImport,
		packageNameToAlias: packageNameToAlias,
	}
}

// CollectAllIdentifiers walks through all files and collects every identifier
// that could potentially conflict with import package names.
func (fm *FileMerger) CollectAllIdentifiers(fileInfos []FileInfo) map[string]bool {
	usedIdentifiers := make(map[string]bool)

	for _, fileInfo := range fileInfos {
		ast.Inspect(fileInfo.AST, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GenDecl:
				switch x.Tok {
				case token.VAR:
					// Variable declarations
					for _, spec := range x.Specs {
						if valueSpec, ok := spec.(*ast.ValueSpec); ok {
							for _, name := range valueSpec.Names {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				case token.CONST:
					// Constant declarations
					for _, spec := range x.Specs {
						if valueSpec, ok := spec.(*ast.ValueSpec); ok {
							for _, name := range valueSpec.Names {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				case token.TYPE:
					// Type declarations
					for _, spec := range x.Specs {
						if typeSpec, ok := spec.(*ast.TypeSpec); ok {
							usedIdentifiers[typeSpec.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				// Function names
				if x.Name != nil {
					usedIdentifiers[x.Name.Name] = true
				}

				// Method receivers
				if x.Recv != nil {
					for _, field := range x.Recv.List {
						for _, name := range field.Names {
							if name != nil {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				}

				// Function parameters
				if x.Type.Params != nil {
					for _, field := range x.Type.Params.List {
						for _, name := range field.Names {
							if name != nil {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				}

				// Function return values (named returns)
				if x.Type.Results != nil {
					for _, field := range x.Type.Results.List {
						for _, name := range field.Names {
							if name != nil {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				}
			case *ast.AssignStmt:
				// Assignment statements (including short declarations)
				for _, expr := range x.Lhs {
					if ident, ok := expr.(*ast.Ident); ok {
						usedIdentifiers[ident.Name] = true
					}
				}
			case *ast.RangeStmt:
				// Range loop variables
				if x.Key != nil {
					if ident, ok := x.Key.(*ast.Ident); ok {
						usedIdentifiers[ident.Name] = true
					}
				}
				if x.Value != nil {
					if ident, ok := x.Value.(*ast.Ident); ok {
						usedIdentifiers[ident.Name] = true
					}
				}
			case *ast.TypeSwitchStmt:
				// Type switch statements
				if x.Assign != nil {
					if assignStmt, ok := x.Assign.(*ast.AssignStmt); ok {
						for _, expr := range assignStmt.Lhs {
							if ident, ok := expr.(*ast.Ident); ok {
								usedIdentifiers[ident.Name] = true
							}
						}
					}
				}
			case *ast.StructType:
				// Struct field names
				if x.Fields != nil {
					for _, field := range x.Fields.List {
						for _, name := range field.Names {
							if name != nil {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				}
			case *ast.InterfaceType:
				// Interface method names
				if x.Methods != nil {
					for _, field := range x.Methods.List {
						for _, name := range field.Names {
							if name != nil {
								usedIdentifiers[name.Name] = true
							}
						}
					}
				}
			}
			return true
		})

		// Also collect existing import aliases
		for _, imp := range fileInfo.AST.Imports {
			if imp.Name != nil {
				usedIdentifiers[imp.Name.Name] = true
			}
		}
	}

	return usedIdentifiers
}

// GenerateUniqueAlias creates a unique alias for a package by trying different strategies.
func (fm *FileMerger) GenerateUniqueAlias(baseName string, usedIdentifiers map[string]bool, aliasToPath map[string]string) string {
	// First ensure baseName is a valid Go identifier
	sanitizedBase := fm.SanitizeIdentifier(baseName)

	// If the sanitized base is different from original, it means the original was invalid
	// and we should use the sanitized version as our starting point
	if sanitizedBase != baseName {
		baseName = sanitizedBase
		// For sanitized names, try the base first before adding "pkg"
		if !usedIdentifiers[baseName] && aliasToPath[baseName] == "" {
			return baseName
		}
	}

	// Strategy 1: Try baseName + "pkg"
	candidate := baseName + "pkg"
	if !usedIdentifiers[candidate] && aliasToPath[candidate] == "" {
		return candidate
	}

	// Strategy 2: Try baseName + "pkg" + number, starting from 2
	counter := 2
	for {
		candidate = fmt.Sprintf("%spkg%d", baseName, counter)
		if !usedIdentifiers[candidate] && aliasToPath[candidate] == "" {
			return candidate
		}
		counter++

		// Safety check to prevent infinite loops
		if counter > 1000 {
			// Fallback to a guaranteed unique name
			return fmt.Sprintf("pkg%d", len(aliasToPath)+1)
		}
	}
}

// SanitizeIdentifier ensures the given string is a valid Go identifier.
func (fm *FileMerger) SanitizeIdentifier(name string) string {
	if name == "" || name == "_" || name == "." {
		return "pkg"
	}

	// Check if it's a Go keyword
	goKeywords := map[string]bool{
		"break": true, "case": true, "chan": true, "const": true, "continue": true,
		"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
		"func": true, "go": true, "goto": true, "if": true, "import": true,
		"interface": true, "map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true, "var": true,
	}

	if goKeywords[name] {
		return name + "pkg"
	}

	// Check if it starts with a letter or underscore
	if len(name) > 0 && (name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z' || name[0] == '_') {
		// Basic validation - if it looks like a valid identifier, keep it
		validChars := true
		for _, r := range name[1:] {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
				validChars = false
				break
			}
		}
		if validChars {
			return name
		}
	}

	// If all else fails, return a safe default
	return "pkg"
}

// updateIdentifiersForDeclaration updates identifiers in a declaration based on the
// original file's import context and the final merged import specs.
func (fm *FileMerger) updateIdentifiersForDeclaration(decl ast.Decl, originalImports map[string]string, finalImports map[string]*ast.ImportSpec) {
	// Build a mapping from original alias to final alias
	aliasMap := make(map[string]string)

	for importPath, originalAlias := range originalImports {
		if finalImport, exists := finalImports[importPath]; exists {
			var finalAlias string
			if finalImport.Name != nil {
				finalAlias = finalImport.Name.Name
			} else {
				// No alias in final import, use package name
				pathParts := strings.Split(strings.Trim(importPath, `"`), "/")
				finalAlias = pathParts[len(pathParts)-1]
			}

			// Only add to map if the alias changed
			if originalAlias != finalAlias {
				aliasMap[originalAlias] = finalAlias
			}
		}
	}

	// Update identifiers in this declaration
	if len(aliasMap) > 0 {
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				// Check if this is a package selector (e.g., sql.Selector)
				if ident, ok := x.X.(*ast.Ident); ok {
					// If the identifier matches an original alias that changed
					if newAlias, exists := aliasMap[ident.Name]; exists {
						// Update the identifier to use the new alias
						ident.Name = newAlias
					}
				}
			}
			return true
		})
	}
}

// updateIdentifiersForAliases walks the AST and updates selector expressions
// to use the correct aliases for imports that have been aliased.
func (fm *FileMerger) updateIdentifiersForAliases(node ast.Node, aliasMap map[string]string) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			// Check if this is a package selector (e.g., sql.Selector)
			if ident, ok := x.X.(*ast.Ident); ok {
				// If the identifier matches a package name that was aliased
				if alias, exists := aliasMap[ident.Name]; exists {
					// Update the identifier to use the alias
					ident.Name = alias
				}
			}
		}
		return true
	})
}

// writeMergedFile writes the merged AST to a file.
func (fm *FileMerger) writeMergedFile(outputPath string, mergedAST *ast.File, fileSet *token.FileSet) error {
	// Format the AST
	var buf strings.Builder
	err := format.Node(&buf, fileSet, mergedAST)
	if err != nil {
		return fmt.Errorf("failed to format merged AST: %w", err)
	}

	// Write to file
	err = os.WriteFile(outputPath, []byte(buf.String()), 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputPath, err)
	}

	return nil
}

// generateDeclarationSignature creates a unique signature for a declaration to detect duplicates.
func (fm *FileMerger) generateDeclarationSignature(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.GenDecl:
		// Handle type, const, var declarations
		var signatures []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				// For type declarations, use "type:name"
				signatures = append(signatures, fmt.Sprintf("type:%s", s.Name.Name))
			case *ast.ValueSpec:
				// For var/const declarations, use "var:name" or "const:name"
				for _, name := range s.Names {
					if d.Tok == token.VAR {
						signatures = append(signatures, fmt.Sprintf("var:%s", name.Name))
					} else if d.Tok == token.CONST {
						signatures = append(signatures, fmt.Sprintf("const:%s", name.Name))
					}
				}
			}
		}
		if len(signatures) > 0 {
			return strings.Join(signatures, ",")
		}
	case *ast.FuncDecl:
		// For function declarations, use "func:name" or "method:receiver.name"
		if d.Recv != nil && len(d.Recv.List) > 0 {
			// Method: get receiver type name
			receiverType := fm.getTypeName(d.Recv.List[0].Type)
			return fmt.Sprintf("method:%s.%s", receiverType, d.Name.Name)
		} else {
			// Function
			return fmt.Sprintf("func:%s", d.Name.Name)
		}
	}

	// Fallback: use a hash or string representation
	return fmt.Sprintf("unknown:%p", decl)
}

// getTypeName extracts the type name from an expression (for receiver types).
func (fm *FileMerger) getTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		// Pointer type: *Type -> Type
		return fm.getTypeName(e.X)
	case *ast.SelectorExpr:
		// Package.Type -> Package.Type
		if ident, ok := e.X.(*ast.Ident); ok {
			return fmt.Sprintf("%s.%s", ident.Name, e.Sel.Name)
		}
	}
	return "unknown"
}

// removeOriginalFiles removes the original files after successful merge.
func (fm *FileMerger) removeOriginalFiles(pkg SquishablePackage, outputPath string) error {
	for _, fileName := range pkg.Files {
		filePath := filepath.Join(pkg.Path, fileName)

		// Don't remove if it's the same as output path
		if filePath == outputPath {
			continue
		}

		err := os.Remove(filePath)
		if err != nil {
			return fmt.Errorf("failed to remove file %s: %w", filePath, err)
		}
	}

	return nil
}
