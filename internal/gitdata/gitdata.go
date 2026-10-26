// Package gitdata owns all Git access. Repositories are untrusted input: Git runs
// with hooks, fsmonitor, external diff, and text conversion disabled.
package gitdata

import (
	"bufio"
	"bytes"
	"context"
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
	out, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfiguredEmail returns user.email as Git resolves it for this repository.
func (r Repo) ConfiguredEmail(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "config", "user.email")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TrackedFiles lists the regular files committed at HEAD. Submodules, symlinks,
// and staged but uncommitted files are excluded.
func (r Repo) TrackedFiles(ctx context.Context) ([]string, error) {
	out, err := r.git(ctx, "ls-tree", "-r", "-z", "--full-tree", "HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range strings.Split(string(out), "\x00") {
		// "<mode> <type> <object>\t<path>"
		meta, path, ok := strings.Cut(entry, "\t")
		if ok && (strings.HasPrefix(meta, "100644 blob ") || strings.HasPrefix(meta, "100755 blob ")) {
			files = append(files, path)
		}
	}
	return files, nil
}

// Show returns a file's content at the given revision.
func (r Repo) Show(ctx context.Context, rev, path string) ([]byte, error) {
	return r.git(ctx, "show", "--no-textconv", rev+":"+path)
}

// OwnedLines returns the 1-based line numbers of path at rev whose blamed author
// email is in emails and whose author time is not before since.
func (r Repo) OwnedLines(ctx context.Context, rev, path string, emails []string, since time.Time) (map[int]bool, error) {
	out, err := r.git(ctx, "blame", "--line-porcelain", "-w", "-M", "-C", "--no-textconv", rev, "--", path)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, e := range emails {
		wanted[strings.ToLower(e)] = true
	}
	owned := map[int]bool{}
	var line int
	var email string
	var authored time.Time
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "\t"):
			if wanted[email] && !authored.Before(since) {
				owned[line] = true
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
				line = n
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read blame output: %w", err)
	}
	return owned, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
