package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// Tool is how much the user worked with one tool, from the files they committed
// to. Without Git history, only Files and Repos are known.
type Tool struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // infrastructure, ci, build, packages, quality, testing, hosting
	Repos   int    `json:"repositories"`
	Files   int    `json:"files"`
	Commits int    `json:"commits,omitempty"`
	Lines   int    `json:"lines_added,omitempty"`
	Last    string `json:"last_used,omitempty"` // year and month
}

// Git is how the user writes commits, as shares of their commits.
type Git struct {
	Commits      int     `json:"commits"`
	ShortSubject float64 `json:"short_subject_share"`
	Descriptive  float64 `json:"descriptive_subject_share"`
	Conventional float64 `json:"conventional_share"`
	WithBody     float64 `json:"explained_in_body_share"`
}

// BuildTools lists tools by how much the user used them, most commits first.
func BuildTools(res analysis.Result) []Tool {
	out := []Tool{}
	for name, u := range res.Tools {
		t := Tool{Name: name, Kind: analysis.ToolKind(name), Repos: u.Repos, Files: u.Files, Commits: u.Commits, Lines: u.Lines}
		if !u.Last.IsZero() {
			t.Last = u.Last.Format("2006-01")
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Commits != out[j].Commits {
			return out[i].Commits > out[j].Commits
		}
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// BuildGit reports commit habits, or nil without commits.
func BuildGit(res analysis.Result) *Git {
	g := res.Git
	if g.Commits == 0 {
		return nil
	}
	share := func(n int) float64 { return round2(float64(n) / float64(g.Commits)) }
	return &Git{Commits: g.Commits, ShortSubject: share(g.ShortSubject), Descriptive: share(g.Descriptive),
		Conventional: share(g.Conventional), WithBody: share(g.WithBody)}
}

func writeTools(b *strings.Builder, tools []Tool, git *Git) {
	if len(tools) == 0 && git == nil {
		return
	}
	b.WriteString("\nTOOLS (from the files you committed to; contents are not read)\n")
	if git != nil {
		fmt.Fprintf(b, "  git: %d commits · subject 72 characters or fewer %.0f%% · descriptive subject %.0f%% · explained in body %.0f%% · Conventional Commits %.0f%%\n",
			git.Commits, git.ShortSubject*100, git.Descriptive*100, git.WithBody*100, git.Conventional*100)
	}
	writeToolList(b, tools)
}

// count formats n with a singular or plural noun, such as "1 file" or "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func writeToolList(b *strings.Builder, tools []Tool) {
	for _, kind := range []string{"infrastructure", "ci", "build", "packages", "quality", "testing", "hosting"} {
		var parts []string
		for _, t := range tools {
			if t.Kind != kind {
				continue
			}
			if t.Commits > 0 {
				parts = append(parts, fmt.Sprintf("%s (%s, %s, %s, last %s)", t.Name, count(t.Commits, "commit"), count(t.Files, "file"), count(t.Repos, "repo"), t.Last))
			} else {
				parts = append(parts, fmt.Sprintf("%s (%s, %s)", t.Name, count(t.Files, "file"), count(t.Repos, "project")))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(b, "  %s: %s\n", kind, strings.Join(parts, "; "))
		}
	}
}
