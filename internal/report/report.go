// Package report turns scan results into strengths, weaknesses, and focus points.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/rules"
	"github.com/rburns629/urfluency/internal/sessions"
)

// FormatVersion identifies the report schema.
const FormatVersion = "0.2"

// MinOwnedLines is the volume below which a language shows "not enough data".
const MinOwnedLines = 300

// MinTasks is the number of applicable tasks below which a prompting rule is not
// judged a strength or weakness.
const MinTasks = 5

// Report is the local result of a scan. Locations are included only on request
// and are never part of an upload.
type Report struct {
	Format    string     `json:"format"`
	Catalog   string     `json:"catalog"`
	Commit    string     `json:"commit"`
	Repos     int        `json:"repositories"`
	Since     string     `json:"since"`
	Languages []Language `json:"languages"`
	Prompting *Prompting `json:"prompting,omitempty"`
	AI        *AIShare   `json:"ai_written,omitempty"`
}

// Prompting grades task prompts from local agent sessions.
type Prompting struct {
	Sessions   int            `json:"sessions"`
	Tasks      map[string]int `json:"tasks_by_type"`
	Rules      []PromptRule   `json:"rules"`
	Weaknesses []PromptRule   `json:"weaknesses"`
	Strengths  []string       `json:"strengths"`
	Focus      []string       `json:"focus"`
}

// PromptRule is one prompting rule and the share of applicable tasks that missed it.
type PromptRule struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Missed     int     `json:"missed"`
	Applicable int     `json:"applicable"`
	Share      float64 `json:"share"`
	Why        string  `json:"why"`
	Focus      string  `json:"focus"`
}

// AIShare is how much owned code came from coding agents. It is context, not a
// score, and never counts against anyone. Sessions give a line-level lower bound;
// commit trailers mark whole commits, so their share runs higher.
type AIShare struct {
	OwnedLines        int            `json:"owned_lines"`
	MarkedLines       int            `json:"lines_from_ai_marked_commits"`
	MarkedShare       float64        `json:"marked_share"`
	MarkedByTool      map[string]int `json:"marked_lines_by_tool"`
	Sessions          bool           `json:"sessions_read"`
	AILines           int            `json:"ai_written_lines,omitempty"`
	Share             float64        `json:"share,omitempty"`
	AIFindingsPerK    float64        `json:"findings_per_1000_ai_lines,omitempty"`
	OtherFindingsPerK float64        `json:"findings_per_1000_other_lines,omitempty"`
	// Durability: lines from commits at least 90 days old that a fix changed
	// within 90 days, for AI-marked commits and the rest.
	Durability Durability `json:"durability"`
}

// Durability compares how often added lines were soon fixed.
type Durability struct {
	WindowDays     int     `json:"window_days"`
	AssistedAdded  int     `json:"ai_assisted_added"`
	AssistedFixed  int     `json:"ai_assisted_fixed"`
	AssistedRate   float64 `json:"ai_assisted_fixed_rate"`
	OtherAdded     int     `json:"other_added"`
	OtherFixed     int     `json:"other_fixed"`
	OtherFixedRate float64 `json:"other_fixed_rate"`
}

// Language summarizes one language.
type Language struct {
	Language   string         `json:"language"`
	Frameworks []string       `json:"frameworks"`
	Files      int            `json:"files"`
	OwnedLines int            `json:"owned_lines"`
	Sufficient bool           `json:"sufficient_data"`
	Categories map[string]int `json:"findings_by_category"`
	Weaknesses []RuleResult   `json:"weaknesses"`
	Strengths  []string       `json:"strengths"`
	Focus      []string       `json:"focus"`
	// Authorship splits the findings rate between AI-assisted and other owned
	// lines, so readers can see whose code a result reflects.
	Authorship *Authorship `json:"authorship,omitempty"`
}

// Authorship compares AI-assisted owned lines with the rest.
type Authorship struct {
	AssistedLines       int     `json:"ai_assisted_lines"`
	AssistedShare       float64 `json:"ai_assisted_share"`
	AssistedFindingsPer float64 `json:"findings_per_1000_ai_assisted_lines"`
	OtherFindingsPer    float64 `json:"findings_per_1000_other_lines"`
}

// RuleResult is one rule with occurrences on the user's lines.
type RuleResult struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Title     string   `json:"title"`
	Count     int      `json:"count"`
	PerKLines float64  `json:"per_1000_lines"`
	Why       string   `json:"why"`
	Focus     string   `json:"focus"`
	Locations []string `json:"locations,omitempty"`
}

