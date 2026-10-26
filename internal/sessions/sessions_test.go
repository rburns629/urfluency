package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic transcripts in each agent's format; no real session is read by tests.
const claudeTranscript = `{"type":"user","cwd":"/w/app","timestamp":"2026-10-01T10:00:00Z","message":{"role":"user","content":"Add a retry to fetchUser in api/client.go; it should give up after 3 attempts. Add tests."}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/w/app/api/client.go","old_string":"func fetchUser() {\n\treturn get(url)\n}","new_string":"func fetchUser() {\n\tfor attempt := 0; attempt < 3; attempt++ {\n\treturn get(url)\n}"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"toolUseResult":{"structuredPatch":[{"lines":[" func fetchUser() {","+\tfor attempt := 0; attempt < 3; attempt++ {","\treturn get(url)"]}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Write","input":{"file_path":"/w/app/rejected.go","content":"package rejected_by_user\n"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"The user doesn't want to proceed"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Write","input":{"file_path":"/w/app/new.go","content":"package newpackage\n}\n"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"created"}]},"toolUseResult":{"type":"create"}}
{"type":"user","isSidechain":true,"message":{"content":"Subagent prompt written by the agent, should be ignored"}}
{"type":"user","isMeta":true,"message":{"content":"Caveat: injected by the client"}}
{"type":"user","message":{"content":[{"type":"text","text":"no, that's the wrong file"}]}}
not json at all
`

const codexTranscript = `{"timestamp":"2026-10-02T09:00:00Z","type":"session_meta","payload":{"cwd":"/w/svc"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>ignored</environment_context>"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"Fix the crash in parser.py when the input is empty"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: parser.py\n@@\n-    return data[0]\n+    if not data:\n+        return None\n*** Add File: /w/svc/notes_helper.py\n+def describe_notes():\n*** End Patch"}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"apply_patch\",\"*** Begin Patch\\n*** Update File: lib/util.py\\n+value = compute_total(items)\\n*** End Patch\"]}"}}
`

func TestReadClaude(t *testing.T) {
	s, err := ReadClaude(strings.NewReader(claudeTranscript))
	if err != nil {
		t.Fatal(err)
	}
	if s.Cwd != "/w/app" || s.Start.IsZero() {
		t.Errorf("cwd/start = %q %v", s.Cwd, s.Start)
	}
	if len(s.Prompts) != 2 || s.Prompts[0].Type != Feature || !s.Prompts[0].Context || !s.Prompts[0].Criteria || !s.Prompts[0].Verification || !s.Prompts[1].Correction {
		t.Errorf("prompts = %+v", s.Prompts)
	}
	edited := s.Edits["/w/app/api/client.go"]
	if len(edited) != 1 || !edited["for attempt := 0; attempt < 3; attempt++ {"] {
		t.Errorf("client.go lines = %v, want only the added loop", edited)
	}
	if !s.Edits["/w/app/new.go"]["package newpackage"] {
		t.Errorf("created file lines = %v", s.Edits["/w/app/new.go"])
	}
	if s.Edits["/w/app/rejected.go"] != nil {
		t.Error("a rejected edit was counted")
	}
}

func TestReadClaudeEditWithoutPatchSkipsKeptLines(t *testing.T) {
	in := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"Edit","input":{"file_path":"/w/a.go","old_string":"keep this line here\nold line removed","new_string":"keep this line here\nnew line inserted"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"ok"}]}}
