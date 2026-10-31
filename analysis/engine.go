package analysis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

// dependencyDirs hold installed packages, environments, and package caches, not
// the user's own work. Ambiguous names such as packages/ (monorepo source),
// build/, and target/ are deliberately absent.
var dependencyDirs = map[string]bool{
	"node_modules": true, "bower_components": true, "jspm_packages": true, ".yarn": true, ".pnpm-store": true,
	".venv": true, "venv": true, "site-packages": true, "__pycache__": true, ".tox": true, ".eggs": true,
	"vendor": true, "third_party": true,
	"Pods": true, "Carthage": true,
	".gradle": true, ".m2": true,
	".dart_tool": true, ".pub-cache": true,
	".bundle": true, "elm-stuff": true, ".terraform": true,
}

// IsDependencyDir reports whether a directory name holds installed dependencies.
func IsDependencyDir(name string) bool { return dependencyDirs[name] }

// skippedDirs are other path segments whose files are not the user's own work.
var skippedDirs = map[string]bool{"testdata": true, ".git": true}

// maxScanBytes bounds the files scanned at all.
const maxScanBytes = 2 << 20

// BlameTimeout bounds attributing one file's lines; a file that takes longer is
// skipped and counted rather than stalling the scan.
var BlameTimeout = 60 * time.Second

// Options selects whose work to analyze.
type Options struct {
	Emails []string
	Since  time.Time
	// AgentLines maps repository-relative paths to normalized lines an agent wrote,
	// for attributing owned lines to AI.
	AgentLines map[string]map[string]bool
	// Progress, if set, receives the current stage and, when known, how many of
	// its steps are done. It is called from several goroutines and must be safe
	// for concurrent use.
	Progress func(stage string, done, total int)
	// Extensions are extra file extensions to read and check for secrets, such as
	// ".tf" or ".yml". Files of supported languages are always read; others are
	// ignored unless listed here.
	Extensions []string
	// SkipDir, if set, names absolute directories a folder scan without Git
	// leaves out, such as nested repositories scanned on their own.
	SkipDir func(dir string) bool
	// Dismissed holds DismissalKeys of findings the user dismissed; they are
	// left out of every count and score, and counted in Result.Dismissed.
	Dismissed map[string]bool

	// present, set by Scan, reports whether a committed line is still in the
	// working copy's version of a file. A secret found at HEAD counts only
	// while it is: one removed locally is no longer in the user's files,
	// even if the change was never committed.
	present func(path string, line []byte) bool
}

// wanted reports whether a file is read as code: a supported language, or an
// extension the user included.
func (o Options) wanted(p string) bool {
	if ForPath(p) != nil {
		return true
	}
	ext := strings.ToLower(path.Ext(p))
	return ext != "" && slices.Contains(o.Extensions, ext)
}

func (o Options) progress(stage string, done, total int) {
	if o.Progress != nil {
		o.Progress(stage, done, total)
	}
}

// LanguageResult holds findings on the user's owned lines for one language or
// scanner.
type LanguageResult struct {
	Language   string
	Frameworks []string
	// FrameworkUse counts repositories and owned files per framework.
	FrameworkUse map[string]Usage
	Files        int
	OwnedLines   int
	Findings     []Finding
	// AssistedLines are owned lines an agent wrote in local sessions or that
	// come from commits marked AI-assisted; AssistedFindings are findings on them.
	AssistedLines    int
	AssistedFindings int
	AssistedByRule   map[string]int // findings on AI-assisted lines, per rule
	TestLines        int            // owned lines in test files, included in OwnedLines
	// Idioms counts positive findings on owned lines, per rule.
	Idioms map[string]IdiomUse
}

