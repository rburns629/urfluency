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
	// AgentLines maps repository-relative paths to normalized lines an agent wrote,
	// for attributing owned lines to AI.
	AgentLines map[string]map[string]bool
}

// LanguageResult holds findings on the user's owned lines for one language or
// scanner.
type LanguageResult struct {
	Language   string
	Frameworks []string
	Files      int
	OwnedLines int
	Findings   []Finding
	// AssistedLines are owned lines an agent wrote in local sessions or that
	// come from commits marked AI-assisted; AssistedFindings are findings on them.
	AssistedLines    int
	AssistedFindings int
	AssistedByRule   map[string]int // findings on AI-assisted lines, per rule
	TestLines        int            // owned lines in test files, included in OwnedLines
}

// Result is the outcome of scanning one or more repositories.
type Result struct {
	Head      string // empty when several repositories are merged
	Repos     int
	Languages []LanguageResult
	// OwnedLines counts each code file once; AILines are the owned lines that
	// match agent-written lines. Findings and AIFindings count findings in code
	// files, so prose and configuration do not dilute the comparison.
	OwnedLines int
	AILines    int
	AIFindings int
	Findings   int
	// MarkedLines are owned code lines from commits whose trailers name a coding
	// tool; MarkedByTool splits them by tool (a commit can name several).
	MarkedLines  int
	MarkedByTool map[string]int
	Durability   Durability
	Tests        TestHabits
	PerRepo      []RepoPractices
}

type ownedFile struct {
	analyzer Analyzer // nil when no analyzer applies or it skips the file
	text     bool     // eligible for scanners
	file     File
	owned    map[int]string // line → commit
	ai       map[int]bool   // lines matching agent-written session lines
	assisted map[int]bool   // ai lines plus lines from AI-marked commits
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
	// Protected files are dropped here, before blame or show, so their contents are
	// never read.
	var candidates []string
	for _, p := range paths {
		if !inSkippedDir(p) && !IsProtected(p) && (ForPath(p) != nil || anyScannerApplies(p) || readEvenWithoutAnalyzer(p)) {
			candidates = append(candidates, p)
		}
	}

	files, err := loadOwned(ctx, repo, head, candidates, opts)
	if err != nil {
		return Result{}, err
	}

	result := Result{Head: head, Repos: 1, MarkedByTool: map[string]int{}}
	commits, err := repo.Commits(ctx, opts.Emails)
	if err != nil {
		return Result{}, err
	}
	tools := map[string][]string{}
	for hash, c := range commits {
		if t := AITools(c); len(t) > 0 {
			tools[hash] = t
		}
	}
	added, err := repo.AddedLines(ctx, opts.Emails)
	if err != nil {
		return Result{}, err
	}
	if result.Durability, err = durability(ctx, repo, added, commits, tools, opts.Since, time.Now()); err != nil {
		return Result{}, err
	}
	result.Tests = testHabits(added, commits, tools, opts.Since)
	var scripts []string
	for _, p := range paths {
		for _, s := range buildScriptPaths {
			if p == s {
				if content, err := repo.Show(ctx, head, p); err == nil {
					scripts = append(scripts, string(content))
				}
			}
		}
	}
	ai := map[string]map[int]bool{}
	for i := range files {
		f := &files[i]
		f.assisted = map[int]bool{}
		for line, commit := range f.owned {
			if f.ai[line] || len(tools[commit]) > 0 {
				f.assisted[line] = true
			}
		}
		if f.analyzer == nil {
			continue
		}
		result.OwnedLines += len(f.owned)
		result.AILines += len(f.ai)
		ai[f.file.Path] = f.ai
		for _, commit := range f.owned {
			if t := tools[commit]; len(t) > 0 {
				result.MarkedLines++
				for _, name := range t {
					result.MarkedByTool[name]++
				}
			}
		}
	}
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
	result.PerRepo = []RepoPractices{practicesOf(files, scripts, result.Languages, result.Tests)}
	for _, lr := range result.Languages {
		for _, fd := range lr.Findings {
			lines, code := ai[fd.Path]
			if code {
				result.Findings++
			}
			if lines[fd.Line] {
				result.AIFindings++
			}
		}
	}
	return result, nil
}

// run analyzes a group of files and keeps findings on owned lines.
func run(ctx context.Context, name string, group []ownedFile,
	analyze func(context.Context, []File) ([]Finding, error)) (LanguageResult, error) {
	lr := LanguageResult{Language: name, Files: len(group), AssistedByRule: map[string]int{}}
	owned := map[string]map[int]string{}
	assisted := map[string]map[int]bool{}
	inputs := make([]File, 0, len(group))
	for _, f := range group {
		inputs = append(inputs, f.file)
		owned[f.file.Path] = f.owned
		assisted[f.file.Path] = f.assisted
		lr.OwnedLines += len(f.owned)
		lr.AssistedLines += len(f.assisted)
		if IsTestPath(f.file.Path) {
			lr.TestLines += len(f.owned)
		}
	}
	findings, err := analyze(ctx, inputs)
	if err != nil {
		return lr, fmt.Errorf("%s analyzer: %w", name, err)
	}
	for _, fd := range findings {
		if owned[fd.Path][fd.Line] != "" {
			lr.Findings = append(lr.Findings, fd)
			if assisted[fd.Path][fd.Line] {
				lr.AssistedFindings++
				lr.AssistedByRule[fd.Rule]++
			}
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
			of := ownedFile{file: f, owned: owned, text: len(content) <= maxScanBytes,
				ai: agentWritten(content, owned, opts.AgentLines[p])}
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

// agentWritten returns the owned lines whose normalized text an agent wrote.
func agentWritten(content []byte, owned map[int]string, agent map[string]bool) map[int]bool {
	ai := map[int]bool{}
	if len(agent) == 0 {
		return ai
	}
	for i, line := range strings.Split(string(content), "\n") {
		if owned[i+1] != "" && agent[strings.TrimSpace(line)] {
			ai[i+1] = true
		}
	}
	return ai
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
