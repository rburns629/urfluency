package analysis_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rburns629/urfluency/analysis"
	_ "github.com/rburns629/urfluency/analyzers/golang"
	_ "github.com/rburns629/urfluency/analyzers/secrets"
	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

const theirs = `package a

func theirs() { panic("not mine") }
`

const mine = `package a

func theirs() { panic("not mine") }

func mine() { panic("mine") }
`

func TestScanKeepsOnlyOwnedFindings(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("other@example.com", "", map[string]string{"a/a.go": theirs, "vendor/v/v.go": "package v\nfunc f() { panic(1) }\n"})
	r.Commit("me@example.com", "", map[string]string{"a/a.go": mine, "vendor/v/v.go": "package v\nfunc f() { panic(2) }\n"})

	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir},
		analysis.Options{Emails: []string{"me@example.com"}, Since: time.Time{}})
	if err != nil {
		t.Fatal(err)
	}
	// Analyzers come first, then scanners (secrets).
	if len(res.Languages) != 2 || res.Languages[0].Language != "go" || res.Languages[1].Language != "secrets" {
		t.Fatalf("languages = %+v", res.Languages)
	}
	lr := res.Languages[0]
	if lr.Files != 1 {
		t.Errorf("files = %d, want 1 (vendor skipped)", lr.Files)
	}
	if len(lr.Findings) != 1 || lr.Findings[0].Line != 5 || lr.Findings[0].Rule != "go.panic-outside-main" {
		t.Errorf("findings = %+v, want one panic on line 5", lr.Findings)
	}
}

func TestScannersRunOnSupportedOrIncludedFiles(t *testing.T) {
	// The fake token is assembled at runtime; see the secrets package tests.
	token := "gh" + "p_" + strings.Repeat("z", 36)
	r := testutil.NewRepo(t)
	r.Commit("other@example.com", "", map[string]string{"deploy/config.yaml": "a: 1\ntoken: " + token + "\n", "logo.png": "\x89PNG\x00"})
	r.Commit("me@example.com", "", map[string]string{"deploy/config.yaml": "a: 1\ntoken: " + token + "\nmine: " + token + "\n"})

	secretsOf := func(opts analysis.Options) *analysis.LanguageResult {
		t.Helper()
		res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, opts)
		if err != nil {
			t.Fatal(err)
		}
		for i := range res.Languages {
			if res.Languages[i].Language == "secrets" {
				return &res.Languages[i]
			}
		}
		return nil
	}
	// YAML is not a supported language, so it is ignored unless included.
	if s := secretsOf(analysis.Options{Emails: []string{"me@example.com"}}); s != nil {
		t.Errorf("an unsupported extension was scanned: %+v", s)
	}
	s := secretsOf(analysis.Options{Emails: []string{"me@example.com"}, Extensions: []string{".yaml"}})
	if s == nil || len(s.Findings) != 1 || s.Findings[0].Line != 3 {
		t.Fatalf("want one owned token finding on line 3, got %+v", s)
	}
}

func TestProtectedFilesAreNeverRead(t *testing.T) {
	token := "gh" + "p_" + strings.Repeat("y", 36)
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{
		".env":           "TOKEN=" + token + "\n",
		"app/.env.local": "TOKEN=" + token + "\n",
		"certs/app.pem":  "TOKEN=" + token + "\n",
		"app/config.go":  "package app\n",
	})
	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir},
		analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, lr := range res.Languages {
		if lr.Language == "secrets" && (lr.Files != 1 || len(lr.Findings) != 0) {
			t.Errorf("protected files reached the scanner: %+v", lr)
		}
	}
}

func TestScanRequiresEmails(t *testing.T) {
	if _, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: t.TempDir()}, analysis.Options{}); err == nil {
		t.Error("expected an error without emails")
	}
}