`
	s, err := ReadClaude(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Edits["/w/a.go"]; len(got) != 1 || !got["new line inserted"] {
		t.Errorf("lines = %v", got)
	}
}

func TestReadCodex(t *testing.T) {
	s, err := ReadCodex(strings.NewReader(codexTranscript))
	if err != nil {
		t.Fatal(err)
	}
	if s.Cwd != "/w/svc" || len(s.Prompts) != 1 || s.Prompts[0].Type != Bug {
		t.Errorf("session = %+v", s)
	}
	want := map[string]string{
		"/w/svc/parser.py":       "return None",
		"/w/svc/notes_helper.py": "def describe_notes():",
		"/w/svc/lib/util.py":     "value = compute_total(items)",
	}
	for path, line := range want {
		if !s.Edits[path][line] {
			t.Errorf("%s lines = %v, want %q", path, s.Edits[path], line)
		}
	}
	if s.Edits["/w/svc/parser.py"]["return data[0]"] {
		t.Error("a removed line was counted")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		text string
		want Prompt
	}{
		{"yes", Prompt{}},
		{"/commit", Prompt{}},
		{"No, use the existing helper", Prompt{Correction: true}},
		{"it still doesn't work", Prompt{Correction: true}},
		{"Fix the login error shown in auth/session.go", Prompt{Type: Bug, Context: true}},
		{"Implement CSV export so that admins can download reports, and run the tests", Prompt{Type: Feature, Criteria: true, Verification: true}},
		{"Refactor the payment module please", Prompt{Type: Refactor}},
		{"What does this function actually do?", Prompt{Type: Other}},
		{"Can you add a cache to the user lookup?", Prompt{Type: Feature}},
	}
	for _, tt := range tests {
		if got := Classify(tt.text); got != tt.want {
			t.Errorf("Classify(%q) = %+v, want %+v", tt.text, got, tt.want)
		}
	}
}

func TestGrade(t *testing.T) {
	feature := Prompt{Type: Feature, Context: true}
	s := Session{Prompts: []Prompt{
		feature, {Correction: true}, {Correction: true}, // one task, corrected
		{Type: Other}, {Correction: true}, // a correction after a question is ignored
		{Type: Bug, Context: true, Verification: true},
	}}
	g := Grade([]Session{s})
	if g.Sessions != 1 || g.Tasks[Feature] != 1 || g.Tasks[Bug] != 1 {
		t.Errorf("grades = %+v", g)
	}
	want := map[string]Tally{
		"prompt.no-context":             {Applicable: 2, Missed: 0},
		"prompt.no-acceptance-criteria": {Applicable: 1, Missed: 1},
		"prompt.no-verification":        {Applicable: 2, Missed: 1},
		"prompt.needed-correction":      {Applicable: 2, Missed: 1},
	}
	for id, tally := range want {
		if g.Rules[id] != tally {
			t.Errorf("%s = %+v, want %+v", id, g.Rules[id], tally)
		}
	}
}

func TestDiscover(t *testing.T) {
	d := Dirs{Claude: t.TempDir(), Codex: t.TempDir()}
	write := func(path string, age time.Duration) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(d.Claude, "projects", "-w-app", "s1.jsonl"), time.Hour)
	write(filepath.Join(d.Claude, "projects", "-w-app", "s1", "subagents", "a.jsonl"), time.Hour)
	write(filepath.Join(d.Claude, "projects", "-w-app", "old.jsonl"), 400*24*time.Hour)
	write(filepath.Join(d.Codex, "sessions", "2026", "10", "02", "rollout-x.jsonl"), time.Hour)
	write(filepath.Join(d.Codex, "sessions", "2026", "10", "02", "other.jsonl"), time.Hour)

	got, err := Discover(d, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Agent != "claude-code" || filepath.Base(got[0].Path) != "s1.jsonl" || got[1].Agent != "codex" {
		t.Errorf("transcripts = %+v", got)
	}
	if _, err := Discover(Dirs{Claude: t.TempDir(), Codex: filepath.Join(t.TempDir(), "missing")}, time.Time{}); err != nil {
		t.Errorf("missing directories should be ignored: %v", err)
	}
}

func TestClassifyTestTasks(t *testing.T) {
	if got := Classify("Add unit tests for the date parser"); got.Type != Test {
		t.Errorf("test task = %+v", got)
	}
}
