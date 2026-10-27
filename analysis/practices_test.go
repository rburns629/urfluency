package analysis_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestIsTestPath(t *testing.T) {
	tests := map[string]bool{
		"a/b_test.go": true, "test_api.py": true, "api_test.py": true, "conftest.py": true,
		"src/App.test.tsx": true, "src/api.spec.ts": true, "src/__tests__/x.js": true, "tests/helpers.py": true,
		"a/b.go": false, "src/testing.ts": false, "contest.py": false, "src/latest/x.ts": false,
	}
	for p, want := range tests {
		if got := analysis.IsTestPath(p); got != want {
			t.Errorf("IsTestPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestScanMeasuresPractices(t *testing.T) {
	r := testutil.NewRepo(t)
	lines := func(n int, prefix string) string {
		var b strings.Builder
		b.WriteString("package a\n")
		for i := 0; i < n; i++ {
			b.WriteString("var " + prefix + string(rune('a'+i)) + " = 1\n")
		}
		return b.String()
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(r.Dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An AI-marked commit with tests, a hand-written one without, and one too
	// small to need tests.
	write("a.go", lines(15, "x"))
	write("a_test.go", "package a\n")
	r.Git("me@example.com", "2025-02-01T00:00:00Z", "add", "-A")
	r.Git("me@example.com", "2025-02-01T00:00:00Z", "commit", "-qm", "Add a\n\nCo-Authored-By: Claude <noreply@anthropic.com>")
	r.Commit("me@example.com", "2025-02-02T00:00:00Z", map[string]string{"b.go": lines(12, "y")})
	r.Commit("me@example.com", "2025-02-03T00:00:00Z", map[string]string{"c.go": lines(2, "z")})
	// CI the user wrote, delegating to the Makefile.
	r.Commit("me@example.com", "2025-02-04T00:00:00Z", map[string]string{
		".github/workflows/ci.yml": "jobs:\n  check:\n    steps:\n      - run: go test ./...\n      - run: make check\n",
		"Makefile":                 "check:\n\tgolangci-lint run\n\tgovulncheck ./...\n",
		"AGENTS.md":                "# Agents\n\nRun the tests before finishing. Never read .env files.\n",
	})

	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (analysis.TestHabits{AssistedCommits: 1, AssistedWithTests: 1, OtherCommits: 1}); res.Tests != want {
		t.Errorf("tests = %+v, want %+v", res.Tests, want)
	}
	if len(res.PerRepo) != 1 {
		t.Fatalf("per repo = %+v", res.PerRepo)
	}
	rp := res.PerRepo[0]
	if !rp.CIOwned || !reflect.DeepEqual(rp.CIGates, []string{"lint", "tests", "security"}) {
		t.Errorf("ci = %v %v, want lint, tests, security", rp.CIOwned, rp.CIGates)
	}
	if !rp.InstructionsOwned || !reflect.DeepEqual(rp.InstructionTopics, []string{"verification", "secrets"}) {
		t.Errorf("instructions = %v %v", rp.InstructionsOwned, rp.InstructionTopics)
	}
	if !reflect.DeepEqual(rp.Languages, []string{"go"}) || rp.AssistedLines != 17 || rp.Repo() != "this repository" {
		t.Errorf("repo = %+v", rp)
	}
	if g := res.Languages[0]; g.Language != "go" || g.TestLines != 1 {
		t.Errorf("go = %+v, want 1 owned test line", g)
	}
}

func TestDependencyDirs(t *testing.T) {
	for _, d := range []string{"node_modules", ".venv", "Pods", "vendor", ".terraform", "site-packages"} {
		if !analysis.IsDependencyDir(d) {
			t.Errorf("%s should be a dependency directory", d)
		}
	}
	for _, d := range []string{"packages", "build", "target", "src", "lib"} {
		if analysis.IsDependencyDir(d) {
			t.Errorf("%s may hold the user's own code and should not be skipped", d)
		}
	}
}

func TestScanSkipsCommittedDependencies(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{
		"cmd/app.go":                   "package main\n",
		".venv/lib/site-packages/x.go": "package x\n",
		"ios/Pods/Lib/a.go":            "package lib\n",
		"web/node_modules/pad/i.go":    "package pad\n",
		"packages/ui/button.go":        "package ui\n",
	})
	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir}, analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if g := res.Languages[0]; g.Language != "go" || g.Files != 2 {
		t.Errorf("go = %+v; dependencies should be skipped and packages/ kept (2 files)", g)
	}
}
