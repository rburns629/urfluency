// Package gitdata owns all Git access. Repositories are untrusted input: Git runs
// with hooks, fsmonitor, external diff, and text conversion disabled.
package gitdata

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// safeConfig disables repository-controlled settings that can run commands.
var safeConfig = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "diff.external=",
	"-c", "core.pager=cat",
}

// Repo is a local Git working tree.
type Repo struct {
	Dir string
}

func (r Repo) git(ctx context.Context, args ...string) ([]byte, error) {
	full := append(append([]string{"-C", r.Dir}, safeConfig...), args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Head returns the commit the working tree is at.
func (r Repo) Head(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		// --verify --quiet exits 1 without output when HEAD names no commit.
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
			return "", ErrNoCommits
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ErrNoCommits is returned for a repository with no commits yet.
var ErrNoCommits = errors.New("no commits yet")

// TopLevel returns the root of the Git working tree containing the directory, or
// "" when it is not inside one.
func (r Repo) TopLevel(ctx context.Context) string {
	out, err := r.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ConfiguredName returns user.name as Git resolves it for this repository, or ""
// when it is not set.
func (r Repo) ConfiguredName(ctx context.Context) string {
	out, err := r.git(ctx, "config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Author is one name and email pair that authored commits.
type Author struct {
	Name    string
	Email   string
	Commits int
}

// Authors lists every author name and email pair on any branch, with how many
// commits each authored. Emails are lowercased.
func (r Repo) Authors(ctx context.Context) ([]Author, error) {
	out, err := r.git(ctx, "log", "--all", "--format=%an%x09%ae")
	if err != nil {
		return nil, err
	}
	counts := map[[2]string]int{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, email, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		counts[[2]string{name, strings.ToLower(email)}]++
	}
	var authors []Author
	for k, n := range counts {
		authors = append(authors, Author{Name: k[0], Email: k[1], Commits: n})
	}
	return authors, nil
}

// ConfiguredEmail returns user.email as Git resolves it for this repository.
func (r Repo) ConfiguredEmail(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "config", "user.email")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TrackedFiles lists the regular files committed at HEAD, excluding files larger
// than maxBytes when it is above zero. Submodules, symlinks, and staged but
// uncommitted files are excluded. Sizes come from Git's object database, so
// excluded files are never read.
func (r Repo) TrackedFiles(ctx context.Context, maxBytes int64) ([]string, error) {
	out, err := r.git(ctx, "ls-tree", "-r", "-z", "-l", "--full-tree", "HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range strings.Split(string(out), "\x00") {
		// "<mode> <type> <object> <size>\t<path>", the size padded with spaces
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			continue
		}
		if size, err := strconv.ParseInt(fields[3], 10, 64); err == nil && maxBytes > 0 && size > maxBytes {
			continue
		}
		files = append(files, path)
	}
	return files, nil
}

// Show returns a file's content at the given revision.
func (r Repo) Show(ctx context.Context, rev, path string) ([]byte, error) {
	return r.git(ctx, "show", "--no-textconv", rev+":"+path)
}

// OwnedLines maps the 1-based line numbers of path at rev whose blamed author
// email is in emails, and whose author time is not before since, to the commit
// that last changed them.
func (r Repo) OwnedLines(ctx context.Context, rev, path string, emails []string, since time.Time) (map[int]string, error) {
	out, err := r.git(ctx, "blame", "--line-porcelain", "-w", "-M", "-C", "--no-textconv", rev, "--", path)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, e := range emails {
		wanted[strings.ToLower(e)] = true
	}
	owned := map[int]string{}
	var line int
	var commit, email string
	var authored time.Time
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "\t"):
			if wanted[email] && !authored.Before(since) {
				owned[line] = commit
			}
		case strings.HasPrefix(text, "author-mail "):
			email = strings.ToLower(strings.Trim(strings.TrimPrefix(text, "author-mail "), "<>"))
		case strings.HasPrefix(text, "author-time "):
			secs, err := strconv.ParseInt(strings.TrimPrefix(text, "author-time "), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse blame author-time: %w", err)
			}
			authored = time.Unix(secs, 0)
		default:
			// Header lines start with a commit hash (SHA-1 or SHA-256), then the
			// original and final line numbers.
			fields := strings.Fields(text)
			if len(fields) >= 3 && (len(fields[0]) == 40 || len(fields[0]) == 64) && isHex(fields[0]) {
				n, err := strconv.Atoi(fields[2])
				if err != nil {
					return nil, fmt.Errorf("parse blame line number: %w", err)
				}
				commit, line = fields[0], n
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read blame output: %w", err)
	}
	return owned, nil
}

// authorArgs limits git log to commits whose author email is one of emails. The
// emails are matched as fixed strings with their angle brackets, so "+" in a
// GitHub noreply address is literal (Git's basic regular expressions read an
// escaped "\+" as "one or more") and "me@example.com" does not also match
// "nome@example.com". Case is ignored, as email addresses are case-insensitive.
func authorArgs(emails []string) []string {
	args := []string{"--fixed-strings", "--regexp-ignore-case"}
	for _, e := range emails {
		args = append(args, "--author=<"+e+">")
	}
	return args
}

// Commit is what urFluency reads about a commit: its author name and time, and the
// values of its Co-authored-by trailers.
type Commit struct {
	AuthorName string
	Time       time.Time
	CoAuthors  []string
}

// Commits returns the non-merge commits reachable from HEAD whose author email is
// one of emails, keyed by hash.
func (r Repo) Commits(ctx context.Context, emails []string) (map[string]Commit, error) {
	args := []string{"log", "--no-merges",
		"--format=%H%x1f%an%x1f%at%x1f%(trailers:key=Co-authored-by,valueonly,separator=%x1e)%x1d"}
	args = append(args, authorArgs(emails)...)
	out, err := r.git(ctx, append(args, "HEAD")...)
	if err != nil {
		return nil, err
	}
	commits := map[string]Commit{}
	for _, rec := range strings.Split(string(out), "\x1d") {
		fields := strings.SplitN(strings.TrimSpace(rec), "\x1f", 4)
		if len(fields) < 3 {
			continue
		}
		c := Commit{AuthorName: fields[1]}
		if secs, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			c.Time = time.Unix(secs, 0)
		}
		if len(fields) == 4 {
			for _, v := range strings.Split(fields[3], "\x1e") {
				if v = strings.TrimSpace(v); v != "" {
					c.CoAuthors = append(c.CoAuthors, v)
				}
			}
		}
		commits[fields[0]] = c
	}
	return commits, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