// IdiomUse is how often, and in how many files, owned code used an idiom.
type IdiomUse struct {
	Uses  int
	Files int
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
	Written      Written
	Tests        TestHabits
	PerRepo      []RepoPractices
	// SkippedSlow counts files skipped because attributing them took longer
	// than BlameTimeout.
	SkippedSlow int
	// Dismissed counts findings left out because the user dismissed them.
	Dismissed int
	// Declared is true for folders without Git history, where every line counts
	// as the user's because nothing records who wrote it.
	Declared bool
	// Tools is how much the user worked with each tool; Git is how they write
	// commits (empty without history).
	Tools map[string]ToolUse
	Git   GitHabits
	// Design is how much owned code worked in each design area, from imports.
	Design map[string]DesignUse
	// Platforms is how much the user worked with each platform, from SDK
	// imports and deploy and infrastructure files.
	Platforms map[string]PlatformUse
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
	// Files are read through one git process rather than one per file.
	blobs, err := repo.OpenBlobs(ctx)
	if err != nil {
		return Result{}, err
	}
	defer blobs.Close()
	show := func(p string) ([]byte, error) { return blobs.Show(head, p) }
	// Files over maxScanBytes are vendored or generated, and blaming them can take
	// minutes, so they are excluded before any Git command reads them.
	paths, err := repo.TrackedFiles(ctx, maxScanBytes)
	if err != nil {
		return Result{}, err
	}
	// Only files the authors ever committed to can hold their lines, and blame is
	// most of a scan's cost, so other files are never blamed. (Code someone else
	// moved into a file the authors never touched is the one case this misses.)
	touched, err := repo.TouchedPaths(ctx, opts.Emails)
	if err != nil {
		return Result{}, err
	}
	// Protected files are dropped here, before blame or show, so their contents are
	// never read.
	var candidates []string
	for _, p := range paths {
		if touched[p] && !inSkippedDir(p) && !IsProtected(p) && (opts.wanted(p) || readEvenWithoutAnalyzer(p)) {
			candidates = append(candidates, p)
		}
	}
	generated := scaffolding(paths, func(p string) []byte {
		b, _ := show(p)
		return b
	})
	kept := candidates[:0]
	for _, p := range candidates {
		if !generated[p] {
			kept = append(kept, p)
		}
	}
	candidates = kept
	if candidates, err = dropVendored(ctx, repo, candidates); err != nil {
		return Result{}, err
	}

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
	blame, err := needsBlame(ctx, repo, opts, tools)
	if err != nil {
		return Result{}, err
	}

	files, slow, err := loadOwned(ctx, repo, head, candidates, blame, show, opts)
	if err != nil {
		return Result{}, err
	}

	result := Result{Head: head, Repos: 1, MarkedByTool: map[string]int{}, SkippedSlow: slow}
	result.Tools = toolUsage(added, commits, opts.Since)
	result.Platforms = platformConfig(added, commits, opts.Since, show)
	result.Git = gitHabits(commits, opts.Since)
	excluded, err := notUserCode(ctx, repo, added, files)
	if err != nil {
		return Result{}, err
	}
	if result.Durability, err = durability(ctx, repo, added, commits, tools, touched, excluded, opts.Since, time.Now(), opts.progress); err != nil {
		return Result{}, err
	}
	// A file still at HEAD counts only if the scan kept it as the user's code,
	// so scaffolding, vendored, and oversized files are not written lines
	// either. paths leaves oversized files out, so every tracked file is listed.
	all, err := repo.TrackedFiles(ctx, 0)
	if err != nil {
		return Result{}, err
	}
	atHead, scanned := map[string]bool{}, map[string]bool{}
	for _, p := range all {
		atHead[p] = true
	}
	for _, p := range candidates {
		scanned[p] = true
	}
	result.Written = written(added, commits, tools, func(p string) bool { return excluded(p) || atHead[p] && !scanned[p] }, opts.Since)
	result.Tests = testHabits(added, commits, tools, opts.Since)
	var scripts []string
	for _, p := range paths {
		for _, s := range buildScriptPaths {
			if p == s {
				if content, err := show(p); err == nil {
					scripts = append(scripts, string(content))
				}
			}
		}
	}
	opts.present = workingCopy(repo.Dir)
	when := func(commit string) time.Time { return commits[commit].Time }
	if err := analyzeFiles(ctx, &result, files, tools, when, scripts, opts); err != nil {
		return Result{}, err
	}
	return result, nil
}

