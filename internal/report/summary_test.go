package report

import (
	"strings"
	"testing"
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
	writeSummary(&b, s)
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

func TestRatedLanguagesCombineGitAndFolders(t *testing.T) {
	ts := func(lines, score int) Language {
		return Language{Language: "typescript", OwnedLines: lines, Proficiency: &Proficiency{Score: score, Range: score,
			Categories: map[string]CategoryScore{"type-safety": {Score: score}}}}
	}
	r := Report{Languages: []Language{ts(30000, 60)}, Unversioned: &Unversioned{Projects: 2, Languages: []Language{ts(10000, 100)}}}
	langs := ratedLanguages(&r)
	if len(langs) != 1 {
		t.Fatalf("got %d entries, want one combined TypeScript", len(langs))
	}
	l := langs[0]
	if p := l.lang.Proficiency; p.Score != 70 || p.Level != "Advanced" || p.Range != 60 || p.Categories["type-safety"].Score != 70 || l.verified || l.gitLines != 30000 {
		t.Errorf("combined = %+v verified=%v gitLines=%d, want 70 Advanced, range of the larger part, declared", p, l.verified, l.gitLines)
	}
	if got := l.evidence(); got != "30,000 lines with Git, 10,000 without, idiom range 60%" {
		t.Errorf("evidence = %q", got)
	}
}
