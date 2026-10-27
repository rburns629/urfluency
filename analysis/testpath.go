package analysis

import (
	"path"
	"strings"
)

// testDirs hold tests by convention in every supported language.
var testDirs = map[string]bool{"tests": true, "test": true, "__tests__": true}

// IsTestPath reports whether a repository path is a test file by the common
// conventions: _test.go; test_*.py, *_test.py, *_tests.py, tests.py, and
// conftest.py; *.test.* and *.spec.*; and files under tests, test, or __tests__.
func IsTestPath(p string) bool {
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"),
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"), strings.HasSuffix(base, "_tests.py"),
		base == "tests.py", base == "conftest.py",
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."):
		return true
	}
	parts := strings.Split(p, "/")
	for _, dir := range parts[:len(parts)-1] {
		if testDirs[dir] {
			return true
		}
	}
	return false
}
