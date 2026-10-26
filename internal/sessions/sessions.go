// Package sessions reads local coding-agent transcripts. Prompt text is reduced to
// signals while parsing and is never kept, printed, or uploaded.
package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
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
	VSCode []string // VS Code "User" directories, for Copilot Chat
}

// DefaultDirs honors CLAUDE_CONFIG_DIR and CODEX_HOME, as the agents do, and
// covers VS Code and VS Code Insiders.
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
	config, err := os.UserConfigDir()
	if err == nil {
		for _, app := range []string{"Code", "Code - Insiders"} {
			d.VSCode = append(d.VSCode, filepath.Join(config, app, "User"))
		}
	}
	return d, nil
}

// Transcript is one session file.
type Transcript struct {
	Agent string
	Path  string
	Cwd   string // set when the location, not the file, names the workspace
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
	if err != nil {
		return nil, err
	}
	for _, user := range d.VSCode {
		workspaces, err := filepath.Glob(filepath.Join(user, "workspaceStorage", "*"))
		if err != nil {
			return nil, err
		}
		for _, ws := range workspaces {
			chats, _ := filepath.Glob(filepath.Join(ws, "chatSessions", "*.json*"))
			cwd := ""
			for _, p := range chats {
				if !modifiedSince(p, since) || !(strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".jsonl")) {
					continue
				}
				if cwd == "" {
					cwd = workspaceFolder(ws)
				}
				out = append(out, Transcript{Agent: "copilot-vscode", Path: p, Cwd: cwd})
			}
		}
	}
	return out, nil
}

// workspaceFolder reads the folder a VS Code workspace storage directory belongs
// to, or "" for multi-root workspaces and unreadable files.
func workspaceFolder(storage string) string {
	data, err := os.ReadFile(filepath.Join(storage, "workspace.json"))
	if err != nil {
		return ""
	}
	var ws struct {
		Folder string `json:"folder"`
	}
	if json.Unmarshal(data, &ws) != nil {
		return ""
	}
	u, err := url.Parse(ws.Folder)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if len(p) > 2 && p[0] == '/' && p[2] == ':' { // file:///c:/... on Windows
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// Load parses one transcript.
func Load(t Transcript) (Session, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	switch t.Agent {
	case "codex":
		return ReadCodex(f)
	case "copilot-vscode":
		return ReadCopilotChat(f, t.Cwd)
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

// addReplacement records the lines of newText that were not already in oldText.
func (s *Session) addReplacement(path, oldText, newText string) {
	kept := map[string]bool{}
	for _, l := range strings.Split(oldText, "\n") {
		kept[strings.TrimSpace(l)] = true
	}
	for _, l := range strings.Split(newText, "\n") {
		if !kept[strings.TrimSpace(l)] {
			s.addLines(path, l)
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
