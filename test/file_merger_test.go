package test

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/codelite7/entsquish"
)

func TestResolveImportConflicts(t *testing.T) {
	tests := []struct {
		name            string
		sourceFiles     []string
		expectedImports map[string]string // import path -> expected alias
		expectError     bool
	}{
		{
			name: "variable name collision with errors package",
			sourceFiles: []string{
				`package test

import "errors"

func foo() {
	errors := retrieveErrors()
	return errors.New("x")
}`,
			},
			expectedImports: map[string]string{
				`"errors"`: "errorspkg",
			},
		},
		{
			name: "multiple imports with same base name",
			sourceFiles: []string{
				`package test

import (
	"github.com/a/bar"
	"github.com/b/bar"
)

func test() {
	bar.FuncA()
	bar.FuncB()
}`,
			},
			expectedImports: map[string]string{
				`"github.com/a/bar"`: "",       // first one keeps original name
				`"github.com/b/bar"`: "barpkg", // second one gets aliased
			},
		},
		{
			name: "no collision case",
			sourceFiles: []string{
				`package test

import "fmt"

func test() {
	fmt.Println("hello")
}`,
			},
			expectedImports: map[string]string{
				`"fmt"`: "", // no alias needed
			},
		},
		{
			name: "go keyword as package name",
			sourceFiles: []string{
				`package test

import forlib "github.com/example/for"

func test() {
	forlib.DoSomething()
}`,
			},
			expectedImports: map[string]string{
				`"github.com/example/for"`: "forlib",
			},
		},
		{
			name: "complex file with nested scopes",
			sourceFiles: []string{
				`package test

import (
	"errors" 
	"fmt"
)

type User struct {
	errors []string
}

func (u *User) Process() error {
	fmt := "custom format"
	errors := u.errors
	
	if len(errors) > 0 {
		return errors.New("has errors")
	}
	return nil
}

func helper(errors string, fmt int) {
	// function parameters that conflict
}`,
			},
			expectedImports: map[string]string{
				`"errors"`: "errorspkg",
				`"fmt"`:    "fmtpkg",
			},
		},
		{
			name: "constants and types collision",
			sourceFiles: []string{
				`package test

import "time"

const time = "constant"

type Duration struct {
	time int
}

func test() {
	d := time.Second
}`,
			},
			expectedImports: map[string]string{
				`"time"`: "timepkg",
			},
		},
		{
			name: "range loop variables collision",
			sourceFiles: []string{
				`package test

import "strings"

func test() {
	for strings := range getData() {
		result := strings.Split("test", ",")
	}
}`,
			},
			expectedImports: map[string]string{
				`"strings"`: "stringspkg",
			},
		},
		{
			name: "standard aliases are applied",
			sourceFiles: []string{
				`package test

import (
	"database/sql"
	"entgo.io/ent/dialect/sql"
)

func test() {
	var db *sql.DB
	var query *sql.Selector
}`,
			},
			expectedImports: map[string]string{
				`"database/sql"`:             "stdsql",
				`"entgo.io/ent/dialect/sql"`: "entsql",
			},
		},
		{
			name: "existing aliases are preserved",
			sourceFiles: []string{
				`package test

import myalias "errors"

func test() {
	return myalias.New("test")
}`,
			},
			expectedImports: map[string]string{
				`"errors"`: "myalias",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse source files
			fileInfos := parseTestFiles(t, tt.sourceFiles)

			// Create file merger and resolve conflicts
			fm := entsquash.NewFileMerger(false, false, 1000000)
			mapping := fm.ResolveImportConflicts(fileInfos)

			// Verify expected imports
			for expectedPath, expectedAlias := range tt.expectedImports {
				importSpec, exists := mapping.PathToImport[expectedPath]
				if !exists {
					t.Errorf("Expected import path %s not found", expectedPath)
					continue
				}

				var actualAlias string
				if importSpec.Name != nil {
					actualAlias = importSpec.Name.Name
				}

				if actualAlias != expectedAlias {
					t.Errorf("Import %s: expected alias %q, got %q",
						expectedPath, expectedAlias, actualAlias)
				}
			}
		})
	}
}

