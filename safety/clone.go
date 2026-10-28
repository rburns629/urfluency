package safety

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CloneLimits bound a clone of an untrusted repository.
type CloneLimits struct {
	Timeout  time.Duration
	MaxBytes int64 // checked-out size; larger repositories are refused
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
// filesystem monitors, LFS and other filters, local and ext transports,
// submodules, and credential helpers.
var gitConfig = []string{
	"core.hooksPath=/dev/null",
	"core.symlinks=false",
	"core.fsmonitor=false",
	"core.protectNTFS=true",
	"protocol.file.allow=never",
	"protocol.ext.allow=never",
	"submodule.recurse=false",
	"filter.lfs.smudge=",
	"filter.lfs.process=",
	"filter.lfs.required=false",
	"credential.helper=",
	"init.templateDir=",
}

// Clone shallow-clones a validated https URL into dir, which must not exist.
// The user's Git configuration is ignored, so no saved credentials or URL
// rewrites apply. The checkout is refused when it is over limits.MaxBytes.
func Clone(ctx context.Context, repoURL, dir string, limits CloneLimits) error {
	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	args := []string{}
	for _, c := range gitConfig {
		args = append(args, "-c", c)
	}
	args = append(args, "clone", "--depth=1", "--single-branch", "--no-tags", "--no-recurse-submodules", "--quiet", "--", repoURL, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/bin/false",
		"GIT_LFS_SKIP_SMUDGE=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clone took longer than %s", limits.Timeout)
		}
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "not found") || strings.Contains(msg, "Authentication failed") || strings.Contains(msg, "could not read Username") {
			return errors.New("repository not found or not public")
		}
		return fmt.Errorf("clone failed: %s", firstLine(msg))
	}
	var size int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		if size > limits.MaxBytes {
			return ErrTooLarge
		}
		return nil
	})
	return err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
