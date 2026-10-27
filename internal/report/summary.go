package report

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/rburns629/urfluency/gates"
	"github.com/rburns629/urfluency/internal/sessions"
	"github.com/rburns629/urfluency/rules"
)

// Summary answers whether the user is proficient: ratings for every area the
// scan measured, then what the evidence shows they do well, how they design and
// communicate, and what holds the ratings down. It is built from the rest of the
// report, so it adds no data of its own.
type Summary struct {
	Headline      string   `json:"headline"`
	Overall       *Rating  `json:"overall,omitempty"`
	Strengths     []string `json:"strengths"`
	Design        []string `json:"design_trends"`
	Communication []string `json:"communication"`
	Growth        []string `json:"growth_areas"`
	Ratings       []Rating `json:"ratings"`
	Caveats       []string `json:"caveats"`
}

// Rating is one 0–100 rating with its level. Basis says what it measures:
// "code" ratings judge how well the user writes a language; "experience"
// ratings judge how much they used something; "habits" and "outcomes" judge
// how they work.
type Rating struct {
	Area       string `json:"area"` // language, framework, design, tool, practice, prompting
	Name       string `json:"name"`
	Score      int    `json:"score"`
	Level      string `json:"level"`
	Basis      string `json:"basis"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence"`
	// Verified is false for work in folders without Git, which counts as the
	// user's by declaration.
	Verified bool `json:"verified"`
}

// Communication describes how the user briefs agents: the share of task
// prompts with each signal. Only counts are kept, never prompt text.
type Communication struct {
	Tasks  int                `json:"tasks"`
	Shares map[string]float64 `json:"shares"`
	Traits []string           `json:"traits"`
}

// communicationTraits describe each signal when it is common enough to be a
// habit. Context and criteria are listed but were not found to predict
// outcomes (see DESIGN.md), so none of these are scored.
var communicationTraits = []struct {
	signal, text string
	min          float64
}{
	{"rationale", "Explains the goal or reason behind requests", 0.2},
	{"constraints", "Sets explicit limits on what must not change or happen", 0.25},
	{"criteria", "States expected behavior or when the work is done", 0.3},
	{"context", "Points the agent at specific files, errors, or code", 0.3},
	{"verification", "Asks for tests or checks", 0.2},
}

func buildCommunication(g sessions.Grades) *Communication {
	tasks := 0
	for _, n := range g.Tasks {
		tasks += n
	}
	if tasks < MinTasks {
		return nil
	}
	c := &Communication{Tasks: tasks, Shares: map[string]float64{}, Traits: []string{}}
	for _, t := range communicationTraits {
		share := float64(g.Style[t.signal]) / float64(tasks)
		c.Shares[t.signal] = round2(share)
		if share >= t.min {
			c.Traits = append(c.Traits, fmt.Sprintf("%s (%.0f%% of %d requests)", t.text, share*100, tasks))
		}
	}
	if g.Edits >= 20 && float64(g.Reviewed)/float64(g.Edits) >= 0.05 {
		c.Traits = append(c.Traits, fmt.Sprintf("Reviews agent edits instead of accepting them wholesale (rejected or changed %s of %s)",
			thousands(g.Reviewed), thousands(g.Edits)))
	}
	return c
}

// categoryNames are the scored categories as they read in sentences.
var categoryNames = map[string]string{
	string(rules.Security):            "Security",
	string(rules.TypeSafety):          "Type safety",
	string(rules.CleanCode):           "Clean code",
	string(rules.LanguageProficiency): "Language usage",
}

