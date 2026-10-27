package report

import (
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"

	_ "github.com/rburns629/urfluency/analyzers/golang"
)

func goIdioms() map[string]analysis.IdiomUse {
	all := map[string]analysis.IdiomUse{}
	for _, r := range rules.ForLanguage("go") {
		if r.Category == rules.Idiom {
			all[r.ID] = analysis.IdiomUse{Uses: 5, Files: 3}
		}
	}
	return all
}

func TestProficiency(t *testing.T) {
	tests := []struct {
		name       string
		lines      int
		idioms     map[string]analysis.IdiomUse
		counts     map[rules.Category]int
		score      int
		level      string
		confidence string
	}{
		{"clean code, no idioms", 10000, nil, nil, 65, "Proficient", "high"},
		{"clean code, every idiom", 10000, goIdioms(), nil, 100, "Expert", "high"},
		// Type safety at 70 per 1,000 lines scores 12.5, which caps the language
		// at 42.5 however strong the rest is.
		{"one weak category", 10000, goIdioms(), map[rules.Category]int{rules.TypeSafety: 700}, 43, "Developing", "high"},
		{"one-off idioms do not count", 3000, map[string]analysis.IdiomUse{"go.idiom.generics": {Uses: 2, Files: 1}}, nil, 65, "Proficient", "medium"},
	}
	for _, tc := range tests {
		lr := analysis.LanguageResult{Language: "go", OwnedLines: tc.lines, Idioms: tc.idioms}
		p, _ := proficiency(lr, tc.counts)
		if p == nil || p.Score != tc.score || p.Level != tc.level || p.Confidence != tc.confidence {
			t.Errorf("%s: proficiency = %+v, want %d %s %s", tc.name, p, tc.score, tc.level, tc.confidence)
		}
	}
	if p, _ := proficiency(analysis.LanguageResult{Language: "go", OwnedLines: MinOwnedLines - 1}, nil); p != nil {
		t.Errorf("scored a language with too little code: %+v", p)
	}
}
