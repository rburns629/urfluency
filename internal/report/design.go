package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// DesignArea is one kind of system the user builds, such as cloud services or
// integrations, judged from the libraries their own files import and, for
// infrastructure, the tool files they wrote. It rates experience, not quality.
type DesignArea struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Repos     int          `json:"repositories"`
	Files     int          `json:"files"`
	Libraries []LibraryUse `json:"libraries"`
	// Languages are the owned lines of the area's files per language.
	Languages []LanguageLines `json:"languages,omitempty"`
	// First and Last are the first and latest months with committed work in
	// the area, and ActiveMonths how many months had some; empty without Git.
	First        string `json:"first_active,omitempty"`
	Last         string `json:"last_active,omitempty"`
	ActiveMonths int    `json:"active_months,omitempty"`
	Score        int    `json:"score"`
	Level        string `json:"level"`
	months       analysis.Months
}

// LanguageLines is a language and a count of owned lines in it.
type LanguageLines struct {
	Language string `json:"language"`
	Lines    int    `json:"lines"`
}

// LibraryUse is a library and the number of owned files that import it.
type LibraryUse struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
}

// FrameworkRating rates experience with one framework of a language.
type FrameworkRating struct {
	Name       string `json:"name"`
	Repos      int    `json:"repositories"`
	Files      int    `json:"files"`
	Score      int    `json:"score"`
	Level      string `json:"level"`
	Confidence string `json:"confidence"`
}

// BuildDesign lists the design areas the user worked in, most repositories
// first. Infrastructure also counts the infrastructure tool files from tools.
func BuildDesign(res analysis.Result, tools []Tool) []DesignArea {
	out := []DesignArea{}
	for _, a := range analysis.DesignAreas {
		u := res.Design[a.ID]
		d := DesignArea{ID: a.ID, Title: a.Title, Repos: u.Repos, Files: u.Files, Libraries: []LibraryUse{}, months: analysis.Months{}}
		for name, n := range u.Libraries {
			d.Libraries = append(d.Libraries, LibraryUse{Name: name, Files: n})
		}
		for lang, n := range u.Languages {
			d.Languages = append(d.Languages, LanguageLines{Language: lang, Lines: n})
		}
		sort.Slice(d.Languages, func(i, j int) bool {
			if d.Languages[i].Lines != d.Languages[j].Lines {
				return d.Languages[i].Lines > d.Languages[j].Lines
			}
			return d.Languages[i].Language < d.Languages[j].Language
		})
		d.months.Union(u.Months)
		if a.ID == "infrastructure" {
			// Tool use is summed per tool, so the repositories of the most used
			// tool are a lower bound for the area.
			for _, t := range tools {
				if t.Kind == "infrastructure" {
					d.Repos = max(d.Repos, t.Repos)
					d.Files += t.Files
					d.Libraries = append(d.Libraries, LibraryUse{Name: t.Name, Files: t.Files})
					d.months.Union(t.months)
				}
			}
		}
		if d.Files == 0 {
			continue
		}
		sort.Slice(d.Libraries, func(i, j int) bool {
			if d.Libraries[i].Files != d.Libraries[j].Files {
				return d.Libraries[i].Files > d.Libraries[j].Files
			}
			return d.Libraries[i].Name < d.Libraries[j].Name
		})
		d.First, d.Last, d.ActiveMonths = span(d.months)
		d.Score = areaExperience(d.ID, d.Repos, d.Files)
		d.Level = level(d.Score)
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Repos != out[j].Repos {
			return out[i].Repos > out[j].Repos
		}
		return out[i].Files > out[j].Files
	})
	return out
}

// span returns the first and latest month of a set and its size.
func span(m analysis.Months) (first, last string, n int) {
	for k := range m {
		if first == "" || k < first {
			first = k
		}
		if k > last {
			last = k
		}
	}
	return first, last, len(m)
}

func buildFrameworkRatings(lr analysis.LanguageResult) []FrameworkRating {
	out := []FrameworkRating{}
	for _, fw := range lr.Frameworks {
		u := lr.FrameworkUse[fw]
		if u.Files == 0 {
			continue
		}
		s := experience(u.Repos, u.Files)
		out = append(out, FrameworkRating{Name: fw, Repos: u.Repos, Files: u.Files, Score: s, Level: level(s), Confidence: experienceConfidence(u.Repos)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// libraryNames lists up to n library names.
func libraryNames(libs []LibraryUse, n int) string {
	var names []string
	for i, l := range libs {
		if i == n {
			break
		}
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}

// writeDesign prints design areas as counts. These tables are parts of the
// result, so their ratings are left to the summary, which rates all work.
func writeDesign(b *strings.Builder, heading string, areas []DesignArea) {
	if len(areas) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n", heading)
	for _, d := range areas {
		fmt.Fprintf(b, "  %-34s %s, %s · %s\n", d.Title,
			count(d.Repos, "repo"), count(d.Files, "file"), libraryNames(d.Libraries, 5))
	}
}
