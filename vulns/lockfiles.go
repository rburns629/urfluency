package vulns

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// A project's lockfiles are bounded as a whole as well as one by one, since a
// hosted scan reads untrusted repositories: a repository of many large
// lockfiles must not exhaust the scan's memory. Real projects stay far below
// these; a large Rails and React application locks about 1,700 packages.
const (
	maxDirs          = 10000
	maxLockfileBytes = 64 << 20
	maxTotalBytes    = 256 << 20
	maxPackages      = 100_000
)

// ErrLimit is returned, wrapped, by Lockfiles when a project has more
// directories, lockfile bytes, or packages than one check reads. The packages
// found until then are returned with it.
var ErrLimit = fmt.Errorf("more than %d directories, %d MB of lockfiles, or %d packages", maxDirs, maxTotalBytes>>20, maxPackages)

// skippedDirs hold installed dependencies, virtual environments, or build
// output: the lockfiles in them describe other projects. Test data folders
// (analysis.IsTestDataDir) are skipped too.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "target": true, "build": true, "dist": true,
}

// Lockfiles walks a project directory and returns the packages its lockfiles
// lock. It skips dependency, environment, build, and test data directories and protected
// paths (analysis.IsProtected), never follows symbolic links, and reads at
// most 10,000 directories. Lockfiles larger than 64 MB are skipped.
func Lockfiles(root string) ([]Package, error) {
	var pkgs []Package
	dirs := 0
	var read int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable entries are skipped
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if p != root && (skippedDirs[d.Name()] || analysis.IsTestDataDir(d.Name()) || analysis.IsProtectedDir(d.Name())) {
				return filepath.SkipDir
			}
			if dirs++; dirs > maxDirs {
				return ErrLimit
			}
			return nil
		}
		if !d.Type().IsRegular() || !isLockfile(d.Name()) || analysis.IsProtected(rel) {
			return nil
		}
		content, ok := readRegular(p, d)
		if !ok {
			return nil
		}
		if read += int64(len(content)); read > maxTotalBytes {
			return ErrLimit
		}
		found, _ := ParseLockfile(rel, content)
		if len(pkgs)+len(found) > maxPackages {
			return ErrLimit
		}
		pkgs = append(pkgs, found...)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return pkgs, fmt.Errorf("find lockfiles in %s: %w", root, err)
		}
		return nil, fmt.Errorf("find lockfiles: %w", err)
	}
	return pkgs, nil
}

// readRegular reads a file the walk found, checking that the opened file is
// still that regular file and not a link swapped in since.
func readRegular(p string, d fs.DirEntry) ([]byte, bool) {
	walked, err := d.Info()
	if err != nil || walked.Size() > maxLockfileBytes {
		return nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(walked, opened) {
		return nil, false
	}
	content, err := io.ReadAll(io.LimitReader(f, maxLockfileBytes+1))
	if err != nil || len(content) > maxLockfileBytes {
		return nil, false
	}
	return content, true
}

func isLockfile(name string) bool {
	_, ok := lockfileParser(name)
	return ok
}

type lockParser func(content []byte) []Package

func lockfileParser(name string) (lockParser, bool) {
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json":
		return parseNPMLock, true
	case "yarn.lock":
		return parseYarnLock, true
	case "pnpm-lock.yaml":
		return parsePNPMLock, true
	case "poetry.lock":
		return parsePoetryLock, true
	case "Pipfile.lock":
		return parsePipfileLock, true
	case "go.mod":
		return parseGoMod, true
	case "composer.lock":
		return parseComposerLock, true
	case "Gemfile.lock":
		return parseGemfileLock, true
	case "Cargo.lock":
		return parseCargoLock, true
	case "packages.lock.json":
		return parseNuGetLock, true
	case "pubspec.lock":
		return parsePubspecLock, true
	}
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return parseRequirements, true
	}
	return nil, false
}

// ParseLockfile parses a lockfile chosen by its file name and returns its
// packages with Lockfile set to path. It returns false for a file name it does
// not know; a known file that does not parse yields no packages. path may use
// / or \ separators.
func ParseLockfile(path string, content []byte) ([]Package, bool) {
	parse, ok := lockfileParser(path[strings.LastIndexAny(path, `/\`)+1:])
	if !ok {
		return nil, false
	}
	path = filepath.ToSlash(path)
	var out []Package
	seen := map[Package]bool{}
	for _, p := range parse(content) {
		p.Name, p.Version, p.Lockfile = strings.TrimSpace(p.Name), strings.TrimSpace(p.Version), path
		if p.Name == "" || p.Version == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, true
}

// lines splits text into lines without their line endings.
func lines(content []byte) []string {
	ls := strings.Split(string(content), "\n")
	for i, l := range ls {
		ls[i] = strings.TrimSuffix(l, "\r")
	}
	return ls
}

// indent counts a line's leading spaces.
func indent(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// unquote strips one pair of matching single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// tomlString parses a `key = "value"` line, as the TOML lockfiles write them.
func tomlString(line string) (key, value string, ok bool) {
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	v = strings.TrimSpace(v)
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return "", "", false
	}
	return strings.TrimSpace(k), v[1 : len(v)-1], true
}
