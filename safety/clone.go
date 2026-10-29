package safety

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CloneLimits bound a clone of an untrusted repository.
type CloneLimits struct {
	Timeout  time.Duration
	MaxBytes int64 // checked-out size; larger repositories are refused
	// Since, if set, keeps the history back to that time, for scoring the
	// repository's practices; otherwise only the latest commit is fetched.
	Since time.Time
}

// DefaultCloneLimits suit public repositories people run tools from.
var DefaultCloneLimits = CloneLimits{Timeout: 2 * time.Minute, MaxBytes: 500 << 20}

// ErrTooLarge marks a repository over CloneLimits.MaxBytes.
var ErrTooLarge = errors.New("repository is too large to check")

// ValidateURL accepts an https URL of a repository, optionally only on the
// given hosts. Credentials, ports, queries, and fragments are refused.
func ValidateURL(raw string, hosts []string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errors.New("use an https URL, such as https://github.com/owner/repo")
	}
	if u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the URL must not include credentials, a port, a query, or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	if len(hosts) > 0 && !slices.Contains(hosts, host) {
		return "", fmt.Errorf("only repositories on %s can be checked", strings.Join(hosts, ", "))
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("the URL must name a repository, such as https://github.com/owner/repo")
	}
	for _, p := range parts {
		if p == "." || p == ".." || strings.HasPrefix(p, "-") {
			return "", errors.New("the URL path is not a repository")
		}
	}
	return "https://" + host + "/" + strings.Join(parts, "/"), nil
}

// gitConfig neutralizes everything a repository or the local machine could
// use to run code or leak credentials during a clone: hooks, symlinks,
// filesystem monitors, LFS and other filters, every transport but HTTPS,
// submodules, and credential helpers. fsckObjects has Git check every object
// it receives and reject malformed ones, its defense against objects crafted
// to exploit Git itself; the checks that only flag old, harmless mistakes in
// real repositories' histories are relaxed so those still clone.
var gitConfig = []string{
	"core.hooksPath=/dev/null",
	"core.symlinks=false",
	"core.fsmonitor=false",
	"core.protectNTFS=true",
	"protocol.allow=never",
	"protocol.https.allow=always",
	"transfer.fsckObjects=true",
	"fetch.fsck.zeroPaddedFilemode=ignore",
	"fetch.fsck.badTimezone=ignore",
	"fetch.fsck.missingSpaceBeforeDate=ignore",
	"submodule.recurse=false",
	"filter.lfs.smudge=",
	"filter.lfs.process=",
	"filter.lfs.required=false",
	"credential.helper=",
	"init.templateDir=",
}

// testGitConfig is added to gitConfig by tests, which serve repositories over
// plain HTTP from a local server.
var testGitConfig []string

// Clone shallow-clones a validated https URL into dir, which must not exist.
// The user's Git configuration is ignored, so no saved credentials or URL
// rewrites apply. Files are checked out only after their total size, read
// from the commit, is within limits.MaxBytes, so a small download that
// expands into a huge checkout is refused before anything is written.
func Clone(ctx context.Context, repoURL, dir string, limits CloneLimits) error {
	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	depth := "--depth=1"
	if !limits.Since.IsZero() {
		depth = "--shallow-since=" + limits.Since.UTC().Format(time.RFC3339)
	}
	err := clone(ctx, repoURL, dir, depth, limits.Timeout)
	if errors.Is(err, errNothingSince) {
		// Nothing was committed since then: the latest commit still has the code.
		err = clone(ctx, repoURL, dir, "--depth=1", limits.Timeout)
	}
	if err != nil {
		return err
	}
	size, err := checkoutSize(ctx, dir)
	if err != nil {
		return fmt.Errorf("read the commit: %w", err)
	}
	if size > limits.MaxBytes {
		return ErrTooLarge
	}
	if _, err := git(ctx, dir, "checkout", "--quiet"); err != nil {
		return fmt.Errorf("check out: %w", err)
	}
	return nil
}

// checkoutSize adds up the sizes of the files in the cloned commit, without
// checking them out.
func checkoutSize(ctx context.Context, dir string) (int64, error) {
	out, err := git(ctx, dir, "ls-tree", "-r", "-l", "-z", "HEAD")
	if err != nil {
		return 0, err
	}
	var total int64
	// Each entry is "<mode> <type> <object> <size>\t<path>"; submodules have
	// size "-".
	for entry := range strings.SplitSeq(string(out), "\x00") {
		meta, _, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 {
			continue
		}
		if n, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
			total += n
		}
	}
	return total, nil
}

// git runs a Git command in dir with the clone's safe configuration.
func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append(append(configArgs(), "-C", dir), args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("%s", firstLine(strings.TrimSpace(string(exit.Stderr))))
		}
		return nil, err
	}
	return out, nil
}

func configArgs() []string {
	var args []string
	for _, c := range append(append([]string{}, gitConfig...), testGitConfig...) {
		args = append(args, "-c", c)
	}
	return args
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/bin/false",
		"GIT_LFS_SKIP_SMUDGE=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
}

// errNothingSince is Git refusing --shallow-since when no commit is that recent.
var errNothingSince = errors.New("no commits in the history window")

func clone(ctx context.Context, repoURL, dir, depth string, timeout time.Duration) error {
	args := append(configArgs(), "clone", depth, "--no-checkout", "--single-branch", "--no-tags", "--no-recurse-submodules", "--quiet", "--", repoURL, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clone took longer than %s", timeout)
		}
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "no commits selected for shallow") || strings.Contains(msg, "error processing shallow info") {
			return errNothingSince
		}
		if strings.Contains(msg, "not found") || strings.Contains(msg, "Authentication failed") || strings.Contains(msg, "could not read Username") {
			return errors.New("repository not found or not public")
		}
		return fmt.Errorf("clone failed: %s", firstLine(msg))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