// Summarize builds the summary from a finished report. Call it last, after the
// optional sections are filled in.
func Summarize(r *Report) {
	s := &Summary{Strengths: []string{}, Design: []string{}, Communication: []string{}, Growth: []string{}, Ratings: []Rating{}, Caveats: []string{}}
	langs := ratedLanguages(r)
	design := combinedDesign(r)
	tools := combinedTools(r)

	// Ratings: languages, frameworks, design areas, tools, ways of working, prompting.
	weighted, lines := 0.0, 0
	for _, l := range langs {
		p := l.lang.Proficiency
		s.Ratings = append(s.Ratings, Rating{Area: "language", Name: l.lang.Language, Score: p.Score, Level: p.Level, Basis: "code",
			Confidence: p.Confidence, Verified: l.verified, Evidence: l.evidence()})
		weighted += float64(p.Score * l.lang.OwnedLines)
		lines += l.lang.OwnedLines
	}
	s.Ratings = append(s.Ratings, frameworkRatings(r)...)
	for _, d := range design {
		s.Ratings = append(s.Ratings, Rating{Area: "design", Name: d.ID, Score: d.Score, Level: d.Level, Basis: "experience",
			Confidence: experienceConfidence(d.Repos), Verified: r.Unversioned == nil,
			Evidence: fmt.Sprintf("%s, %s: %s", count(d.Repos, "repo"), count(d.Files, "file"), libraryNames(d.Libraries, 4))})
	}
	for _, t := range tools {
		if t.Kind == "packages" {
			continue // a manifest shows a language, not a skill
		}
		score := experience(t.Repos, t.Files)
		s.Ratings = append(s.Ratings, Rating{Area: "tool", Name: t.Name, Score: score, Level: level(score), Basis: "experience",
			Confidence: experienceConfidence(t.Repos), Verified: t.Commits > 0,
			Evidence: fmt.Sprintf("%s, %s", count(t.Repos, "repo"), count(t.Files, "file"))})
	}
	practices := practiceRatings(r)
	s.Ratings = append(s.Ratings, practices...)
	prompting := promptingRating(r.Prompting)
	if prompting != nil {
		s.Ratings = append(s.Ratings, *prompting)
	}
	areas := map[string]int{"language": 0, "framework": 1, "design": 2, "tool": 3, "practice": 4, "prompting": 5}
	sort.SliceStable(s.Ratings, func(i, j int) bool {
		if a, b := areas[s.Ratings[i].Area], areas[s.Ratings[j].Area]; a != b {
			return a < b
		}
		return s.Ratings[i].Score > s.Ratings[j].Score
	})
	if lines > 0 {
		score := int(math.Round(weighted / float64(lines)))
		s.Overall = &Rating{Area: "overall", Name: "code", Score: score, Level: level(score), Basis: "code",
			Confidence: linesConfidence(lines), Verified: r.Unversioned == nil,
			Evidence: fmt.Sprintf("%s across %s, weighted by lines", thousands(lines), count(len(langs), "rated language"))}
	}

	// Strengths come first and lead with the strongest evidence.
	s.Strengths = append(s.Strengths, languageStrengths(langs, lines)...)
	if secure := securityStrength(langs); secure != "" {
		s.Strengths = append(s.Strengths, secure)
	}
	if fw := topRated(s.Ratings, "framework", 4); fw != "" {
		s.Strengths = append(s.Strengths, "Framework experience: "+fw)
	}
	if t := topRated(s.Ratings, "tool", 5); t != "" {
		s.Strengths = append(s.Strengths, "Delivery and infrastructure tools: "+t)
	}
	for _, p := range practices {
		if p.Score >= 70 {
			s.Strengths = append(s.Strengths, fmt.Sprintf("%s: %d (%s); %s", sentenceCase(p.Name), p.Score, p.Level, p.Evidence))
		}
	}
	if prompting != nil && prompting.Score >= 70 {
		s.Strengths = append(s.Strengths, fmt.Sprintf("Agent work holds up: %d (%s); %s", prompting.Score, prompting.Level, prompting.Evidence))
	}

	s.Design = designTrends(design, max(r.Repos+projects(r), 1))
	if p := r.Prompting; p != nil && p.Communication != nil {
		s.Communication = append(s.Communication, p.Communication.Traits...)
	}
	s.Growth = growthAreas(r, langs, practices)
	s.Headline = headline(s.Overall, langs, design, max(r.Repos+projects(r), 1))
	s.Caveats = caveats(r, langs)
	r.Summary = s
}

// ratedLanguage is one language's rating across work with and without Git.
// lang holds the combined figures; verified is false when any of its lines are
// declared rather than found in Git history.
type ratedLanguage struct {
	lang     Language
	verified bool
	gitLines int
}

