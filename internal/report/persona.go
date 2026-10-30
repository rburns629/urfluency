package report

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/analysis"
)

// Persona describes the work the ratings show: the strongest areas, the role
// families the evidence fits, and adjacent roles with what is missing for
// each. It is built from the summary's ratings by fixed rules (roleFamilies),
// with no AI service. It says what the work shows, never who the person is:
// no seniority, salary, or personal traits. Profiles show it only when their
// owner publishes it.
type Persona struct {
	Summary string `json:"summary"`
	// Window is the scan's "since": "all", or the first day counted, which
	// bounds every role's timeline.
	Window    string   `json:"window"`
	Strongest []Rating `json:"strongest"`
	Fits      []Role   `json:"fits"`
	Adjacent  []Role   `json:"adjacent"`
}

// Role is one role family and how well the ratings meet its requirements.
// Fit is the mean of the requirements' best ratings. Repos is how widely the
// evidence appears: the fewest repositories behind any requirement with a
// count (design areas, platforms, and tools; languages have none). Code is the
// rating of the languages the role's work is written in, weighted by lines,
// and Since, Last, and ActiveMonths when that work was committed. Roles are
// ranked by strength (see roleStrength), since experience ratings saturate
// for broad work.
type Role struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Fit           int      `json:"fit"`
	Repos         int      `json:"repositories,omitempty"`
	Code          int      `json:"code,omitempty"`
	CodeLanguages []string `json:"code_languages,omitempty"`
	Since         string   `json:"since,omitempty"` // "2006-01"
	Last          string   `json:"last,omitempty"`
	ActiveMonths  int      `json:"active_months,omitempty"`
	Met           []Need   `json:"met"`
	Gaps          []Need   `json:"gaps"`
	strength      float64
}

// Need is one requirement of a role: the best rating that meets it, and for a
// gap, the next step that would raise it when there is one.
type Need struct {
	Title string `json:"title"`
	Best  string `json:"best,omitempty"` // the rating's name; "" when nothing counts yet
	Score int    `json:"score"`
	Next  string `json:"next,omitempty"`
}

// needBar is the rating a requirement needs to count as met: Intermediate.
const needBar = 40

const (
	maxStrongest = 5
	maxFits      = 8
	maxAdjacent  = 3
)

// requirement is one kind of evidence a role needs, met by any of its ratings,
// given as "area:name".
type requirement struct {
	title string
	any   []string
}

// roleFamilies are the published mapping rules: a role fits when every one of
// its requirements is rated Intermediate (40) or higher. It is adjacent when at
// least half are met and its first requirement, the one that defines it, has
// some evidence. Requirements name ratings from the summary: design areas,
// languages, platforms, and tools.
// Infrastructure roles are the platforms that application roles run on; the
// summary names both when the work spans them. Work is how the summary says
// what the role builds.
var roleFamilies = []struct {
	id, title, work string
	infra           bool
	needs           []requirement
}{
	{"backend", "Backend and API engineering", "backend and API services", false, []requirement{
		{"Web APIs and services", []string{"design:api"}},
		{"Databases and storage", []string{"design:data"}},
		{"A server-side language", languages("go", "java", "kotlin", "python", "csharp", "rust", "php", "ruby", "typescript", "javascript")},
	}},
	{"frontend", "Front-end engineering", "web front ends", false, []requirement{
		{"Web front ends", []string{"design:frontend"}},
		{"JavaScript or TypeScript", languages("javascript", "typescript")},
	}},
	{"fullstack", "Full-stack engineering", "full-stack applications", false, []requirement{
		{"Web front ends", []string{"design:frontend"}},
		{"Web APIs and services", []string{"design:api"}},
		{"Databases and storage", []string{"design:data"}},
	}},
	{"mobile", "Mobile engineering", "mobile apps", false, []requirement{
		{"Mobile apps", []string{"design:mobile"}},
		{"A mobile language", languages("swift", "kotlin", "dart", "typescript", "javascript", "java")},
	}},
	{"platform", "Platform and DevOps engineering", "platform engineering", true, []requirement{
		{"Infrastructure as code", []string{"design:infrastructure"}},
		{"Delivery tools", tools("docker", "terraform", "helm", "kustomize", "pulumi", "ansible", "github-actions", "gitlab-ci", "jenkins", "cloud-build", "azure-pipelines", "circleci")},
		{"A cloud platform or Kubernetes", platforms("aws", "gcp", "azure", "kubernetes")},
	}},
	{"sre", "Site reliability engineering", "site reliability", true, []requirement{
		{"Infrastructure as code", []string{"design:infrastructure"}},
		{"Logging, metrics, and tracing", []string{"design:observability"}},
		{"A cloud platform or Kubernetes", platforms("aws", "gcp", "azure", "kubernetes")},
	}},
	{"cloud", "Cloud engineering", "cloud infrastructure", true, []requirement{
		{"Cloud services", []string{"design:cloud"}},
		{"A cloud platform", platforms("aws", "gcp", "azure", "firebase")},
		{"Infrastructure as code", []string{"design:infrastructure"}},
	}},
	{"services", "Microservices and distributed systems", "microservices", false, []requirement{
		{"Web APIs and services", []string{"design:api"}},
		{"Messaging and events", []string{"design:messaging"}},
		{"A cloud platform or Kubernetes", platforms("aws", "gcp", "azure", "kubernetes")},
	}},
	{"integration", "Integration engineering", "service integrations", false, []requirement{
		{"Integrations with other services", []string{"design:integration"}},
		{"Web APIs and services", []string{"design:api"}},
		{"Messaging and events", []string{"design:messaging"}},
	}},
	{"data", "Data engineering", "data pipelines", false, []requirement{
		{"Data processing and analytics", append([]string{"design:analytics"}, platforms("snowflake", "databricks", "palantir")...)},
		{"Databases and storage", []string{"design:data"}},
		{"A data language", languages("python", "java", "kotlin", "go", "rust")},
	}},
	{"ml", "Machine learning engineering", "machine learning", false, []requirement{
		{"Machine learning", []string{"design:ml"}},
		{"Python", languages("python")},
	}},
	{"ai", "AI application engineering", "AI applications", false, []requirement{
		{"AI and LLM applications", []string{"design:ai"}},
		{"Web APIs and services", []string{"design:api"}},
	}},
	{"tooling", "Developer tooling", "developer tools", false, []requirement{
		{"Command-line tools", []string{"design:cli"}},
		{"A language", languages("go", "rust", "python", "typescript", "javascript", "csharp", "java", "kotlin", "swift", "shell", "powershell")},
	}},
	{"systems", "Systems programming", "systems software", false, []requirement{
		{"C, C++, or Rust", languages("c", "cpp", "rust")},
	}},
	{"salesforce", "Salesforce development", "Salesforce apps", false, []requirement{
		{"Salesforce", platforms("salesforce")},
	}},
	{"foundry", "Palantir Foundry development", "Palantir Foundry pipelines", false, []requirement{
		{"Palantir Foundry", platforms("palantir")},
	}},
}

