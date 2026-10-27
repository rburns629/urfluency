package analysis

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// declared marks lines whose authorship the user declared rather than Git
// recorded. Ownership values must be non-empty for findings to count.
const declared = "declared"

// projectManifests mark a folder as a project in its own right.
var projectManifests = map[string]bool{
	"package.json": true, "go.mod": true, "pyproject.toml": true, "requirements.txt": true,
	"setup.py": true, "Pipfile": true, "Cargo.toml": true, "pom.xml": true, "build.gradle": true,
	"composer.json": true, "Gemfile": true,
}

// ProjectFiles reports whether a folder holds a project manifest such as
// package.json or go.mod, and whether it holds source files urFluency analyzes.
func ProjectFiles(dir string) (manifest, source bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		manifest = manifest || projectManifests[e.Name()]
		source = source || ForPath(e.Name()) != nil
	}
	return manifest, source
}

// ScanDir analyzes a folder without Git history. With no history to attribute
// lines, every line counts as the user's: the result is Declared, and history
// measures (AI trailers, durability, test habits) are left empty. The same
// filters as a Git scan apply: dependency folders, secret files, files over
// maxScanBytes, symlinks, and binaries are never read.
func ScanDir(ctx context.Context, dir string, opts Options) (Result, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if p != dir && ((strings.HasPrefix(name, ".") && name != ".github") || IsDependencyDir(name) || skippedDirs[name] || IsProtectedDir(name) ||
				(opts.SkipDir != nil && opts.SkipDir(p))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || IsProtected(rel) || inSkippedDir(rel) ||
			!(ForPath(rel) != nil || anyScannerApplies(rel) || readEvenWithoutAnalyzer(rel)) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxScanBytes {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	var files []ownedFile
	opts.progress("reading files", 0, len(paths))
	for i, rel := range paths {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		opts.progress("reading files", i+1, len(paths))
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || bytes.IndexByte(content, 0) >= 0 {
			continue // unreadable or binary
		}
		owned := map[int]string{}
		lines := bytes.Count(content, []byte("\n"))
		if len(content) > 0 && content[len(content)-1] != '\n' {
			lines++
		}
		for n := 1; n <= lines; n++ {
			owned[n] = declared
		}
		f := File{Path: rel, Content: content}
		of := ownedFile{file: f, owned: owned, text: true, ai: agentWritten(content, owned, opts.AgentLines[rel])}
		if a := ForPath(rel); a != nil && !a.Skip(f) {
			of.analyzer = a
		}
		files = append(files, of)
	}

	var scripts []string
	for _, s := range buildScriptPaths {
		if content, err := os.ReadFile(filepath.Join(dir, s)); err == nil {
			scripts = append(scripts, string(content))
		}
	}
	result := Result{Repos: 1, MarkedByTool: map[string]int{}, Declared: true}
	if err := analyzeFiles(ctx, &result, files, nil, scripts, opts); err != nil {
		return Result{}, err
	}
	return result, nil
}