func (l ratedLanguage) evidence() string {
	text := thousands(l.lang.OwnedLines) + " lines"
	if l.gitLines > 0 && l.gitLines < l.lang.OwnedLines {
		text = fmt.Sprintf("%s lines with Git, %s without", thousands(l.gitLines), thousands(l.lang.OwnedLines-l.gitLines))
	} else if l.gitLines == 0 {
		text += " without Git"
	}
	return fmt.Sprintf("%s, idiom range %d%%", text, l.lang.Proficiency.Range)
}

func unversionedLanguages(r *Report) []Language {
	if r.Unversioned == nil {
		return nil
	}
	return r.Unversioned.Languages
}

func projects(r *Report) int {
	if r.Unversioned == nil {
		return 0
	}
	return r.Unversioned.Projects
}

func withUnversioned(ls []Language, verified bool) []ratedLanguage {
	var out []ratedLanguage
	for _, l := range ls {
		out = append(out, ratedLanguage{lang: l, verified: verified})
	}
	return out
}

// ratedLanguages lists the languages with a proficiency score, highest first.
// A language rated both with and without Git is combined, weighted by lines.
func ratedLanguages(r *Report) []ratedLanguage {
	index := map[string]int{}
	var parts [][]ratedLanguage
	for _, l := range append(withUnversioned(r.Languages, true), withUnversioned(unversionedLanguages(r), false)...) {
		if l.lang.Proficiency == nil {
			continue
		}
		i, ok := index[l.lang.Language]
		if !ok {
			i = len(parts)
			index[l.lang.Language] = i
			parts = append(parts, nil)
		}
		parts[i] = append(parts[i], l)
	}
	var out []ratedLanguage
	for _, group := range parts {
		out = append(out, combineLanguage(group))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].lang.Proficiency.Score != out[j].lang.Proficiency.Score {
			return out[i].lang.Proficiency.Score > out[j].lang.Proficiency.Score
		}
		return out[i].lang.OwnedLines > out[j].lang.OwnedLines
	})
	return out
}

// combineLanguage weights scores by lines, takes the idiom range of the larger
// part, and pools idioms and weaknesses.
func combineLanguage(group []ratedLanguage) ratedLanguage {
	if len(group) == 1 {
		l := group[0]
		if l.verified {
			l.gitLines = l.lang.OwnedLines
		}
		return l
	}
	largest := group[0]
	c := ratedLanguage{verified: true}
	c.lang.Language = largest.lang.Language
	score := 0.0
	categories := map[string][2]float64{} // weighted score sum, lines
	idioms := map[string]IdiomResult{}
	for _, l := range group {
		if l.lang.OwnedLines > largest.lang.OwnedLines {
			largest = l
		}
		c.lang.OwnedLines += l.lang.OwnedLines
		if l.verified {
			c.gitLines += l.lang.OwnedLines
		} else {
			c.verified = false
		}
		score += float64(l.lang.Proficiency.Score * l.lang.OwnedLines)
		for name, cs := range l.lang.Proficiency.Categories {
			sum := categories[name]
			categories[name] = [2]float64{sum[0] + float64(cs.Score*l.lang.OwnedLines), sum[1] + float64(l.lang.OwnedLines)}
		}
		for _, id := range l.lang.Idioms {
			sum := idioms[id.ID]
			id.Uses += sum.Uses
			id.Files += sum.Files
			idioms[id.ID] = id
		}
		c.lang.Weaknesses = append(c.lang.Weaknesses, l.lang.Weaknesses...)
	}
	p := &Proficiency{Score: int(math.Round(score / float64(c.lang.OwnedLines))), Range: largest.lang.Proficiency.Range,
		Confidence: linesConfidence(c.lang.OwnedLines), Model: ProficiencyModel, Categories: map[string]CategoryScore{}}
	p.Level = level(p.Score)
	for name, sum := range categories {
		cs := sum[0] / sum[1]
		p.Categories[name] = CategoryScore{Score: int(math.Round(cs)), Band: band(cs)}
	}
	c.lang.Proficiency = p
	for _, id := range idioms {
		c.lang.Idioms = append(c.lang.Idioms, id)
	}
	sort.Slice(c.lang.Idioms, func(i, j int) bool { return c.lang.Idioms[i].Uses > c.lang.Idioms[j].Uses })
	sort.SliceStable(c.lang.Weaknesses, func(i, j int) bool { return c.lang.Weaknesses[i].Count > c.lang.Weaknesses[j].Count })
	return c
}

