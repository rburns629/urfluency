package sessions

import (
	"encoding/json"
	"io"
	"strings"
	"time"
)

type claudeLine struct {
	Type             string    `json:"type"`
	Cwd              string    `json:"cwd"`
	Timestamp        time.Time `json:"timestamp"`
	IsMeta           bool      `json:"isMeta"`
	IsSidechain      bool      `json:"isSidechain"`
	IsCompactSummary bool      `json:"isCompactSummary"`
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

type claudeEdit struct {
	FilePath  string       `json:"file_path"`
	OldString string       `json:"old_string"`
	NewString string       `json:"new_string"`
	Content   string       `json:"content"`
	Edits     []claudePair `json:"edits"`
}

type claudePair struct {
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

var claudeEditTools = map[string]bool{"Edit": true, "MultiEdit": true, "Write": true}

// ReadClaude parses a Claude Code transcript. Edits count only when their tool
// result succeeded, so rejected or failed edits are ignored.
func ReadClaude(r io.Reader) (Session, error) {
	s := Session{Agent: "claude-code", Edits: map[string]map[string]bool{}}
	pending := map[string]claudeEdit{}
	err := eachLine(r, func(line []byte) {
		var l claudeLine
		if json.Unmarshal(line, &l) != nil || l.IsSidechain {
			return
		}
		if s.Cwd == "" {
			s.Cwd = l.Cwd
		}
		if s.Start.IsZero() {
			s.Start = l.Timestamp
		}
		var text string
		var blocks []claudeBlock
		if json.Unmarshal(l.Message.Content, &text) != nil {
			_ = json.Unmarshal(l.Message.Content, &blocks)
		}
		switch l.Type {
		case "assistant":
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				var in claudeEdit
				var shell struct {
					Command string `json:"command"`
				}
				switch {
				case claudeEditTools[b.Name]:
					s.record(edit)
					if json.Unmarshal(b.Input, &in) == nil {
						pending[b.ID] = in
					}
				case exploreTools[b.Name]:
					s.record(explore)
				case b.Name == "Bash" && json.Unmarshal(b.Input, &shell) == nil:
					s.record(commandKind(shell.Command))
				}
			}
		case "user":
			toolResult := false
			for _, b := range blocks {
				switch b.Type {
				case "text":
					text += b.Text + "\n"
				case "tool_result":
					toolResult = true
					if in, ok := pending[b.ToolUseID]; ok && !b.IsError {
						s.addClaudeEdit(in, l.ToolUseResult)
					}
					delete(pending, b.ToolUseID)
				}
			}
			if !toolResult && !l.IsMeta && !l.IsCompactSummary {
				s.Prompts = append(s.Prompts, Classify(text))
			}
		}
	})
	return s, err
}

// addClaudeEdit prefers the applied patch, which holds only added lines; the tool
// input is the fallback, minus lines the edit kept unchanged.
func (s *Session) addClaudeEdit(in claudeEdit, result json.RawMessage) {
	var applied struct {
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
	}
	if json.Unmarshal(result, &applied) == nil && len(applied.StructuredPatch) > 0 {
		for _, hunk := range applied.StructuredPatch {
			for _, l := range hunk.Lines {
				if strings.HasPrefix(l, "+") {
					s.addLines(in.FilePath, l[1:])
				}
			}
		}
		return
	}
	s.addLines(in.FilePath, strings.Split(in.Content, "\n")...)
	for _, p := range append(in.Edits, claudePair{in.OldString, in.NewString}) {
		s.addReplacement(in.FilePath, p.OldString, p.NewString)
	}
}
