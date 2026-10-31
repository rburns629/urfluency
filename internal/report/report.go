// Package report turns scan results into strengths, weaknesses, and focus points.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/internal/sessions"
	"github.com/rburns629/urfluency/rules"
)

// FormatVersion identifies the report schema.
const FormatVersion = "0.4"

// MinOwnedLines is the volume below which a language shows "not enough data".
const MinOwnedLines = 300

// MinTasks is the number of applicable tasks below which a prompting rule is not
// judged a strength or weakness.
const MinTasks = 5

// Report is the local result of a scan. Locations are included only on request
// and are never part of an upload.
type Report struct {
	Format  string `json:"format"`
	Catalog string `json:"catalog"`
	Commit  string `json:"commit"`
	Repos   int    `json:"repositories"`
	// SkippedSlow counts files skipped because attributing them took too long.
	SkippedSlow int `json:"files_skipped_too_slow,omitempty"`
	// Dismissed counts findings the user dismissed with urfluency dismiss.
	Dismissed int      `json:"findings_dismissed,omitempty"`
	Since     string   `json:"since"`
	Summary   *Summary `json:"summary,omitempty"`
	// Combined is the final result: all work with and without Git, scored as
	// one. Languages, Tools, Design, and Unversioned are its traceable parts.
	Combined      *Combined      `json:"combined,omitempty"`
	Languages     []Language     `json:"languages"`
	Prompting     *Prompting     `json:"prompting,omitempty"`
	AI            *AIShare       `json:"ai_written,omitempty"`
	AIEngineering *AIEngineering `json:"ai_engineering,omitempty"`
	Practices     *Practices     `json:"practices,omitempty"`
	Tools         []Tool         `json:"tools"`
	Design        []DesignArea   `json:"design"`
	Platforms     []Platform     `json:"platforms"`
	Git           *Git           `json:"git,omitempty"`
	// Unversioned covers folders without Git, kept apart because their
	// authorship is declared rather than recorded.
	Unversioned *Unversioned `json:"unversioned,omitempty"`
	// Vulnerabilities are known vulnerabilities in locked dependencies and
	// pinned runtimes, for the person who ran the scan; never uploaded.
	Vulnerabilities *Vulnerabilities `json:"vulnerabilities,omitempty"`
}

// Prompting grades task prompts from local agent sessions.
type Prompting struct {
	Sessions   int            `json:"sessions"`
	Tasks      map[string]int `json:"tasks_by_type"`
	Rules      []PromptRule   `json:"rules"`
	Weaknesses []PromptRule   `json:"weaknesses"`
	Strengths  []string       `json:"strengths"`
	Focus      []string       `json:"focus"`
	Edits      int            `json:"agent_edits_proposed"`
	Reviewed   int            `json:"agent_edits_rejected_or_changed"`
	// EditTasks are agent tasks with edits; GateTasks counts those that ran each
	// quality gate.
	EditTasks int `json:"tasks_with_edits"`
	// Communication describes how the user briefs agents; it is not scored.
	Communication *Communication `json:"communication,omitempty"`
	GateTasks     map[string]int `json:"tasks_running_gate"`
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
	Standard   string  `json:"standard,omitempty"`
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
	Language   string   `json:"language"`
	Frameworks []string `json:"frameworks"`
	// FrameworkRatings rate experience with each framework.
	FrameworkRatings []FrameworkRating `json:"framework_ratings"`
	Files            int               `json:"files"`
	OwnedLines       int               `json:"owned_lines"`
	Sufficient       bool              `json:"sufficient_data"`
	// Scanner rows, such as secrets, check every text file: Files is how many
	// were checked, and they have no owned lines or per-line rates.
	Scanner    bool           `json:"scanner,omitempty"`
	Categories map[string]int `json:"findings_by_category"`
	Weaknesses []RuleResult   `json:"weaknesses"`
	Strengths  []string       `json:"strengths"`
	Focus      []string       `json:"focus"`
	// Proficiency scores the language from idiom range and category rates; it is
	// absent with too little owned code. Idioms are the idioms demonstrated.
	Proficiency *Proficiency  `json:"proficiency,omitempty"`
	Idioms      []IdiomResult `json:"idioms"`
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
	ID       string `json:"id"`
	Category string `json:"category"`
	Title    string `json:"title"`
	// Framework names the framework a rule checks, such as "react", if any.
	Framework string   `json:"framework,omitempty"`
	Count     int      `json:"count"`
	PerKLines float64  `json:"per_1000_lines"`
	Why       string   `json:"why"`
	Focus     string   `json:"focus"`
	Locations []string `json:"locations,omitempty"`
	// AssistedCount is how many of Count are on AI-assisted lines.
	AssistedCount int `json:"ai_assisted_count"`
	// Standard links the clean-agent-code document behind the rule, if any.
	Standard string `json:"standard,omitempty"`
}

