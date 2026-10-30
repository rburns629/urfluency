package vulns

import (
	"os"
	"path/filepath"
	"testing"
)

// lockfileNames are the files each parser reads, for fuzzing them all.
var lockfileNames = []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "poetry.lock", "Pipfile.lock", "requirements.txt",
	"go.mod", "composer.lock", "Gemfile.lock", "Cargo.lock", "packages.lock.json", "pubspec.lock"}

// FuzzParseLockfile feeds every parser arbitrary input: hosted scans read
// lockfiles from untrusted repositories, so no input may panic, and every
// package returned must have a name and a version.
func FuzzParseLockfile(f *testing.F) {
	fixtures, _ := filepath.Glob(filepath.Join("testdata", "lockfiles", "*", "*"))
	for _, p := range fixtures {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte("GEM\n  specs:\n    x ()\n    ( (1)\n"))
	f.Add([]byte("packages:\n  @:\n  '/@1(':\n"))
	f.Fuzz(func(t *testing.T, content []byte) {
		for _, name := range lockfileNames {
			pkgs, ok := ParseLockfile("dir/"+name, content)
			if !ok {
				t.Fatalf("%s not recognized", name)
			}
			for _, p := range pkgs {
				if p.Name == "" || p.Version == "" || p.Lockfile != "dir/"+name {
					t.Fatalf("%s: incomplete package %+v", name, p)
				}
			}
		}
	})
}
