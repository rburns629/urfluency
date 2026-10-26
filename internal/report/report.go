// Package report turns scan results into strengths, weaknesses, and focus points.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/rules"
)

// FormatVersion identifies the report schema.
const FormatVersion = "0.1"

// MinOwnedLines is the volume below which a language shows "not enough data".
const MinOwnedLines = 300

// Report is the local result of a scan. Locations are included only on request
// and are never part of an upload.
type Report struct {
	Format    string     `json:"format"`
	Catalog   string     `json:"catalog"`
	Commit    string     `json:"commit"`
	Since     string     `json:"since"`
	Languages []Language `json:"languages"`
}

// Language summarizes one language.
type Language struct {
	Language   string         `json:"language"`
	Frameworks []string       `json:"frameworks"`
	Files      int            `json:"files"`
	OwnedLines int            `json:"owned_lines"`
	Sufficient bool           `json:"sufficient_data"`
	Categories map[string]int `json:"findings_by_category"`
	Weaknesses []RuleResult   `json:"weaknesses"`
	Strengths  []string       `json:"strengths"`
	Focus      []string       `json:"focus"`
}

// RuleResult is one rule with occurrences on the user's lines.
type RuleResult struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Title     string   `json:"title"`
	Count     int      `json:"count"`
	PerKLines float64  `json:"per_1000_lines"`
	Why       string   `json:"why"`
	Focus     string   `json:"focus"`
	Locations []string `json:"locations,omitempty"`
}

// Build creates a report from a scan result.
func Build(res analysis.Result, since time.Time, withLocations bool) Report {
	r := Report{Format: FormatVersion, Catalog: rules.Version, Commit: shortHash(res.Head), Since: since.Format("2006-01-02")}
	for _, lr := range res.Languages {
		r.Languages = append(r.Languages, buildLanguage(lr, withLocations))
	}
	return r
}

func buildLanguage(lr analysis.LanguageResult, withLocations bool) Language {
	// Slices start empty, not nil, so JSON consumers always receive arrays.
	lang := Language{
		Language:   lr.Language,
		Frameworks: append([]string{}, lr.Frameworks...),
		Files:      lr.Files,
		OwnedLines: lr.OwnedLines,
		Sufficient: lr.OwnedLines >= MinOwnedLines,
		Categories: map[string]int{},
		Weaknesses: []RuleResult{},
		Strengths:  []string{},
		Focus:      []string{},
	}
	byRule := map[string]*RuleResult{}
	for _, f := range lr.Findings {
		rule, ok := rules.Lookup(f.Rule)
		if !ok {
			continue
		}
		rr := byRule[f.Rule]
		if rr == nil {
			rr = &RuleResult{ID: rule.ID, Category: string(rule.Category), Title: rule.Title, Why: rule.Why, Focus: rule.Focus}
			byRule[f.Rule] = rr
		}
		rr.Count++
		lang.Categories[rr.Category]++
		if withLocations {
			rr.Locations = append(rr.Locations, fmt.Sprintf("%s:%d", f.Path, f.Line))
		}
	}
	for _, rr := range byRule {
		if lr.OwnedLines > 0 {
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
			checked[string(rule.Category)]++
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
	return lang
}

// WriteText prints a human-readable summary.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "urFluency scan · commit %s · since %s · catalog %s\n", r.Commit, r.Since, r.Catalog)
	if len(r.Languages) == 0 {
		b.WriteString("\nNo owned lines in supported languages were found for these authors.\n")
	}
	for _, l := range r.Languages {
		name := strings.ToUpper(l.Language)
		if len(l.Frameworks) > 0 {
			name += " (" + strings.Join(l.Frameworks, ", ") + ")"
		}
		fmt.Fprintf(&b, "\n%s: %d owned lines in %d files\n", name, l.OwnedLines, l.Files)
		if !l.Sufficient {
			fmt.Fprintf(&b, "  Not enough data for strengths (needs %d owned lines).\n", MinOwnedLines)
		}
		if len(l.Strengths) > 0 {
			b.WriteString("  Strengths\n")
			for _, s := range l.Strengths {
				fmt.Fprintf(&b, "    + %s\n", s)
			}
		}
		if len(l.Weaknesses) > 0 {
			b.WriteString("  Areas to improve\n")
			for _, w := range l.Weaknesses {
				fmt.Fprintf(&b, "    - %s [%s]: %d (%.2f per 1,000 lines)\n", w.Title, w.Category, w.Count, w.PerKLines)
				fmt.Fprintf(&b, "      Why: %s\n      Focus: %s\n", w.Why, w.Focus)
				for _, loc := range w.Locations {
					fmt.Fprintf(&b, "      at %s\n", loc)
				}
			}
		}
		if len(l.Focus) > 0 {
			fmt.Fprintf(&b, "  Points of focus: %s\n", strings.Join(l.Focus, ", "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
