package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/sessions"
)

// localRuntime is a local model runtime the experimental reviews can use. Only
// loopback addresses are probed.
type localRuntime struct {
	name, url string
}

var localRuntimes = []localRuntime{
	{"Ollama", "http://127.0.0.1:11434/api/tags"},
	{"LM Studio", "http://127.0.0.1:1234/v1/models"},
}

// installHint is the official install command for a dependency on this system.
// urFluency never runs it.
func installHint(dep string) string {
	hints := map[string]map[string]string{
		"git": {
			"darwin":  "xcode-select --install   (or: brew install git)",
			"linux":   "sudo apt install git   (Debian or Ubuntu; otherwise your distribution's package manager)",
			"windows": "winget install --id Git.Git -e",
		},
		"runtime": {
			"darwin":  "brew install ollama   (or LM Studio from https://lmstudio.ai)",
			"linux":   "see https://ollama.com/download   (or LM Studio from https://lmstudio.ai)",
			"windows": "see https://ollama.com/download   (or LM Studio from https://lmstudio.ai)",
		},
	}
	if h, ok := hints[dep][runtime.GOOS]; ok {
		return h
	}
	return hints[dep]["linux"]
}

// doctor checks what urFluency needs and what its optional features use. It
// never installs anything; it prints how to install what is missing.
func doctor(ctx context.Context, stdout io.Writer) error {
	var b strings.Builder
	b.WriteString("urfluency doctor: nothing is installed or changed\n\n")
	gitOK := checkGit(ctx, &b)
	checkSessions(&b)
	checkRuntimes(ctx, &b)
	if gitOK {
		b.WriteString("\nEverything required is present.\n")
	} else {
		b.WriteString("\nA required dependency is missing; see above.\n")
	}
	if _, err := io.WriteString(stdout, b.String()); err != nil {
		return err
	}
	if !gitOK {
		return fmt.Errorf("git is missing or too old")
	}
	return nil
}

// checkGit verifies Git by capability: it commits with a Co-authored-by trailer
// in a temporary repository and reads the trailer back in the format scans use.
func checkGit(ctx context.Context, b *strings.Builder) bool {
	path, err := exec.LookPath("git")
	if err != nil {
		fmt.Fprintf(b, "  [missing] Git (required)\n            Install: %s\n", installHint("git"))
		return false
	}
	version, _ := exec.CommandContext(ctx, path, "--version").Output()
	name := strings.TrimSpace(strings.TrimPrefix(string(version), "git version "))
	if err := probeTrailers(ctx, path); err != nil {
		fmt.Fprintf(b, "  [missing] Git %s cannot read commit trailers as urFluency needs (%v)\n            Upgrade: %s\n", name, err, installHint("git"))
		return false
	}
	fmt.Fprintf(b, "  [ok]      Git %s: blame and commit trailers supported (required)\n", name)
	return true
}

func probeTrailers(ctx context.Context, git string) error {
	dir, err := os.MkdirTemp("", "urfluency-doctor-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=urfluency", "GIT_AUTHOR_EMAIL=doctor@example.com",
		"GIT_COMMITTER_NAME=urfluency", "GIT_COMMITTER_EMAIL=doctor@example.com")
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("init", "-q"); err != nil {
		return fmt.Errorf("git init: %s", strings.TrimSpace(out))
	}
	if out, err := run("commit", "-q", "--allow-empty", "-m", "probe\n\nCo-authored-by: Probe <probe@example.com>"); err != nil {
		return fmt.Errorf("git commit: %s", strings.TrimSpace(out))
	}
	out, err := run("log", "-1", "--format=%(trailers:key=Co-authored-by,valueonly,separator=%x1e)")
	if err != nil || !strings.Contains(out, "Probe <probe@example.com>") {
		return fmt.Errorf("trailer format unsupported")
	}
	return nil
}

func checkSessions(b *strings.Builder) {
	dirs, err := sessions.DefaultDirs()
	if err != nil {
		fmt.Fprintf(b, "  [--]      Agent sessions: home directory unavailable (%v)\n", err)
		return
	}
	found, err := sessions.Discover(dirs, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		fmt.Fprintf(b, "  [--]      Agent sessions: %v\n", err)
		return
	}
	counts := map[string]int{}
	for _, t := range found {
		counts[t.Agent]++
	}
	agents := []struct{ id, name, where string }{
		{"claude-code", "Claude Code", dirs.Claude},
		{"codex", "Codex", dirs.Codex},
		{"copilot-vscode", "GitHub Copilot Chat (VS Code)", "VS Code storage"},
	}
	for _, a := range agents {
		if n := counts[a.id]; n > 0 {
			fmt.Fprintf(b, "  [ok]      %s: %d sessions from the last year in %s (optional: --sessions)\n", a.name, n, tildePath(a.where))
		} else {
			fmt.Fprintf(b, "  [--]      %s: no sessions found (optional: --sessions)\n", a.name)
		}
	}
}

func checkRuntimes(ctx context.Context, b *strings.Builder) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	var running []string
	for _, r := range localRuntimes {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
		if err != nil {
			continue
		}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				running = append(running, r.name)
			}
		}
	}
	if len(running) > 0 {
		fmt.Fprintf(b, "  [ok]      Local model runtime: %s running (optional: experimental reviews)\n", strings.Join(running, ", "))
		return
	}
	fmt.Fprintf(b, "  [--]      Local model runtime: none running (optional: experimental reviews)\n"+
		"            Start Ollama or LM Studio's local server, or install one: %s\n", installHint("runtime"))
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return p
}
