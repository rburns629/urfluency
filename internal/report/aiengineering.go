package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/gates"
)

// AIEngineering answers one question: does the user's agent-assisted work hold up
// as well as their own? Using agents is never a weakness; assisted code that holds
// up worse than the user's own work is.
type AIEngineering struct {
	Strengths  []string        `json:"strengths"`
	Weaknesses []AIWeakness    `json:"weaknesses"`
	Engagement *ReviewActivity `json:"review_engagement,omitempty"`
}

// AIWeakness is something to tighten, with the evidence and what to practice.
type AIWeakness struct {
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Focus    string `json:"focus"`
	Standard string `json:"standard,omitempty"`
}

// ReviewActivity counts agent edits the user rejected or changed before accepting.
// It is shown, not judged, until it passes validation.
type ReviewActivity struct {
	Proposed int     `json:"edits_proposed"`
	Reviewed int     `json:"edits_rejected_or_changed"`
	Share    float64 `json:"share"`
}

// Parity thresholds: assisted code matches the user's bar when its findings rate
// is within 10% of theirs, and falls short when it is at least 1.5 times theirs
// and at least one finding per 1,000 lines higher.
const (
	parityTolerance = 1.1
	parityGap       = 1.5
	parityMinDelta  = 1.0
)

// BuildAIEngineering combines quality parity per language, durability, prompting
// outcomes, and review activity from a built report.
func BuildAIEngineering(r Report) *AIEngineering {
	e := &AIEngineering{Strengths: []string{}, Weaknesses: []AIWeakness{}}
	for _, l := range r.Languages {
		e.addParity(l)
	}
	if r.AI != nil {
		e.addDurability(r.AI.Durability)
	}
	if p := r.Prompting; p != nil {
		e.Strengths = append(e.Strengths, p.Strengths...)
		for _, w := range p.Weaknesses {
			e.Weaknesses = append(e.Weaknesses, AIWeakness{
				Title:    w.Title,
				Detail:   fmt.Sprintf("%d of %d tasks (%.0f%%). %s", w.Missed, w.Applicable, w.Share*100, w.Why),
				Focus:    w.Focus,
				Standard: w.Standard,
			})
		}
		if p.Edits > 0 {
			e.Engagement = &ReviewActivity{Proposed: p.Edits, Reviewed: p.Reviewed, Share: round4(float64(p.Reviewed) / float64(p.Edits))}
		}
	}
	return e
}

// addParity compares a language's AI-assisted lines with the user's other lines.
// Both need enough volume; scanners such as secrets are not languages.
func (e *AIEngineering) addParity(l Language) {
	a := l.Authorship
	if a == nil || l.Language == "secrets" || a.AssistedLines < MinOwnedLines || l.OwnedLines-a.AssistedLines < MinOwnedLines {
		return
	}
	name := languageName(l.Language)
	switch {
	case a.AssistedFindingsPer <= a.OtherFindingsPer*parityTolerance:
		e.Strengths = append(e.Strengths, fmt.Sprintf(
			"Agent-assisted %s meets your own bar: %.2f findings per 1,000 lines against %.2f in your own code", name, a.AssistedFindingsPer, a.OtherFindingsPer))
	case a.AssistedFindingsPer >= a.OtherFindingsPer*parityGap && a.AssistedFindingsPer-a.OtherFindingsPer >= parityMinDelta:
		detail := fmt.Sprintf("%.2f findings per 1,000 AI-assisted lines against %.2f in your own code", a.AssistedFindingsPer, a.OtherFindingsPer)
		if top := concentratedRules(l); top != "" {
			detail += ", mostly " + top
		}
		e.Weaknesses = append(e.Weaknesses, AIWeakness{
			Title:    fmt.Sprintf("Agent-assisted %s falls below your own bar", name),
			Detail:   detail + ".",
			Focus:    "Review agent changes for these rules before committing, and add them to the project's linters and agent instructions so the agent stops producing them.",
			Standard: "https://github.com/rburns629/clean-agent-code",
		})
	}
}

// concentratedRules names up to two rules whose findings sit mostly on
// AI-assisted lines, with their counts.
func concentratedRules(l Language) string {
	type ruleCount struct {
		title           string
		assisted, total int
	}
	var picked []ruleCount
	for _, w := range l.Weaknesses {
		if w.AssistedCount >= 3 && float64(w.AssistedCount)/float64(w.Count) > l.Authorship.AssistedShare {
			picked = append(picked, ruleCount{w.Title, w.AssistedCount, w.Count})
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].assisted > picked[j].assisted })
	var parts []string
	for i, p := range picked {
		if i == 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d of %d)", strings.ToLower(p.title), p.assisted, p.total))
	}
	return strings.Join(parts, " and ")
}

// minDurabilityLines is the volume each side needs before durability is judged.
const minDurabilityLines = 1000

