package entsquash

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// PackageDetector identifies packages that can be safely squashed.
type PackageDetector struct {
	verboseLogging bool
	config         SquashingConfig
}

// NewPackageDetector creates a new package detector.
func NewPackageDetector(verboseLogging bool, maxFileSize int64) *PackageDetector {
	config := DefaultSquashingConfig()
	config.MaxFileSize = maxFileSize
	return &PackageDetector{
		verboseLogging: verboseLogging,
		config:         config,
	}
}

// FindSquashablePackages finds all packages that can be safely squashed.
func (pd *PackageDetector) FindSquashablePackages() ([]SquashablePackage, error) {
	var squashablePackages []SquashablePackage

	// First, analyze the root gen directory itself for files that can be squashed
	if pd.verboseLogging {
		log.Printf("entsquash: analyzing root directory: %s", pd.config.BaseDir)
	}

	rootPkg, shouldSquash, err := pd.analyzeDirectory(pd.config.BaseDir)
	if err != nil {
		if pd.verboseLogging {
			log.Printf("entsquash: warning: failed to analyze root directory %s: %v", pd.config.BaseDir, err)
		}
	} else if shouldSquash {
		squashablePackages = append(squashablePackages, rootPkg)
		if pd.verboseLogging {
			log.Printf("entsquash: found squashable root package: %s (%d files)", rootPkg.Path, len(rootPkg.Files))
		}
	}

	// Walk through the gen directory for subdirectories
	err = filepath.Walk(pd.config.BaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip files, we only care about directories
		if !info.IsDir() {
			return nil
		}

		// Skip the root gen directory itself (we already analyzed it above)
		if path == pd.config.BaseDir {
			return nil
		}

		// Analyze this directory
		pkg, shouldSquash, err := pd.analyzeDirectory(path)
		if err != nil {
			if pd.verboseLogging {
				log.Printf("entsquash: warning: failed to analyze directory %s: %v", path, err)
			}
			return nil // Continue with other directories
		}

		if shouldSquash {
			squashablePackages = append(squashablePackages, pkg)
			if pd.verboseLogging {
				log.Printf("entsquash: found squashable package: %s (%d files)", pkg.Path, len(pkg.Files))
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk gen directory: %w", err)
	}

	return squashablePackages, nil
}

// analyzeDirectory analyzes a directory to determine if it should be squashed.
func (pd *PackageDetector) analyzeDirectory(dirPath string) (SquashablePackage, bool, error) {
	pkg := SquashablePackage{
		Path: dirPath,
	}

	// Get package type
	pkgType := pd.classifyPackage(dirPath)

	if pkgType != PackageTypeEntity && pkgType != PackageTypeRoot {
		if pd.verboseLogging {
			log.Printf("entsquash: skipping %s package: %s", pkgType.String(), dirPath)
		}
		return pkg, false, nil
	}

	// List Go files in the directory
	files, err := pd.listGoFiles(dirPath)
	if err != nil {
		return pkg, false, err
	}

	pkg.Files = files

	// Handle root directory differently than entity directories
	if pkgType == PackageTypeRoot {
		// For root directory, we don't expect specific entity/where files
		pkg.EntityName = "gen" // Use "gen" as the entity name for the root package
		pkg.HasEntityFile = len(files) > 0
		pkg.HasWhereFile = false // Root package doesn't have where files
	} else {
		// Determine entity name from directory path for entity packages
		pkg.EntityName = pd.extractEntityName(dirPath)

		// Check for expected files
		pkg.HasEntityFile, pkg.HasWhereFile = pd.checkExpectedFiles(files, pkg.EntityName)
	}

	// Decide if this package should be squashed
	shouldSquash := pd.shouldSquashPackage(pkg)

	return pkg, shouldSquash, nil
}

// classifyPackage determines the type of package.
func (pd *PackageDetector) classifyPackage(dirPath string) PackageType {
	// Check if this is the root gen directory
	if dirPath == pd.config.BaseDir {
		return PackageTypeRoot
	}

	relPath, err := filepath.Rel(pd.config.BaseDir, dirPath)
	if err != nil {
		return PackageTypeUnknown
	}

	// Special packages that should not be squashed
	specialPackages := []string{
		"entsf", "entsearch", "enttest", "hook", "intercept",
		"internal", "migrate", "predicate", "privacy", "runtime",
		"sfsync", "sfreconcile",
	}

	for _, special := range specialPackages {
		if relPath == special || strings.HasPrefix(relPath, special+"/") {
			return PackageTypeSpecial
		}
	}

	// Check if it's a direct subdirectory (entity package)
	if !strings.Contains(relPath, "/") {
		return PackageTypeEntity
	}

	// If it contains slashes, it's likely a nested special package
	return PackageTypeSpecial
}

// listGoFiles lists all .go files in a directory.
func (pd *PackageDetector) listGoFiles(dirPath string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var goFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	return goFiles, nil
}

// extractEntityName extracts the entity name from the directory path.
func (pd *PackageDetector) extractEntityName(dirPath string) string {
	return filepath.Base(dirPath)
}

// checkExpectedFiles checks if the package has the expected entity and where files.
func (pd *PackageDetector) checkExpectedFiles(files []string, entityName string) (bool, bool) {
	hasEntityFile := false
	hasWhereFile := false

	expectedEntityFile := entityName + ".go"
	expectedWhereFile := "where.go"

	for _, file := range files {
		if file == expectedEntityFile {
			hasEntityFile = true
		}
		if file == expectedWhereFile {
			hasWhereFile = true
		}
	}

	return hasEntityFile, hasWhereFile
}

// shouldSquashPackage determines if a package should be squashed.
func (pd *PackageDetector) shouldSquashPackage(pkg SquashablePackage) bool {
	// Handle root package differently
	if pd.classifyPackage(pkg.Path) == PackageTypeRoot {
		// For root package, we want to squash if there are multiple Go files
		if len(pkg.Files) < 2 {
			if pd.verboseLogging {
				log.Printf("entsquash: skipping root package %s: has %d files (need at least 2)", pkg.Path, len(pkg.Files))
			}
			return false
		}
		// Root package should be squashed if it has multiple Go files
		return true
	}

	// For entity packages, use the original logic
	// Must have exactly 2 files
	if len(pkg.Files) != 2 {
		if pd.verboseLogging {
			log.Printf("entsquash: skipping %s: has %d files (expected 2)", pkg.Path, len(pkg.Files))
		}
		return false
	}

	// Must have both entity and where files
	if !pkg.HasEntityFile || !pkg.HasWhereFile {
		if pd.verboseLogging {
			log.Printf("entsquash: skipping %s: missing expected files (entity=%v, where=%v)",
				pkg.Path, pkg.HasEntityFile, pkg.HasWhereFile)
		}
		return false
	}

	// Additional safety checks could go here
	// (e.g., file size limits, modification time checks, etc.)

	return true
}
