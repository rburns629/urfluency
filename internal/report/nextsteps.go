package report

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/gates"
	"github.com/rburns629/urfluency/rules"
)

// Step is one change that would raise a rating, with the rating the scoring
// formulas predict once it is made. Steps are worked out by rerunning the same
// formulas with the change applied, so they name only changes that count.
type Step struct {
	Area   string `json:"area"`           // language or practice
	Name   string `json:"name"`           // the rating it raises, such as "go" or "testing"
	Kind   string `json:"kind"`           // fix, idiom, or habit
	Rule   string `json:"rule,omitempty"` // the rule or idiom, when there is one
	Action string `json:"action"`
	Detail string `json:"detail"`
	From   int    `json:"from"`
	To     int    `json:"to"`
}

const (
	// maxSteps and maxStepsPerRating keep the plan short enough to act on.
	maxSteps          = 6
	maxStepsPerRating = 2
	// minStepGain leaves out changes that would barely move a rating.
	minStepGain = 2
)

// nextSteps ranks the changes that would raise the user's ratings most.
func nextSteps(langs []ratedLanguage, r *Report) []Step {
	var all []Step
	for _, l := range langs {
		all = append(all, languageSteps(l.lang)...)
	}
	all = append(all, habitSteps(r)...)
	sort.SliceStable(all, func(i, j int) bool {
		gi, gj := all[i].To-all[i].From, all[j].To-all[j].From
		if gi != gj {
			return gi > gj
		}
		return all[i].Kind == "fix" && all[j].Kind != "fix" // fixing what is wrong before adding more
	})
	out := []Step{}
	per := map[string]int{}
	for _, s := range all {
		key := s.Area + "/" + s.Name
		if len(out) == maxSteps || per[key] == maxStepsPerRating {
			continue
		}
		per[key]++
		out = append(out, s)
	}
	return out
}

// languageSteps simulates fixing each rule's findings and demonstrating one
// more idiom in a rated language.
func languageSteps(lang Language) []Step {
	p := lang.Proficiency
	if p == nil || lang.OwnedLines == 0 {
		return nil
	}
	catalog := 0
	shown := map[string]bool{}
	for _, i := range lang.Idioms {
		shown[i.ID] = true
	}
	var missing []rules.Rule
	for _, r := range rules.ForLanguage(lang.Language) {
		if r.Category == rules.Idiom {
			catalog++
			if !shown[r.ID] {
				missing = append(missing, r)
			}
		}
	}
	if catalog == 0 {
		return nil
	}
	score := func(counts map[string]int, idioms int) int {
		kLines := float64(lang.OwnedLines) / 1000
		var scores []float64
		for c := range p.Categories {
			scores = append(scores, categoryScore(rules.Category(c), float64(counts[c])/kLines))
		}
		s := blend(100*float64(idioms)/float64(catalog), scores)
		if p.Confidence == "low" {
			s = min(s, beginnerMax)
		}
		return int(math.Round(s))
	}
	// Gains are measured from the formula's own baseline and applied to the
	// published score, so rounding cannot invent or hide a point.
	base := score(lang.Categories, len(lang.Idioms))
	name := displayLanguage(lang.Language)
	var out []Step
	for _, w := range lang.Weaknesses {
		if _, scored := p.Categories[w.Category]; !scored {
			continue
		}
		counts := map[string]int{}
		for c, n := range lang.Categories {
			counts[c] = n
		}
		counts[w.Category] -= w.Count
		if gain := score(counts, len(lang.Idioms)) - base; gain >= minStepGain {
			out = append(out, Step{Area: "language", Name: lang.Language, Kind: "fix", Rule: w.ID,
				Action: fmt.Sprintf("%s: %s", w.Title, w.Focus),
				Detail: fmt.Sprintf("%s in %s %s lines", count(w.Count, "finding"), thousands(lang.OwnedLines), name),
				From:   p.Score, To: min(100, p.Score+gain)})
		}
	}
	if len(missing) > 0 {
		if gain := score(lang.Categories, len(lang.Idioms)+1) - base; gain >= minStepGain {
			for _, r := range missing[:min(2, len(missing))] {
				action := r.Focus
				if action == "" {
					action = r.Why
				}
				out = append(out, Step{Area: "language", Name: lang.Language, Kind: "idiom", Rule: r.ID,
					Action: fmt.Sprintf("Use %s where it fits: %s", lowerFirst(r.Title), action),
					Detail: fmt.Sprintf("an idiom counts once used %d times across %d files; %d of %d %s idioms shown so far",
						minIdiomUses, minIdiomFiles, len(lang.Idioms), catalog, name),
					From: p.Score, To: min(100, p.Score+gain)})
			}
		}
	}
	return out
}

