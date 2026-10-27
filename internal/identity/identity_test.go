package identity

import (
	"slices"
	"testing"
)

func TestDetect(t *testing.T) {
	authors := []Author{
		{Name: "Ana Diaz", Email: "ana.diaz@corp.example", Commits: 300, Repos: 20},
		{Name: "Ana M Diaz", Email: "ana.diaz@old.example", Commits: 50, Repos: 3}, // middle initial
		{Name: "Ana Diaz", Email: "adiaz@laptop.local", Commits: 4, Repos: 1},      // machine default address
		{Name: "anad", Email: "ana.diaz@old.example", Commits: 2, Repos: 1},        // one-word name: counted, not linked further
		{Name: "anad", Email: "anad@shared.example", Commits: 9, Repos: 1},
		{Name: "Ben Diaz", Email: "ben@corp.example", Commits: 80, Repos: 5},    // shares the last name
		{Name: "Cleo", Email: "adiaz42@personal.example", Commits: 7, Repos: 2}, // looks like hers
		{Name: "dependabot[bot]", Email: "49699333+dependabot[bot]@users.noreply.github.com", Commits: 40, Repos: 9},
		{Name: "GitHub", Email: "noreply@github.com", Commits: 30, Repos: 9},
		{Name: "Ana Diaz", Email: "noreply@github.com", Commits: 3, Repos: 1}, // merged on the web
		{Name: "Dev Lee", Email: "dev@corp.example", Commits: 500, Repos: 20},
	}
	id := Detect([]string{"Ana Diaz"}, []string{"123+anadiaz@users.noreply.github.com"}, authors)

	wantEmails := []string{"123+anadiaz@users.noreply.github.com", "adiaz@laptop.local", "ana.diaz@corp.example", "ana.diaz@old.example"}
	if !slices.Equal(id.Emails, wantEmails) {
		t.Errorf("emails = %v, want %v", id.Emails, wantEmails)
	}
	if id.Commits != 300+50+4+2 {
		t.Errorf("commits = %d", id.Commits)
	}
	var possible []string
	for _, p := range id.Possible {
		possible = append(possible, p.Email)
	}
	// Ben shares the last name and Cleo's address resembles hers: shown, not
	// counted. Dev Lee, bots, and the shared web-flow address are neither.
	wantPossible := []string{"ben@corp.example", "adiaz42@personal.example"}
	if !slices.Equal(possible, wantPossible) {
		t.Errorf("possible = %v, want %v", possible, wantPossible)
	}
}

func TestDetectWithoutSeedsInHistory(t *testing.T) {
	id := Detect(nil, []string{"me@example.com"}, []Author{{Name: "Someone", Email: "x@example.com", Commits: 3}})
	if !slices.Equal(id.Emails, []string{"me@example.com"}) || id.Commits != 0 || len(id.Possible) != 0 {
		t.Errorf("identity = %+v", id)
	}
}

func TestNameKey(t *testing.T) {
	for in, want := range map[string]string{"Robert L. Burns": "robert burns", "  robert   BURNS ": "robert burns", "tothex": "tothex", "": ""} {
		if got := nameKey(in); got != want {
			t.Errorf("nameKey(%q) = %q, want %q", in, got, want)
		}
	}
}
