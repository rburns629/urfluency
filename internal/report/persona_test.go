package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/analysis"
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
	p := buildPersona(s, nil, "all")
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
	for _, want := range []string{"= Backend and API engineering, fit 72 · code 82 in Go: Web APIs and services 75, Databases and storage 60, A server-side language (Go 82)", "~ Full-stack engineering", "missing Web front ends 20"} {
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
	p := buildPersona(s, nil, "all")
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
	p := buildPersona(&Summary{Ratings: []Rating{{Area: "design", Name: "api", Score: 90}, {Area: "design", Name: "data", Score: 90}, {Area: "language", Name: "go", Score: 90}}}, nil, "all")
	b, _ := json.Marshal(p)
	for _, word := range []string{"senior", "junior", "salary", "lead", "staff", "principal", "you are"} {
		if strings.Contains(strings.ToLower(string(b)), word) {
			t.Errorf("persona mentions %q: %s", word, b)
		}
	}
	if buildPersona(&Summary{}, nil, "all") != nil {
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
	p := buildPersona(s, c, "all")
	var ids []string
	for _, r := range p.Fits {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "cloud,services,backend,fullstack,sre" {
		t.Errorf("fits = %s, want the broadest first, backend (code rated) ahead of full stack at equal breadth, and SRE, with observability in 8 repos, last", got)
	}
	if p.Fits[0].Repos != 42 {
		t.Errorf("cloud breadth = %d, want 42 (infrastructure, its narrowest requirement)", p.Fits[0].Repos)
	}
	if !strings.HasPrefix(p.Summary, "The work spans applications and the platforms they run on: microservices, backend and API services, and full-stack applications, and the cloud infrastructure and site reliability under them") {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestPersonaWeighsCodeAndRecency(t *testing.T) {
	defer func(f func() time.Time) { personaNow = f }(personaNow)
	personaNow = func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }
	s := &Summary{Ratings: []Rating{
		{Area: "language", Name: "go", Score: 90}, {Area: "language", Name: "typescript", Score: 50},
		{Area: "design", Name: "api", Score: 90}, {Area: "design", Name: "data", Score: 90},
		{Area: "design", Name: "frontend", Score: 90}, {Area: "design", Name: "cli", Score: 90},
	}}
	months := func(ms ...string) analysis.Months {
		m := analysis.Months{}
		for _, x := range ms {
			m[x] = true
		}
		return m
	}
	c := &Combined{Design: []DesignArea{
		{ID: "api", Repos: 20, Languages: []LanguageLines{{"go", 4000}, {"typescript", 1000}}, months: months("2019-04", "2026-09")},
		{ID: "data", Repos: 20, Languages: []LanguageLines{{"go", 2000}}, months: months("2020-01", "2026-08")},
		{ID: "frontend", Repos: 20, Languages: []LanguageLines{{"typescript", 6000}}, months: months("2022-02", "2026-10")},
		{ID: "cli", Repos: 20, Languages: []LanguageLines{{"go", 900}}, months: months("2018-01", "2021-06")},
	}}
	p := buildPersona(s, c, "all")
	byID := map[string]Role{}
	var ids []string
	for _, r := range p.Fits {
		byID[r.ID] = r
		ids = append(ids, r.ID)
	}
	if b := byID["backend"]; b.Code != 84 || strings.Join(b.CodeLanguages, ",") != "Go,TypeScript" || b.Since != "2019-04" || b.Last != "2026-09" || b.ActiveMonths != 4 {
		t.Errorf("backend = %+v, want code 84 (6,000 Go and 1,000 TypeScript lines), 2019-04 to 2026-09 over 4 months", b)
	}
	if f := byID["frontend"]; f.Code != 50 {
		t.Errorf("front end code = %d, want TypeScript's 50", f.Code)
	}
	// Equal breadth: well-written, recent backend work leads; front end in
	// weaker code follows; command-line work last done in 2021 comes last.
	if got := strings.Join(ids, ","); got != "backend,fullstack,frontend,tooling" {
		t.Errorf("fits = %s", got)
	}
	var b strings.Builder
	writePersona(&b, p)
	if !strings.Contains(b.String(), "= Backend and API engineering, fit 90 · 20 repos · code 84 in Go and TypeScript · 2019-04 to 2026-09, active 4 months:") {
		t.Errorf("text:\n%s", b.String())
	}
	if w := buildPersona(s, c, "2025-10-30"); w.Window != "2025-10-30" {
		t.Errorf("window = %q", w.Window)
	}
}
