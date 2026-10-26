package analysis

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rburns629/urfluency/internal/gitdata"
)

// skippedDirs are path segments whose files are not the user's own work.
var skippedDirs = []string{"vendor", "third_party", "node_modules", "testdata", ".git"}

// maxScanBytes bounds the files given to scanners.
const maxScanBytes = 2 << 20

// Options selects whose work to analyze.
type Options struct {
	Emails []string
	Since  time.Time
}

// LanguageResult holds findings on the user's owned lines for one language or
// scanner.
type LanguageResult struct {
	Language   string
	Frameworks []string
	Files      int
	OwnedLines int
	Findings   []Finding
}

// Result is the outcome of scanning one repository.
type Result struct {
	Head      string
	Languages []LanguageResult
}

type ownedFile struct {
	analyzer Analyzer // nil when no analyzer applies or it skips the file
	text     bool     // eligible for scanners
	file     File
	owned    map[int]bool
}

// Scan analyzes the files at HEAD and keeps findings on lines owned by the user.
func Scan(ctx context.Context, repo gitdata.Repo, opts Options) (Result, error) {
	if len(opts.Emails) == 0 {
		return Result{}, fmt.Errorf("no author emails given")
	}
	head, err := repo.Head(ctx)
	if err != nil {
		return Result{}, err
	}
	paths, err := repo.TrackedFiles(ctx)
	if err != nil {
		return Result{}, err
	}
	var candidates []string
	for _, p := range paths {
		if !inSkippedDir(p) && (ForPath(p) != nil || anyScannerApplies(p)) {
			candidates = append(candidates, p)
		}
	}

	files, err := loadOwned(ctx, repo, head, candidates, opts)
	if err != nil {
		return Result{}, err
	}

	result := Result{Head: head}
	byAnalyzer := map[Analyzer][]ownedFile{}
	for _, f := range files {
		if f.analyzer != nil {
			byAnalyzer[f.analyzer] = append(byAnalyzer[f.analyzer], f)
		}
	}
	for _, a := range Analyzers() {
		group := byAnalyzer[a]
		if len(group) == 0 {
			continue
		}
		lr, err := run(ctx, a.Language(), group, a.Analyze)
		if err != nil {
			return Result{}, err
		}
		if fd, ok := a.(FrameworkDetector); ok {
			lr.Frameworks = frameworks(fd, group)
		}
		result.Languages = append(result.Languages, lr)
	}
	for _, s := range Scanners() {
		var group []ownedFile
		for _, f := range files {
			if f.text && s.Applies(f.file.Path) {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			continue
		}
		lr, err := run(ctx, s.Name(), group, s.Analyze)
		if err != nil {
			return Result{}, err
		}
		result.Languages = append(result.Languages, lr)
	}
	return result, nil
}

// run analyzes a group of files and keeps findings on owned lines.
func run(ctx context.Context, name string, group []ownedFile,
	analyze func(context.Context, []File) ([]Finding, error)) (LanguageResult, error) {
	lr := LanguageResult{Language: name, Files: len(group)}
	owned := map[string]map[int]bool{}
	inputs := make([]File, 0, len(group))
	for _, f := range group {
		inputs = append(inputs, f.file)
		owned[f.file.Path] = f.owned
		lr.OwnedLines += len(f.owned)
	}
	findings, err := analyze(ctx, inputs)
	if err != nil {
		return lr, fmt.Errorf("%s analyzer: %w", name, err)
	}
	for _, fd := range findings {
		if owned[fd.Path][fd.Line] {
			lr.Findings = append(lr.Findings, fd)
		}
	}
	sort.Slice(lr.Findings, func(i, j int) bool {
		if lr.Findings[i].Path != lr.Findings[j].Path {
			return lr.Findings[i].Path < lr.Findings[j].Path
		}
		return lr.Findings[i].Line < lr.Findings[j].Line
	})
	return lr, nil
}

func frameworks(fd FrameworkDetector, group []ownedFile) []string {
	seen := map[string]bool{}
	for _, f := range group {
		for _, fw := range fd.Frameworks(f.file) {
			seen[fw] = true
		}
	}
	out := []string{}
	for fw := range seen {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}

func anyScannerApplies(path string) bool {
	for _, s := range scanners {
		if s.Applies(path) {
			return true
		}
	}
	return false
}

// loadOwned reads each candidate file and its owned lines with a bounded worker
// pool. Results are written by index, so no shared slice is appended concurrently.
func loadOwned(ctx context.Context, repo gitdata.Repo, head string, paths []string, opts Options) ([]ownedFile, error) {
	results := make([]ownedFile, len(paths))
	errs := make([]error, len(paths))
	limit := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			owned, err := repo.OwnedLines(ctx, head, p, opts.Emails, opts.Since)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", p, err)
				return
			}
			if len(owned) == 0 {
				return
			}
			content, err := repo.Show(ctx, head, p)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", p, err)
				return
			}
			f := File{Path: p, Content: content}
			if bytes.IndexByte(content, 0) >= 0 {
				return // binary
			}
			of := ownedFile{file: f, owned: owned, text: len(content) <= maxScanBytes}
			if a := ForPath(p); a != nil && !a.Skip(f) {
				of.analyzer = a
			}
			results[i] = of
		}(i, p)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func inSkippedDir(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		for _, d := range skippedDirs {
			if segment == d {
				return true
			}
		}
	}
	return false
}