// notUserCode returns which paths in the history the scan leaves out of the
// user's code: files skipped at HEAD (generated, bundled, or with a library
// banner), files ruled out by path, and files .gitattributes marks as vendored or
// generated, including ones deleted since.
func notUserCode(ctx context.Context, repo gitdata.Repo, added map[string]map[string]int, files []ownedFile) (func(string) bool, error) {
	skippedAtHead := map[string]bool{}
	for _, f := range files {
		if f.analyzer == nil && ForPath(f.file.Path) != nil {
			skippedAtHead[f.file.Path] = true
		}
	}
	seen := map[string]bool{}
	var paths []string
	for _, perFile := range added {
		for p := range perFile {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	vendored, err := repo.Vendored(ctx, paths)
	if err != nil {
		return nil, err
	}
	return func(p string) bool { return skippedAtHead[p] || vendored[p] || skipsPath(p) }, nil
}

// dropVendored removes the paths .gitattributes marks as vendored or generated.
func dropVendored(ctx context.Context, repo gitdata.Repo, paths []string) ([]string, error) {
	vendored, err := repo.Vendored(ctx, paths)
	if err != nil {
		return nil, err
	}
	kept := paths[:0]
	for _, p := range paths {
		if !vendored[p] {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

// analyzeFiles runs every analyzer and scanner over the loaded files and fills in
// the result's languages, AI-assisted counts, and per-repository practices. tools
// maps commits to the coding tools their trailers name, and when gives a
// commit's time; both are empty for folders without Git.
func analyzeFiles(ctx context.Context, result *Result, files []ownedFile, tools map[string][]string, when func(string) time.Time, scripts []string, opts Options) error {
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
	if result.Platforms == nil {
		result.Platforms = map[string]PlatformUse{}
	}
	result.Design = designUsage(files, result.Platforms, when)
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
		opts.progress("analyzing "+a.Language(), 0, 0)
		lr, dismissed, err := run(ctx, a.Language(), group, a.Analyze, opts)
		result.Dismissed += dismissed
		if err != nil {
			return err
		}
		if fd, ok := a.(FrameworkDetector); ok {
			lr.Frameworks, lr.FrameworkUse = frameworks(fd, group)
		}
		result.Languages = append(result.Languages, lr)
	}
	for _, s := range Scanners() {
		var group []ownedFile
		for _, f := range files {
			if f.text && opts.wanted(f.file.Path) && s.Applies(f.file.Path) {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			continue
		}
		lr, dismissed, err := run(ctx, s.Name(), group, s.Analyze, opts)
		result.Dismissed += dismissed
		if err != nil {
			return err
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
	return nil
}

// run analyzes a group of files and keeps findings on owned lines, except ones
// the user dismissed, which it counts, and secrets no longer in the working
// copy.
func run(ctx context.Context, name string, group []ownedFile,
	analyze func(context.Context, []File) ([]Finding, error), opts Options) (LanguageResult, int, error) {
	lr := LanguageResult{Language: name, Files: len(group), AssistedByRule: map[string]int{}, Idioms: map[string]IdiomUse{}}
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
		return lr, 0, fmt.Errorf("%s analyzer: %w", name, err)
	}
	content := map[string][]byte{}
	for _, f := range group {
		content[f.file.Path] = f.file.Content
	}
	lineOf := func(path string, n int) []byte {
		lines := bytes.Split(content[path], []byte("\n"))
		if n < 1 || n > len(lines) {
			return nil
		}
		return lines[n-1]
	}
	skipped := 0
	idiomFiles := map[string]map[string]bool{}
	for _, fd := range findings {
		if owned[fd.Path][fd.Line] == "" {
			continue
		}
		if !fd.Positive && len(opts.Dismissed) > 0 && opts.Dismissed[DismissalKey(fd.Rule, lineOf(fd.Path, fd.Line))] {
			skipped++
			continue
		}
		if opts.present != nil && strings.HasPrefix(fd.Rule, "sec.") && !opts.present(fd.Path, lineOf(fd.Path, fd.Line)) {
			continue
		}
		if fd.Positive {
			u := lr.Idioms[fd.Rule]
			u.Uses++
			if idiomFiles[fd.Rule] == nil {
				idiomFiles[fd.Rule] = map[string]bool{}
			}
			if !idiomFiles[fd.Rule][fd.Path] {
				idiomFiles[fd.Rule][fd.Path] = true
				u.Files++
			}
			lr.Idioms[fd.Rule] = u
			continue
		}
		lr.Findings = append(lr.Findings, fd)
		if assisted[fd.Path][fd.Line] {
			lr.AssistedFindings++
			lr.AssistedByRule[fd.Rule]++
		}
	}
	sort.Slice(lr.Findings, func(i, j int) bool {
		if lr.Findings[i].Path != lr.Findings[j].Path {
			return lr.Findings[i].Path < lr.Findings[j].Path
		}
		return lr.Findings[i].Line < lr.Findings[j].Line
	})
	return lr, skipped, nil
}

// workingCopy returns a check for whether a committed line is still in the
// working copy of a file under dir, anywhere in it. Each file is read once; a
// file deleted or unreadable has none of its lines.
func workingCopy(dir string) func(path string, line []byte) bool {
	var mu sync.Mutex
	files := map[string][]byte{}
	return func(path string, line []byte) bool {
		mu.Lock()
		content, ok := files[path]
		if !ok {
			content, _ = os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
			files[path] = content
		}
		mu.Unlock()
		line = bytes.TrimSpace(line)
		return len(line) > 0 && bytes.Contains(content, line)
	}
}

// frameworks lists the frameworks the files use, and how many files use each.
func frameworks(fd FrameworkDetector, group []ownedFile) ([]string, map[string]Usage) {
	use := map[string]Usage{}
	for _, f := range group {
		for _, fw := range fd.Frameworks(f.file) {
			use[fw] = Usage{Repos: 1, Files: use[fw].Files + 1}
		}
	}
	out := []string{}
	for fw := range use {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out, use
}

// loadOwned reads each candidate file and its owned lines with a bounded worker
// pool. Results are written by index, so no shared slice is appended concurrently.
// needsBlame returns which paths must be blamed, or nil for all of them. Scoring
// everyone's work in a shallow clone, every line counts (the clone's oldest
// commits are inside the window and carry all older lines), so blame only finds
// which lines came from AI-marked commits, and only files those commits touched,
// under their current names, can hold such lines. For big repositories that is
// a small share of the files: 135 of 31,639 in kubernetes, measured 2026-10-30.
// Lines that -C would trace to an AI-marked commit through a copy into an
// untouched file are missed.
func needsBlame(ctx context.Context, repo gitdata.Repo, opts Options, tools map[string][]string) (func(string) bool, error) {
	if !gitdata.IsEveryone(opts.Emails) {
		return nil, nil
	}
	shallow, err := repo.Shallow(ctx)
	if err != nil || !shallow {
		return nil, err
	}
	aiCommits := map[string]bool{}
	for hash := range tools {
		aiCommits[hash] = true
	}
	marked, err := repo.ChangedPaths(ctx, aiCommits)
	if err != nil {
		return nil, err
	}
	return func(p string) bool { return marked[p] }, nil
}

// everyLine maps each line of content to commit, as blame would for a file
// whose lines all come from that one commit.
func everyLine(content []byte, commit string) map[int]string {
	owned := map[int]string{}
	n := bytes.Count(content, []byte("\n"))
	if len(content) > 0 && content[len(content)-1] != '\n' {
		n++
	}
	for i := 1; i <= n; i++ {
		owned[i] = commit
	}
	return owned
}

// loadOwned blames each path, or only those blame accepts when it is not nil,
// and loads the files with owned lines through show.
func loadOwned(ctx context.Context, repo gitdata.Repo, head string, paths []string, blame func(string) bool, show func(string) ([]byte, error), opts Options) ([]ownedFile, int, error) {
	results := make([]ownedFile, len(paths))
	errs := make([]error, len(paths))
	limit := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	var done, slow atomic.Int64
	opts.progress("reading files", 0, len(paths))
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			defer func() { opts.progress("reading files", int(done.Add(1)), len(paths)) }()
			limit <- struct{}{}
			defer func() { <-limit }()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			var owned map[int]string
			if blame == nil || blame(p) {
				fileCtx, cancel := context.WithTimeout(ctx, BlameTimeout)
				var err error
				owned, err = repo.OwnedLines(fileCtx, head, p, opts.Emails, opts.Since)
				timedOut := errors.Is(fileCtx.Err(), context.DeadlineExceeded) // checked before cancel
				cancel()
				if err != nil {
					if timedOut && ctx.Err() == nil {
						slow.Add(1)
						return
					}
					errs[i] = fmt.Errorf("%s: %w", p, err)
					return
				}
				if len(owned) == 0 {
					return
				}
			}
			content, err := show(p)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", p, err)
				return
			}
			if owned == nil {
				owned = everyLine(content, "")
				if len(owned) == 0 {
					return
				}
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
			return nil, 0, err
		}
	}
	return results, int(slow.Load()), nil
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
		if dependencyDirs[segment] || skippedDirs[segment] {
			return true
		}
	}
	return false
}
