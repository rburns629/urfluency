// Package sessions reads local coding-agent transcripts. Prompt text is reduced to
// signals while parsing and is never kept, printed, or uploaded.
package sessions

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Session is what urFluency keeps from one transcript.
type Session struct {
	Agent   string // "claude-code" or "codex"
	Cwd     string
	Start   time.Time
	Prompts []Prompt
	// Edits maps absolute file paths to the normalized lines the agent added.
	Edits map[string]map[string]bool
}

// Dirs are the agents' configuration directories.
type Dirs struct {
	Claude string
	Codex  string
}

// DefaultDirs honors CLAUDE_CONFIG_DIR and CODEX_HOME, as the agents do.
func DefaultDirs() (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, err
	}
	d := Dirs{Claude: os.Getenv("CLAUDE_CONFIG_DIR"), Codex: os.Getenv("CODEX_HOME")}
	if d.Claude == "" {
		d.Claude = filepath.Join(home, ".claude")
	}
	if d.Codex == "" {
		d.Codex = filepath.Join(home, ".codex")
	}
	return d, nil
}

// Transcript is one session file.
type Transcript struct {
	Agent string
	Path  string
}

// Discover lists transcripts modified since the given time. Subagent transcripts
// are not listed: their prompts were written by an agent.
func Discover(d Dirs, since time.Time) ([]Transcript, error) {
	var out []Transcript
	claude, err := filepath.Glob(filepath.Join(d.Claude, "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, p := range claude {
		if modifiedSince(p, since) {
			out = append(out, Transcript{Agent: "claude-code", Path: p})
		}
	}
	err = filepath.WalkDir(filepath.Join(d.Codex, "sessions"), func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") && modifiedSince(p, since) {
			out = append(out, Transcript{Agent: "codex", Path: p})
		}
		return nil
	})
	return out, err
}

// Load parses one transcript.
func Load(t Transcript) (Session, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	if t.Agent == "codex" {
		return ReadCodex(f)
	}
	return ReadClaude(f)
}

// Kinds of agent activity recorded after a prompt.
const (
	explore = "explore"
	edit    = "edit"
	check   = "check"
)

var (
	exploreTools = map[string]bool{"Read": true, "Grep": true, "Glob": true, "LS": true}
	exploreCmd   = regexp.MustCompile(`^\s*(grep|rg|ag|find|fd|ls|cat|head|tail|tree|wc|sed -n|git (grep|log|show|diff|status|blame))\b`)
	checkCmd     = regexp.MustCompile(`\b(test|tests|pytest|jest|vitest|mocha|tsc|eslint|ruff|mypy|pyright|flake8|lint|typecheck|staticcheck|govulncheck|go (test|vet|build)|cargo (test|check|build|clippy)|(npm|pnpm|yarn|bun)( run)? (build|check))\b`)
)

// commandKind classifies a shell command as exploration, a check (tests,
// linters, builds, type checks), or neither.
func commandKind(cmd string) string {
	switch {
	case exploreCmd.MatchString(cmd):
		return explore
	case checkCmd.MatchString(cmd):
		return check
	}
	return ""
}

// record counts agent activity against the latest prompt.
func (s *Session) record(kind string) {
	if len(s.Prompts) == 0 || kind == "" {
		return
	}
	p := &s.Prompts[len(s.Prompts)-1]
	switch kind {
	case explore:
		p.Explores++
		if p.Edits == 0 {
			p.ExploresBeforeEdit++
		}
	case edit:
		p.Edits++
	case check:
		p.Checks++
	}
}

func modifiedSince(path string, since time.Time) bool {
	info, err := os.Stat(path)
	return err == nil && !info.ModTime().Before(since)
}

// Normalize returns the comparable form of a code line, or "" for lines too short
// to attribute, such as braces or "return".
func Normalize(line string) string {
	line = strings.TrimSpace(line)
	if len(line) < 10 {
		return ""
	}
	return line
}

func (s *Session) addLines(path string, lines ...string) {
	if path == "" {
		return
	}
	for _, l := range lines {
		if l = Normalize(l); l != "" {
			if s.Edits[path] == nil {
				s.Edits[path] = map[string]bool{}
			}
			s.Edits[path][l] = true
		}
	}
}

// eachLine calls fn for every non-blank line, however long.
func eachLine(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			fn(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
