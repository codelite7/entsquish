package entsquash

import (
	"go/ast"
	"go/token"
)

// SquashablePackage represents a package that can be safely squashed.
type SquashablePackage struct {
	// Path is the relative path to the package directory
	Path string

	// Files is the list of Go files in the package
	Files []string

	// EntityName is the name of the entity (e.g., "Contact" for contact package)
	EntityName string

	// HasEntityFile indicates if there's an entity.go file
	HasEntityFile bool

	// HasWhereFile indicates if there's a where.go file
	HasWhereFile bool
}

// FileInfo represents information about a Go file to be merged.
type FileInfo struct {
	// Path is the absolute path to the file
	Path string

	// PackageName is the Go package declaration
	PackageName string

	// AST is the parsed AST of the file
	AST *ast.File

	// FileSet is the token file set for position information
	FileSet *token.FileSet

	// Size is the file size in bytes
	Size int64
}

// MergeResult represents the result of merging files.
type MergeResult struct {
	// Success indicates if the merge was successful
	Success bool

	// OutputPath is the path to the merged file
	OutputPath string

	// OriginalFiles are the original files that were merged
	OriginalFiles []string

	// Error is any error that occurred during merging
	Error error

	// Stats contains statistics about the merge
	Stats MergeStats
}

// MergeStats contains statistics about a merge operation.
type MergeStats struct {
	// FilesProcessed is the number of files processed
	FilesProcessed int

	// LinesTotal is the total number of lines in merged file
	LinesTotal int

	// ImportsDeduped is the number of duplicate imports removed
	ImportsDeduped int

	// DeclarationsAdded is the number of declarations added
	DeclarationsAdded int

	// SizeReduction is the size reduction achieved (in bytes)
	SizeReduction int64
}

// PackageType represents the type of package for squashing decisions.
type PackageType int

const (
	// PackageTypeEntity represents a regular entity package (contact/, property/, etc.)
	PackageTypeEntity PackageType = iota

	// PackageTypeSpecial represents special packages that should not be squashed
	PackageTypeSpecial

	// PackageTypeRoot represents files in the root gen directory
	PackageTypeRoot

	// PackageTypeUnknown represents unclassified packages
	PackageTypeUnknown
)

// String returns the string representation of PackageType.
func (pt PackageType) String() string {
	switch pt {
	case PackageTypeEntity:
		return "entity"
	case PackageTypeSpecial:
		return "special"
	case PackageTypeRoot:
		return "root"
	default:
		return "unknown"
	}
}

// SquashingConfig represents configuration for the squashing process.
type SquashingConfig struct {
	// BaseDir is the base directory for Ent generated files
	BaseDir string

	// DryRun indicates if this is a dry run (no actual changes)
	DryRun bool

	// VerboseLogging enables detailed logging
	VerboseLogging bool

	// MaxFileSize is the maximum file size to process (safety limit)
	MaxFileSize int64
}

// DefaultSquashingConfig returns a default configuration.
func DefaultSquashingConfig() SquashingConfig {
	return SquashingConfig{
		BaseDir:        "src/ent/gen",
		DryRun:         false,
		VerboseLogging: false,
		MaxFileSize:    100 * 1024 * 1024, // 100MB safety limit
	}
}
