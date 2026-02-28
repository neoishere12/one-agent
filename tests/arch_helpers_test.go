// arch_helpers_test.go — shared helpers for arch_test.go.
// Kept in a separate file to stay within the 300-line limit enforced by TestFileSizeLimits.
package tests

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// sensitiveLogKey matches slog field names that would expose token values.
// Logging these keys violates BELIEFS.md §2 ("Never Log Sensitive Values").
var sensitiveLogKey = regexp.MustCompile(
	`"(access_token|refresh_token|payment_token|master_key|device_header|secret|password)"`,
)

// projectRoot resolves the repository root relative to this test file.
// Uses runtime.Caller so it works regardless of the working directory at test time.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("arch: cannot resolve test file path via runtime.Caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

// layerOf maps a relative file path to its dependency layer identifier.
// Returns "" for files outside tracked layers (tests/, docs/, etc.).
func layerOf(relPath string) string {
	rel := filepath.ToSlash(relPath)
	switch {
	case strings.HasPrefix(rel, "internal/types/"):
		return "internal/types"
	case strings.HasPrefix(rel, "internal/config/"):
		return "internal/config"
	case strings.HasPrefix(rel, "internal/store/"):
		return "internal/store"
case strings.HasPrefix(rel, "internal/platforms/"):
		return "internal/platforms"
	case strings.HasPrefix(rel, "internal/refresh/"):
		return "internal/refresh"
	case strings.HasPrefix(rel, "internal/mcp/"):
		return "internal/mcp"
	case strings.HasPrefix(rel, "cmd/"):
		return "cmd"
	default:
		return ""
	}
}

// fileImports parses a Go source file and returns its import paths (unquoted).
func fileImports(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("parse %s: %v", path, err)
		return nil
	}
	imports := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		imports = append(imports, strings.Trim(imp.Path.Value, `"`))
	}
	return imports
}

// checkFunctionSizes uses go/ast to report functions exceeding maxLines.
func checkFunctionSizes(t *testing.T, rel, path string, maxLines int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return // parse errors surfaced elsewhere
	}
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			return true
		}
		start := fset.Position(fd.Body.Lbrace).Line
		end := fset.Position(fd.Body.Rbrace).Line
		if lines := end - start; lines > maxLines {
			t.Errorf(
				"function too long: %s %s() is %d lines (max %d) — extract helpers (AGENTS.md)",
				rel, fd.Name.Name, lines, maxLines,
			)
		}
		return true
	})
}

// countLines returns the total number of lines in a file.
func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Errorf("open %s: %v", path, err)
		return 0
	}
	defer f.Close()

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}

// scanForPattern fails the test for each line in path that matches the regex pattern.
func scanForPattern(t *testing.T, path, rel, pattern, msg string) {
	t.Helper()
	re := regexp.MustCompile(pattern)
	f, err := os.Open(path)
	if err != nil {
		t.Errorf("open %s: %v", rel, err)
		return
	}
	defer f.Close()

	lineNo := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lineNo++
		if re.MatchString(sc.Text()) {
			t.Errorf("%s:%d: %s", rel, lineNo, msg)
		}
	}
}

// scanForSensitiveKeys flags lines that are log calls containing sensitive key names.
func scanForSensitiveKeys(t *testing.T, path, rel string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Errorf("open %s: %v", rel, err)
		return
	}
	defer f.Close()

	lineNo := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if !strings.Contains(line, "slog.") && !strings.Contains(line, "log.") {
			continue
		}
		if sensitiveLogKey.MatchString(line) {
			t.Errorf(
				"%s:%d: sensitive key in log call — log expiry or last-4 chars only, never the value (BELIEFS.md §2): %s",
				rel, lineNo, strings.TrimSpace(line),
			)
		}
	}
}
