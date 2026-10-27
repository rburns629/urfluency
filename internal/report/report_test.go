package report

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	_ "github.com/rburns629/urfluency/internal/analyzers/golang"
	"github.com/rburns629/urfluency/internal/sessions"
)

func result(owned int, rules ...string) analysis.Result {
	lr := analysis.LanguageResult{Language: "go", Files: 2, OwnedLines: owned}
	for i, r := range rules {
		lr.Findings = append(lr.Findings, analysis.Finding{Rule: r, Path: "a.go", Line: i + 1})
	}
	return analysis.Result{Head: "0123456789abcdef0123", Languages: []analysis.LanguageResult{lr}}
}

func TestBuildRanksWeaknessesAndFindsStrengths(t *testing.T) {
	res := result(500,
		"go.ignored-error", "go.ignored-error", "go.ignored-error",
		"go.unchecked-type-assertion", "go.unchecked-type-assertion",
		"go.panic-outside-main", "unknown.rule")
	r := Build(res, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true)

	if r.Commit != "0123456789ab" || r.Since != "2026-01-01" || r.Format != FormatVersion {
		t.Errorf("header = %+v", r)
	}
	l := r.Languages[0]
	if !l.Sufficient {
		t.Error("500 owned lines should be sufficient")
	}
	if want := []string{"go.ignored-error", "go.unchecked-type-assertion", "go.panic-outside-main"}; !reflect.DeepEqual(l.Focus, want) {
		t.Errorf("focus = %v, want %v", l.Focus, want)
	}
	if w := l.Weaknesses[0]; w.Count != 3 || w.PerKLines != 6 || len(w.Locations) != 3 || w.Why == "" {
		t.Errorf("top weakness = %+v", w)
	}
	if l.Categories["clean-code"] != 4 || l.Categories["type-safety"] != 2 {
		t.Errorf("categories = %v", l.Categories)
	}
	if len(l.Strengths) != 2 || !strings.Contains(l.Strengths[0], "language-proficiency") || !strings.Contains(l.Strengths[1], "security") {
		t.Errorf("strengths = %v, want language-proficiency and security", l.Strengths)
	}
}

func TestBuildWithoutLocationsOrEnoughData(t *testing.T) {
	r := Build(result(100, "go.ignored-error"), time.Now(), false)
	l := r.Languages[0]
	if l.Sufficient || len(l.Strengths) != 0 {
		t.Errorf("100 lines should not produce strengths: %+v", l)
	}
	if len(l.Weaknesses[0].Locations) != 0 {
		t.Error("locations included without request")
	}
}