// Unversioned reports findings in project folders without Git history. Every
// line counts as the user's, so it is declared rather than verified authorship.
type Unversioned struct {
	Projects  int          `json:"projects"`
	Languages []Language   `json:"languages"`
	Tools     []Tool       `json:"tools"`
	Design    []DesignArea `json:"design"`
	Platforms []Platform   `json:"platforms"`
}

// Combined is all the user's work, with and without Git, scored as one, so a
// language is rated on every line of it rather than averaged from its parts.
type Combined struct {
	Repos    int `json:"repositories"`
	Projects int `json:"projects_without_git"`
	// VerifiedShare is the share of owned code lines traced to the user's
	// commits; the rest are in folders without Git, yours by declaration.
	VerifiedShare float64      `json:"verified_share"`
	Languages     []Language   `json:"languages"`
	Tools         []Tool       `json:"tools"`
	Design        []DesignArea `json:"design"`
	Platforms     []Platform   `json:"platforms"`
	// Written is what the user's commits added, from Git history only.
	Written *Written `json:"written,omitempty"`
}

// Written is the code the user's commits added, by month, language, and tool,
// split into lines from commits marked AI-assisted and the rest. It counts
// what was written when, including code since rewritten, and is context, never
// scored.
type Written struct {
	HumanLines    int               `json:"human_lines"`
	AssistedLines int               `json:"ai_assisted_lines"`
	AssistedShare float64           `json:"ai_assisted_share"`
	ByTool        map[string]int    `json:"ai_assisted_lines_by_tool"`
	Languages     []WrittenLanguage `json:"languages"`
	// Months runs from the first month with code to the last, with no gaps.
	Months []WrittenMonth `json:"months"`
}

// WrittenMonth is one UTC month of added code lines.
type WrittenMonth struct {
	Month         string `json:"month"` // YYYY-MM
	HumanLines    int    `json:"human_lines"`
	AssistedLines int    `json:"ai_assisted_lines"`
}

// WrittenLanguage is one language's added code lines.
type WrittenLanguage struct {
	Language      string `json:"language"`
	HumanLines    int    `json:"human_lines"`
	AssistedLines int    `json:"ai_assisted_lines"`
}

// BuildWritten reports a result's written lines, or nil when there are none.
func BuildWritten(w analysis.Written) *Written {
	if len(w.Months) == 0 {
		return nil
	}
	out := &Written{ByTool: map[string]int{}, Languages: []WrittenLanguage{}, Months: []WrittenMonth{}}
	for tool, n := range w.Tools {
		out.ByTool[tool] = n
	}
	for lang, l := range w.Languages {
		out.Languages = append(out.Languages, WrittenLanguage{Language: lang, HumanLines: l.Other, AssistedLines: l.Assisted})
		out.HumanLines += l.Other
		out.AssistedLines += l.Assisted
	}
	sort.Slice(out.Languages, func(i, j int) bool {
		a, b := out.Languages[i], out.Languages[j]
		if x, y := a.HumanLines+a.AssistedLines, b.HumanLines+b.AssistedLines; x != y {
			return x > y
		}
		return a.Language < b.Language
	})
	if total := out.HumanLines + out.AssistedLines; total > 0 {
		out.AssistedShare = round2(float64(out.AssistedLines) / float64(total))
	}
	first, last := "", ""
	for m := range w.Months {
		if first == "" || m < first {
			first = m
		}
		if m > last {
			last = m
		}
	}
	start, err := time.Parse("2006-01", first)
	if err != nil {
		return out
	}
	for at := start; at.Format("2006-01") <= last; at = at.AddDate(0, 1, 0) {
		m := at.Format("2006-01")
		l := w.Months[m]
		out.Months = append(out.Months, WrittenMonth{Month: m, HumanLines: l.Other, AssistedLines: l.Assisted})
	}
	return out
}

// BuildCombined merges the Git and folder results and scores them as one.
// Locations are left out: the parts carry them.
func BuildCombined(git, folders analysis.Result) *Combined {
	var parts []analysis.Result
	var labels []string
	if git.Repos > 0 {
		parts, labels = append(parts, git), append(labels, "git")
	}
	if folders.Repos > 0 {
		parts, labels = append(parts, folders), append(labels, "folders")
	}
	c := &Combined{Repos: git.Repos, Projects: folders.Repos, Languages: []Language{}, Tools: []Tool{}, Design: []DesignArea{}, Platforms: []Platform{}}
	if len(parts) == 0 {
		return c
	}
	c.Written = BuildWritten(git.Written)
	all := analysis.Merge(labels, parts)
	c.Tools = BuildTools(all)
	c.Design = BuildDesign(all, c.Tools)
	c.Platforms = BuildPlatforms(all)
	verified, total := 0, 0
	for _, lr := range all.Languages {
		c.Languages = append(c.Languages, buildLanguage(lr, false))
		if !analysis.IsScanner(lr.Language) {
			total += lr.OwnedLines
		}
	}
	for _, lr := range git.Languages {
		if !analysis.IsScanner(lr.Language) {
			verified += lr.OwnedLines
		}
	}
	if total > 0 {
		c.VerifiedShare = round2(float64(verified) / float64(total))
	}
	return c
}

