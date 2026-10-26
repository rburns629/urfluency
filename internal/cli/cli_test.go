package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/internal/sessions"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	tests := map[string]string{"90d": "2026-07-28", "2026-01-31": "2026-01-31"}
	for in, want := range tests {
		got, err := parseSince(in, now)
		if err != nil || got.Format("2006-01-02") != want {
			t.Errorf("parseSince(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"0d", "-3d", "soon", "2026-13-01"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) accepted", bad)
		}
	}
}

func TestScanJSON(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a.go": "package a\n\nfunc f() { panic(1) }\n"})
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--locations", r.Dir}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	var goLang *report.Language
	for i := range rep.Languages {
		if rep.Languages[i].Language == "go" {
			goLang = &rep.Languages[i]
		}
	}
	if goLang == nil || len(goLang.Weaknesses) != 1 || goLang.Weaknesses[0].Locations[0] != "a.go:3" {
		t.Errorf("report = %+v", rep)
	}
}

func TestUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"bogus"}, nil, &out, &errOut); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestScanFolderWithSessions(t *testing.T) {
	root := t.TempDir()
	newRepo := func(name string) *testutil.Repo {
		r := &testutil.Repo{T: t, Dir: filepath.Join(root, "work", name)}
		if err := os.MkdirAll(r.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		r.Git("", "", "init", "-q", "-b", "main")
		return r
	}
	one, two := newRepo("one"), newRepo("two")
	one.Commit("me@example.com", "", map[string]string{"a.go": "package a\n\nfunc f() { panic(\"agent wrote\") }\n"})
	two.Commit("me@example.com", "", map[string]string{"b.go": "package b\n\nfunc g() { panic(\"by hand\") }\n"})

	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", t.TempDir())
	edit := filepath.Join(one.Dir, "a.go")
	transcript := `{"type":"user","cwd":"` + one.Dir + `","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"content":"Add a function f to a.go that panics, then run the tests"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"Write","input":{"file_path":"` + edit + `","content":"func f() { panic(\"agent wrote\") }"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"ok"}]}}
`
	path := filepath.Join(claude, "projects", "-one", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--locations", "--sessions", root}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Repos != 2 || rep.Prompting == nil || rep.Prompting.Sessions != 1 || rep.Prompting.Tasks["feature"] != 1 {
		t.Fatalf("report = %s", out.String())
	}
	if rep.AI == nil || rep.AI.AILines != 1 || rep.AI.OwnedLines != 6 {
		t.Errorf("ai = %+v", rep.AI)
	}
	if strings.Contains(out.String(), "panics, then run") {
		t.Error("prompt text leaked into the report")
	}
}

func TestCalibrate(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Commit("me@example.com", "", map[string]string{"a.go": "package a\n"})
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", t.TempDir())
	transcript := `{"type":"user","cwd":"` + repo.Dir + `","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"content":"<ide_opened_file>a.go</ide_opened_file> Fix the crash in parser.go on empty input"}}
{"type":"user","message":{"content":"yes"}}
`
	path := filepath.Join(claude, "projects", "-a", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	// An invalid answer is asked again; "b ynn" matches the rules exactly.
	code := Run(context.Background(), []string{"calibrate", repo.Dir}, strings.NewReader("maybe\nb ynn\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	flat := strings.Join(strings.Fields(got), " ")
	for _, want := range []string{"1/1", "Fix the crash in parser.go", "Not understood", "type[bfrto] context[yn]", "1. Task type", "1 prompts labeled", "context 100%", "task type 100%"} {
		if !strings.Contains(flat, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ide_opened_file") {
		t.Error("injected editor block was shown")
	}
}

func TestWriteAgreement(t *testing.T) {
	rules := []sessions.Prompt{{Type: "bug", Context: true}, {Type: "feature"}}
	labels := []sessions.Prompt{{Type: "bug"}, {Type: "refactor", Criteria: true}}
	var b strings.Builder
	if err := writeAgreement(&b, rules, labels); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(b.String()), " ")
	for _, want := range []string{"context 50% 1 0", "criteria 50% 0 1", "verification 100% 0 0", "task type 50%", "you refactor, rule feature: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, b.String())
		}
	}
}

func TestHelpDocumentsCalibrateAnswers(t *testing.T) {
	var out bytes.Buffer
	Run(context.Background(), []string{"help"}, nil, &out, &out)
	for _, want := range []string{"urfluency calibrate", "--sample N", "4. Verification", `"f yny"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}
