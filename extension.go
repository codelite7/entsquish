package entsquash

import (
	"fmt"
	"log"
	"os"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

type (
	// Extension implements the entc.Extension for file squashing optimization.
	Extension struct {
		entc.DefaultExtension
		verboseLogging bool
		dryRun         bool
		maxFileSize    int64
	}

	// ExtensionOption allows for managing the Extension configuration
	// using functional options.
	ExtensionOption func(*Extension) error
)

// NewExtension creates a new squashing extension with the given options.
func NewExtension(opts ...ExtensionOption) (*Extension, error) {
	ex := &Extension{
		verboseLogging: false,             // Default to quiet operation
		dryRun:         false,             // Default to actual operation
		maxFileSize:    100 * 1024 * 1024, // Default to 100MB limit
	}

	for _, opt := range opts {
		if err := opt(ex); err != nil {
			return nil, err
		}
	}

	// Check environment variables for overrides
	if os.Getenv("DISABLE_ENT_SQUASHING") == "true" {
		log.Printf("entsquash: disabled via DISABLE_ENT_SQUASHING environment variable")
		return &Extension{}, nil // Return no-op extension
	}

	if os.Getenv("ENT_SQUASHING_VERBOSE") == "true" {
		ex.verboseLogging = true
	}

	if os.Getenv("ENT_SQUASHING_DRY_RUN") == "true" {
		ex.dryRun = true
	}

	return ex, nil
}

// Hooks returns the list of hooks for file squashing.
// This hook runs AFTER normal generation to merge files.
func (e *Extension) Hooks() []gen.Hook {
	// If extension is disabled, return no hooks
	if e == nil || (e.verboseLogging == false && e.dryRun == false) {
		return []gen.Hook{}
	}

	return []gen.Hook{
		func(next gen.Generator) gen.Generator {
			return gen.GenerateFunc(func(g *gen.Graph) error {
				if e.verboseLogging {
					log.Printf("entsquash: starting file squashing process")
				}

				// Let normal generation complete first
				err := next.Generate(g)
				if err != nil {
					return fmt.Errorf("entsquash: normal generation failed: %w", err)
				}

				// Then squash the files
				return e.squashFiles(g)
			})
		},
	}
}

// squashFiles performs the actual file squashing operation.
func (e *Extension) squashFiles(g *gen.Graph) error {
	if e.verboseLogging {
		log.Printf("entsquash: analyzing %d nodes for squashing opportunities", len(g.Nodes))
	}

	detector := NewPackageDetector(e.verboseLogging, e.maxFileSize)
	merger := NewFileMerger(e.verboseLogging, e.dryRun, e.maxFileSize)

	// Detect packages that can be safely squashed
	squashablePackages, err := detector.FindSquashablePackages()
	if err != nil {
		return fmt.Errorf("entsquash: failed to detect squashable packages: %w", err)
	}

	if e.verboseLogging {
		log.Printf("entsquash: found %d squashable packages", len(squashablePackages))
	}

	if len(squashablePackages) == 0 {
		if e.verboseLogging {
			log.Printf("entsquash: no packages found for squashing")
		}
		return nil
	}

	// Merge files in each squashable package
	successCount := 0
	for _, pkg := range squashablePackages {
		err := merger.MergePackage(pkg)
		if err != nil {
			log.Printf("entsquash: warning: failed to merge package %s: %v", pkg.Path, err)
			continue // Continue with other packages on error
		}
		successCount++
	}

	if e.verboseLogging {
		log.Printf("entsquash: successfully squashed %d/%d packages", successCount, len(squashablePackages))
	}

	if e.dryRun {
		log.Printf("entsquash: DRY RUN completed - no files were actually modified")
	}

	return nil
}

// WithVerboseLogging enables or disables verbose logging.
func WithVerboseLogging(enabled bool) ExtensionOption {
	return func(e *Extension) error {
		e.verboseLogging = enabled
		return nil
	}
}

// WithDryRun enables or disables dry run mode (analyze only, no changes).
func WithDryRun(enabled bool) ExtensionOption {
	return func(e *Extension) error {
		e.dryRun = enabled
		return nil
	}
}

// WithMaxFileSize sets the maximum file size that can be processed.
func WithMaxFileSize(size int64) ExtensionOption {
	return func(e *Extension) error {
		e.maxFileSize = size
		return nil
	}
}
