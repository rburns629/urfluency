package report

import (
	"fmt"
	"strings"

	"github.com/rburns629/urfluency/internal/analysis"
)

// Practices are engineering habits measured from code and history. They are
// shown but not scored until they are validated against durability.
type Practices struct {
	Tests      []TestRatio `json:"tests_per_code_line"`
	WithTests  TestHabits  `json:"code_changes_with_tests"`
	CIRepos    int         `json:"repositories_with_your_ci"`
	CIRunsWith []CICount   `json:"ci_practices"`
}

// TestRatio is owned test lines per owned code line in one language.
type TestRatio struct {
	Language  string  `json:"language"`
	TestLines int     `json:"test_lines"`
	CodeLines int     `json:"code_lines"`
	Ratio     float64 `json:"ratio"`
}

// TestHabits counts code-changing commits of at least analysis.MinCodeChange
// lines and those that also changed tests.
type TestHabits struct {
	MinLines          int `json:"min_code_lines"`
	AssistedCommits   int `json:"ai_assisted_commits"`
	AssistedWithTests int `json:"ai_assisted_with_tests"`
	OtherCommits      int `json:"other_commits"`
	OtherWithTests    int `json:"other_with_tests"`
}

// CICount is how many of the user's CI configurations run one practice.
type CICount struct {
	Practice string `json:"practice"`
	Repos    int    `json:"repositories"`
}

// BuildPractices summarizes test volume, test habits, and CI from a scan.
func BuildPractices(res analysis.Result) *Practices {
	p := &Practices{Tests: []TestRatio{}, CIRunsWith: []CICount{}, CIRepos: res.CI.Repos,
		WithTests: TestHabits{MinLines: analysis.MinCodeChange,
			AssistedCommits: res.Tests.AssistedCommits, AssistedWithTests: res.Tests.AssistedWithTests,
			OtherCommits: res.Tests.OtherCommits, OtherWithTests: res.Tests.OtherWithTests}}
	for _, lr := range res.Languages {
		code := lr.OwnedLines - lr.TestLines
		if lr.Language == "secrets" || code <= 0 {
			continue
		}
		p.Tests = append(p.Tests, TestRatio{Language: lr.Language, TestLines: lr.TestLines, CodeLines: code,
			Ratio: round2(float64(lr.TestLines) / float64(code))})
	}
	for _, name := range analysis.PracticeNames() {
		if n := res.CI.Practices[name]; n > 0 {
			p.CIRunsWith = append(p.CIRunsWith, CICount{Practice: name, Repos: n})
		}
	}
	return p
}

func writePractices(b *strings.Builder, p *Practices, repos int) {
	if p == nil {
		return
	}
	b.WriteString("\nPRACTICES (measured; shown but not scored until validated against durability)\n")
	if len(p.Tests) > 0 {
		parts := make([]string, 0, len(p.Tests))
		for _, t := range p.Tests {
			parts = append(parts, fmt.Sprintf("%s %.2f (%d test lines for %d code lines)", languageName(t.Language), t.Ratio, t.TestLines, t.CodeLines))
		}
		fmt.Fprintf(b, "  Tests per code line: %s\n", strings.Join(parts, "; "))
	}
	h := p.WithTests
	if h.AssistedCommits+h.OtherCommits > 0 {
		share := func(with, total int) string {
			if total == 0 {
				return "none"
			}
			return fmt.Sprintf("%d of %d commits (%.0f%%)", with, total, float64(with)*100/float64(total))
		}
		fmt.Fprintf(b, "  Code changes of %d+ lines committed with tests: AI-assisted %s; other %s\n",
			h.MinLines, share(h.AssistedWithTests, h.AssistedCommits), share(h.OtherWithTests, h.OtherCommits))
	}
	if repos == 0 {
		repos = 1
	}
	fmt.Fprintf(b, "  CI you helped write: %d of %d repositories", p.CIRepos, repos)
	if len(p.CIRunsWith) > 0 {
		parts := make([]string, 0, len(p.CIRunsWith))
		for _, c := range p.CIRunsWith {
			parts = append(parts, fmt.Sprintf("%s %d", c.Practice, c.Repos))
		}
		fmt.Fprintf(b, "; it runs %s", strings.Join(parts, ", "))
	}
	b.WriteString("\n")
}
