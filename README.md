# entsquish

A Go library that optimizes [Ent](https://entgo.io/) generated code by merging multiple files within each package into a single file. This reduces the number of generated files and can improve build performance for large schemas.

## Features

- **Automatic file merging**: Combines entity.go and where.go files into a single file per entity
- **Import conflict resolution**: Intelligently handles naming conflicts between imports and local identifiers
- **Configurable**: Multiple options for controlling the squishing behavior
- **Ent integration**: Works seamlessly as an Ent extension

## Installation

```bash
go get github.com/codelite7/entsquish
```

## Quick Start

Add the entsquish extension to your Ent code generation:

```go
package main

import (
    "log"
    
    "entgo.io/ent/entc"
    "entgo.io/ent/entc/gen"
    "github.com/codelite7/entsquish"
)

func main() {
    // Create the entsquish extension
    squishExt, err := entsquish.NewExtension()
    if err != nil {
        log.Fatalf("creating entsquish extension: %v", err)
    }

    // Generate code with squishing
    err = entc.Generate("./schema", &gen.Config{}, entc.Extensions(squishExt))
    if err != nil {
        log.Fatalf("running ent codegen: %v", err)
    }
}
```

## Configuration Examples

### Basic Configuration

```go
// Default configuration
ext, err := entsquish.NewExtension()
```

### Verbose Logging

```go
// Enable detailed logging to see what's being merged
ext, err := entsquish.NewExtension(
    entsquish.WithVerboseLogging(true),
)
```

### Dry Run Mode

```go
// Analyze what would be merged without making changes
ext, err := entsquish.NewExtension(
    entsquish.WithDryRun(true),
    entsquish.WithVerboseLogging(true), // Recommended with dry run
)
```

### Custom File Size Limit

```go
// Set custom file size limit (default: 100MB)
ext, err := entsquish.NewExtension(
    entsquish.WithMaxFileSize(50 * 1024 * 1024), // 50MB limit
)
```

### Production Configuration

```go
// Recommended production setup
ext, err := entsquish.NewExtension(
    entsquish.WithVerboseLogging(false), // Quiet operation
    entsquish.WithMaxFileSize(100 * 1024 * 1024), // 100MB limit
)
```

## Environment Variables

You can control entsquish behavior using environment variables:

```bash
# Completely disable entsquish
export DISABLE_ENT_SQUISHING=true

# Enable verbose logging
export ENT_SQUISHING_VERBOSE=true

# Enable dry run mode
export ENT_SQUISHING_DRY_RUN=true
```

## Integration with Existing Ent Setup

### With existing extensions

```go
func main() {
    // Your existing extensions
    existingExt := &MyCustomExtension{}
    
    // Add entsquish
    squishExt, err := entsquish.NewExtension()
    if err != nil {
        log.Fatalf("creating entsquish extension: %v", err)
    }

    // Generate with multiple extensions
    err = entc.Generate("./schema", &gen.Config{}, 
        entc.Extensions(existingExt, squishExt))
    if err != nil {
        log.Fatalf("running ent codegen: %v", err)
    }
}
```

### With custom generation directory

```go
func main() {
    squishExt, err := entsquish.NewExtension()
    if err != nil {
        log.Fatalf("creating entsquish extension: %v", err)
    }

    config := &gen.Config{
        Target: "./custom/ent/gen", // Custom generation directory
    }

    err = entc.Generate("./schema", config, entc.Extensions(squishExt))
    if err != nil {
        log.Fatalf("running ent codegen: %v", err)
    }
}
```

## What Gets Squished

### Entity Packages
For each entity (e.g., `User`), entsquish merges:
- `user.go` (entity definition)
- `where.go` (query predicates)

Into a single `user.go` file.

### Root Package Files
Files in the root generation directory are also consolidated when possible.

### What's NOT Squished
- Special packages: `migrate`, `runtime`, `hook`, `intercept`, etc.
- Packages with non-standard file structures
- Files exceeding the size limit

## Import Conflict Resolution

entsquish automatically handles import naming conflicts:

```go
// Before: conflicts between import and local variable
import "errors"

func process() {
    errors := []string{"validation failed"} // conflicts with import
    return errors.New("processing failed")   // uses import
}

// After: automatic aliasing
import errorspkg "errors"

func process() {
    errors := []string{"validation failed"} // local variable unchanged
    return errorspkg.New("processing failed") // import aliased
}
```

## Troubleshooting

### Large Files
If you encounter file size errors:
```go
ext, err := entsquish.NewExtension(
    entsquish.WithMaxFileSize(200 * 1024 * 1024), // Increase limit
)
```

### Debugging Issues
Enable verbose logging to see what's happening:
```go
ext, err := entsquish.NewExtension(
    entsquish.WithVerboseLogging(true),
    entsquish.WithDryRun(true), // Don't make changes while debugging
)
```

### Disable Temporarily
Set environment variable:
```bash
export DISABLE_ENT_SQUISHING=true
```

