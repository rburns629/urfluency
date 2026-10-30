package report

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPersona(t *testing.T) {
	s := &Summary{
		Ratings: []Rating{
			{Area: "language", Name: "go", Score: 82},
			{Area: "language", Name: "python", Score: 64},
			{Area: "design", Name: "api", Score: 75},
			{Area: "design", Name: "data", Score: 60},
			{Area: "design", Name: "infrastructure", Score: 55},
			{Area: "design", Name: "frontend", Score: 20},
			{Area: "platform", Name: "gcp", Score: 58},
			{Area: "tool", Name: "terraform", Score: 50},
			{Area: "practice", Name: "testing", Score: 90},
		},
		NextSteps: []Step{{Area: "language", Name: "typescript", Action: "Type your TypeScript props"}},
	}
	p := buildPersona(s, nil)
	if p == nil {
		t.Fatal("no persona")
	}
	ids := func(roles []Role) []string {
		var out []string
		for _, r := range roles {
			out = append(out, r.ID)
		}
		return out
	}
	if got := strings.Join(ids(p.Fits), ","); got != "backend,platform" {
		t.Errorf("fits = %s, want backend,platform", got)
	}
	if p.Fits[0].Fit != 72 { // (75 + 60 + 82) / 3
		t.Errorf("backend fit = %d", p.Fits[0].Fit)
	}
	if !strings.Contains(strings.Join(ids(p.Adjacent), ","), "fullstack") {
		t.Errorf("adjacent = %v, want full-stack (front ends missing)", ids(p.Adjacent))
	}
	if p.Summary != "The work spans applications and the platforms they run on: backend and API services, and the platform engineering under them, with Go and Python on Google Cloud." {
		t.Errorf("summary = %q", p.Summary)
	}
	if len(p.Strongest) != 3 || p.Strongest[0].Name != "testing" {
		t.Errorf("strongest = %+v", p.Strongest)
	}
	for _, r := range p.Adjacent {
		if r.ID == "tooling" || r.ID == "mobile" {
			t.Errorf("%s is adjacent on a language alone, without its defining area: %+v", r.ID, r)
		}
	}

	var b strings.Builder
	writePersona(&b, p)
	text := b.String()
	for _, want := range []string{"= Backend and API engineering, fit 72: Web APIs and services 75, Databases and storage 60, A server-side language (Go 82)", "~ Full-stack engineering", "missing Web front ends 20"} {
		if !strings.Contains(text, want) {
			t.Errorf("persona text missing %q:\n%s", want, text)
		}
	}
}

func TestPersonaLinksGapsToNextSteps(t *testing.T) {
	s := &Summary{
		Ratings: []Rating{
			{Area: "design", Name: "frontend", Score: 70},
			{Area: "language", Name: "typescript", Score: 30},
		},
		NextSteps: []Step{{Area: "language", Name: "typescript", Action: "Type exported functions"}},
	}
	p := buildPersona(s, nil)
	if len(p.Fits) != 0 || len(p.Adjacent) == 0 || p.Adjacent[0].ID != "frontend" {
		t.Fatalf("persona = %+v", p)
	}
	if g := p.Adjacent[0].Gaps; len(g) != 1 || g[0].Next != "Type exported functions" || g[0].Best != "TypeScript" {
		t.Errorf("gaps = %+v", g)
	}
	if !strings.HasPrefix(p.Summary, "No role family is fully matched yet; the closest is front-end engineering") {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestPersonaMakesNoClaimsAboutThePerson(t *testing.T) {
	p := buildPersona(&Summary{Ratings: []Rating{{Area: "design", Name: "api", Score: 90}, {Area: "design", Name: "data", Score: 90}, {Area: "language", Name: "go", Score: 90}}}, nil)
	b, _ := json.Marshal(p)
	for _, word := range []string{"senior", "junior", "salary", "lead", "staff", "principal", "you are"} {
		if strings.Contains(strings.ToLower(string(b)), word) {
			t.Errorf("persona mentions %q: %s", word, b)
		}
	}
	if buildPersona(&Summary{}, nil) != nil {
		t.Error("persona built without ratings")
	}
}

func TestPersonaRanksRolesByBreadth(t *testing.T) {
	// Broad work saturates experience ratings, so the roles whose evidence
	// spans the most repositories lead, not the highest average.
	s := &Summary{Ratings: []Rating{
		{Area: "language", Name: "go", Score: 87},
		{Area: "design", Name: "api", Score: 97}, {Area: "design", Name: "data", Score: 96},
		{Area: "design", Name: "frontend", Score: 100}, {Area: "design", Name: "messaging", Score: 94},
		{Area: "design", Name: "infrastructure", Score: 100}, {Area: "design", Name: "observability", Score: 99},
		{Area: "design", Name: "cloud", Score: 98}, {Area: "platform", Name: "gcp", Score: 100},
	}}
	c := &Combined{
		Design: []DesignArea{{ID: "api", Repos: 30}, {ID: "data", Repos: 25}, {ID: "frontend", Repos: 29}, {ID: "messaging", Repos: 29},
			{ID: "infrastructure", Repos: 42}, {ID: "observability", Repos: 8}, {ID: "cloud", Repos: 68}},
		Platforms: []Platform{{ID: "gcp", Repos: 60}},
	}
	p := buildPersona(s, c)
	var ids []string
	for _, r := range p.Fits {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "cloud,services,fullstack,backend,sre" {
		t.Errorf("fits = %s, want the broadest first and SRE, with observability in 8 repos, last", got)
	}
	if p.Fits[0].Repos != 42 {
		t.Errorf("cloud breadth = %d, want 42 (infrastructure, its narrowest requirement)", p.Fits[0].Repos)
	}
	if !strings.HasPrefix(p.Summary, "The work spans applications and the platforms they run on: microservices, full-stack applications, and backend and API services, and the cloud infrastructure and site reliability under them") {
		t.Errorf("summary = %q", p.Summary)
	}
}
