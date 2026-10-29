package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/internal/sessions"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	tests := map[string]string{"90d": "2026-07-28", "2026-01-31": "2026-01-31", "all": "0001-01-01"}
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
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--locations", r.Dir}, &out, &errOut)
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
	if code := Run(context.Background(), []string{"bogus"}, &out, &errOut); code != 2 {
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
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--locations", "--sessions", root}, &out, &errOut)
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

func TestScanFoldersWithoutGit(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	panics := "package a\n\nfunc f() { panic(1) }\n"
	write("old/go.mod", "module old\n")
	write("old/a.go", panics)
	write("old/cmd/b.go", panics) // part of old, not a project of its own
	write("archive/x/a.go", panics)
	write("stray.py", "print(1)\n") // the root holds projects, so it is not one
	write("fresh/main.py", "print(2)\n")
	fresh := &testutil.Repo{T: t, Dir: filepath.Join(root, "fresh")}
	fresh.Git("", "", "init", "-q", "-b", "main") // no commits: scanned as a folder
	nested := &testutil.Repo{T: t, Dir: filepath.Join(root, "old", "lib")}
	if err := os.MkdirAll(nested.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested.Git("", "", "init", "-q", "-b", "main")
	nested.Commit("me@example.com", "", map[string]string{"c.go": panics})

	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--exclude", "archive", root}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Repos != 1 || len(rep.Languages) == 0 || rep.Languages[0].OwnedLines != 3 {
		t.Errorf("git part = %+v", rep.Languages)
	}
	u := rep.Unversioned
	if u == nil || u.Projects != 2 || len(u.Languages) == 0 || u.Languages[0].OwnedLines != 6 || u.Languages[0].Files != 2 {
		t.Fatalf("unversioned = %+v", u)
	}

	// A folder inside a repository is scanned as that repository.
	sub := filepath.Join(nested.Dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", sub}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	rep = report.Report{}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Repos != 1 || rep.Unversioned != nil {
		t.Errorf("subfolder scan = %s", out.String())
	}
}

func TestScanDetectsIdentity(t *testing.T) {
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Ana Diaz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	r := testutil.NewRepo(t)
	panics := "package a\n\nfunc f() { panic(1) }\n"
	commit := func(author, file string) {
		if err := os.WriteFile(filepath.Join(r.Dir, file), []byte(panics), 0o644); err != nil {
			t.Fatal(err)
		}
		r.Git("x@example.com", "", "add", "-A")
		r.Git("x@example.com", "", "commit", "-q", "-m", "change", "--author="+author)
	}
	commit("Ana Diaz <ana@old.example>", "a.go")
	commit("Ana M. Diaz <ana@new.example>", "b.go")
	commit("Ben Diaz <ben@example.com>", "c.go")

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--json", "--since", "all", r.Dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Languages) == 0 || rep.Languages[0].OwnedLines != 6 {
		t.Errorf("languages = %+v", rep.Languages)
	}
	for _, want := range []string{"Counted as yours: 2 addresses", "ana@new.example, ana@old.example", "Possibly yours, not counted", "ben@example.com (1 commit in 1 repository)"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut.String())
		}
	}
	if strings.Contains(out.String(), "@") {
		t.Errorf("an address leaked into the report:\n%s", out.String())
	}
}