// gateNames say what each mandatory CI gate runs.
var gateNames = map[string]string{
	gates.Format: "a formatting check",
	gates.Lint:   "a linter",
	gates.Types:  "a type check",
	gates.Build:  "a build",
	gates.Tests:  "the tests",
}

// habitSteps work out what would raise each ways-of-working rating, with the
// same formulas as practiceRatings.
func habitSteps(r *Report) []Step {
	var out []Step
	add := func(name, action, detail string, from, to int) {
		if to-from >= minStepGain {
			out = append(out, Step{Area: "practice", Name: name, Kind: "habit", Action: action, Detail: detail, From: from, To: to})
		}
	}
	if g := r.Git; g != nil && g.Commits >= 20 {
		score := func(desc, short, body float64) int {
			return int(math.Round(100 * (0.4*desc + 0.2*short + 0.4*math.Min(1, body/0.3))))
		}
		from := score(g.Descriptive, g.ShortSubject, g.WithBody)
		add("commit messages", "Explain why in a commit body, not just what in the subject",
			fmt.Sprintf("%.0f%% of your commits have a body; 30%% earns full credit", g.WithBody*100),
			from, score(g.Descriptive, g.ShortSubject, 0.3))
		add("commit messages", "Write subjects that say what changed, such as \"Retry uploads after a timeout\" rather than \"fix\"",
			fmt.Sprintf("%.0f%% of your subjects are descriptive", g.Descriptive*100),
			from, score(1, g.ShortSubject, g.WithBody))
	}
	p := r.Practices
	if p == nil {
		return out
	}
	if commits := p.WithTests.AssistedCommits + p.WithTests.OtherCommits; commits >= 10 {
		share := float64(p.WithTests.AssistedWithTests+p.WithTests.OtherWithTests) / float64(commits)
		from := int(math.Round(100 * math.Min(1, share/0.5)))
		if next := nextLevel(from); next > 0 {
			add("testing", "Commit tests in the same change as the code they cover",
				fmt.Sprintf("%.0f%% of %s larger code changes included tests; %.0f%% rates %s", share*100, thousands(commits), float64(next)/2, level(next)),
				from, next)
		}
	}
	if len(p.Gates) > 0 {
		missingIn := map[string]int{}
		sum := 0.0
		for _, g := range p.Gates {
			sum += 1 - float64(len(g.Missing))/float64(len(gates.Mandatory))
			for _, m := range g.Missing {
				missingIn[m]++
			}
		}
		from := int(math.Round(100 * sum / float64(len(p.Gates))))
		for _, gate := range gates.Mandatory {
			if n := missingIn[gate]; n > 0 {
				gain := 100 * float64(n) / float64(len(gates.Mandatory)) / float64(len(p.Gates))
				add("quality gates", fmt.Sprintf("Run %s in CI", gateNames[gate]),
					fmt.Sprintf("missing from the CI you wrote in %s", count(n, "repo")),
					from, min(100, from+int(math.Round(gain))))
			}
		}
	}
	return out
}

// nextLevel is the lowest score of the level above score's, or 0 at Expert.
func nextLevel(score int) int {
	for _, t := range []int{40, 70, 85} {
		if score < t {
			return t
		}
	}
	return 0
}

// lowerFirst lowers a title's first letter for use mid-sentence, unless it
// starts an acronym such as HTTP.
func lowerFirst(s string) string {
	if len(s) < 2 || strings.ToUpper(s[1:2]) == s[1:2] {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