func TestJSONUsesEmptyArrays(t *testing.T) {
	b, err := json.Marshal(Build(result(100), time.Now(), false))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"weaknesses":[]`, `"strengths":[]`, `"focus":[]`} {
		if !strings.Contains(string(b), field) {
			t.Errorf("JSON missing %s: %s", field, b)
		}
	}
}

func TestWriteText(t *testing.T) {
	var b strings.Builder
	if err := WriteText(&b, Build(result(500, "go.ignored-error"), time.Now(), true)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GO: 500 owned lines", "Strengths", "Areas to improve", "Why:", "Focus:", "at a.go:1", "Points of focus: go.ignored-error"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("output missing %q:\n%s", want, b.String())
		}
	}
	b.Reset()
	if err := WriteText(&b, Report{}); err != nil || !strings.Contains(b.String(), "No owned lines") {
		t.Errorf("empty report output = %q, %v", b.String(), err)
	}
}

func TestBuildSplitsAuthorship(t *testing.T) {
	res := result(1000, "go.ignored-error", "go.ignored-error", "go.ignored-error")
	res.Languages[0].AssistedLines = 400
	res.Languages[0].AssistedFindings = 2
	r := Build(res, time.Time{}, false)
	a := r.Languages[0].Authorship
	if a == nil || a.AssistedShare != 0.4 || a.AssistedFindingsPer != 5 || a.OtherFindingsPer != 1.67 {
		t.Errorf("authorship = %+v", a)
	}
	if r.Since != "all" {
		t.Errorf("since = %q, want all for a zero time", r.Since)
	}
	var b strings.Builder
	if err := WriteText(&b, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "AI-assisted: 400 lines (40%). Findings per 1,000 lines: AI-assisted 5.00, other 1.67") {
		t.Errorf("text:\n%s", b.String())
	}
	if Build(result(500, "go.ignored-error"), time.Time{}, false).Languages[0].Authorship != nil {
		t.Error("no AI-assisted lines should mean no authorship split")
	}
}

func TestBuildPrompting(t *testing.T) {
	g := sessions.Grades{Sessions: 3, Tasks: map[string]int{"feature": 6, "bug": 4}, Rules: map[string]sessions.Tally{
		"prompt.unverified-change": {Applicable: 8, Missed: 5},
		"prompt.needed-correction": {Applicable: 10, Missed: 1},
		"prompt.retired-rule":      {Applicable: 10, Missed: 9}, // not in the catalog, so ignored
	}}
	p := BuildPrompting(g)
	if len(p.Rules) != 2 || len(p.Weaknesses) != 1 || p.Weaknesses[0].ID != "prompt.unverified-change" || p.Weaknesses[0].Share != 0.63 {
		t.Errorf("weaknesses = %+v", p.Weaknesses)
	}
	if len(p.Strengths) != 1 || !strings.Contains(p.Strengths[0], "1 of 10") {
		t.Errorf("strengths = %v", p.Strengths)
	}
	if !reflect.DeepEqual(p.Focus, []string{"prompt.unverified-change"}) {
		t.Errorf("focus = %v", p.Focus)
	}
	small := BuildPrompting(sessions.Grades{Rules: map[string]sessions.Tally{"prompt.unverified-change": {Applicable: 3, Missed: 3}}})
	if len(small.Weaknesses) != 0 {
		t.Errorf("too few tasks should not be judged: %+v", small.Weaknesses)
	}
}

func TestBuildAI(t *testing.T) {
	res := analysis.Result{OwnedLines: 1000, AILines: 250, Findings: 10, AIFindings: 1, MarkedLines: 600, MarkedByTool: map[string]int{"claude": 600},
		Durability: analysis.Durability{AssistedAdded: 400, AssistedFixed: 6, OtherAdded: 800, OtherFixed: 4}}
	if a := BuildAI(res, false); a.MarkedShare != 0.6 || a.Sessions || a.AILines != 0 {
		t.Errorf("without sessions: %+v", a)
	}
	a := BuildAI(res, true)
	if a.Share != 0.25 || a.AIFindingsPerK != 4 || a.OtherFindingsPerK != 12 || a.MarkedByTool["claude"] != 600 ||
		a.Durability.AssistedRate != 0.015 || a.Durability.OtherFixedRate != 0.005 || a.Durability.WindowDays != 90 {
		t.Errorf("ai = %+v", a)
	}
	var b strings.Builder
	if err := WriteText(&b, Report{Repos: 2, AI: a, Prompting: BuildPrompting(sessions.Grades{})}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 repositories", "Share (never scored): 600 of 1000 owned code lines (60%) from commits marked AI-assisted (claude 600)",
		"At least 250 lines (25%) were written by an agent", "No code-change tasks",
		"Lines fixed within 90 days of being written: AI-assisted 1.5% (6 of 400); other 0.5% (4 of 800)"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text missing %q:\n%s", want, b.String())
		}
	}
}

func TestDurabilityWithoutOldEnoughCommits(t *testing.T) {
	a := BuildAI(analysis.Result{OwnedLines: 100, Durability: analysis.Durability{OtherAdded: 50, OtherFixed: 1}}, false)
	var b strings.Builder
	if err := WriteText(&b, Report{AI: a}); err != nil {
		t.Fatal(err)
	}
	if want := "AI-assisted no commits older than 90 days yet; other 2.0% (1 of 50)"; !strings.Contains(b.String(), want) {
		t.Errorf("text missing %q:\n%s", want, b.String())
	}
}

func TestBuildAIEngineering(t *testing.T) {
	ts := Language{Language: "typescript", OwnedLines: 2000,
		Authorship: &Authorship{AssistedLines: 1500, AssistedShare: 0.75, AssistedFindingsPer: 5.4, OtherFindingsPer: 1.4},
		Weaknesses: []RuleResult{
			{Title: "Non-null assertion (x!)", Count: 40, AssistedCount: 34},
			{Title: "Double type assertion", Count: 10, AssistedCount: 10},
			{Title: "Explicit any", Count: 4, AssistedCount: 2}, // not concentrated on AI lines
		}}
	goLang := Language{Language: "go", OwnedLines: 4000,
		Authorship: &Authorship{AssistedLines: 2000, AssistedShare: 0.5, AssistedFindingsPer: 2.9, OtherFindingsPer: 3.3}}
	small := Language{Language: "python", OwnedLines: 400, Authorship: &Authorship{AssistedLines: 380, AssistedFindingsPer: 9}}
	r := Report{
		Languages: []Language{ts, goLang, small},
		AI: &AIShare{OwnedLines: 6400, Durability: Durability{WindowDays: 90,
			AssistedAdded: 5000, AssistedFixed: 50, AssistedRate: 0.01, OtherAdded: 5000, OtherFixed: 60, OtherFixedRate: 0.012}},
		Prompting: &Prompting{Sessions: 4, Tasks: map[string]int{"feature": 9}, Edits: 200, Reviewed: 10,
			EditTasks: 8, GateTasks: map[string]int{"tests": 6, "lint": 2},
			Strengths:  []string{"Rarely: tasks that needed a correction (1 of 9 tasks)"},
			Weaknesses: []PromptRule{{Title: "Agent code changes never tested or checked", Missed: 6, Applicable: 9, Share: 0.67, Why: "w", Focus: "f"}}},
	}
	e := BuildAIEngineering(r)
	strengths := strings.Join(e.Strengths, "\n")
	for _, want := range []string{"Agent-assisted Go meets your own bar: 2.90", "Agent-assisted code holds up: 1.0% of its lines", "Rarely: tasks that needed a correction"} {
		if !strings.Contains(strengths, want) {
			t.Errorf("strengths missing %q:\n%s", want, strengths)
		}
	}
	if len(e.Weaknesses) != 2 || e.Weaknesses[0].Title != "Agent-assisted TypeScript falls below your own bar" ||
		!strings.Contains(e.Weaknesses[0].Detail, "mostly non-null assertion (x!) (34 of 40) and double type assertion (10 of 10)") ||
		e.Weaknesses[1].Title != "Agent code changes never tested or checked" {
		t.Errorf("weaknesses = %+v", e.Weaknesses)
	}
	if e.Engagement == nil || e.Engagement.Share != 0.05 {
		t.Errorf("engagement = %+v", e.Engagement)
	}
	r.AIEngineering = e
	var b strings.Builder
	if err := WriteText(&b, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AI ENGINEERING", "  Strengths", "  To tighten", "rejected or changed 10 of 200 proposed agent edits (5.0%)", "Based on 9 code-change tasks (feature 9) and 0 questions or other requests, in 4 sessions.",
		"Quality gates agents ran, share of 8 tasks with edits: format 0%, lint 25%, types 0%, build 0%, tests 75%, security 0%"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text missing %q:\n%s", want, b.String())
		}
	}
	if strings.Contains(b.String(), "Python") {
		t.Error("a language without a hand-written baseline was judged")
	}
}

func TestBuildPractices(t *testing.T) {
	res := analysis.Result{
		Languages: []analysis.LanguageResult{{Language: "go", OwnedLines: 1500, TestLines: 500}, {Language: "secrets", OwnedLines: 9}},
		Tests:     analysis.TestHabits{AssistedCommits: 10, AssistedWithTests: 8, OtherCommits: 4, OtherWithTests: 1},
		PerRepo: []analysis.RepoPractices{
			{Label: "api", Languages: []string{"go", "typescript"}, CIOwned: true, CIGates: []string{"lint", "tests", "security"},
				InstructionsOwned: true, InstructionTopics: []string{"verification"},
				AssistedLines: 1000, AssistedFindings: 2, Tests: analysis.TestHabits{AssistedCommits: 10, AssistedWithTests: 9}},
			{Label: "web", Languages: []string{"python"}, AssistedLines: 500, AssistedFindings: 4,
				Tests: analysis.TestHabits{AssistedCommits: 4, AssistedWithTests: 1}},
		},
	}
	p := BuildPractices(res)
	// Go's tests imply its type checking and build; TypeScript's do not.
	if len(p.Gates) != 1 || !reflect.DeepEqual(p.Gates[0].Missing, []string{"format", "types", "build"}) || !p.Gates[0].Security {
		t.Errorf("gates = %+v", p.Gates)
	}
	in := p.Instructions
	if in.Repos != 1 || in.With == nil || in.With.FindingsPerK != 2 || in.Without.FindingsPerK != 8 || in.With.AssistedTestsShare != 0.9 {
		t.Errorf("instructions = %+v with %+v without %+v", in, in.With, in.Without)
	}
	var b strings.Builder
	writePractices(&b, p, 2)
	for _, want := range []string{"Tests per code line: Go 0.50 (500 test lines for 1000 code lines)",
		"committed with tests: AI-assisted 8 of 10 commits (80%); other 1 of 4 commits (25%)",
		"api: missing format, types, build; security scanning",
		"Agent instructions you wrote: 1 of 2 repositories (covering verification 1, secrets 0, conventions 0)",
		"2.00 vs 8.00 findings per 1,000 lines; with tests 90% vs 25% of commits",
		"standards/QUALITY_GATES.md"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text missing %q:\n%s", want, b.String())
		}
	}
}
