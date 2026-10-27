package report

import (
	"fmt"
	"strings"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/gates"
)

// Practices are engineering habits measured from code and history. They are
// shown but not scored until they are validated against durability.
type Practices struct {
	Tests        []TestRatio       `json:"tests_per_code_line"`
	WithTests    TestHabits        `json:"code_changes_with_tests"`
	Gates        []RepoGates       `json:"quality_gates"`
	Instructions InstructionsUsage `json:"agent_instructions"`
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

// RepoGates is the quality-gate coverage of CI the user helped write in one
// repository, against clean-agent-code's QUALITY_GATES.md.
type RepoGates struct {
	Repo     string   `json:"repository"`
	Ran      []string `json:"gates"`
	Missing  []string `json:"missing_mandatory"`
	Security bool     `json:"security_scanning"`
}

// InstructionsUsage counts repositories with agent instructions the user wrote,
// and compares AI-assisted work in repositories with and without them.
type InstructionsUsage struct {
	Repos   int            `json:"repositories"`
	Topics  map[string]int `json:"topics"`
	With    *GroupQuality  `json:"with_instructions,omitempty"`
	Without *GroupQuality  `json:"without_instructions,omitempty"`
}

// GroupQuality is the AI-assisted quality of a group of repositories.
type GroupQuality struct {
	Repos              int     `json:"repositories"`
	AssistedLines      int     `json:"ai_assisted_lines"`
	FindingsPerK       float64 `json:"findings_per_1000_ai_assisted_lines"`
	AssistedCommits    int     `json:"ai_assisted_commits"`
	AssistedTestsShare float64 `json:"ai_assisted_with_tests_share"`
}

// BuildPractices summarizes test volume, test habits, quality gates, and agent
// instructions from a scan.
func BuildPractices(res analysis.Result) *Practices {
	p := &Practices{Tests: []TestRatio{}, Gates: []RepoGates{}, Instructions: InstructionsUsage{Topics: map[string]int{}},
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
	var with, without GroupQuality
	var withFindings, withoutFindings, withTests, withoutTests int
	for _, r := range res.PerRepo {
		if r.CIOwned {
			p.Gates = append(p.Gates, repoGates(r))
		}
		group, findings, tests := &without, &withoutFindings, &withoutTests
		if r.InstructionsOwned {
			p.Instructions.Repos++
			for _, t := range r.InstructionTopics {
				p.Instructions.Topics[t]++
			}
			group, findings, tests = &with, &withFindings, &withTests
		}
		group.Repos++
		group.AssistedLines += r.AssistedLines
		*findings += r.AssistedFindings
		group.AssistedCommits += r.Tests.AssistedCommits
		*tests += r.Tests.AssistedWithTests
	}
	// Compare only when both groups have enough AI-assisted code.
	if with.AssistedLines >= MinOwnedLines && without.AssistedLines >= MinOwnedLines {
		finish := func(g *GroupQuality, findings, tests int) *GroupQuality {
			g.FindingsPerK = round2(float64(findings) * 1000 / float64(g.AssistedLines))
			if g.AssistedCommits > 0 {
				g.AssistedTestsShare = round2(float64(tests) / float64(g.AssistedCommits))
			}
			return g
		}
		p.Instructions.With = finish(&with, withFindings, withTests)
		p.Instructions.Without = finish(&without, withoutFindings, withoutTests)
	}
	return p
}

// repoGates checks a repository's CI against the mandatory gates that apply to
// its languages; compiling Go satisfies type checking and build.
func repoGates(r analysis.RepoPractices) RepoGates {
	ran := map[string]bool{}
	for _, g := range r.CIGates {
		ran[g] = true
	}
	label := r.Repo()
	out := RepoGates{Repo: label, Ran: append([]string{}, r.CIGates...), Missing: []string{}, Security: ran[gates.Security]}
	for _, gate := range gates.Mandatory {
		for _, lang := range r.Languages {
			if gates.Applies(gate, lang) && !gates.Implied(lang, ran)[gate] {
				out.Missing = append(out.Missing, gate)
				break
			}
		}
	}
	return out
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
	fmt.Fprintf(b, "  Quality gates in CI you helped write (%d of %d repositories), per clean-agent-code's QUALITY_GATES.md:\n", len(p.Gates), repos)
	for _, g := range p.Gates {
		status := "all applicable mandatory gates"
		if len(g.Missing) > 0 {
			status = "missing " + strings.Join(g.Missing, ", ")
		}
		security := "no security scanning"
		if g.Security {
			security = "security scanning"
		}
		fmt.Fprintf(b, "    %s: %s; %s\n", g.Repo, status, security)
	}
	in := p.Instructions
	fmt.Fprintf(b, "  Agent instructions you wrote: %d of %d repositories", in.Repos, repos)
	if in.Repos > 0 {
		parts := []string{}
		for _, t := range analysis.InstructionTopics() {
			parts = append(parts, fmt.Sprintf("%s %d", t, in.Topics[t]))
		}
		fmt.Fprintf(b, " (covering %s)", strings.Join(parts, ", "))
	}
	b.WriteString("\n")
	if in.With != nil {
		fmt.Fprintf(b, "    AI-assisted code with instructions vs without: %.2f vs %.2f findings per 1,000 lines; with tests %.0f%% vs %.0f%% of commits\n",
			in.With.FindingsPerK, in.Without.FindingsPerK, in.With.AssistedTestsShare*100, in.Without.AssistedTestsShare*100)
	}
	fmt.Fprintf(b, "  Standards: %s\n", gates.Standard)
}
