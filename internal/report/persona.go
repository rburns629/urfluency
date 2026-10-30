package report

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Persona describes the work the ratings show: the strongest areas, the role
// families the evidence fits, and adjacent roles with what is missing for
// each. It is built from the summary's ratings by fixed rules (roleFamilies),
// with no AI service. It says what the work shows, never who the person is:
// no seniority, salary, or personal traits. Profiles show it only when their
// owner publishes it.
type Persona struct {
	Summary   string   `json:"summary"`
	Strongest []Rating `json:"strongest"`
	Fits      []Role   `json:"fits"`
	Adjacent  []Role   `json:"adjacent"`
}

// Role is one role family and how well the ratings meet its requirements.
// Fit is the mean of the requirements' best ratings.
type Role struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Fit   int    `json:"fit"`
	Met   []Need `json:"met"`
	Gaps  []Need `json:"gaps"`
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
	maxFits      = 3
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
var roleFamilies = []struct {
	id, title string
	needs     []requirement
}{
	{"backend", "Backend engineering", []requirement{
		{"Web APIs and services", []string{"design:api"}},
		{"Databases and storage", []string{"design:data"}},
		{"A server-side language", languages("go", "java", "kotlin", "python", "csharp", "rust", "php", "ruby", "typescript", "javascript")},
	}},
	{"frontend", "Front-end engineering", []requirement{
		{"Web front ends", []string{"design:frontend"}},
		{"JavaScript or TypeScript", languages("javascript", "typescript")},
	}},
	{"fullstack", "Full-stack engineering", []requirement{
		{"Web front ends", []string{"design:frontend"}},
		{"Web APIs and services", []string{"design:api"}},
		{"Databases and storage", []string{"design:data"}},
	}},
	{"mobile", "Mobile engineering", []requirement{
		{"Mobile apps", []string{"design:mobile"}},
		{"A mobile language", languages("swift", "kotlin", "dart", "typescript", "javascript", "java")},
	}},
	{"platform", "Platform and DevOps engineering", []requirement{
		{"Infrastructure as code", []string{"design:infrastructure"}},
		{"Delivery tools", tools("docker", "terraform", "helm", "kustomize", "pulumi", "ansible", "github-actions", "gitlab-ci", "jenkins", "cloud-build", "azure-pipelines", "circleci")},
		{"A cloud platform or Kubernetes", platforms("aws", "gcp", "azure", "kubernetes")},
	}},
	{"sre", "Site reliability engineering", []requirement{
		{"Infrastructure as code", []string{"design:infrastructure"}},
		{"Logging, metrics, and tracing", []string{"design:observability"}},
		{"A cloud platform or Kubernetes", platforms("aws", "gcp", "azure", "kubernetes")},
	}},
	{"cloud", "Cloud engineering", []requirement{
		{"Cloud services", []string{"design:cloud"}},
		{"A cloud platform", platforms("aws", "gcp", "azure", "firebase")},
		{"Infrastructure as code", []string{"design:infrastructure"}},
	}},
	{"integration", "Integration engineering", []requirement{
		{"Integrations with other services", []string{"design:integration"}},
		{"Web APIs and services", []string{"design:api"}},
		{"Messaging and events", []string{"design:messaging"}},
	}},
	{"data", "Data engineering", []requirement{
		{"Data processing and analytics", append([]string{"design:analytics"}, platforms("snowflake", "databricks", "palantir")...)},
		{"Databases and storage", []string{"design:data"}},
		{"A data language", languages("python", "java", "kotlin", "go", "rust")},
	}},
	{"ml", "Machine learning engineering", []requirement{
		{"Machine learning", []string{"design:ml"}},
		{"Python", languages("python")},
	}},
	{"ai", "AI application engineering", []requirement{
		{"AI and LLM applications", []string{"design:ai"}},
		{"Web APIs and services", []string{"design:api"}},
	}},
	{"tooling", "Developer tooling", []requirement{
		{"Command-line tools", []string{"design:cli"}},
		{"A language", languages("go", "rust", "python", "typescript", "javascript", "csharp", "java", "kotlin", "swift", "shell", "powershell")},
	}},
	{"systems", "Systems programming", []requirement{
		{"C, C++, or Rust", languages("c", "cpp", "rust")},
	}},
	{"salesforce", "Salesforce development", []requirement{
		{"Salesforce", platforms("salesforce")},
	}},
	{"foundry", "Palantir Foundry development", []requirement{
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

// buildPersona works out the persona from the summary's ratings and next
// steps. It returns nil when nothing is rated.
func buildPersona(s *Summary) *Persona {
	if len(s.Ratings) == 0 {
		return nil
	}
	ratings := map[string]Rating{}
	for _, r := range s.Ratings {
		ratings[r.Area+":"+r.Name] = r
	}
	p := &Persona{Strongest: strongest(s.Ratings), Fits: []Role{}, Adjacent: []Role{}}
	for _, f := range roleFamilies {
		role := Role{ID: f.id, Title: f.title, Met: []Need{}, Gaps: []Need{}}
		total := 0
		defined := false
		for i, req := range f.needs {
			n := Need{Title: req.title}
			for _, key := range req.any {
				if r, ok := ratings[key]; ok && r.Score > n.Score {
					n.Best, n.Score = ratingLabel(r.Area, r.Name), r.Score
				}
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
		switch {
		case len(role.Gaps) == 0:
			p.Fits = append(p.Fits, role)
		case defined && len(role.Met) >= len(role.Gaps):
			p.Adjacent = append(p.Adjacent, role)
		}
	}
	byFit := func(roles []Role, n int) []Role {
		sort.SliceStable(roles, func(i, j int) bool { return roles[i].Fit > roles[j].Fit })
		return roles[:min(n, len(roles))]
	}
	p.Fits, p.Adjacent = byFit(p.Fits, maxFits), byFit(p.Adjacent, maxAdjacent)
	p.Summary = personaSummary(p, s.Ratings)
	return p
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
		fmt.Fprintf(b, "    = %s, fit %d: %s\n", r.Title, r.Fit, needList(r.Met))
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
