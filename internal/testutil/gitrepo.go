// Package testutil builds throwaway Git repositories for tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Repo is a temporary Git repository.
type Repo struct {
	T   *testing.T
	Dir string
}

// NewRepo initializes an empty repository in a temporary directory.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	r := &Repo{T: t, Dir: t.TempDir()}
	r.Git("", "", "init", "-q", "-b", "main")
	return r
}

// Git runs git with the given author identity and date (RFC 3339 or "" for now).
func (r *Repo) Git(email, date string, args ...string) {
	r.T.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL="+email)
	if date != "" {
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		r.T.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Commit writes files and commits them as the given author at the given date.
func (r *Repo) Commit(email, date string, files map[string]string) {
	r.T.Helper()
	for name, body := range files {
		path := filepath.Join(r.Dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.T.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			r.T.Fatal(err)
		}
	}
	r.Git(email, date, "add", "-A")
	r.Git(email, date, "commit", "-q", "-m", "change")
}