func TestScanAttributesAgentWrittenLines(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a/a.go": mine})
	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{
		Emails:     []string{"me@example.com"},
		AgentLines: map[string]map[string]bool{"a/a.go": {`func mine() { panic("mine") }`: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnedLines != 5 || res.AILines != 1 || res.Findings != 2 || res.AIFindings != 1 {
		t.Errorf("owned %d, ai %d, findings %d, ai findings %d; want 5, 1, 2, 1",
			res.OwnedLines, res.AILines, res.Findings, res.AIFindings)
	}
}

func TestMergeCombinesRepositories(t *testing.T) {
	a := analysis.Result{Head: "a", Repos: 1, OwnedLines: 10, AILines: 2, Languages: []analysis.LanguageResult{
		{Language: "go", Files: 1, OwnedLines: 10, Frameworks: []string{}, Findings: []analysis.Finding{{Rule: "r", Path: "x.go", Line: 1}}}}}
	b := analysis.Result{Head: "b", Repos: 1, OwnedLines: 5, Languages: []analysis.LanguageResult{
		{Language: "typescript", Files: 2, OwnedLines: 5, Frameworks: []string{"react"}},
		{Language: "go", Files: 3, OwnedLines: 0}}}
	m := analysis.Merge([]string{"one", "two"}, []analysis.Result{a, b})
	if m.Head != "" || m.Repos != 2 || m.OwnedLines != 15 || m.AILines != 2 || len(m.Languages) != 2 {
		t.Fatalf("merged = %+v", m)
	}
	if g := m.Languages[0]; g.Language != "go" || g.Files != 4 || g.Findings[0].Path != "one/x.go" {
		t.Errorf("go = %+v", g)
	}
	if ts := m.Languages[1]; ts.Frameworks[0] != "react" {
		t.Errorf("typescript = %+v", ts)
	}
}

func TestScanCountsLinesFromAIMarkedCommits(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a/a.go": "package a\n"})
	if err := os.WriteFile(filepath.Join(r.Dir, "a/a.go"), []byte("package a\n\nfunc f() { panic(1) }\nfunc g() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("me@example.com", "", "commit", "-qam", "Add f and g\n\nCo-Authored-By: Claude Opus <noreply@anthropic.com>")
	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnedLines != 4 || res.MarkedLines != 3 || res.MarkedByTool["claude"] != 3 {
		t.Errorf("owned %d, marked %d, by tool %v; want 4, 3, claude 3", res.OwnedLines, res.MarkedLines, res.MarkedByTool)
	}
	if g := res.Languages[0]; g.Language != "go" || g.AssistedLines != 3 || g.AssistedFindings != 1 || len(g.Findings) != 1 {
		t.Errorf("go = %+v, want 3 AI-assisted lines holding the 1 finding", g)
	}
}

func TestAITools(t *testing.T) {
	tests := []struct {
		commit gitdata.Commit
		want   []string
	}{
		{gitdata.Commit{AuthorName: "Robert", CoAuthors: []string{"Claude Opus 5.5 <noreply@anthropic.com>"}}, []string{"claude"}},
		{gitdata.Commit{AuthorName: "Robert (aider)"}, []string{"aider"}},
		{gitdata.Commit{CoAuthors: []string{"Copilot <198982749+Copilot@users.noreply.github.com>", "Cursor Agent <cursoragent@cursor.com>"}}, []string{"copilot", "cursor"}},
		{gitdata.Commit{CoAuthors: []string{"Codex <codex@openai.com>"}}, []string{"codex"}},
		// People who share a tool's name are not tools.
		{gitdata.Commit{AuthorName: "Claude Monet", CoAuthors: []string{"Claude Shannon <claude@example.com>", "Cursor Smith <cs@example.com>"}}, []string{}},
	}
	for _, tt := range tests {
		if got := analysis.AITools(tt.commit); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("AITools(%+v) = %v, want %v", tt.commit, got, tt.want)
		}
	}
}

func TestScanMeasuresDurability(t *testing.T) {
	r := testutil.NewRepo(t)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n\nfunc f() int { return 1 }\nfunc g() int { return 2 }\n")
	r.Git("me@example.com", "2025-01-01T00:00:00Z", "add", "a.go")
	r.Git("me@example.com", "2025-01-01T00:00:00Z", "commit", "-qm", "Add f and g\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	r.Commit("me@example.com", "2025-01-02T00:00:00Z", map[string]string{"b.go": "package a\n\nfunc h() int { return 3 }\nfunc k() int { return 4 }\n"})
	write("a.go", "package a\n\nfunc f() int { return 10 }\nfunc g() int { return 2 }\n")
	r.Git("other@example.com", "2025-01-20T00:00:00Z", "commit", "-qam", "fix: f returned the wrong value")
	write("b.go", "package a\n\nfunc h() int { return 30 }\nfunc k() int { return 4 }\n")
	r.Git("other@example.com", "2025-09-01T00:00:00Z", "commit", "-qam", "fix: h, months later")

	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	want := analysis.Durability{AssistedAdded: 4, AssistedFixed: 1, OtherAdded: 4, OtherFixed: 0}
	if res.Durability != want {
		t.Errorf("durability = %+v, want %+v (the late fix is outside the window)", res.Durability, want)
	}
}

func TestScanReportsProgress(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a.go": "package a\n", "b.go": "package a\n"})
	var mu sync.Mutex
	var last [2]int
	stages := map[string]bool{}
	_, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{
		Emails: []string{"me@example.com"},
		Progress: func(stage string, done, total int) {
			mu.Lock()
			defer mu.Unlock()
			stages[stage] = true
			if stage == "reading files" && done > last[0] {
				last = [2]int{done, total}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if last != [2]int{2, 2} || !stages["analyzing go"] {
		t.Errorf("last reading-files progress = %v, stages = %v", last, stages)
	}
}

func TestScanSkipsFilesThatTakeTooLongToAttribute(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a.go": "package a\n", "b.go": "package a\n"})
	saved := analysis.BlameTimeout
	analysis.BlameTimeout = time.Nanosecond
	defer func() { analysis.BlameTimeout = saved }()
	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatalf("a slow file should be skipped, not fail the scan: %v", err)
	}
	if res.SkippedSlow != 2 {
		t.Errorf("skipped = %d, want 2", res.SkippedSlow)
	}
}
