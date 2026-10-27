// Package cli implements the urfluency command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/internal/sessions"

	// Registered analyzers.
	_ "github.com/rburns629/urfluency/analyzers/golang"
	_ "github.com/rburns629/urfluency/analyzers/javascript"
	_ "github.com/rburns629/urfluency/analyzers/python"
	_ "github.com/rburns629/urfluency/analyzers/secrets"
)

// Version is set at build time with -ldflags "-X .../internal/cli.Version=...".
var Version = "dev"

const usage = `urfluency analyzes your own commits and reports strengths and areas to improve.

Usage:
  urfluency scan [flags] [path]   Analyze the repository at path, or every project
                                  below it, with or without Git (default ".")
  urfluency validate [path]       For maintainers: show whether prompt signals
                                  predict what happened next in agent sessions
  urfluency doctor                Check dependencies and optional features; installs nothing
  urfluency version               Print the version

Scan flags:
  --author EMAIL   Your commit email; repeat for several (default: git user.email)
  --since WHEN     Only lines authored since WHEN: a duration like 90d, a date
                   like 2026-01-31, or all (default 365d)
  --json           Print the report as JSON
  --locations      Include file:line for each finding (local only, never uploaded)
  --sessions       Also grade your prompts in local Claude Code and Codex sessions
                   for these repositories, and report how much code agents wrote.
                   Prompt text is read in memory only; nothing is stored.
  --exclude GLOB   Skip folders whose name or relative path matches GLOB; repeat
                   for several (for example --exclude 'Archive*')
`

// errUsage marks errors caused by invalid arguments.
var errUsage = errors.New("usage")

// Run executes the command line and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "scan":
		err = scan(ctx, args[1:], stdout, stderr)
	case "validate":
		err = validate(ctx, args[1:], stdout)
	case "doctor":
		err = doctor(ctx, stdout)
	case "version":
		_, err = fmt.Fprintln(stdout, "urfluency", Version)
	case "help", "-h", "--help":
		_, err = fmt.Fprint(stdout, usage)
	default:
		err = fmt.Errorf("%w: unknown command %q", errUsage, args[0])
	}
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "urfluency:", err)
	if errors.Is(err, errUsage) {
		fmt.Fprint(stderr, "\n", usage)
		return 2
	}
	return 1
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func scan(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var authors stringList
	fs.Var(&authors, "author", "")
	sinceFlag := fs.String("since", "365d", "")
	asJSON := fs.Bool("json", false, "")
	locations := fs.Bool("locations", false, "")
	withSessions := fs.Bool("sessions", false, "")
	var excludes stringList
	fs.Var(&excludes, "exclude", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("%w: scan takes at most one path", errUsage)
	}
	path := "."
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	since, err := parseSince(*sinceFlag, time.Now())
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	prog := newProgress(stderr)
	for _, pattern := range excludes {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("%w: --exclude %q: %v", errUsage, pattern, err)
		}
	}
	prog.phase("Finding projects under " + tildePath(abs))
	if top := (gitdata.Repo{Dir: abs}).TopLevel(ctx); top != "" && !isRepo(abs) {
		abs = top // a folder inside a repository is scanned as that repository
	}
	repos, folders, err := findProjects(abs, excludes)
	if err != nil {
		return err
	}
	if len(repos) == 0 && len(folders) == 0 {
		return fmt.Errorf("no projects found at %s", abs)
	}
	if len(authors) == 0 && len(repos) > 0 {
		email, err := gitdata.Repo{Dir: repos[0]}.ConfiguredEmail(ctx)
		if err != nil || email == "" {
			return fmt.Errorf("%w: no --author given and git user.email is not set", errUsage)
		}
		authors = append(authors, email)
	}
	var graded []sessions.Session
	agentLines := map[string]map[string]map[string]bool{}
	if *withSessions {
		prog.phase("Reading agent sessions")
		if graded, agentLines, err = loadSessions(abs, repos, since); err != nil {
			return err
		}
	}
	// In a folder scan, a repository that fails (for example one with no commits)
	// is reported and skipped; a single-repository scan fails.
	// Folders without Git are scanned and merged separately: their authorship is
	// declared, not recorded, so it never mixes with attributed results.
	total := len(repos) + len(folders)
	var labels, folderLabels []string
	var parts, folderParts []analysis.Result
	for i, dir := range append(repos, folders...) {
		label, _ := filepath.Rel(abs, dir)
		label = filepath.ToSlash(label)
		shown := label
		if label == "." {
			shown = filepath.Base(dir)
		}
		git := i < len(repos)
		if !git {
			shown += " (no Git)"
		}
		prog.repo(i+1, total, shown)
		opts := analysis.Options{Emails: authors, Since: since, AgentLines: agentLines[dir], Progress: prog.update,
			SkipDir: func(d string) bool { return isRepo(d) || excluded(abs, d, excludes) }}
		var part analysis.Result
		if git {
			part, err = analysis.Scan(ctx, gitdata.Repo{Dir: dir}, opts)
			if errors.Is(err, gitdata.ErrNoCommits) {
				git = false // nothing committed yet, so there is no history to attribute
			}
		}
		if !git {
			part, err = analysis.ScanDir(ctx, dir, opts)
		}
		if err != nil {
			if total == 1 || ctx.Err() != nil {
				prog.clear()
				return err
			}
			prog.note(fmt.Sprintf("urfluency: skipping %s: %v", label, err))
			continue
		}
		if git {
			labels, parts = append(labels, label), append(parts, part)
		} else {
			folderLabels, folderParts = append(folderLabels, label), append(folderParts, part)
		}
	}
	prog.finish(len(parts) + len(folderParts))
	if len(parts)+len(folderParts) == 0 {
		return fmt.Errorf("no project under %s could be scanned", abs)
	}
	var res analysis.Result
	if len(parts) > 0 {
		res = analysis.Merge(labels, parts)
	}
	r := report.Build(res, since, *locations)
	if len(folderParts) > 0 {
		r.Unversioned = report.BuildUnversioned(analysis.Merge(folderLabels, folderParts), *locations)
	}
	r.AI = report.BuildAI(res, *withSessions)
	if *withSessions {
		r.Prompting = report.BuildPrompting(sessions.Grade(graded))
	}
	r.AIEngineering = report.BuildAIEngineering(r)
	r.Practices = report.BuildPractices(res)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	return report.WriteText(stdout, r)
}