// Build creates a report from a scan result.
func Build(res analysis.Result, since time.Time, withLocations bool) Report {
	r := Report{Format: FormatVersion, Catalog: rules.Version, Commit: shortHash(res.Head), Repos: res.Repos, Since: "all"}
	if !since.IsZero() {
		r.Since = since.Format("2006-01-02")
	}
	for _, lr := range res.Languages {
		r.Languages = append(r.Languages, buildLanguage(lr, withLocations))
	}
	return r
}

func buildLanguage(lr analysis.LanguageResult, withLocations bool) Language {
	// Slices start empty, not nil, so JSON consumers always receive arrays.
	lang := Language{
		Language:   lr.Language,
		Frameworks: append([]string{}, lr.Frameworks...),
		Files:      lr.Files,
		OwnedLines: lr.OwnedLines,
		Sufficient: lr.OwnedLines >= MinOwnedLines,
		Categories: map[string]int{},
		Weaknesses: []RuleResult{},
		Strengths:  []string{},
		Focus:      []string{},
	}
	if lr.AssistedLines > 0 && lr.OwnedLines > 0 {
		a := &Authorship{AssistedLines: lr.AssistedLines, AssistedShare: round2(float64(lr.AssistedLines) / float64(lr.OwnedLines))}
		a.AssistedFindingsPer = round2(float64(lr.AssistedFindings) * 1000 / float64(lr.AssistedLines))
		if other := lr.OwnedLines - lr.AssistedLines; other > 0 {
			a.OtherFindingsPer = round2(float64(len(lr.Findings)-lr.AssistedFindings) * 1000 / float64(other))
		}
		lang.Authorship = a
	}
	byRule := map[string]*RuleResult{}
	for _, f := range lr.Findings {
		rule, ok := rules.Lookup(f.Rule)
		if !ok {
			continue
		}
		rr := byRule[f.Rule]
		if rr == nil {
			rr = &RuleResult{ID: rule.ID, Category: string(rule.Category), Title: rule.Title, Why: rule.Why, Focus: rule.Focus}
			byRule[f.Rule] = rr
		}
		rr.Count++
		lang.Categories[rr.Category]++
		if withLocations {
			rr.Locations = append(rr.Locations, fmt.Sprintf("%s:%d", f.Path, f.Line))
		}
	}
	for _, rr := range byRule {
		if lr.OwnedLines > 0 {
			rr.PerKLines = round2(float64(rr.Count) * 1000 / float64(lr.OwnedLines))
		}
		lang.Weaknesses = append(lang.Weaknesses, *rr)
	}
	// Security first, then by count: a leaked key outranks frequent style issues.
	sort.Slice(lang.Weaknesses, func(i, j int) bool {
		si, sj := lang.Weaknesses[i].Category == string(rules.Security), lang.Weaknesses[j].Category == string(rules.Security)
		if si != sj {
			return si
		}
		if lang.Weaknesses[i].Count != lang.Weaknesses[j].Count {
			return lang.Weaknesses[i].Count > lang.Weaknesses[j].Count
		}
		return lang.Weaknesses[i].ID < lang.Weaknesses[j].ID
	})
	for i, w := range lang.Weaknesses {
		if i == 3 {
			break
		}
		lang.Focus = append(lang.Focus, w.ID)
	}
	if lang.Sufficient {
		checked := map[string]int{}
		for _, rule := range rules.ForLanguage(lr.Language) {
			checked[string(rule.Category)]++
		}
		var categories []string
		for c := range checked {
			categories = append(categories, c)
		}
		sort.Strings(categories)
		for _, c := range categories {
			if lang.Categories[c] == 0 {
				lang.Strengths = append(lang.Strengths, fmt.Sprintf(
					"No %s issues in %d owned lines (%d rules checked)", c, lr.OwnedLines, checked[c]))
			}
		}
	}
	return lang
}