// frameworkRatings rate each framework once, pooling every language and
// folder that uses it.
func frameworkRatings(r *Report) []Rating {
	type pooled struct {
		repos, files int
		languages    []string
		verified     bool
	}
	byName := map[string]*pooled{}
	var names []string
	for _, l := range append(withUnversioned(r.Languages, true), withUnversioned(unversionedLanguages(r), false)...) {
		for _, f := range l.lang.FrameworkRatings {
			p := byName[f.Name]
			if p == nil {
				p = &pooled{verified: true}
				byName[f.Name] = p
				names = append(names, f.Name)
			}
			p.repos += f.Repos
			p.files += f.Files
			p.verified = p.verified && l.verified
			p.languages = append(p.languages, displayLanguage(l.lang.Language))
		}
	}
	var out []Rating
	for _, name := range names {
		p := byName[name]
		score := experience(p.repos, p.files)
		out = append(out, Rating{Area: "framework", Name: name, Score: score, Level: level(score), Basis: "experience",
			Confidence: experienceConfidence(p.repos), Verified: p.verified,
			Evidence: fmt.Sprintf("%s, %s in %s", count(p.repos, "repo"), count(p.files, "file"), strings.Join(dedupe(p.languages), ", "))})
	}
	return out
}

// combinedDesign adds design areas from folders without Git to those with it.
func combinedDesign(r *Report) []DesignArea {
	if r.Unversioned == nil {
		return r.Design
	}
	index := map[string]int{}
	files := map[string]map[string]int{}
	var out []DesignArea
	for _, d := range append(append([]DesignArea{}, r.Design...), r.Unversioned.Design...) {
		i, ok := index[d.ID]
		if !ok {
			i = len(out)
			index[d.ID] = i
			files[d.ID] = map[string]int{}
			out = append(out, DesignArea{ID: d.ID, Title: d.Title})
		}
		out[i].Repos += d.Repos
		out[i].Files += d.Files
		for _, l := range d.Libraries {
			files[d.ID][l.Name] += l.Files
		}
	}
	for i := range out {
		for name, n := range files[out[i].ID] {
			out[i].Libraries = append(out[i].Libraries, LibraryUse{Name: name, Files: n})
		}
		libs := out[i].Libraries
		sort.Slice(libs, func(a, b int) bool {
			if libs[a].Files != libs[b].Files {
				return libs[a].Files > libs[b].Files
			}
			return libs[a].Name < libs[b].Name
		})
		out[i].Score = areaExperience(out[i].Repos, out[i].Files)
		out[i].Level = level(out[i].Score)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Repos != out[j].Repos {
			return out[i].Repos > out[j].Repos
		}
		return out[i].Files > out[j].Files
	})
	return out
}

// combinedTools adds tools from folders without Git to those with it.
func combinedTools(r *Report) []Tool {
	all := append([]Tool{}, r.Tools...)
	if r.Unversioned != nil {
		all = append(all, r.Unversioned.Tools...)
	}
	byName := map[string]int{}
	var out []Tool
	for _, t := range all {
		i, ok := byName[t.Name]
		if !ok {
			byName[t.Name] = len(out)
			out = append(out, t)
			continue
		}
		out[i].Repos += t.Repos
		out[i].Files += t.Files
		out[i].Commits += t.Commits
	}
	return out
}

func languageStrengths(langs []ratedLanguage, lines int) []string {
	var out []string
	for i, l := range langs {
		p := l.lang.Proficiency
		if i > 0 && (p.Score < 70 || i == 3) {
			break
		}
		lead := "Strongest language"
		if i > 0 {
			lead = "Also strong"
		}
		name := displayLanguage(l.lang.Language)
		if l.gitLines == 0 {
			name += " (without Git)"
		}
		text := fmt.Sprintf("%s: %s, %d (%s) on %s lines", lead, name, p.Score, p.Level, thousands(l.lang.OwnedLines))
		if len(l.lang.Idioms) > 0 {
			var titles []string
			for j, id := range l.lang.Idioms {
				if j == 3 {
					break
				}
				titles = append(titles, fmt.Sprintf("%s (%s)", id.Title, thousands(id.Uses)))
			}
			text += "; uses " + strings.Join(titles, ", ")
		}
		out = append(out, text)
	}
	if len(langs) >= 3 {
		var names []string
		for _, l := range langs {
			names = append(names, displayLanguage(l.lang.Language))
		}
		out = append(out, fmt.Sprintf("Range: rated in %d languages (%s) on %s lines of your own",
			len(langs), strings.Join(names, ", "), thousands(lines)))
	}
	return out
}

