// Package tests contains structural tests that enforce architectural invariants
// defined in ARCHITECTURE.md and BELIEFS.md.
//
// Run with: go test ./tests/...
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const moduleName = "one-agent"

// forbiddenImports maps each internal package prefix to the repo-internal
// prefixes it must NOT import, per ARCHITECTURE.md dependency layer rules.
var forbiddenImports = map[string][]string{
	"internal/types": {
		"internal/config", "internal/store",
		"internal/platforms", "internal/mcp", "internal/refresh", "cmd",
	},
	"internal/config": {
		"internal/store", "internal/platforms",
		"internal/mcp", "internal/refresh", "cmd",
	},
	"internal/store": {
		"internal/platforms", "internal/mcp", "internal/refresh", "cmd",
	},
	"internal/refresh": {"internal/mcp", "cmd"},
	"internal/mcp":     {"cmd"},
}

// TestLayerDependencies enforces the import graph rules from ARCHITECTURE.md.
func TestLayerDependencies(t *testing.T) {
	root := projectRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		layer := layerOf(rel)
		if layer == "" || layer == "cmd" {
			return nil
		}

		imports := fileImports(t, path)
		banned := forbiddenImports[layer]

		for _, imp := range imports {
			if !strings.HasPrefix(imp, moduleName+"/") {
				continue
			}
			internalPath := strings.TrimPrefix(imp, moduleName+"/")
			for _, bad := range banned {
				if strings.HasPrefix(internalPath, bad) {
					t.Errorf(
						"dependency violation: %s (layer %q) imports %q — "+
							"fix: move shared code to a lower layer; layer order is types→config→store→platforms→mcp→cmd",
						rel, layer, internalPath,
					)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestCrossPlatformImports ensures no platform package imports a peer platform package.
func TestCrossPlatformImports(t *testing.T) {
	root := projectRoot(t)
	platformsDir := filepath.Join(root, "internal", "platforms")

	if _, err := os.Stat(platformsDir); os.IsNotExist(err) {
		t.Skip("internal/platforms not yet created")
	}

	entries, err := os.ReadDir(platformsDir)
	if err != nil {
		t.Fatalf("read platforms dir: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		goFiles, _ := filepath.Glob(filepath.Join(platformsDir, entry.Name(), "*.go"))
		for _, f := range goFiles {
			rel, _ := filepath.Rel(root, f)
			for _, imp := range fileImports(t, f) {
				if !strings.HasPrefix(imp, moduleName+"/internal/platforms/") {
					continue
				}
				peer := strings.TrimPrefix(imp, moduleName+"/internal/platforms/")
				if peer != entry.Name() {
					t.Errorf(
						"cross-platform import: %s imports platform %q — "+
							"platform packages must not import each other (ARCHITECTURE.md)",
						rel, peer,
					)
				}
			}
		}
	}
}

// TestFileSizeLimits enforces the 300-line file size limit from AGENTS.md.
func TestFileSizeLimits(t *testing.T) {
	const maxLines = 300
	root := projectRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if n := countLines(t, path); n > maxLines {
			t.Errorf(
				"file too large: %s has %d lines (max %d) — split by responsibility (AGENTS.md)",
				rel, n, maxLines,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestFunctionSizeLimits enforces the 50-line function limit from AGENTS.md.
func TestFunctionSizeLimits(t *testing.T) {
	const maxLines = 50
	root := projectRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		checkFunctionSizes(t, rel, path, maxLines)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestNoPrintlnInInternal ensures internal packages use slog, not fmt.Println.
func TestNoPrintlnInInternal(t *testing.T) {
	root := projectRoot(t)
	internalDir := filepath.Join(root, "internal")

	_ = filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		scanForPattern(t, path, rel, `fmt\.Println`,
			"use slog instead of fmt.Println in internal packages (AGENTS.md)")
		return nil
	})
}

// TestNoSensitiveLogKeys flags slog calls that use sensitive field key names,
// which would leak token values into logs (BELIEFS.md §2).
func TestNoSensitiveLogKeys(t *testing.T) {
	root := projectRoot(t)

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil // test files may reference key names in assertions
		}
		rel, _ := filepath.Rel(root, path)
		scanForSensitiveKeys(t, path, rel)
		return nil
	})
}
