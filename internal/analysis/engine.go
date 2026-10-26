package analysis

import (
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

// Options selects whose work to analyze.
type Options struct {
	Emails []string
	Since  time.Time
}

// LanguageResult holds findings on the user's owned lines for one language.
type LanguageResult struct {
	Language   string
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
	analyzer Analyzer
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
		if ForPath(p) != nil && !inSkippedDir(p) {
			candidates = append(candidates, p)
		}
	}

	files, err := loadOwned(ctx, repo, head, candidates, opts)
	if err != nil {
		return Result{}, err
	}

	byAnalyzer := map[Analyzer][]ownedFile{}
	for _, f := range files {
		if f.analyzer != nil {
			byAnalyzer[f.analyzer] = append(byAnalyzer[f.analyzer], f)
		}
	}
	result := Result{Head: head}
	for _, a := range Analyzers() {
		group := byAnalyzer[a]
		if len(group) == 0 {
			continue
		}
		lr := LanguageResult{Language: a.Language(), Files: len(group)}
		owned := map[string]map[int]bool{}
		inputs := make([]File, 0, len(group))
		for _, f := range group {
			inputs = append(inputs, f.file)
			owned[f.file.Path] = f.owned
			lr.OwnedLines += len(f.owned)
		}
		findings, err := a.Analyze(ctx, inputs)
		if err != nil {
			return Result{}, fmt.Errorf("%s analyzer: %w", a.Language(), err)
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
		result.Languages = append(result.Languages, lr)
	}
	return result, nil
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
			a := ForPath(p)
			f := File{Path: p, Content: content}
			if a.Skip(f) {
				return
			}
			results[i] = ownedFile{analyzer: a, file: f, owned: owned}
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
