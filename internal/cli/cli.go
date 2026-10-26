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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/gitdata"
	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/internal/sessions"

	// Registered analyzers.
	_ "github.com/rburns629/urfluency/internal/analyzers/golang"
	_ "github.com/rburns629/urfluency/internal/analyzers/javascript"
	_ "github.com/rburns629/urfluency/internal/analyzers/python"
	_ "github.com/rburns629/urfluency/internal/analyzers/secrets"
)

// Version is set at build time with -ldflags "-X .../internal/cli.Version=...".
var Version = "dev"

const usage = `urfluency analyzes your own commits and reports strengths and areas to improve.

Usage:
  urfluency scan [flags] [path]   Analyze the repository at path, or every repository
                                  below it (default ".")
  urfluency validate [path]       For maintainers: show whether prompt signals
                                  predict what happened next in agent sessions
  urfluency version               Print the version

Scan flags:
  --author EMAIL   Your commit email; repeat for several (default: git user.email)
  --since WHEN     Only lines authored since WHEN: a duration like 90d or a date
                   like 2026-01-31 (default 365d)
  --json           Print the report as JSON
  --locations      Include file:line for each finding (local only, never uploaded)
  --sessions       Also grade your prompts in local Claude Code and Codex sessions
                   for these repositories, and report how much code agents wrote.
                   Prompt text is read in memory only; nothing is stored.
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
	repos, err := findRepos(abs)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("no Git repositories found at %s", abs)
	}
	if len(authors) == 0 {
		email, err := gitdata.Repo{Dir: repos[0]}.ConfiguredEmail(ctx)
		if err != nil || email == "" {
			return fmt.Errorf("%w: no --author given and git user.email is not set", errUsage)
		}
		authors = append(authors, email)
	}
	var graded []sessions.Session
	agentLines := map[string]map[string]map[string]bool{}
	if *withSessions {
		if graded, agentLines, err = loadSessions(abs, repos, since); err != nil {
			return err
		}
	}
	// In a folder scan, a repository that fails (for example one with no commits)
	// is reported and skipped; a single-repository scan fails.
	var labels []string
	var parts []analysis.Result
	for _, dir := range repos {
		label, _ := filepath.Rel(abs, dir)
		label = filepath.ToSlash(label)
		part, err := analysis.Scan(ctx, gitdata.Repo{Dir: dir},
			analysis.Options{Emails: authors, Since: since, AgentLines: agentLines[dir]})
		if err != nil {
			if len(repos) == 1 || ctx.Err() != nil {
				return err
			}
			fmt.Fprintf(stderr, "urfluency: skipping %s: %v\n", label, err)
			continue
		}
		labels, parts = append(labels, label), append(parts, part)
	}
	if len(parts) == 0 {
		return fmt.Errorf("no repository under %s could be scanned", abs)
	}
	res := analysis.Merge(labels, parts)
	r := report.Build(res, since, *locations)
	if *withSessions {
		r.Prompting = report.BuildPrompting(sessions.Grade(graded))
		r.AI = report.BuildAI(res)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	return report.WriteText(stdout, r)
}

// findRepos returns root if it is a Git repository, otherwise the repositories up
// to four levels below it. Hidden, dependency, and secret directories are skipped.
func findRepos(root string) ([]string, error) {
	if isRepo(root) {
		return []string{root}, nil
	}
	var repos []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil // unreadable entries are skipped
		}
		name := d.Name()
		if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || analysis.IsProtectedDir(name)) {
			return filepath.SkipDir
		}
		if isRepo(p) {
			repos = append(repos, p)
			return filepath.SkipDir
		}
		if rel, _ := filepath.Rel(root, p); strings.Count(rel, string(filepath.Separator)) >= 3 {
			return filepath.SkipDir
		}
		return nil
	})
	return repos, err
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

// parseSince accepts "<n>d" or a YYYY-MM-DD date.
func parseSince(v string, now time.Time) (time.Time, error) {
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