// securityStrength names the languages whose security category is strong.
func securityStrength(langs []ratedLanguage) string {
	var names []string
	lines := 0
	for _, l := range langs {
		if c, ok := l.lang.Proficiency.Categories[string(rules.Security)]; ok && c.Score >= 85 {
			names = append(names, displayLanguage(l.lang.Language))
			lines += l.lang.OwnedLines
		}
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("Rarely writes security bugs in %s (%s lines checked)", joinAnd(dedupe(names)), thousands(lines))
}

// topRated lists up to n ratings of an area at Advanced or above.
func topRated(ratings []Rating, area string, n int) string {
	var picked []Rating
	for _, r := range ratings {
		if r.Area == area && r.Score >= 70 {
			picked = append(picked, r)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].Score > picked[j].Score })
	var parts []string
	seen := map[string]bool{}
	for _, r := range picked {
		if len(parts) == n || seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		parts = append(parts, fmt.Sprintf("%s %d (%s; %s)", r.Name, r.Score, r.Level, r.Evidence))
	}
	return strings.Join(parts, ", ")
}

// archetypes are kinds of engineer recognized from design areas that appear
// together; an archetype applies when every one of its areas is substantial.
var archetypes = []struct {
	name  string
	areas []string
}{
	{"cloud integration", []string{"cloud", "integration"}},
	{"backend services", []string{"api", "data"}},
	{"event-driven systems", []string{"messaging", "cloud"}},
	{"platform and infrastructure", []string{"infrastructure"}},
	{"data engineering", []string{"analytics", "data"}},
	{"AI applications", []string{"ai"}},
	{"machine learning", []string{"ml"}},
	{"mobile apps", []string{"mobile"}},
	{"web front ends", []string{"frontend"}},
	{"developer tooling", []string{"cli"}},
}

// substantial is the repositories an area needs to describe the user's work:
// at least three, and at least a tenth of what was scanned.
func substantial(total int) int { return max(3, (total+9)/10) }

func profile(design []DesignArea, total int) []string {
	repos := map[string]int{}
	for _, d := range design {
		repos[d.ID] = d.Repos
	}
	type match struct {
		name  string
		repos int
	}
	var found []match
	for _, a := range archetypes {
		least := math.MaxInt
		for _, area := range a.areas {
			least = min(least, repos[area])
		}
		if least >= substantial(total) {
			found = append(found, match{a.name, least})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].repos > found[j].repos })
	var out []string
	for i, m := range found {
		if i == 3 {
			break
		}
		out = append(out, m.name)
	}
	return out
}

func designTrends(design []DesignArea, total int) []string {
	var out []string
	if p := profile(design, total); len(p) > 0 {
		out = append(out, "Most of your work is "+joinAnd(p))
	}
	for _, d := range design {
		if len(out) == 6 || d.Repos < 2 {
			break
		}
		out = append(out, fmt.Sprintf("%s: %s, %s (%s)", d.Title, count(d.Repos, "repo"), count(d.Files, "file"), libraryNames(d.Libraries, 4)))
	}
	return out
}