func TestInitSavesConfirmedAddresses(t *testing.T) {
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Ana Diaz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	config := t.TempDir()
	t.Setenv("URFLUENCY_CONFIG_DIR", config)
	r := testutil.NewRepo(t)
	for i, author := range []string{"Ana Diaz <ana@old.example>", "Cleo <anadiaz@personal.example>", "Ben Diaz <ben@example.com>"} {
		if err := os.WriteFile(filepath.Join(r.Dir, fmt.Sprintf("f%d.go", i)), []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r.Git("x@example.com", "", "add", "-A")
		r.Git("x@example.com", "", "commit", "-q", "-m", "change", "--author="+author)
	}
	stdin, interactive = strings.NewReader("y\nn\n"), func() bool { return true }
	t.Cleanup(func() { stdin, interactive = os.Stdin, func() bool { return false } })

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"init", r.Dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Is anadiaz@personal.example yours?") || !strings.Contains(out.String(), "Is ben@example.com yours?") {
		t.Errorf("prompts missing:\n%s", out.String())
	}
	data, err := os.ReadFile(filepath.Join(config, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Emails, []string{"ana@old.example", "anadiaz@personal.example"}) || !slices.Equal(s.NotMine, []string{"ben@example.com"}) {
		t.Errorf("settings = %+v", s)
	}
	if info, err := os.Stat(filepath.Join(config, "settings.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("settings mode = %v, %v", info.Mode(), err)
	}

	// Scans start from the saved answers: the claimed address counts and the
	// declined one is not suggested again.
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"scan", "--json", r.Dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "ana@old.example, anadiaz@personal.example") || strings.Contains(errOut.String(), "Possibly yours") {
		t.Errorf("scan stderr:\n%s", errOut.String())
	}
}

func TestWriteValidation(t *testing.T) {
	found := []sessions.Session{
		{Agent: "claude-code", Models: map[string]bool{"claude-test-1": true}, Prompts: []sessions.Prompt{
			{Type: "feature", Context: true, ExploresBeforeEdit: 1, Edits: 2, Checks: 1},
			{Type: "feature", Context: true, ExploresBeforeEdit: 3, Edits: 1},
		}},
		{Agent: "copilot-vscode", Models: map[string]bool{"claude-test-1": true, "lmstudio/qwen3-coder": true}, Prompts: []sessions.Prompt{
			{Type: "bug", ExploresBeforeEdit: 8, Edits: 1},
			{Correction: true}, {Correction: true}, {Correction: true},
			{Type: "other", Context: true}, // not a code task
		}},
	}
	var b strings.Builder
	if err := writeValidation(&b, found); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(b.String()), " ")
	for _, want := range []string{
		"4 tasks in 2 sessions",
		"claude-code 1 sessions, 2 user messages, 3 edits, 1 checks",
		"copilot-vscode 1 sessions, 5 user messages, 1 edits, 0 checks",
		"Models (sessions using each): claude-test-1 2 lmstudio/qwen3-coder 1",
		"context: 2 tasks with the signal, 1 without",
		"needed a correction 0% 100%",
		"follow-up prompts per task 0.0 3.0",
		"reads and searches before first edit 2.0 8.0",
		"code changes checked or tested 50% 0%",
		"criteria: 0 tasks with the signal, 2 without",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
}

func TestDoctor(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	running := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer running.Close()
	saved := localRuntimes
	defer func() { localRuntimes = saved }()
	localRuntimes = []localRuntime{{"Test runtime", running.URL}, {"Stopped runtime", "http://127.0.0.1:1/"}}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"doctor"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	for _, want := range []string{"nothing is installed or changed", "[ok]      Git", "commit trailers supported",
		"[--]      Claude Code: no sessions found", "Local model runtime: Test runtime running", "Everything required is present."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"doctor"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(out.String(), "[missing] Git (required)") || !strings.Contains(out.String(), "Install: ") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestScanNoGit(t *testing.T) {
	r := testutil.NewRepo(t)
	panics := "package a\n\nfunc f() { panic(1) }\n"
	// Committed by someone else, plus a file never committed: both are read.
	r.Commit("other@example.com", "", map[string]string{"a.go": panics})
	if err := os.WriteFile(filepath.Join(r.Dir, "b.go"), []byte(panics), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--no-git", "--json", r.Dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	u := rep.Unversioned
	if rep.Repos != 0 || len(rep.Languages) != 0 || u == nil || u.Projects != 1 || len(u.Languages) == 0 || u.Languages[0].OwnedLines != 6 {
		t.Errorf("--no-git scan = %s", out.String())
	}
}