// BuildPrompting turns session grades into strengths, weaknesses, and focus points.
func BuildPrompting(g sessions.Grades) *Prompting {
	p := &Prompting{Sessions: g.Sessions, Tasks: g.Tasks, Rules: []PromptRule{}, Weaknesses: []PromptRule{}, Strengths: []string{}, Focus: []string{}}
	ids := make([]string, 0, len(g.Rules))
	for id := range g.Rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rule, ok := rules.Lookup(id)
		tally := g.Rules[id]
		if !ok || tally.Applicable == 0 {
			continue
		}
		pr := PromptRule{ID: id, Title: rule.Title, Missed: tally.Missed, Applicable: tally.Applicable,
			Share: round2(float64(tally.Missed) / float64(tally.Applicable)), Why: rule.Why, Focus: rule.Focus}
		p.Rules = append(p.Rules, pr)
		switch {
		case tally.Applicable < MinTasks:
		case pr.Share >= 0.4:
			p.Weaknesses = append(p.Weaknesses, pr)
		case pr.Share <= 0.15:
			p.Strengths = append(p.Strengths, fmt.Sprintf("Rarely: %s (%d of %d tasks)", strings.ToLower(rule.Title), tally.Missed, tally.Applicable))
		}
	}
	sort.SliceStable(p.Weaknesses, func(i, j int) bool { return p.Weaknesses[i].Share > p.Weaknesses[j].Share })
	for i, w := range p.Weaknesses {
		if i == 3 {
			break
		}
		p.Focus = append(p.Focus, w.ID)
	}
	return p
}

// BuildAI summarizes how much owned code came from coding agents. Session-based
// fields are filled only when sessions were read.
func BuildAI(res analysis.Result, sessionsRead bool) *AIShare {
	a := &AIShare{OwnedLines: res.OwnedLines, MarkedLines: res.MarkedLines, MarkedByTool: map[string]int{}, Sessions: sessionsRead}
	d := res.Durability
	a.Durability = Durability{WindowDays: int(analysis.DurabilityWindow.Hours() / 24),
		AssistedAdded: d.AssistedAdded, AssistedFixed: d.AssistedFixed, OtherAdded: d.OtherAdded, OtherFixed: d.OtherFixed}
	if d.AssistedAdded > 0 {
		a.Durability.AssistedRate = round4(float64(d.AssistedFixed) / float64(d.AssistedAdded))
	}
	if d.OtherAdded > 0 {
		a.Durability.OtherFixedRate = round4(float64(d.OtherFixed) / float64(d.OtherAdded))
	}
	for tool, n := range res.MarkedByTool {
		a.MarkedByTool[tool] = n
	}
	if res.OwnedLines > 0 {
		a.MarkedShare = round2(float64(res.MarkedLines) / float64(res.OwnedLines))
	}
	if !sessionsRead {
		return a
	}
	a.AILines = res.AILines
	if res.OwnedLines > 0 {
		a.Share = round2(float64(res.AILines) / float64(res.OwnedLines))
	}
	if res.AILines > 0 {
		a.AIFindingsPerK = round2(float64(res.AIFindings) * 1000 / float64(res.AILines))
	}
	if other := res.OwnedLines - res.AILines; other > 0 {
		a.OtherFindingsPerK = round2(float64(res.Findings-res.AIFindings) * 1000 / float64(other))
	}
	return a
}

