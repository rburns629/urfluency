package sessions

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type codexLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		Type    string `json:"type"`
		Cwd     string `json:"cwd"`
		Message string `json:"message"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Input     string `json:"input"`
	} `json:"payload"`
}

// ReadCodex parses a Codex CLI rollout transcript. User prompts come from
// user_message events; transcripts without them fall back to user messages,
// skipping the environment and instruction blocks Codex injects.
func ReadCodex(r io.Reader) (Session, error) {
	s := Session{Agent: "codex", Edits: map[string]map[string]bool{}}
	var fallback []Prompt
	err := eachLine(r, func(line []byte) {
		var l codexLine
		if json.Unmarshal(line, &l) != nil {
			return
		}
		if s.Start.IsZero() {
			s.Start = l.Timestamp
		}
		p := l.Payload
		if p.Cwd != "" {
			s.Cwd = p.Cwd
		}
		switch {
		case l.Type == "event_msg" && p.Type == "user_message":
			s.Prompts = append(s.Prompts, Classify(p.Message))
		case l.Type == "response_item" && p.Type == "message" && p.Role == "user":
			for _, c := range p.Content {
				if !strings.HasPrefix(strings.TrimSpace(c.Text), "<") {
					fallback = append(fallback, Classify(c.Text))
				}
			}
		case l.Type == "response_item" && p.Type == "custom_tool_call" && p.Name == "apply_patch":
			s.record(edit)
			s.addPatch(p.Input)
		case l.Type == "response_item" && p.Type == "function_call":
			// Shell calls carry the command as an argument list, and patches as
			// ["apply_patch", "*** Begin Patch..."].
			var args struct {
				Command []string `json:"command"`
				Cmd     string   `json:"cmd"`
			}
			if json.Unmarshal([]byte(p.Arguments), &args) != nil {
				return
			}
			patched := false
			for _, a := range args.Command {
				if i := strings.Index(a, "*** Begin Patch"); i >= 0 {
					s.addPatch(a[i:])
					patched = true
				}
			}
			if patched {
				s.record(edit)
			} else {
				s.record(commandKind(shellCommand(args.Command, args.Cmd)))
			}
		}
	})
	if len(s.Prompts) == 0 {
		s.Prompts = fallback
	}
	return s, err
}

var shells = map[string]bool{"bash": true, "sh": true, "zsh": true}

// shellCommand returns the command a shell call runs, unwrapping
// ["bash", "-lc", "go test ./..."].
func shellCommand(argv []string, cmd string) string {
	if cmd != "" {
		return cmd
	}
	if len(argv) == 3 && strings.HasPrefix(argv[1], "-") && shells[filepath.Base(argv[0])] {
		return argv[2]
	}
	return strings.Join(argv, " ")
}

// addPatch records the added lines of an apply_patch envelope. Paths are relative
// to the session's working directory unless absolute.
func (s *Session) addPatch(patch string) {
	file := ""
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "*** Add File: "), strings.HasPrefix(line, "*** Update File: "), strings.HasPrefix(line, "*** Move to: "):
			file = strings.TrimSpace(line[strings.Index(line, ": ")+2:])
			if !filepath.IsAbs(file) {
				file = filepath.Join(s.Cwd, file)
			}
		case strings.HasPrefix(line, "*** "):
			file = ""
		case file != "" && strings.HasPrefix(line, "+"):
			s.addLines(file, line[1:])
		}
	}
}