func languages(ids ...string) []string { return prefixed("language:", ids) }
func tools(ids ...string) []string     { return prefixed("tool:", ids) }
func platforms(ids ...string) []string { return prefixed("platform:", ids) }

func prefixed(prefix string, ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = prefix + id
	}
	return out
}

// personaNow is the time recency is judged at; tests set it.
var personaNow = time.Now

// buildPersona works out the persona from the summary's ratings and next
// steps, with repository counts, languages, and months from the combined
// result. since is the scan window. It returns nil when nothing is rated.
func buildPersona(s *Summary, c *Combined, since string) *Persona {
	if len(s.Ratings) == 0 {
		return nil
	}
	ratings := map[string]Rating{}
	for _, r := range s.Ratings {
		ratings[r.Area+":"+r.Name] = r
	}
	repos := repoCounts(c)
	work := workOf(c)
	if since == "" {
		since = "all"
	}
	p := &Persona{Window: since, Strongest: strongest(s.Ratings), Fits: []Role{}, Adjacent: []Role{}}
	for _, f := range roleFamilies {
		role := Role{ID: f.id, Title: f.title, Met: []Need{}, Gaps: []Need{}}
		total := 0
		defined := false
		months := analysis.Months{}
		lines := map[string]int{}
		languageNeed := Need{}
		for i, req := range f.needs {
			n := Need{Title: req.title}
			// Breadth is the widest alternative that meets the bar, such as
			// the delivery tool used in the most repositories.
			widest, counted := 0, false
			for _, key := range req.any {
				r, ok := ratings[key]
				if !ok {
					continue
				}
				if r.Score > n.Score {
					n.Best, n.Score = ratingLabel(r.Area, r.Name), r.Score
				}
				if count, has := repos[key]; has && r.Score >= needBar {
					widest, counted = max(widest, count), true
				}
				if w, has := work[key]; has && r.Score >= needBar {
					months.Union(w.months)
					for lang, n := range w.lines {
						lines[lang] += n
					}
				}
			}
			if strings.HasPrefix(req.any[0], "language:") && n.Score > languageNeed.Score {
				languageNeed = n
			}
			if counted && (role.Repos == 0 || widest < role.Repos) {
				role.Repos = widest
			}
			total += n.Score
			defined = defined || (i == 0 && n.Score > 0)
			if n.Score >= needBar {
				role.Met = append(role.Met, n)
				continue
			}
			n.Next = nextStepFor(s.NextSteps, req.any)
			role.Gaps = append(role.Gaps, n)
		}
		role.Fit = total / len(f.needs)
		role.Code, role.CodeLanguages = roleCode(lines, ratings, languageNeed)
		role.Since, role.Last, role.ActiveMonths = span(months)
		role.strength = roleStrength(role, personaNow())
		switch {
		case len(role.Gaps) == 0:
			p.Fits = append(p.Fits, role)
		case defined && len(role.Met) >= len(role.Gaps):
			p.Adjacent = append(p.Adjacent, role)
		}
	}
	sort.SliceStable(p.Fits, func(i, j int) bool {
		if p.Fits[i].strength != p.Fits[j].strength {
			return p.Fits[i].strength > p.Fits[j].strength
		}
		return p.Fits[i].Fit > p.Fits[j].Fit
	})
	sort.SliceStable(p.Adjacent, func(i, j int) bool { return p.Adjacent[i].Fit > p.Adjacent[j].Fit })
	p.Fits, p.Adjacent = p.Fits[:min(maxFits, len(p.Fits))], p.Adjacent[:min(maxAdjacent, len(p.Adjacent))]
	p.Summary = personaSummary(p, s.Ratings)
	return p
}

