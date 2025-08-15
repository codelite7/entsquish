package entsquish

import (
	"fmt"
	"log"
	"os"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

type (
	// Extension implements the entc.Extension for file squishing optimization.
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

// NewExtension creates a new squishing extension with the given options.
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
	if os.Getenv("DISABLE_ENT_SQUISHING") == "true" {
		log.Printf("entsquish: disabled via DISABLE_ENT_SQUISHING environment variable")
		return &Extension{}, nil // Return no-op extension
	}

	if os.Getenv("ENT_SQUISHING_VERBOSE") == "true" {
		ex.verboseLogging = true
	}

	if os.Getenv("ENT_SQUISHING_DRY_RUN") == "true" {
		ex.dryRun = true
	}

	return ex, nil
}

// Hooks returns the list of hooks for file squishing.
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
					log.Printf("entsquish: starting file squishing process")
				}

				// Let normal generation complete first
				err := next.Generate(g)
				if err != nil {
					return fmt.Errorf("entsquish: normal generation failed: %w", err)
				}

				// Then squish the files
				return e.squishFiles(g)
			})
		},
	}
}

// squishFiles performs the actual file squishing operation.
func (e *Extension) squishFiles(g *gen.Graph) error {
	if e.verboseLogging {
		log.Printf("entsquish: analyzing %d nodes for squishing opportunities", len(g.Nodes))
	}

	detector := NewPackageDetector(e.verboseLogging, e.maxFileSize)
	merger := NewFileMerger(e.verboseLogging, e.dryRun, e.maxFileSize)

	// Detect packages that can be safely squished
	squishablePackages, err := detector.FindSquishablePackages()
	if err != nil {
		return fmt.Errorf("entsquish: failed to detect squishable packages: %w", err)
	}

	if e.verboseLogging {
		log.Printf("entsquish: found %d squishable packages", len(squishablePackages))
	}

	if len(squishablePackages) == 0 {
		if e.verboseLogging {
			log.Printf("entsquish: no packages found for squishing")
		}
		return nil
	}

	// Merge files in each squishable package
	successCount := 0
	for _, pkg := range squishablePackages {
		err := merger.MergePackage(pkg)
		if err != nil {
			log.Printf("entsquish: warning: failed to merge package %s: %v", pkg.Path, err)
			continue // Continue with other packages on error
		}
		successCount++
	}

	if e.verboseLogging {
		log.Printf("entsquish: successfully squished %d/%d packages", successCount, len(squishablePackages))
	}

	if e.dryRun {
		log.Printf("entsquish: DRY RUN completed - no files were actually modified")
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
