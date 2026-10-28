package report

import (
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"

	_ "github.com/rburns629/urfluency/analyzers/python"
)

func TestSummarize(t *testing.T) {
	r := Report{Repos: 20,
		Languages: []Language{
			{Language: "python", OwnedLines: 40000, Proficiency: &Proficiency{Score: 78, Level: "Advanced", Confidence: "high",
				Categories: map[string]CategoryScore{"security": {Score: 95}, "type-safety": {Score: 40}}},
				Idioms:           []IdiomResult{{Title: "Dataclasses and typed models", Uses: 300}},
				Weaknesses:       []RuleResult{{Category: "type-safety", Title: "Public functions without type hints", Count: 600}},
				FrameworkRatings: []FrameworkRating{{Name: "flask", Repos: 8, Files: 120, Score: 85, Level: "Expert", Confidence: "high"}}},
			{Language: "go", OwnedLines: 10000, Proficiency: &Proficiency{Score: 50, Level: "Intermediate", Confidence: "high",
				Categories: map[string]CategoryScore{"security": {Score: 100}}}},
			{Language: "secrets", Scanner: true, Weaknesses: []RuleResult{{Category: "security", Count: 3}}},
		},
		Design: []DesignArea{
			{ID: "cloud", Title: "Cloud services", Repos: 12, Files: 200, Score: 95, Libraries: []LibraryUse{{Name: "gcp", Files: 150}}},
			{ID: "integration", Title: "Integrations with other services", Repos: 9, Files: 90, Score: 80},
			{ID: "mobile", Title: "Mobile apps", Repos: 1, Files: 4, Score: 20},
		},
		Git: &Git{Commits: 500, Descriptive: 0.9, ShortSubject: 0.95, WithBody: 0.01},
	}
	Summarize(&r)
	s := r.Summary
	if s.Overall == nil || s.Overall.Score != 72 || s.Overall.Level != "Advanced" {
		t.Errorf("overall = %+v, want 72 Advanced (lines-weighted)", s.Overall)
	}
	if !strings.HasPrefix(s.Strengths[0], "Strongest language: Python, 78 (Advanced)") {
		t.Errorf("strengths should lead with the strongest language: %q", s.Strengths)
	}
	want := []string{"Rarely writes security bugs in Python and Go", "Framework experience: flask 91 (Expert"}
	for _, w := range want {
		if !strings.Contains(strings.Join(s.Strengths, "\n"), w) {
			t.Errorf("strengths missing %q: %q", w, s.Strengths)
		}
	}
	if len(s.Design) == 0 || s.Design[0] != "Most of your work is cloud integration" {
		t.Errorf("design trends = %q, want the cloud integration profile first", s.Design)
	}
	if !strings.Contains(s.Headline, "builds cloud integration") {
		t.Errorf("headline = %q", s.Headline)
	}
	growth := strings.Join(s.Growth, "\n")
	for _, w := range []string{"Type safety holds the score down in Python", "Commit messages", "3 hard-coded secrets"} {
		if !strings.Contains(growth, w) {
			t.Errorf("growth areas missing %q: %q", w, s.Growth)
		}
	}
	var b strings.Builder
	writeSummary(&b, s, nil)
	if text := b.String(); strings.Index(text, "Strengths") > strings.Index(text, "What holds the ratings down") {
		t.Errorf("strengths must print before what holds the ratings down:\n%s", text)
	}
}

func TestExperience(t *testing.T) {
	for _, tc := range []struct{ repos, files, want int }{{0, 0, 0}, {1, 5, 31}, {10, 200, 100}, {40, 1000, 100}} {
		if got := experience(tc.repos, tc.files); got != tc.want {
			t.Errorf("experience(%d, %d) = %d, want %d", tc.repos, tc.files, got, tc.want)
		}
	}
}

func TestCombinedScoresAllWorkAsOne(t *testing.T) {
	python := func(lines int, fw string) analysis.LanguageResult {
		return analysis.LanguageResult{Language: "python", OwnedLines: lines, Frameworks: []string{fw},
			FrameworkUse: map[string]analysis.Usage{fw: {Repos: 2, Files: 10}},
			Findings:     []analysis.Finding{{Rule: "py.bare-except", Path: "a.py", Line: 1}}}
	}
	git := analysis.Result{Repos: 3, Languages: []analysis.LanguageResult{python(30000, "flask")}}
	folders := analysis.Result{Repos: 2, Declared: true, Languages: []analysis.LanguageResult{python(10000, "flask")}}
	c := BuildCombined(git, folders)
	if c.Repos != 3 || c.Projects != 2 || c.VerifiedShare != 0.75 || len(c.Languages) != 1 {
		t.Fatalf("combined = %+v, want 3 repos, 2 projects, 75%% verified, one language", c)
	}
	l := c.Languages[0]
	if l.OwnedLines != 40000 || l.Proficiency == nil || l.Weaknesses[0].Count != 2 || len(l.Weaknesses[0].Locations) != 0 {
		t.Errorf("python = %+v, want 40,000 lines scored together, both findings counted, no locations", l)
	}
	if f := l.FrameworkRatings; len(f) != 1 || f[0].Repos != 4 || f[0].Files != 20 {
		t.Errorf("framework ratings = %+v, want flask pooled over 4 repos and 20 files", f)
	}

	r := Report{Languages: []Language{buildLanguage(git.Languages[0], false)}, Combined: c, Unversioned: &Unversioned{Projects: 2}}
	langs := ratedLanguages(&r, c)
	if len(langs) != 1 || langs[0].verified || langs[0].gitLines != 30000 {
		t.Errorf("rated = %+v, want one python, partly declared, 30,000 lines from Git", langs)
	}
	if got := langs[0].evidence(); !strings.HasPrefix(got, "30,000 lines with Git, 10,000 without") {
		t.Errorf("evidence = %q", got)
	}
}