// findProjects returns root if it is a Git repository; otherwise the repositories
// and the project folders without Git up to four levels below it. A folder without
// Git is a project when it holds a manifest, or source files and no other
// projects (a folder of projects with a stray script is not one). Folders inside
// a project are part of it, except nested repositories, which are scanned on
// their own. Hidden, dependency, secret, and excluded directories are skipped.
func findProjects(root string, excludes []string) (repos, folders []string, err error) {
	if isRepo(root) {
		return []string{root}, nil, nil
	}
	type candidate struct {
		dir      string
		manifest bool
	}
	var candidates []candidate
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil // unreadable entries are skipped
		}
		name := d.Name()
		if p != root && (strings.HasPrefix(name, ".") || analysis.IsDependencyDir(name) || analysis.IsProtectedDir(name) || excluded(root, p, excludes)) {
			return filepath.SkipDir
		}
		if isRepo(p) {
			repos = append(repos, p)
			return filepath.SkipDir
		}
		if manifest, source := analysis.ProjectFiles(p); manifest || source {
			candidates = append(candidates, candidate{p, manifest})
		}
		if rel, _ := filepath.Rel(root, p); strings.Count(rel, string(filepath.Separator)) >= 3 {
			return filepath.SkipDir
		}
		return nil
	})
	// Candidates come in walk order, so a folder's parent project, if any, is
	// the last one kept.
	contains := func(dir string) bool {
		for _, r := range repos {
			if within(dir, r) {
				return true
			}
		}
		for _, c := range candidates {
			if c.dir != dir && within(dir, c.dir) {
				return true
			}
		}
		return false
	}
	for _, c := range candidates {
		if len(folders) > 0 && within(folders[len(folders)-1], c.dir) {
			continue
		}
		if c.manifest || !contains(c.dir) {
			folders = append(folders, c.dir)
		}
	}
	return repos, folders, err
}

// excluded reports whether --exclude matches the directory's name or its path
// relative to root.
func excluded(root, dir string, patterns []string) bool {
	rel, _ := filepath.Rel(root, dir)
	rel = filepath.ToSlash(rel)
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, filepath.Base(dir)); ok {
			return true
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
	}
	return false
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// loadSessions reads transcripts since the given time and keeps the sessions that
// ran in or edited the scanned repositories, plus the lines agents wrote in each
// repository keyed by repository-relative path. Unreadable transcripts are skipped.
func loadSessions(root string, repos []string, since time.Time) ([]sessions.Session, map[string]map[string]map[string]bool, error) {
	dirs, err := sessions.DefaultDirs()
	if err != nil {
		return nil, nil, err
	}
	transcripts, err := sessions.Discover(dirs, since)
	if err != nil {
		return nil, nil, err
	}
	var kept []sessions.Session
	lines := map[string]map[string]map[string]bool{}
	for _, t := range transcripts {
		s, err := sessions.Load(t)
		if err != nil || s.Start.Before(since) {
			continue
		}
		used := within(root, s.Cwd)
		for path, added := range s.Edits {
			for _, repo := range repos {
				rel, ok := relative(repo, path)
				if !ok {
					continue
				}
				used = true
				if lines[repo] == nil {
					lines[repo] = map[string]map[string]bool{}
				}
				if lines[repo][rel] == nil {
					lines[repo][rel] = map[string]bool{}
				}
				for l := range added {
					lines[repo][rel][l] = true
				}
			}
		}
		if used {
			kept = append(kept, s)
		}
	}
	return kept, lines, nil
}

func within(root, path string) bool {
	_, ok := relative(root, path)
	return ok
}

// relative returns path relative to root, slash-separated, if path is inside root.
func relative(root, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// parseSince accepts "all", "<n>d", or a YYYY-MM-DD date.
func parseSince(v string, now time.Time) (time.Time, error) {
	if v == "all" {
		return time.Time{}, nil
	}
	if strings.HasSuffix(v, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err != nil || days <= 0 {
			return time.Time{}, fmt.Errorf("invalid --since %q", v)
		}
		return now.AddDate(0, 0, -days), nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q: use 90d or 2026-01-31", v)
	}
	return t, nil
}