// WriteText prints a human-readable summary.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder
	scope := "commit " + r.Commit
	if r.Repos > 1 {
		scope = fmt.Sprintf("%d repositories", r.Repos)
	}
	fmt.Fprintf(&b, "urFluency scan · %s · since %s · catalog %s\n", scope, r.Since, r.Catalog)
	if len(r.Languages) == 0 {
		b.WriteString("\nNo owned lines in supported languages were found for these authors.\n")
	}
	for _, l := range r.Languages {
		name := strings.ToUpper(l.Language)
		if len(l.Frameworks) > 0 {
			name += " (" + strings.Join(l.Frameworks, ", ") + ")"
		}
		fmt.Fprintf(&b, "\n%s: %d owned lines in %d files\n", name, l.OwnedLines, l.Files)
		if !l.Sufficient {
			fmt.Fprintf(&b, "  Not enough data for strengths (needs %d owned lines).\n", MinOwnedLines)
		}
		if a := l.Authorship; a != nil {
			fmt.Fprintf(&b, "  AI-assisted: %d lines (%.0f%%). Findings per 1,000 lines: AI-assisted %.2f, other %.2f\n",
				a.AssistedLines, a.AssistedShare*100, a.AssistedFindingsPer, a.OtherFindingsPer)
		}
		if len(l.Strengths) > 0 {
			b.WriteString("  Strengths\n")
			for _, s := range l.Strengths {
				fmt.Fprintf(&b, "    + %s\n", s)
			}
		}
		if len(l.Weaknesses) > 0 {
			b.WriteString("  Areas to improve\n")
			for _, w := range l.Weaknesses {
				fmt.Fprintf(&b, "    - %s [%s]: %d (%.2f per 1,000 lines)\n", w.Title, w.Category, w.Count, w.PerKLines)
				fmt.Fprintf(&b, "      Why: %s\n      Focus: %s\n", w.Why, w.Focus)
				for _, loc := range w.Locations {
					fmt.Fprintf(&b, "      at %s\n", loc)
				}
			}
		}
		if len(l.Focus) > 0 {
			fmt.Fprintf(&b, "  Points of focus: %s\n", strings.Join(l.Focus, ", "))
		}
	}
	if p := r.Prompting; p != nil {
		writePrompting(&b, p)
	}
	if a := r.AI; a != nil && a.OwnedLines > 0 {
		fmt.Fprintf(&b, "\nAI-ASSISTED CODE (context only, not a score) in %d owned code lines\n", a.OwnedLines)
		fmt.Fprintf(&b, "  From commits marked AI-assisted: %d lines (%.0f%%)", a.MarkedLines, a.MarkedShare*100)
		if len(a.MarkedByTool) > 0 {
			tools := make([]string, 0, len(a.MarkedByTool))
			for t, n := range a.MarkedByTool {
				tools = append(tools, fmt.Sprintf("%s %d", t, n))
			}
			sort.Strings(tools)
			fmt.Fprintf(&b, ": %s", strings.Join(tools, ", "))
		}
		b.WriteString("\n    A marked commit can also contain your own lines; unmarked commits may still be AI-assisted.\n")
		if d := a.Durability; d.AssistedAdded+d.OtherAdded > 0 {
			rate := func(r float64, fixed, added int) string {
				if added == 0 {
					return fmt.Sprintf("no commits older than %d days yet", d.WindowDays)
				}
				return fmt.Sprintf("%.1f%% (%d of %d)", r*100, fixed, added)
			}
			fmt.Fprintf(&b, "  Lines fixed within %d days of being written: AI-assisted %s; other %s\n",
				d.WindowDays, rate(d.AssistedRate, d.AssistedFixed, d.AssistedAdded), rate(d.OtherFixedRate, d.OtherFixed, d.OtherAdded))
		}
		if a.Sessions {
			fmt.Fprintf(&b, "  Written by an agent in local sessions: at least %d lines (%.0f%%)\n", a.AILines, a.Share*100)
			if a.AILines > 0 {
				fmt.Fprintf(&b, "    Findings per 1,000 lines: agent-written %.2f, other %.2f\n", a.AIFindingsPerK, a.OtherFindingsPerK)
			}
			b.WriteString("    Counts sessions still on this machine; inline completions and pasted code are not included.\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writePrompting(b *strings.Builder, p *Prompting) {
	types := make([]string, 0, len(p.Tasks))
	total := 0
	for t, n := range p.Tasks {
		types = append(types, fmt.Sprintf("%s %d", t, n))
		total += n
	}
	sort.Strings(types)
	fmt.Fprintf(b, "\nPROMPTING: %d tasks in %d sessions", total, p.Sessions)
	if len(types) > 0 {
		fmt.Fprintf(b, " (%s)", strings.Join(types, ", "))
	}
	b.WriteString("\n")
	if total == 0 {
		b.WriteString("  No code-change tasks found in sessions for these repositories.\n")
	}
	if len(p.Strengths) > 0 {
		b.WriteString("  Strengths\n")
		for _, s := range p.Strengths {
			fmt.Fprintf(b, "    + %s\n", s)
		}
	}
	if len(p.Weaknesses) > 0 {
		b.WriteString("  Areas to improve\n")
		for _, w := range p.Weaknesses {
			fmt.Fprintf(b, "    - %s: %d of %d tasks (%.0f%%)\n", w.Title, w.Missed, w.Applicable, w.Share*100)
			fmt.Fprintf(b, "      Why: %s\n      Focus: %s\n", w.Why, w.Focus)
		}
	}
	if len(p.Focus) > 0 {
		fmt.Fprintf(b, "  Points of focus: %s\n", strings.Join(p.Focus, ", "))
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

func round4(v float64) float64 { return float64(int(v*10000+0.5)) / 10000 }