// areaWork is what a rating's work was written in and when.
type areaWork struct {
	lines  map[string]int // owned lines per language
	months analysis.Months
}

// workOf maps "area:name" to the languages and months behind design areas
// and tools.
func workOf(c *Combined) map[string]areaWork {
	out := map[string]areaWork{}
	if c == nil {
		return out
	}
	for _, d := range c.Design {
		w := areaWork{lines: map[string]int{}, months: d.months}
		for _, l := range d.Languages {
			w.lines[l.Language] = l.Lines
		}
		out["design:"+d.ID] = w
	}
	for _, t := range c.Tools {
		out["tool:"+t.Name] = areaWork{months: t.months}
	}
	return out
}

// roleCode is the code rating of a role's work: the language ratings of the
// lines behind its design areas, weighted by lines, once at least
// MinOwnedLines of them are rated. A role without such lines, such as systems
// programming, takes the rating of its language requirement. It also returns
// the two languages with the most lines.
func roleCode(lines map[string]int, ratings map[string]Rating, language Need) (int, []string) {
	type rated struct {
		name         string
		lines, score int
	}
	var langs []rated
	total, weighted := 0, 0
	for lang, n := range lines {
		r, ok := ratings["language:"+lang]
		if !ok {
			continue
		}
		langs = append(langs, rated{lang, n, r.Score})
		total += n
		weighted += n * r.Score
	}
	if total < MinOwnedLines {
		if language.Score >= needBar {
			return language.Score, []string{language.Best}
		}
		return 0, nil
	}
	sort.Slice(langs, func(i, j int) bool {
		if langs[i].lines != langs[j].lines {
			return langs[i].lines > langs[j].lines
		}
		return langs[i].name < langs[j].name
	})
	var names []string
	for i, l := range langs {
		if i == 2 {
			break
		}
		names = append(names, displayLanguage(l.name))
	}
	return int(math.Round(float64(weighted) / float64(total))), names
}

// roleStrength ranks fitted roles: breadth (repositories), scaled by code
// quality and recency. Quality scales from 0.5 at a code rating of 0 to 1 at
// 100, and 0.85 for roles with no code to rate, such as infrastructure as
// code. Recency is 1 when the work was last committed within a year, 0.75
// within three, and 0.5 before; work without dates counts as recent.
func roleStrength(r Role, now time.Time) float64 {
	quality := 0.85
	if r.Code > 0 {
		quality = 0.5 + float64(r.Code)/200
	}
	recency := 1.0
	if last, err := time.Parse("2006-01", r.Last); err == nil {
		switch age := now.Sub(last); {
		case age > 3*365*24*time.Hour:
			recency = 0.5
		case age > 13*30*24*time.Hour:
			recency = 0.75
		}
	}
	return float64(max(r.Repos, 1)) * quality * recency
}

// repoCounts maps "area:name" to the repositories behind a rating, for the
// design areas, platforms, and tools of the combined result.
func repoCounts(c *Combined) map[string]int {
	out := map[string]int{}
	if c == nil {
		return out
	}
	for _, d := range c.Design {
		out["design:"+d.ID] = d.Repos
	}
	for _, p := range c.Platforms {
		out["platform:"+p.ID] = p.Repos
	}
	for _, t := range c.Tools {
		out["tool:"+t.Name] = t.Repos
	}
	return out
}

// strongest picks the highest ratings at Advanced or above, one per name.
func strongest(ratings []Rating) []Rating {
	picked := []Rating{}
	for _, r := range ratings {
		if r.Score >= 70 {
			picked = append(picked, r)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].Score > picked[j].Score })
	out := []Rating{}
	seen := map[string]bool{}
	for _, r := range picked {
		if len(out) == maxStrongest || seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		out = append(out, r)
	}
	return out
}

