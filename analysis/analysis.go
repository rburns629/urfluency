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
	// Positive marks an idiom the code uses well, such as error wrapping, rather
	// than a problem. Idioms count toward proficiency and never toward findings.
	Positive bool
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

// PathSkipper is implemented by analyzers that can rule out files by path alone,
// such as build output. Durability uses it for files that no longer exist.
type PathSkipper interface {
	SkipPath(path string) bool
}

// skipsPath reports whether the path's analyzer rules it out by path.
func skipsPath(p string) bool {
	ps, ok := ForPath(p).(PathSkipper)
	return ok && ps.SkipPath(p)
}

// Scanner analyzes every text file regardless of language, such as secret
// detection. Its results are reported under its name.
type Scanner interface {
	Name() string
	Applies(path string) bool
	Analyze(ctx context.Context, files []File) ([]Finding, error)
}

// FrameworkDetector is implemented by analyzers that recognize the frameworks a
// file uses, such as "react" or "django".
type FrameworkDetector interface {
	Frameworks(f File) []string
}

var (
	registry []Analyzer
	scanners []Scanner
)

// Register makes an analyzer available to the engine.
func Register(a Analyzer) { registry = append(registry, a) }

// RegisterScanner makes a cross-language scanner available to the engine.
func RegisterScanner(s Scanner) { scanners = append(scanners, s) }

// Scanners returns registered scanners sorted by name.
func Scanners() []Scanner {
	out := append([]Scanner(nil), scanners...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// IsScanner reports whether a result name belongs to a scanner, such as
// "secrets", rather than a language.
func IsScanner(name string) bool {
	for _, s := range scanners {
		if s.Name() == name {
			return true
		}
	}
	return false
}

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
