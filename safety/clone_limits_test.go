package safety

import (
	"context"
	"errors"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests serve repositories from a local plain-HTTP server; production
// clones are HTTPS only.
func init() { testGitConfig = []string{"protocol.http.allow=always"} }

// serveRepo builds a bare repository with build and serves it over HTTP.
func serveRepo(t *testing.T, build func(git func(args ...string) string, work string)) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	backend := filepath.Join(strings.TrimSpace(gitOutput(t, "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend not available")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0o755)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	build(git, work)
	if out, err := exec.Command("git", "clone", "-q", "--bare", work, filepath.Join(root, "repo.git")).CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}
	srv := httptest.NewServer(&cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

func TestCloneRefusesALargeCheckoutBeforeWritingIt(t *testing.T) {
	url := serveRepo(t, func(git func(...string) string, work string) {
		// Zeros compress to almost nothing, as a checkout bomb would.
		os.WriteFile(filepath.Join(work, "big.bin"), make([]byte, 4<<20), 0o644)
		git("add", "-A")
		git("commit", "-q", "-m", "big")
	})
	dest := filepath.Join(t.TempDir(), "clone")
	limits := DefaultCloneLimits
	limits.MaxBytes = 1 << 20
	if err := Clone(context.Background(), url, dest, limits); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Clone: %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "big.bin")); err == nil {
		t.Error("the oversized file was written before the clone was refused")
	}
}

func TestCloneRejectsMalformedObjects(t *testing.T) {
	url := serveRepo(t, func(git func(...string) string, work string) {
		// A tree with an entry named .git, the shape of past Git exploits that
		// overwrite repository configuration on checkout; fsck rejects it.
		blob := git("hash-object", "-w", "--stdin")
		cmd := exec.Command("git", "mktree")
		cmd.Dir = work
		cmd.Stdin = strings.NewReader("100644 blob " + blob + "\t.git\n")
		out, err := cmd.Output()
		if err != nil {
			t.Skipf("this Git refuses to write the tree itself: %v", err)
		}
		commit := git("commit-tree", strings.TrimSpace(string(out)), "-m", "evil")
		git("update-ref", "refs/heads/main", commit)
	})
	dest := filepath.Join(t.TempDir(), "clone")
	err := Clone(context.Background(), url, dest, DefaultCloneLimits)
	if err == nil {
		t.Fatal("a repository with a .git tree entry was cloned")
	}
	if !strings.Contains(err.Error(), "fsck") && !strings.Contains(err.Error(), "hasDotgit") && !strings.Contains(err.Error(), "invalid object") {
		t.Errorf("refused for another reason than the object check: %v", err)
	}
}

func TestCloneRefusesPlainHTTPInProduction(t *testing.T) {
	url := serveRepo(t, func(git func(...string) string, work string) {
		os.WriteFile(filepath.Join(work, "a.txt"), []byte("a"), 0o644)
		git("add", "-A")
		git("commit", "-q", "-m", "a")
	})
	saved := testGitConfig
	testGitConfig = nil
	defer func() { testGitConfig = saved }()
	if err := Clone(context.Background(), url, filepath.Join(t.TempDir(), "clone"), DefaultCloneLimits); err == nil {
		t.Fatal("cloned over plain HTTP with the production configuration")
	}
}