// nextStepFor returns the action of the first next step that raises one of
// the ratings, or "".
func nextStepFor(steps []Step, keys []string) string {
	for _, st := range steps {
		if slices.Contains(keys, st.Area+":"+st.Name) {
			return st.Action
		}
	}
	return ""
}

// personaSummary says in one line which roles the work fits and what it is
// built with: the top languages and platform rated Intermediate or above.
func personaSummary(p *Persona, ratings []Rating) string {
	var with []string
	langs := 0
	for _, r := range ratings {
		if r.Area == "language" && r.Score >= needBar && langs < 2 {
			with = append(with, displayLanguage(r.Name))
			langs++
		}
	}
	on := ""
	for _, r := range ratings {
		if r.Area == "platform" && r.Score >= needBar {
			on = " on " + platformTitle(r.Name)
			break
		}
	}
	built := ""
	if len(with) > 0 {
		built = ", with " + joinAnd(dedupe(with)) + on
	} else if on != "" {
		built = "," + on
	}
	if len(p.Fits) == 0 {
		if len(p.Adjacent) == 0 {
			return "Not enough evidence yet to match a role family."
		}
		return fmt.Sprintf("No role family is fully matched yet; the closest is %s%s.", inSentence(p.Adjacent[0].Title), built)
	}
	var apps, infra []string
	for _, r := range p.Fits {
		for _, f := range roleFamilies {
			if f.id != r.ID {
				continue
			}
			if f.infra && len(infra) < 2 {
				infra = append(infra, f.work)
			} else if !f.infra && len(apps) < 3 {
				apps = append(apps, f.work)
			}
		}
	}
	if len(apps) > 0 && len(infra) > 0 {
		return fmt.Sprintf("The work spans applications and the platforms they run on: %s, and the %s under them%s.", joinAnd(apps), joinAnd(infra), built)
	}
	var titles []string
	for i, r := range p.Fits {
		if i == 2 {
			break
		}
		titles = append(titles, inSentence(r.Title))
	}
	return fmt.Sprintf("The work fits %s%s.", joinAnd(titles), built)
}

// inSentence writes a role title for the middle of a sentence, keeping
// acronyms and product names capitalized.
func inSentence(title string) string {
	if strings.HasPrefix(title, "Salesforce") || strings.HasPrefix(title, "Palantir") {
		return title
	}
	return lowerFirst(title)
}

// writePersona prints the persona inside the summary.
func writePersona(b *strings.Builder, p *Persona) {
	if p == nil {
		return
	}
	b.WriteString("  Persona (from the ratings above; private unless you publish it)\n")
	fmt.Fprintf(b, "    %s\n", p.Summary)
	if len(p.Strongest) > 0 {
		var parts []string
		for _, r := range p.Strongest {
			parts = append(parts, fmt.Sprintf("%s %d", ratingLabel(r.Area, r.Name), r.Score))
		}
		fmt.Fprintf(b, "    Strongest: %s\n", strings.Join(parts, ", "))
	}
	for _, r := range p.Fits {
		fmt.Fprintf(b, "    = %s, fit %d%s: %s\n", r.Title, r.Fit, roleFacts(r), needList(r.Met))
	}
	if p.Window != "all" {
		fmt.Fprintf(b, "    Timelines cover work since %s; scan with --since all for older work.\n", p.Window)
	}
	for _, r := range p.Adjacent {
		fmt.Fprintf(b, "    ~ %s, fit %d: missing %s\n", r.Title, r.Fit, needList(r.Gaps))
		for _, g := range r.Gaps {
			if g.Next != "" {
				fmt.Fprintf(b, "        next: %s\n", g.Next)
			}
		}
	}
}

// roleFacts says how widely, how well, and when a role's work was done.
func roleFacts(r Role) string {
	var parts []string
	if r.Repos > 0 {
		parts = append(parts, count(r.Repos, "repo"))
	}
	if r.Code > 0 {
		parts = append(parts, fmt.Sprintf("code %d in %s", r.Code, strings.Join(r.CodeLanguages, " and ")))
	}
	if r.Since != "" {
		parts = append(parts, fmt.Sprintf("%s to %s, active %s", r.Since, r.Last, count(r.ActiveMonths, "month")))
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

func needList(needs []Need) string {
	parts := make([]string, len(needs))
	for i, n := range needs {
		switch n.Best {
		case "":
			parts[i] = n.Title + " (no evidence yet)"
		case n.Title:
			parts[i] = fmt.Sprintf("%s %d", n.Title, n.Score)
		default:
			parts[i] = fmt.Sprintf("%s (%s %d)", n.Title, n.Best, n.Score)
		}
	}
	return strings.Join(parts, ", ")
}