func headline(overall *Rating, langs []ratedLanguage, design []DesignArea, total int) string {
	if overall == nil {
		return "Not enough code of yours to rate yet; ratings start at " + strconv.Itoa(MinOwnedLines) + " lines per language."
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s by code (%d/100, %s confidence)", overall.Level, overall.Score, overall.Confidence))
	var strongest []string
	for i, l := range langs {
		if i == 2 || (i > 0 && l.lang.Proficiency.Score < 70) {
			break
		}
		strongest = append(strongest, displayLanguage(l.lang.Language))
	}
	parts = append(parts, "strongest in "+joinAnd(dedupe(strongest)))
	if p := profile(design, total); len(p) > 0 {
		parts = append(parts, "builds "+joinAnd(p))
	}
	return strings.Join(parts, " · ")
}

// practiceRatings rate ways of working from Git history and CI.
func practiceRatings(r *Report) []Rating {
	var out []Rating
	if g := r.Git; g != nil && g.Commits >= 20 {
		score := int(math.Round(100 * (0.4*g.Descriptive + 0.2*g.ShortSubject + 0.4*math.Min(1, g.WithBody/0.3))))
		out = append(out, Rating{Area: "practice", Name: "commit messages", Score: score, Level: level(score), Basis: "habits",
			Confidence: "high", Verified: true,
			Evidence: fmt.Sprintf("%.0f%% descriptive, %.0f%% explained in a body, of %s commits", g.Descriptive*100, g.WithBody*100, thousands(g.Commits))})
	}
	if p := r.Practices; p != nil {
		commits := p.WithTests.AssistedCommits + p.WithTests.OtherCommits
		if commits >= 10 {
			share := float64(p.WithTests.AssistedWithTests+p.WithTests.OtherWithTests) / float64(commits)
			score := int(math.Round(100 * math.Min(1, share/0.5)))
			out = append(out, Rating{Area: "practice", Name: "testing", Score: score, Level: level(score), Basis: "habits",
				Confidence: linesConfidence(commits * 100), Verified: true,
				Evidence: fmt.Sprintf("%.0f%% of %s larger code changes included tests", share*100, thousands(commits))})
		}
		// Each repository's CI scores the share of mandatory gates it runs.
		if len(p.Gates) > 0 {
			sum := 0.0
			for _, g := range p.Gates {
				sum += 1 - float64(len(g.Missing))/float64(len(gates.Mandatory))
			}
			score := int(math.Round(100 * sum / float64(len(p.Gates))))
			out = append(out, Rating{Area: "practice", Name: "quality gates", Score: score, Level: level(score), Basis: "habits",
				Confidence: experienceConfidence(len(p.Gates)), Verified: true,
				Evidence: fmt.Sprintf("CI you wrote in %s runs %d%% of the mandatory gates on average", count(len(p.Gates), "repo"), score)})
		}
	}
	return out
}

// promptingRating rates agent-task outcomes: the share of tasks that met each
// graded prompting rule with enough tasks to judge.
func promptingRating(p *Prompting) *Rating {
	if p == nil {
		return nil
	}
	sum, n, tasks := 0.0, 0, 0
	var parts []string
	for _, r := range p.Rules {
		if r.Applicable < MinTasks {
			continue
		}
		sum += 1 - r.Share
		n++
		tasks = max(tasks, r.Applicable)
		parts = append(parts, fmt.Sprintf("%.0f%% of %s tasks avoided %q", (1-r.Share)*100, thousands(r.Applicable), strings.ToLower(r.Title)))
	}
	if n == 0 {
		return nil
	}
	score := int(math.Round(100 * sum / float64(n)))
	return &Rating{Area: "prompting", Name: "agent task outcomes", Score: score, Level: level(score), Basis: "outcomes",
		Confidence: linesConfidence(tasks * 50), Verified: true, Evidence: strings.Join(parts, "; ")}
}

// growthAreas name what holds the ratings down, most limiting first: weak
// categories shared across languages, then habits, then prompting, then
// exposed secrets.
func growthAreas(r *Report, langs []ratedLanguage, practices []Rating) []string {
	var out []string
	type weak struct {
		category string
		names    []string
		lines    int
		example  string
	}
	byCategory := map[string]*weak{}
	for _, l := range langs {
		for c, cs := range l.lang.Proficiency.Categories {
			if cs.Score >= 50 {
				continue
			}
			w := byCategory[c]
			if w == nil {
				w = &weak{category: c}
				byCategory[c] = w
			}
			w.names = append(w.names, displayLanguage(l.lang.Language))
			w.lines += l.lang.OwnedLines
			for _, rr := range l.lang.Weaknesses {
				if rr.Category == c {
					if w.example == "" {
						w.example = fmt.Sprintf("%s in %s (%s)", strings.ToLower(rr.Title), displayLanguage(l.lang.Language), thousands(rr.Count))
					}
					break
				}
			}
		}
	}
	var weaks []*weak
	for _, w := range byCategory {
		weaks = append(weaks, w)
	}
	sort.Slice(weaks, func(i, j int) bool {
		if weaks[i].lines != weaks[j].lines {
			return weaks[i].lines > weaks[j].lines
		}
		return weaks[i].category < weaks[j].category
	})
	for _, w := range weaks {
		text := fmt.Sprintf("%s holds the score down in %s", categoryNames[w.category], joinAnd(dedupe(w.names)))
		if w.example != "" {
			text += "; most often " + w.example
		}
		out = append(out, text)
	}
	for _, p := range practices {
		if p.Score < 60 {
			out = append(out, fmt.Sprintf("%s: %d (%s); %s", sentenceCase(p.Name), p.Score, p.Level, p.Evidence))
		}
	}
	if p := r.Prompting; p != nil {
		for _, w := range p.Weaknesses {
			out = append(out, fmt.Sprintf("%s: %d of %d agent tasks", w.Title, w.Missed, w.Applicable))
		}
	}
	secrets := 0
	for _, l := range append(withUnversioned(r.Languages, true), withUnversioned(unversionedLanguages(r), false)...) {
		if l.lang.Scanner {
			for _, w := range l.lang.Weaknesses {
				if w.Category == string(rules.Security) {
					secrets += w.Count
				}
			}
		}
	}
	if secrets > 0 {
		out = append(out, fmt.Sprintf("%s in files you wrote; rotate any that are still live", count(secrets, "hard-coded secret")))
	}
	return out
}

func caveats(r *Report, langs []ratedLanguage) []string {
	out := []string{
		"Ratings use model " + ProficiencyModel + ": reasoned thresholds, not yet calibrated against outcomes.",
		"Language ratings measure how you write code; framework, design, and tool ratings measure how much you used them.",
		"Code written to a team's conventions reflects the team as well as you.",
		"Design sense, reviews, and how you work with people are not in code and are not rated.",
	}
	var low []string
	for _, l := range langs {
		if l.lang.Proficiency.Confidence == "low" {
			low = append(low, displayLanguage(l.lang.Language))
		}
	}
	if len(low) > 0 {
		out = append(out, fmt.Sprintf("Low confidence for %s: under 2,000 lines.", joinAnd(dedupe(low))))
	}
	if u := r.Unversioned; u != nil {
		out = append(out, fmt.Sprintf("%s without Git count as yours by declaration.", count(u.Projects, "project")))
	}
	if p := r.Prompting; p != nil && p.Communication != nil {
		out = append(out, "Communication traits come from the wording of your prompts and are not scored.")
	}
	return out
}

func writeSummary(b *strings.Builder, s *Summary) {
	if s == nil {
		return
	}
	fmt.Fprintf(b, "\nSUMMARY\n  %s\n", s.Headline)
	section := func(title, mark string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(b, "  %s\n", title)
		for _, item := range items {
			fmt.Fprintf(b, "    %s %s\n", mark, item)
		}
	}
	section("Strengths", "+", s.Strengths)
	section("Design trends", "•", s.Design)
	section("How you brief agents", "•", s.Communication)
	section("What holds the ratings down", "-", s.Growth)
	if len(s.Ratings) > 0 {
		b.WriteString("  Ratings\n")
		for _, r := range s.Ratings {
			name := r.Name
			fmt.Fprintf(b, "    %-10s %-26s %3d  %-12s %-6s %s\n", r.Area, name, r.Score, r.Level, r.Confidence, r.Evidence)
		}
	}
	section("How far to trust this", "·", s.Caveats)
}

func linesConfidence(lines int) string {
	switch {
	case lines >= 10000:
		return "high"
	case lines >= 2000:
		return "medium"
	}
	return "low"
}

// displayLanguage names a language as people write it.
func displayLanguage(id string) string {
	switch id {
	case "javascript":
		return "JavaScript"
	case "typescript":
		return "TypeScript"
	case "csharp":
		return "C#"
	case "cpp":
		return "C++"
	case "c":
		return "C"
	}
	return sentenceCase(id)
}

func sentenceCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func dedupe(names []string) []string {
	seen := map[string]bool{}
	out := names[:0:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// joinAnd joins names as "a", "a and b", or "a, b, and c".
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// thousands formats n with comma separators, such as 12,345.
func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