func TestCollectAllIdentifiers(t *testing.T) {
	sourceCode := `package test

import (
	"fmt"
	alias "errors"
)

// Package-level declarations
var (
	globalVar = "test"
	errors    = []string{}
)

const MaxRetries = 5

type User struct {
	Name   string
	errors []string
}

type ErrorHandler interface {
	HandleError(err error)
}

// Function with various parameter types
func ProcessData(input string, errors chan error) (result string, count int) {
	// Local variables
	var localVar string
	temp := "temporary"
	
	// Range loop
	for i, data := range input {
		// Short declarations
		processed := processItem(data)
		result += processed
	}
	
	// Type switch
	switch v := input.(type) {
	case string:
		result = v
	}
	
	return result, len(input)
}

// Method with receiver
func (u *User) AddError(msg string) {
	u.errors = append(u.errors, msg)
}
`

	fileInfos := parseTestFiles(t, []string{sourceCode})
	fm := entsquash.NewFileMerger(false, false, 1000000)
	identifiers := fm.CollectAllIdentifiers(fileInfos)

	expectedIdentifiers := []string{
		// Variables
		"globalVar", "errors", "localVar", "temp", "i", "data", "processed", "v",
		// Constants
		"MaxRetries",
		// Types
		"User", "ErrorHandler",
		// Functions
		"ProcessData", "AddError",
		// Parameters
		"input", "count", "result", "msg",
		// Struct fields
		"Name",
		// Method receivers
		"u",
		// Import aliases
		"alias",
	}

	for _, expected := range expectedIdentifiers {
		if !identifiers[expected] {
			t.Errorf("Expected identifier %q not found", expected)
		}
	}

	// Verify we don't have identifiers that shouldn't be there
	unexpectedIdentifiers := []string{
		"fmt",                    // should not be collected as it's not aliased
		"string", "int", "error", // built-in types
	}

	for _, unexpected := range unexpectedIdentifiers {
		if identifiers[unexpected] {
			t.Errorf("Unexpected identifier %q found", unexpected)
		}
	}
}

func TestGenerateUniqueAlias(t *testing.T) {
	tests := []struct {
		name            string
		baseName        string
		usedIdentifiers map[string]bool
		aliasToPath     map[string]string
		expectedPattern string // pattern to match against result
	}{
		{
			name:            "basic case - no conflicts",
			baseName:        "errors",
			usedIdentifiers: map[string]bool{},
			aliasToPath:     map[string]string{},
			expectedPattern: "errorspkg",
		},
		{
			name:            "first attempt conflicts",
			baseName:        "errors",
			usedIdentifiers: map[string]bool{"errorspkg": true},
			aliasToPath:     map[string]string{},
			expectedPattern: "errorspkg2",
		},
		{
			name:            "multiple conflicts",
			baseName:        "errors",
			usedIdentifiers: map[string]bool{"errorspkg": true, "errorspkg2": true},
			aliasToPath:     map[string]string{},
			expectedPattern: "errorspkg3",
		},
		{
			name:            "keyword as base name",
			baseName:        "for",
			usedIdentifiers: map[string]bool{},
			aliasToPath:     map[string]string{},
			expectedPattern: "forpkg",
		},
		{
			name:            "invalid identifier",
			baseName:        "123invalid",
			usedIdentifiers: map[string]bool{},
			aliasToPath:     map[string]string{},
			expectedPattern: "pkg",
		},
		{
			name:            "empty string",
			baseName:        "",
			usedIdentifiers: map[string]bool{},
			aliasToPath:     map[string]string{},
			expectedPattern: "pkg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := entsquash.NewFileMerger(false, false, 1000000)
			result := fm.GenerateUniqueAlias(tt.baseName, tt.usedIdentifiers, tt.aliasToPath)

			if result != tt.expectedPattern {
				t.Errorf("Expected %q, got %q", tt.expectedPattern, result)
			}

			// Verify the result is not in used identifiers or alias map
			if tt.usedIdentifiers[result] {
				t.Errorf("Generated alias %q conflicts with used identifiers", result)
			}
			if tt.aliasToPath[result] != "" {
				t.Errorf("Generated alias %q conflicts with existing aliases", result)
			}
		})
	}
}

func TestSanitizeIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"validname", "validname"},
		{"_underscore", "_underscore"},
		{"CamelCase", "CamelCase"},
		{"with123numbers", "with123numbers"},
		{"", "pkg"},
		{"_", "pkg"},
		{".", "pkg"},
		{"123invalid", "pkg"},
		{"for", "forpkg"},         // Go keyword
		{"package", "packagepkg"}, // Go keyword
		{"invalid-chars", "pkg"},
		{"unicode∆", "pkg"},
	}

	fm := entsquash.NewFileMerger(false, false, 1000000)
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := fm.SanitizeIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMergeASTsWithCollisions(t *testing.T) {
	// Test the complete flow from source files to merged AST
	sourceFiles := []string{
		`package test

import "errors"

func foo() {
	errors := []string{"test"}
	return errors.New("something")
}`,
		`package test

import "fmt"

func bar() {
	fmt.Println("hello")
}`,
	}

	fileInfos := parseTestFiles(t, sourceFiles)
	fm := entsquash.NewFileMerger(false, false, 1000000)

	merged, err := fm.MergeASTs(fileInfos)
	if err != nil {
		t.Fatalf("mergeASTs failed: %v", err)
	}

	// Verify the merged AST has the correct import structure
	// Look for import declarations in the AST
	var importDecl *ast.GenDecl
	for _, decl := range merged.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecl = genDecl
			break
		}
	}

	if importDecl == nil {
		t.Fatal("Expected import declaration in merged AST")
	}

	if len(importDecl.Specs) == 0 {
		t.Fatal("Expected import specs in merged AST")
	}

	// Look for the errors import - it should be aliased
	var errorsImport *ast.ImportSpec
	for _, spec := range importDecl.Specs {
		if imp, ok := spec.(*ast.ImportSpec); ok && imp.Path.Value == `"errors"` {
			errorsImport = imp
			break
		}
	}

	if errorsImport == nil {
		t.Fatal("errors import not found in merged AST")
	}

	if errorsImport.Name == nil || errorsImport.Name.Name != "errorspkg" {
		t.Errorf("Expected errors import to be aliased as 'errorspkg', got %v", errorsImport.Name)
	}

	// TODO: Add verification that selector expressions are updated
	// This would require inspecting the merged declarations
}

