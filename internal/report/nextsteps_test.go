package report

import (
	"fmt"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

// findings returns n findings of rule in distinct files.
func findings(rule string, n int) []analysis.Finding {
	out := make([]analysis.Finding, n)
	for i := range out {
		out[i] = analysis.Finding{Rule: rule, Path: fmt.Sprintf("f%d.go", i), Line: 1}
	}
	return out
}

func TestLanguageSteps(t *testing.T) {
	// 10,000 clean-ish lines with a heavy type-safety problem: type safety caps
	// the score, so fixing it should be the biggest step.
	f := append(findings("go.unchecked-type-assertion", 300), findings("go.discarded-result", 20)...)
	lang := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 10000, Findings: f}, false)
	if lang.Proficiency == nil {
		t.Fatal("not rated")
	}
	steps := languageSteps(lang)
	if len(steps) == 0 {
		t.Fatal("no steps")
	}
	byRule := map[string]Step{}
	for _, s := range steps {
		if s.From != lang.Proficiency.Score {
			t.Errorf("%s starts at %d, want the published %d", s.Rule, s.From, lang.Proficiency.Score)
		}
		if s.To-s.From < minStepGain {
			t.Errorf("%s gains %d, under the minimum", s.Rule, s.To-s.From)
		}
		byRule[s.Rule] = s
	}
	fix, ok := byRule["go.unchecked-type-assertion"]
	if !ok {
		t.Fatalf("no step for the capping rule: %+v", steps)
	}
	// The step must predict exactly what the scorer gives once fixed.
	fixed := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 10000, Findings: findings("go.discarded-result", 20)}, false)
	if fix.To != fixed.Proficiency.Score {
		t.Errorf("predicted %d after the fix, scorer gives %d", fix.To, fixed.Proficiency.Score)
	}
	if small, ok := byRule["go.discarded-result"]; ok && small.To-small.From >= fix.To-fix.From {
		t.Errorf("2 findings per 1,000 lines gains %d, at least the capping rule's %d", small.To-small.From, fix.To-fix.From)
	}
}

func TestIdiomStepPredictsTheScore(t *testing.T) {
	lang := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 10000}, false)
	var idiom *Step
	for _, s := range languageSteps(lang) {
		if s.Kind == "idiom" {
			idiom = &s
			break
		}
	}
	if idiom == nil {
		t.Fatal("no idiom step for code with no idioms")
	}
	used := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 10000,
		Idioms: map[string]analysis.IdiomUse{idiom.Rule: {Uses: minIdiomUses, Files: minIdiomFiles}}}, false)
	if idiom.To != used.Proficiency.Score {
		t.Errorf("predicted %d with one more idiom, scorer gives %d", idiom.To, used.Proficiency.Score)
	}
}

func TestLowConfidenceLanguagesGetNoCodeSteps(t *testing.T) {
	// Under 2,000 lines the score is capped at Beginner, so no fix can raise it.
	lang := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 1000, Findings: findings("go.unchecked-type-assertion", 30)}, false)
	for _, s := range languageSteps(lang) {
		t.Errorf("step for a capped language: %+v", s)
	}
}

func TestHabitSteps(t *testing.T) {
	r := &Report{
		Git: &Git{Commits: 100, Descriptive: 0.9, ShortSubject: 1, WithBody: 0.05},
		Practices: &Practices{
			WithTests: TestHabits{OtherCommits: 100, OtherWithTests: 10},
			Gates:     []RepoGates{{Missing: []string{"tests", "lint"}}, {Missing: []string{"tests"}}},
		},
	}
	got := map[string]Step{}
	for _, s := range habitSteps(r) {
		got[s.Name+"/"+s.Action] = s
	}
	testing_ := got["testing/Commit tests in the same change as the code they cover"]
	if testing_.From != 20 || testing_.To != 40 {
		t.Errorf("testing step = %+v, want 20 -> 40 (the next level)", testing_)
	}
	tests := got["quality gates/Run the tests in CI"]
	if tests.From != 70 || tests.To != 90 {
		t.Errorf("tests gate step = %+v, want 70 -> 90", tests)
	}
	body := got["commit messages/Explain why in a commit body, not just what in the subject"]
	if body.To-body.From < 30 {
		t.Errorf("commit body step = %+v, want a large gain", body)
	}
}

func TestNextStepsAreRankedAndCapped(t *testing.T) {
	f := append(findings("go.unchecked-type-assertion", 300), findings("go.panic-outside-main", 300)...)
	f = append(f, findings("go.switch-empty-case", 300)...)
	lang := buildLanguage(analysis.LanguageResult{Language: "go", OwnedLines: 10000, Findings: f}, false)
	steps := nextSteps([]ratedLanguage{{lang: lang}}, &Report{})
	if len(steps) == 0 || len(steps) > maxStepsPerRating {
		t.Fatalf("got %d steps for one language, want 1 to %d", len(steps), maxStepsPerRating)
	}
	for i := 1; i < len(steps); i++ {
		if steps[i].To-steps[i].From > steps[i-1].To-steps[i-1].From {
			t.Errorf("not ranked by gain: %+v", steps)
		}
	}
}
