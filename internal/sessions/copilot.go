package sessions

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// copilotSession is the part of a VS Code chat session urFluency reads.
type copilotSession struct {
	CreationDate int64 `json:"creationDate"`
	Requests     []struct {
		Timestamp int64 `json:"timestamp"`
		Message   struct {
			Text string `json:"text"`
		} `json:"message"`
		Result struct {
			Metadata struct {
				ToolCallRounds []struct {
					ToolCalls []struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"toolCalls"`
				} `json:"toolCallRounds"`
			} `json:"metadata"`
		} `json:"result"`
	} `json:"requests"`
}

type copilotArgs struct {
	FilePath  string `json:"filePath"`
	Input     string `json:"input"`
	OldString string `json:"oldString"`
	NewString string `json:"newString"`
	Content   string `json:"content"`
	Command   string `json:"command"`
}

var (
	copilotExploreTools = map[string]bool{"read_file": true, "grep_search": true, "file_search": true, "semantic_search": true, "list_dir": true}
	copilotCheckTools   = map[string]bool{"run_tests": true, "runTests": true, "get_errors": true, "test_failure": true}
)

// ReadCopilotChat parses a GitHub Copilot Chat session from VS Code. Older VS Code
// versions store one JSON document; newer ones store a JSONL operation log that
// rebuilds it. cwd is the session's workspace folder, used for relative paths.
func ReadCopilotChat(r io.Reader, cwd string) (Session, error) {
	s := Session{Agent: "copilot-vscode", Cwd: cwd, Edits: map[string]map[string]bool{}}
	data, err := io.ReadAll(r)
	if err != nil {
		return s, err
	}
	// An operation log's lines are {"kind": n, "v": ...}; a document is not.
	first, _, _ := bytes.Cut(data, []byte("\n"))
	var probe struct {
		Kind *int            `json:"kind"`
		V    json.RawMessage `json:"v"`
	}
	if json.Unmarshal(first, &probe) == nil && probe.Kind != nil && probe.V != nil {
		if data, err = replayOpLog(data); err != nil {
			return s, err
		}
	}
	var doc copilotSession
	if err := json.Unmarshal(data, &doc); err != nil {
		return s, err
	}
	if doc.CreationDate > 0 {
		s.Start = time.UnixMilli(doc.CreationDate)
	}
	for _, req := range doc.Requests {
		if s.Start.IsZero() && req.Timestamp > 0 {
			s.Start = time.UnixMilli(req.Timestamp)
		}
		s.Prompts = append(s.Prompts, Classify(req.Message.Text))
		for _, round := range req.Result.Metadata.ToolCallRounds {
			for _, call := range round.ToolCalls {
				var a copilotArgs
				_ = json.Unmarshal([]byte(call.Arguments), &a)
				s.addCopilotCall(call.Name, a)
			}
		}
	}
	return s, nil
}

func (s *Session) addCopilotCall(name string, a copilotArgs) {
	switch {
	case name == "apply_patch":
		s.record(edit)
		s.addPatch(a.Input)
	case name == "create_file":
		s.record(edit)
		s.addLines(a.FilePath, strings.Split(a.Content, "\n")...)
	case name == "replace_string_in_file" || name == "insert_edit_into_file":
		s.record(edit)
		s.addReplacement(a.FilePath, a.OldString, a.NewString)
	case name == "run_in_terminal":
		s.record(commandKind(a.Command))
	case copilotExploreTools[name]:
		s.record(explore)
	case copilotCheckTools[name]:
		s.record(check)
	}
}

// replayOpLog rebuilds a session document from VS Code's JSONL operation log:
// kind 0 sets the whole state, kind 1 sets the value at key path k, and kind 2
// appends v to the array at k, first truncating it to index i when present.
func replayOpLog(data []byte) ([]byte, error) {
	var state any
	err := eachLine(bytes.NewReader(data), func(line []byte) {
		var op struct {
			Kind int   `json:"kind"`
			K    []any `json:"k"`
			V    any   `json:"v"`
			I    *int  `json:"i"`
		}
		if json.Unmarshal(line, &op) != nil {
			return
		}
		switch op.Kind {
		case 0:
			state = op.V
		case 1:
			state = applyAt(state, op.K, func(any) any { return op.V })
		case 2:
			state = applyAt(state, op.K, func(old any) any {
				arr, _ := old.([]any)
				if op.I != nil && *op.I >= 0 && *op.I <= len(arr) {
					arr = arr[:*op.I]
				}
				items, _ := op.V.([]any)
				return append(arr, items...)
			})
		}
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(state)
}

// applyAt replaces the value at path with fn(value). Missing map keys are created;
// out-of-range indexes leave the state unchanged.
func applyAt(node any, path []any, fn func(any) any) any {
	if len(path) == 0 {
		return fn(node)
	}
	switch n := node.(type) {
	case map[string]any:
		if key, ok := path[0].(string); ok {
			n[key] = applyAt(n[key], path[1:], fn)
		}
	case []any:
		if f, ok := path[0].(float64); ok && int(f) >= 0 && int(f) < len(n) {
			n[int(f)] = applyAt(n[int(f)], path[1:], fn)
		}
	}
	return node
}
