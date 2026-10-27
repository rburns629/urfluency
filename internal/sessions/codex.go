package sessions

import (
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"sort"
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
		Model     string `json:"model"`
		Provider  string `json:"model_provider"`
		Item      struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
	} `json:"payload"`
}

var (
	// execCmd finds commands in Codex code-mode scripts, such as
	// tools.exec_command({cmd: "git status"}).
	execCmd = regexp.MustCompile("\\bcmd\\s*:\\s*(\"(?:[^\"\\\\]|\\\\.)*\"|'(?:[^'\\\\]|\\\\.)*'|`(?:[^`\\\\]|\\\\.)*`)")
	// jsUnescape undoes the escapes a script string literal adds.
	jsUnescape = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\'`, `'`, "\\`", "`", `\\`, `\`)
)

// ReadCodex parses a Codex rollout transcript. User prompts come from
// user_message events or, in newer versions, completed UserMessage items;
// transcripts without either fall back to user messages, skipping the
// environment and instruction blocks Codex injects.
func ReadCodex(r io.Reader) (Session, error) {
	s := Session{Agent: "codex", Edits: map[string]map[string]bool{}}
	var fallback []Prompt
	provider := ""
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
		case l.Type == "session_meta":
			provider = p.Provider
		case l.Type == "turn_context":
			if provider != "" && provider != "openai" && p.Model != "" {
				s.addModel(provider + "/" + p.Model)
			} else {
				s.addModel(p.Model)
			}
		case l.Type == "event_msg" && p.Type == "user_message":
			s.Prompts = append(s.Prompts, Classify(p.Message))
		case l.Type == "event_msg" && p.Type == "item_completed" && p.Item.Type == "UserMessage":
			var text strings.Builder
			for _, c := range p.Item.Content {
				text.WriteString(c.Text + "\n")
			}
			s.Prompts = append(s.Prompts, Classify(text.String()))
		case l.Type == "response_item" && p.Type == "message" && p.Role == "user":
			for _, c := range p.Content {
				if !strings.HasPrefix(strings.TrimSpace(c.Text), "<") {
					fallback = append(fallback, Classify(c.Text))
				}
			}
		case l.Type == "response_item" && p.Type == "custom_tool_call" && p.Name == "apply_patch":
			s.record(edit)
			s.addPatch(p.Input)
		case l.Type == "response_item" && p.Type == "custom_tool_call" && p.Name == "exec":
			s.addScript(p.Input)
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
				s.recordCommand(shellCommand(args.Command, args.Cmd))
			}
		}
	})
	if len(s.Prompts) == 0 {
		s.Prompts = fallback
	}
	return s, err
}

var shells = map[string]bool{"bash": true, "sh": true, "zsh": true}

// addScript records the patches and commands in a Codex code-mode script, in the
// order they appear. Patches inside string literals are unescaped first.
func (s *Session) addScript(script string) {
	type step struct {
		at    int
		patch string // set for a patch
		cmd   string // set for a command
	}
	var steps []step
	for at := 0; ; {
		start := strings.Index(script[at:], "*** Begin Patch")
		if start < 0 {
			break
		}
		start += at
		end := strings.Index(script[start:], "*** End Patch")
		if end < 0 {
			end = len(script) - start
		}
		patch := script[start : start+end]
		if !strings.Contains(patch, "\n") {
			patch = jsUnescape.Replace(patch)
		}
		steps = append(steps, step{at: start, patch: patch})
		at = start + end
	}
	for _, m := range execCmd.FindAllStringSubmatchIndex(script, -1) {
		cmd := jsUnescape.Replace(script[m[2]+1 : m[3]-1])
		if !strings.Contains(cmd, "*** Begin Patch") {
			steps = append(steps, step{at: m[0], cmd: cmd})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].at < steps[j].at })
	for _, st := range steps {
		if st.patch != "" {
			s.record(edit)
			s.addPatch(st.patch)
		} else {
			s.recordCommand(st.cmd)
		}
	}
}

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
