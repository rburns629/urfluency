// Package analysis defines the analyzer contract shared by every language.
package analysis

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// File is one source file handed to an analyzer.
type File struct {
	Path    string // repository-relative, slash-separated
	Content []byte
}

// Finding is one occurrence of a catalog rule.
type Finding struct {
	Rule string
	Path string
	Line int
}

// Analyzer analyzes the files of one language. Implementations must not execute
// code from the repository under analysis.
type Analyzer interface {
	// Language is the catalog language, such as "go".
	Language() string
	// Extensions lists the file extensions the analyzer handles, such as ".go".
	Extensions() []string
	// Skip reports whether a file should be excluded, for example generated code.
	Skip(f File) bool
	// Analyze returns findings for the given files.
	Analyze(ctx context.Context, files []File) ([]Finding, error)
}

var registry []Analyzer

// Register makes an analyzer available to the engine.
func Register(a Analyzer) { registry = append(registry, a) }

// Analyzers returns registered analyzers sorted by language.
func Analyzers() []Analyzer {
	out := append([]Analyzer(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Language() < out[j].Language() })
	return out
}

// ForPath returns the analyzer responsible for a path, or nil.
func ForPath(path string) Analyzer {
	ext := strings.ToLower(filepath.Ext(path))
	for _, a := range registry {
		for _, e := range a.Extensions() {
			if e == ext {
				return a
			}
		}
	}
	return nil
}