// BuildUnversioned reports a merged result from folders without Git.
func BuildUnversioned(res analysis.Result, withLocations bool) *Unversioned {
	u := &Unversioned{Projects: res.Repos, Languages: []Language{}, Tools: BuildTools(res)}
	u.Design = BuildDesign(res, u.Tools)
	u.Platforms = BuildPlatforms(res)
	for _, lr := range res.Languages {
		u.Languages = append(u.Languages, buildLanguage(lr, withLocations))
	}
	return u
}

// Build creates a report from a scan result.
func Build(res analysis.Result, since time.Time, withLocations bool) Report {
	r := Report{Format: FormatVersion, Catalog: rules.Version, Commit: shortHash(res.Head), Repos: res.Repos, Since: "all", SkippedSlow: res.SkippedSlow, Dismissed: res.Dismissed,
		Languages: []Language{}, Tools: BuildTools(res), Git: BuildGit(res)}
	r.Design = BuildDesign(res, r.Tools)
	r.Platforms = BuildPlatforms(res)
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
		Language:         lr.Language,
		Frameworks:       append([]string{}, lr.Frameworks...),
		FrameworkRatings: buildFrameworkRatings(lr),
		Files:            lr.Files,
		OwnedLines:       lr.OwnedLines,
		Sufficient:       lr.OwnedLines >= MinOwnedLines,
		Categories:       map[string]int{},
		Weaknesses:       []RuleResult{},
		Strengths:        []string{},
		Focus:            []string{},
		Idioms:           []IdiomResult{},
	}
	if analysis.IsScanner(lr.Language) {
		lang.Scanner, lang.OwnedLines = true, 0
	}
	if lr.AssistedLines > 0 && lr.OwnedLines > 0 && !lang.Scanner {
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
			rr = &RuleResult{ID: rule.ID, Category: string(rule.Category), Title: rule.Title, Framework: rule.Framework, Why: rule.Why, Focus: rule.Focus,
				AssistedCount: lr.AssistedByRule[f.Rule], Standard: rule.Standard()}
			byRule[f.Rule] = rr
		}
		rr.Count++
		lang.Categories[rr.Category]++
		if withLocations {
			rr.Locations = append(rr.Locations, fmt.Sprintf("%s:%d", f.Path, f.Line))
		}
	}
	for _, rr := range byRule {
		if lang.OwnedLines > 0 {
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
			if rule.Category != rules.Idiom {
				checked[string(rule.Category)]++
			}
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
	if !lang.Scanner {
		counts := map[rules.Category]int{}
		for c, n := range lang.Categories {
			counts[rules.Category(c)] = n
		}
		var idioms []IdiomResult
		lang.Proficiency, idioms = proficiency(lr, counts)
		lang.Idioms = append(lang.Idioms, idioms...)
	}
	return lang
}

// BuildPrompting turns session grades into strengths, weaknesses, and focus points.
func BuildPrompting(g sessions.Grades) *Prompting {
	p := &Prompting{Sessions: g.Sessions, Tasks: g.Tasks, Rules: []PromptRule{}, Weaknesses: []PromptRule{}, Strengths: []string{}, Focus: []string{},
		Edits: g.Edits, Reviewed: g.Reviewed, EditTasks: g.EditTasks, GateTasks: map[string]int{}}
	for gate, n := range g.GateTasks {
		p.GateTasks[gate] = n
	}
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
			Share: round2(float64(tally.Missed) / float64(tally.Applicable)), Why: rule.Why, Focus: rule.Focus, Standard: rule.Standard()}
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
	p.Communication = buildCommunication(g)
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
	switch {
	case r.Repos == 0:
		scope = "no Git repositories"
	case r.Repos > 1:
		scope = fmt.Sprintf("%d repositories", r.Repos)
	}
	fmt.Fprintf(&b, "urFluency scan · %s · since %s · catalog %s\n", scope, r.Since, r.Catalog)
	if r.SkippedSlow > 0 {
		fmt.Fprintf(&b, "Skipped %d files that took over %s to attribute; they are usually generated or vendored.\n",
			r.SkippedSlow, analysis.BlameTimeout)
	}
	if len(r.Languages) == 0 && r.Repos > 0 {
		where := "this repository"
		if r.Repos > 1 {
			where = fmt.Sprintf("%d repositories", r.Repos)
		}
		when := ""
		if r.Since != "all" {
			when = " since " + r.Since + " (use --since all for older work)"
		}
		fmt.Fprintf(&b, "\nNo lines of yours in supported languages were found in %s%s.\n", where, when)
	}
	if s := r.Summary; s != nil {
		// The full summary closes the report, where a terminal leaves it.
		fmt.Fprintf(&b, "\n%s\n(Summary of all your work at the end.)\n", s.Headline)
	}
	if r.Unversioned != nil && len(r.Languages) > 0 {
		b.WriteString("\nGIT REPOSITORIES (every line traced to your commits)\n")
	}
	for _, l := range r.Languages {
		writeLanguage(&b, l)
	}
	writeAIEngineering(&b, r)
	writePractices(&b, r.Practices, r.Repos)
	writeTools(&b, r.Tools, r.Git)
	writeDesign(&b, "DESIGN (from the libraries your files import)", r.Design)
	writePlatforms(&b, "PLATFORMS (from SDK imports and deploy and infrastructure files; settings and values are not read)", r.Platforms)
	if u := r.Unversioned; u != nil {
		fmt.Fprintf(&b, "\nPROJECTS WITHOUT GIT: %d (authorship declared, not verified)\n", u.Projects)
		b.WriteString("Every line in these folders counts as yours; AI share, durability, and test habits need Git history.\n")
		for _, l := range u.Languages {
			writeLanguage(&b, l)
		}
		if len(u.Tools) > 0 {
			b.WriteString("\n  TOOLS (files present)\n")
			writeToolList(&b, u.Tools)
		}
		writeDesign(&b, "  DESIGN", u.Design)
		writePlatforms(&b, "  PLATFORMS", u.Platforms)
	}
	writeVulnerabilities(&b, r.Vulnerabilities)
	writeSummary(&b, r.Summary, r.Combined)
	b.WriteString(vulnerabilityPointer(r.Vulnerabilities))
	_, err := io.WriteString(w, b.String())
	return err
}

