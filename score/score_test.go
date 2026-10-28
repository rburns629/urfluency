package score_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/testutil"
	"github.com/rburns629/urfluency/score"
)

// goFile returns a Go file of n small functions that wrap their errors.
func goFile(pkg string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport (\n\t\"context\"\n\t\"fmt\"\n)\n\n", pkg)
	for i := range n {
		fmt.Fprintf(&b, "// Step%d runs step %d.\nfunc Step%d(ctx context.Context, n int) (int, error) {\n\tif err := ctx.Err(); err != nil {\n\t\treturn 0, fmt.Errorf(\"step %d: %%w\", err)\n\t}\n\treturn n + %d, nil\n}\n\n", i, i, i, i, i)
	}
	return b.String()
}

// Repo rates every author's code, not one person's.
func TestRepoScoresEveryAuthor(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("ana@example.com", "2026-09-01T00:00:00Z", map[string]string{"go.mod": "module example.com/x\n\ngo 1.26\n", "a/a.go": goFile("a", 40)})
	r.Commit("ben@example.com", "2026-09-02T00:00:00Z", map[string]string{"b/b.go": goFile("b", 40)})

	p, err := score.Repo(context.Background(), r.Dir, time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), "test")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Ratings []struct {
			Area, Name string
			Score      int
		} `json:"ratings"`
	}
	if err := json.Unmarshal(p.Summary, &s); err != nil {
		t.Fatal(err)
	}
	var combined struct {
		Languages []struct {
			Name       string `json:"language"`
			OwnedLines int    `json:"owned_lines"`
		} `json:"languages"`
	}
	if err := json.Unmarshal(p.Combined, &combined); err != nil {
		t.Fatal(err)
	}
	var goLines int
	for _, l := range combined.Languages {
		if l.Name == "go" {
			goLines = l.OwnedLines
		}
	}
	// Both authors' files count: 2 files of 40 eight-line functions plus headers.
	if goLines < 600 {
		t.Errorf("go lines = %d, want both authors' (over 600); combined = %s", goLines, p.Combined)
	}
	var rated bool
	for _, rt := range s.Ratings {
		rated = rated || (rt.Area == "language" && rt.Name == "go" && rt.Score > 0)
	}
	if !rated {
		t.Errorf("no Go rating in %s", p.Summary)
	}
	if p.Tier != "self-reported" || p.Tool != "test" {
		t.Errorf("payload claims tier %q, tool %q", p.Tier, p.Tool)
	}
}

func TestRepoWithoutCommits(t *testing.T) {
	r := testutil.NewRepo(t)
	if _, err := score.Repo(context.Background(), r.Dir, time.Time{}, "test"); !errors.Is(err, score.ErrNoHistory) {
		t.Errorf("err = %v, want ErrNoHistory", err)
	}
}