// Helper function to parse test source code into FileInfo structs
func parseTestFiles(t *testing.T, sources []string) []entsquash.FileInfo {
	var fileInfos []entsquash.FileInfo
	fileSet := token.NewFileSet()

	for i, source := range sources {
		astFile, err := parser.ParseFile(fileSet, "", source, parser.ParseComments)
		if err != nil {
			t.Fatalf("Failed to parse source file %d: %v", i, err)
		}

		fileInfo := entsquash.FileInfo{
			Path:        "",
			PackageName: astFile.Name.Name,
			AST:         astFile,
			FileSet:     fileSet,
			Size:        int64(len(source)),
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos
}

func TestGeneratedCodeCompiles(t *testing.T) {
	// Test that merged code compiles successfully
	sourceFiles := []string{
		`package test

import (
	"errors"
	"fmt"
	"time"
)

var errors = []string{"test"}
var time = "test time"

func foo() error {
	fmt.Println("processing")
	if len(errors) > 0 {
		return errors.New("has errors")
	}
	
	d := time.Second
	return nil
}`,
		`package test

import "strings"

func bar() {
	data := "hello,world"
	parts := strings.Split(data, ",")
	fmt.Println(parts)
}`,
	}

	fileInfos := parseTestFiles(t, sourceFiles)
	fm := entsquash.NewFileMerger(false, false, 1000000)

	merged, err := fm.MergeASTs(fileInfos)
	if err != nil {
		t.Fatalf("mergeASTs failed: %v", err)
	}

	// Convert merged AST back to source code
	var buf strings.Builder
	fileSet := token.NewFileSet()
	err = format.Node(&buf, fileSet, merged)
	if err != nil {
		t.Fatalf("Failed to format merged AST: %v", err)
	}

	generatedCode := buf.String()
	t.Logf("Generated code:\n%s", generatedCode)

	// Verify the generated code can be parsed again (compilation check)
	_, err = parser.ParseFile(fileSet, "test.go", generatedCode, parser.ParseComments)
	if err != nil {
		t.Fatalf("Generated code does not compile: %v", err)
	}

	// Verify that package references are updated correctly
	if !strings.Contains(generatedCode, "errorspkg.New") {
		t.Error("Expected 'errorspkg.New' in generated code")
	}
	if !strings.Contains(generatedCode, "timepkg.Second") {
		t.Error("Expected 'timepkg.Second' in generated code")
	}
	// fmt should remain as-is since there's no collision
	if !strings.Contains(generatedCode, "fmt.Println") {
		t.Error("Expected 'fmt.Println' in generated code")
	}

	// Verify imports are properly aliased
	if !strings.Contains(generatedCode, `errorspkg "errors"`) {
		t.Error("Expected 'errorspkg \"errors\"' import alias")
	}
	if !strings.Contains(generatedCode, `timepkg "time"`) {
		t.Error("Expected 'timepkg \"time\"' import alias")
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("dot import", func(t *testing.T) {
		sourceCode := `package test

import . "fmt"

func test() {
	Println("hello")
}`

		fileInfos := parseTestFiles(t, []string{sourceCode})
		fm := entsquash.NewFileMerger(false, false, 1000000)
		mapping := fm.ResolveImportConflicts(fileInfos)

		// Dot imports should be handled gracefully
		if len(mapping.PathToImport) != 1 {
			t.Errorf("Expected 1 import, got %d", len(mapping.PathToImport))
		}
	})

	t.Run("underscore import", func(t *testing.T) {
		sourceCode := `package test

import _ "net/http/pprof"

func test() {
	// side effect import
}`

		fileInfos := parseTestFiles(t, []string{sourceCode})
		fm := entsquash.NewFileMerger(false, false, 1000000)
		mapping := fm.ResolveImportConflicts(fileInfos)

		// Underscore imports should be handled gracefully
		if len(mapping.PathToImport) != 1 {
			t.Errorf("Expected 1 import, got %d", len(mapping.PathToImport))
		}
	})

	t.Run("very long package names", func(t *testing.T) {
		longPackageName := strings.Repeat("verylongpackagename", 10)

		fm := entsquash.NewFileMerger(false, false, 1000000)
		result := fm.GenerateUniqueAlias(longPackageName, map[string]bool{}, map[string]string{})

		if !strings.HasSuffix(result, "pkg") {
			t.Errorf("Expected long package name to get 'pkg' suffix, got %q", result)
		}
	})
}
