// Package cli implements the urfluency command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/gitdata"
	"github.com/rburns629/urfluency/internal/report"

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
  urfluency scan [flags] [path]   Analyze the repository at path (default ".")
  urfluency version               Print the version

Scan flags:
  --author EMAIL   Your commit email; repeat for several (default: git user.email)
  --since WHEN     Only lines authored since WHEN: a duration like 90d or a date
                   like 2026-01-31 (default 365d)
  --json           Print the report as JSON
  --locations      Include file:line for each finding (local only, never uploaded)
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
		err = scan(ctx, args[1:], stdout)
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

func scan(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var authors stringList
	fs.Var(&authors, "author", "")
	sinceFlag := fs.String("since", "365d", "")
	asJSON := fs.Bool("json", false, "")
	locations := fs.Bool("locations", false, "")
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
	repo := gitdata.Repo{Dir: abs}
	if len(authors) == 0 {
		email, err := repo.ConfiguredEmail(ctx)
		if err != nil || email == "" {
			return fmt.Errorf("%w: no --author given and git user.email is not set", errUsage)
		}
		authors = append(authors, email)
	}
	res, err := analysis.Scan(ctx, repo, analysis.Options{Emails: authors, Since: since})
	if err != nil {
		return err
	}
	r := report.Build(res, since, *locations)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	return report.WriteText(stdout, r)
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