func (e *AIEngineering) addDurability(d Durability) {
	if d.AssistedAdded < minDurabilityLines || d.OtherAdded < minDurabilityLines {
		return
	}
	switch {
	case d.AssistedRate <= d.OtherFixedRate*parityTolerance:
		e.Strengths = append(e.Strengths, fmt.Sprintf(
			"Agent-assisted code holds up: %.1f%% of its lines needed a fix within %d days, against %.1f%% of yours",
			d.AssistedRate*100, d.WindowDays, d.OtherFixedRate*100))
	case d.AssistedRate >= d.OtherFixedRate*parityGap && d.AssistedRate-d.OtherFixedRate >= 0.005:
		e.Weaknesses = append(e.Weaknesses, AIWeakness{
			Title: "Agent-assisted code needs more fixes",
			Detail: fmt.Sprintf("%.1f%% of AI-assisted lines needed a fix within %d days, against %.1f%% of your own.",
				d.AssistedRate*100, d.WindowDays, d.OtherFixedRate*100),
			Focus: "Ask for tests that cover the change's failure paths, and check edge cases before accepting agent code.",
		})
	}
}

func languageName(id string) string {
	switch id {
	case "javascript":
		return "JavaScript"
	case "typescript":
		return "TypeScript"
	case "python":
		return "Python"
	case "go":
		return "Go"
	case "rust":
		return "Rust"
	case "c":
		return "C"
	case "cpp":
		return "C++"
	case "csharp":
		return "C#"
	case "java":
		return "Java"
	case "kotlin":
		return "Kotlin"
	case "php":
		return "PHP"
	case "ruby":
		return "Ruby"
	case "swift":
		return "Swift"
	case "dart":
		return "Dart"
	case "powershell":
		return "PowerShell"
	}
	return id
}

// writeAIEngineering prints the AI engineering section: share as context, then
// strengths, things to tighten, durability, and review activity.
func writeAIEngineering(b *strings.Builder, r Report) {
	a, p, e := r.AI, r.Prompting, r.AIEngineering
	if a == nil || a.OwnedLines == 0 {
		return
	}
	fmt.Fprintf(b, "\nAI ENGINEERING: how your agent-assisted work holds up\n")
	fmt.Fprintf(b, "  Share (never scored): %d of %d owned code lines (%.0f%%) from commits marked AI-assisted",
		a.MarkedLines, a.OwnedLines, a.MarkedShare*100)
	if len(a.MarkedByTool) > 0 {
		tools := make([]string, 0, len(a.MarkedByTool))
		for t, n := range a.MarkedByTool {
			tools = append(tools, fmt.Sprintf("%s %d", t, n))
		}
		sort.Strings(tools)
		fmt.Fprintf(b, " (%s)", strings.Join(tools, ", "))
	}
	b.WriteString("\n")
	if a.Sessions {
		fmt.Fprintf(b, "    At least %d lines (%.0f%%) were written by an agent in sessions still on this machine.\n", a.AILines, a.Share*100)
	}
	if e != nil && len(e.Strengths) > 0 {
		b.WriteString("  Strengths\n")
		for _, s := range e.Strengths {
			fmt.Fprintf(b, "    + %s\n", s)
		}
	}
	if e != nil && len(e.Weaknesses) > 0 {
		b.WriteString("  To tighten\n")
		for _, w := range e.Weaknesses {
			fmt.Fprintf(b, "    - %s: %s\n      Focus: %s\n", w.Title, w.Detail, w.Focus)
			if w.Standard != "" {
				fmt.Fprintf(b, "      Standard: %s\n", w.Standard)
			}
		}
	}
	if d := a.Durability; d.AssistedAdded+d.OtherAdded > 0 {
		rate := func(r float64, fixed, added int) string {
			if added == 0 {
				return fmt.Sprintf("no commits older than %d days yet", d.WindowDays)
			}
			return fmt.Sprintf("%.1f%% (%d of %d)", r*100, fixed, added)
		}
		fmt.Fprintf(b, "  Lines fixed within %d days of being written: AI-assisted %s; other %s\n",
			d.WindowDays, rate(d.AssistedRate, d.AssistedFixed, d.AssistedAdded), rate(d.OtherFixedRate, d.OtherFixed, d.OtherAdded))
	}
	if p != nil && p.EditTasks > 0 {
		parts := make([]string, 0, len(gates.Named))
		for _, g := range gates.Named {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", g, float64(p.GateTasks[g])*100/float64(p.EditTasks)))
		}
		fmt.Fprintf(b, "  Quality gates agents ran, share of %d tasks with edits: %s (shown, not scored)\n", p.EditTasks, strings.Join(parts, ", "))
	}
	if e != nil && e.Engagement != nil {
		g := e.Engagement
		fmt.Fprintf(b, "  Review: you rejected or changed %d of %d proposed agent edits (%.1f%%) before accepting (shown, not scored)\n",
			g.Reviewed, g.Proposed, g.Share*100)
	}
	if p != nil {
		total := 0
		types := make([]string, 0, len(p.Tasks))
		for t, n := range p.Tasks {
			if t == "other" {
				continue
			}
			types = append(types, fmt.Sprintf("%s %d", t, n))
			total += n
		}
		sort.Strings(types)
		fmt.Fprintf(b, "  Based on %d code-change tasks", total)
		if len(types) > 0 {
			fmt.Fprintf(b, " (%s)", strings.Join(types, ", "))
		}
		fmt.Fprintf(b, " and %d questions or other requests, in %d sessions.\n", p.Tasks["other"], p.Sessions)
		if total == 0 {
			b.WriteString("  No code-change tasks found in sessions for these repositories.\n")
		}
	} else {
		b.WriteString("  Add --sessions to include verification, corrections, and review activity from agent sessions.\n")
	}
}
