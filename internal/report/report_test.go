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
	res := analysis.Result{OwnedLines: 1000, AILines: 250, Findings: 10, AIFindings: 1, MarkedLines: 600, MarkedByTool: map[string]int{"claude": 600}}
	if a := BuildAI(res, false); a.MarkedShare != 0.6 || a.Sessions || a.AILines != 0 {
		t.Errorf("without sessions: %+v", a)
	}
	a := BuildAI(res, true)
	if a.Share != 0.25 || a.AIFindingsPerK != 4 || a.OtherFindingsPerK != 12 || a.MarkedByTool["claude"] != 600 {
		t.Errorf("ai = %+v", a)
	}
	var b strings.Builder
	if err := WriteText(&b, Report{Repos: 2, AI: a, Prompting: BuildPrompting(sessions.Grades{})}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 repositories", "From commits marked AI-assisted: 600 lines (60%): claude 600",
		"local sessions: at least 250 lines (25%)", "not a score", "No code-change tasks"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text missing %q:\n%s", want, b.String())
		}
	}
}
