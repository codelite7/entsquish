# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**entsquish** is a Go library that optimizes generated Ent code by "squishing" multiple files within a package into a single file. This reduces the number of files and improves build performance for large Ent schemas.

### Core Architecture

The project consists of 4 main components:

1. **Extension** (`extension.go:13-25`) - Ent code generation extension that integrates with the Ent framework
2. **PackageDetector** (`package_detector.go:12-25`) - Identifies packages that can be safely squished 
3. **FileMerger** (`file_merger.go:17-33`) - Handles the actual merging of Go files with import conflict resolution
4. **Type Definitions** (`types.go`) - Core data structures and configuration

### Key Types

- `SquishablePackage` (`types.go:8-24`) - Represents a package that can be squished
- `FileInfo` (`types.go:27-42`) - Information about individual Go files  
- `MergeResult` (`types.go:45-63`) - Results of merge operations
- `SquishingConfig` (`types.go:115-130`) - Configuration for the squishing process

### Extension Integration

The extension integrates with Ent via `entc.Extension` and runs hooks after normal code generation (`extension.go:61-85`). It can be configured with functional options:

- `WithVerboseLogging()` - Control logging verbosity
- `WithDryRun()` - Analyze without making changes
- `WithMaxFileSize()` - Set file size limits

### Package Detection Logic

The PackageDetector (`package_detector.go:27-88`) identifies two types of squishable packages:

1. **Entity packages** - Must have exactly 2 files (entity.go + where.go)
2. **Root packages** - Must have at least 2 Go files

Special packages like `entsf`, `migrate`, `runtime` are excluded from squishing.

### Import Conflict Resolution

The FileMerger includes sophisticated import conflict resolution (`file_merger.go:280-363`):

- Detects naming conflicts between imports and local identifiers
- Generates unique aliases for conflicting packages  
- Updates selector expressions to use correct aliases
- Handles standard library conflicts with predefined aliases

## Common Development Commands

### Build & Test
```bash
go build .                    # Build the library
go test .                     # Run all tests  
go test -v .                  # Run tests with verbose output
go vet .                      # Run static analysis
```

### Module Management
```bash
go mod tidy                   # Clean up dependencies
go mod download               # Download dependencies
```

### Testing Specific Components
```bash
go test -run TestResolveImportConflicts    # Test import conflict resolution
go test -run TestMergeASTs                 # Test AST merging
go test -run TestCollectAllIdentifiers     # Test identifier collection
```

## Development Notes

### File Naming Conventions
- `*_test.go` - Test files
- Core functionality split across logical components
- No external build tools or Makefiles required

### Safety Features
- File size limits to prevent processing huge files
- Dry run mode for testing changes
- Import conflict detection and resolution

### Environment Variables
- `DISABLE_ENT_SQUISHING=true` - Completely disable the extension
- `ENT_SQUISHING_VERBOSE=true` - Enable verbose logging
- `ENT_SQUISHING_DRY_RUN=true` - Enable dry run mode

### Testing Strategy
The test suite (`file_merger_test.go`) includes comprehensive tests for:
- Import conflict resolution with various scenarios
- Identifier collection across different Go constructs  
- AST merging with realistic code examples
- Edge cases like dot imports and underscore imports