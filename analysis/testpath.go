package analysis

import (
	"path"
	"strings"
)

// testDirs hold tests by convention in every supported language.
var testDirs = map[string]bool{"tests": true, "test": true, "__tests__": true, "Tests": true, "androidTest": true, "testFixtures": true}

// testSuffixes end test file names by convention in compiled languages:
// FooTest.java, FooTests.cs, foo_test.dart, foo_test.cc, and so on.
var testSuffixes = []string{
	"Test.java", "Tests.java", "IT.java", "Test.kt", "Tests.kt", "Test.cs", "Tests.cs", "Tests.swift", "Test.swift",
	"_test.dart", "_test.rs", "_test.c", "_test.cc", "_test.cpp", "_unittest.cc", "_unittest.cpp",
}

// IsTestPath reports whether a repository path is a test file by the common
// conventions: _test.go; test_*.py, *_test.py, *_tests.py, tests.py, and
// conftest.py; *.test.* and *.spec.*; FooTest.java, FooTests.cs, foo_test.dart and
// similar; and files under tests, test, __tests__, Tests, or .NET *.Tests projects.
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
	for _, s := range testSuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	parts := strings.Split(p, "/")
	for _, dir := range parts[:len(parts)-1] {
		// .NET test projects are folders such as Api.Tests or Api.UnitTests.
		if testDirs[dir] || strings.HasSuffix(dir, ".Tests") || strings.HasSuffix(dir, ".UnitTests") || strings.HasSuffix(dir, ".IntegrationTests") {
			return true
		}
	}
	return false
}