func writeLanguage(b *strings.Builder, l Language) {
	name := strings.ToUpper(l.Language)
	if len(l.Frameworks) > 0 {
		name += " (" + strings.Join(l.Frameworks, ", ") + ")"
	}
	if l.Scanner {
		fmt.Fprintf(b, "\n%s: checked %d files\n", name, l.Files)
	} else {
		fmt.Fprintf(b, "\n%s: %d owned lines in %d files\n", name, l.OwnedLines, l.Files)
	}
	if !l.Sufficient {
		fmt.Fprintf(b, "  Not enough data for strengths (needs %d owned lines).\n", MinOwnedLines)
	}
	writeProficiency(b, l)
	if len(l.FrameworkRatings) > 0 {
		var parts []string
		for _, f := range l.FrameworkRatings {
			parts = append(parts, fmt.Sprintf("%s %d %s (%s, %s)", f.Name, f.Score, f.Level, count(f.Repos, "repo"), count(f.Files, "file")))
		}
		fmt.Fprintf(b, "  Frameworks: %s\n", strings.Join(parts, "; "))
	}
	if a := l.Authorship; a != nil {
		fmt.Fprintf(b, "  AI-assisted: %d lines (%.0f%%). Findings per 1,000 lines: AI-assisted %.2f, other %.2f\n",
			a.AssistedLines, a.AssistedShare*100, a.AssistedFindingsPer, a.OtherFindingsPer)
	}
	if len(l.Strengths) > 0 {
		b.WriteString("  Strengths\n")
		for _, s := range l.Strengths {
			fmt.Fprintf(b, "    + %s\n", s)
		}
	}
	if len(l.Weaknesses) > 0 {
		b.WriteString("  Areas to improve\n")
		for _, w := range l.Weaknesses {
			if l.Scanner {
				fmt.Fprintf(b, "    - %s [%s]: %d\n", w.Title, w.Category, w.Count)
			} else {
				fmt.Fprintf(b, "    - %s [%s]: %d (%.2f per 1,000 lines)\n", w.Title, w.Category, w.Count, w.PerKLines)
			}
			fmt.Fprintf(b, "      Why: %s\n      Focus: %s\n", w.Why, w.Focus)
			if w.Standard != "" {
				fmt.Fprintf(b, "      Standard: %s\n", w.Standard)
			}
			for _, loc := range w.Locations {
				fmt.Fprintf(b, "      at %s\n", loc)
			}
		}
	}
	if len(l.Focus) > 0 {
		fmt.Fprintf(b, "  Points of focus: %s\n", strings.Join(l.Focus, ", "))
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
