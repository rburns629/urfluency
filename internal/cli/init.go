package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rburns629/urfluency/internal/identity"
)

// stdin and interactive are variables so tests can answer prompts.
var (
	stdin       io.Reader = os.Stdin
	interactive           = func() bool {
		info, err := os.Stdin.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
)

// initSettings detects the user's addresses in the repositories under a path,
// asks about addresses that only look like theirs, and saves the answers so
// later scans start from them. Running it again adds to what is saved.
func initSettings(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var authors stringList
	fs.Var(&authors, "author", "")
	yes := fs.Bool("yes", false, "")
	offline := fs.Bool("offline", false, "")
	online := fs.Bool("online", false, "")
	var include stringList
	fs.Var(&include, "include-ext", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("%w: init takes at most one path", errUsage)
	}
	path := "."
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	saved, err := loadSettings()
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	prog := newProgress(stderr)
	prog.phase("Finding your commits under " + tildePath(root))
	repos, _, err := findProjects(root, nil)
	if err != nil {
		return err
	}
	prog.clear()

	seeds := append(slices.Clone(saved.Emails), authors...)
	id := withoutDeclined(detectIdentity(ctx, repos, seeds), saved.NotMine)
	notMine := slices.Clone(saved.NotMine)
	if len(id.Possible) > 0 && interactive() && !*yes {
		fmt.Fprintln(stdout, describeIdentity(identity.Identity{Emails: id.Emails, Commits: id.Commits}, len(repos)))
		in := bufio.NewScanner(stdin)
		var claimed []string
		for _, p := range id.Possible {
			fmt.Fprintf(stdout, "Is %s yours? (%s %s in %d %s) [y/N] ", p.Email, commas(p.Commits), plural(p.Commits, "commit", "commits"),
				p.Repos, plural(p.Repos, "repository", "repositories"))
			if !in.Scan() {
				break
			}
			if answer := strings.ToLower(strings.TrimSpace(in.Text())); answer == "y" || answer == "yes" {
				claimed = append(claimed, p.Email)
			} else {
				notMine = append(notMine, p.Email)
			}
		}
		// A claimed address can link further names and addresses.
		id = withoutDeclined(detectIdentity(ctx, repos, append(seeds, claimed...)), notMine)
	}
	fmt.Fprintln(stdout, describeIdentity(id, len(repos)))
	if len(id.Emails) == 0 {
		return fmt.Errorf("%w: could not tell which commits are yours; set git user.name or pass --author", errUsage)
	}
	exts := append(slices.Clone(saved.Extensions), extensions(include)...)
	if *offline && *online {
		return fmt.Errorf("%w: --offline and --online cannot be used together", errUsage)
	}
	mode := saved.Offline
	if *offline || *online {
		mode = *offline
	}
	where, err := settings{Emails: append(saved.Emails, id.Emails...), NotMine: notMine, Extensions: exts, Dismissed: saved.Dismissed, Offline: mode}.save()
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	fmt.Fprintf(stdout, "Saved to %s. Scans start from these addresses and still find new ones; run init again to update.\n", tildePath(where))
	switch {
	case *offline:
		fmt.Fprintln(stdout, "Offline by default: dependency checks use the downloaded databases (urfluency vulns update) and nothing is sent. Pass --online to a scan to check live.")
	case *online:
		fmt.Fprintln(stdout, "Dependency checks are live again by default: package names and versions are sent to osv.dev.")
	}
	return nil
}

// withoutDeclined drops possible addresses the user said are not theirs.
func withoutDeclined(id identity.Identity, notMine []string) identity.Identity {
	id.Possible = slices.DeleteFunc(id.Possible, func(p identity.Possible) bool { return slices.Contains(notMine, p.Email) })
	return id
}
