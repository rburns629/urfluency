package report

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

// ProficiencyModel versions the scoring below. Its constants are reasoned
// starting points, not yet calibrated against outcomes (see DESIGN.md).
const ProficiencyModel = "0.2-uncalibrated"

// Proficiency rates one language from the user's own code: how much of the
// language's idiom range they use, and how cleanly they write it.
type Proficiency struct {
	Score      int                      `json:"score"`      // 0–100
	Level      string                   `json:"level"`      // Beginner, Intermediate, Advanced, Expert
	Confidence string                   `json:"confidence"` // low, medium, high, from volume
	Range      int                      `json:"range"`      // 0–100: share of the language's idioms demonstrated
	Categories map[string]CategoryScore `json:"categories"`
	Model      string                   `json:"model"`
}

// CategoryScore rates one category of problems, such as type safety.
type CategoryScore struct {
	Score     int     `json:"score"`
	Band      string  `json:"band"` // Strong, Solid, Developing, Focus area
	PerKLines float64 `json:"per_1000_lines"`
}

// IdiomResult is an idiom the user demonstrated.
type IdiomResult struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Uses  int    `json:"uses"`
	Files int    `json:"files"`
	Why   string `json:"why"`
}

// scoredCategories are the problem categories a language score draws on, with
// the findings rate per 1,000 lines at which a category scores 50. Security
// issues weigh far more than style.
var scoredCategories = map[rules.Category]float64{
	rules.Security:            0.5,
	rules.TypeSafety:          10,
	rules.CleanCode:           10,
	rules.LanguageProficiency: 10,
}

const (
	// rangeWeight is the share of the score from idiom range; the rest is from
	// category scores.
	rangeWeight = 0.35
	// weakestLead caps the score this far above the weakest category, so a
	// strong category cannot hide a weak one.
	weakestLead = 30
	// An idiom counts as demonstrated with at least this many uses across at
	// least this many files, so one copied snippet does not count.
	minIdiomUses  = 3
	minIdiomFiles = 2
)

// proficiency scores a language with enough owned code; it returns nil otherwise.
func proficiency(lr analysis.LanguageResult, counts map[rules.Category]int) (*Proficiency, []IdiomResult) {
	var idioms []IdiomResult
	catalog, applicable := 0, map[rules.Category]bool{}
	for _, r := range rules.ForLanguage(lr.Language) {
		if r.Category == rules.Idiom {
			catalog++
			if u := lr.Idioms[r.ID]; u.Uses >= minIdiomUses && u.Files >= minIdiomFiles {
				idioms = append(idioms, IdiomResult{ID: r.ID, Title: r.Title, Uses: u.Uses, Files: u.Files, Why: r.Why})
			}
		} else if _, scored := scoredCategories[r.Category]; scored {
			applicable[r.Category] = true
		}
	}
	sort.Slice(idioms, func(i, j int) bool { return idioms[i].Uses > idioms[j].Uses })
	if lr.OwnedLines < MinOwnedLines || catalog == 0 || len(applicable) == 0 {
		return nil, idioms
	}

	p := &Proficiency{Categories: map[string]CategoryScore{}, Model: ProficiencyModel}
	kLines := float64(lr.OwnedLines) / 1000
	sum, weakest := 0.0, 100.0
	for c := range applicable {
		rate := float64(counts[c]) / kLines
		score := 100 / (1 + rate/scoredCategories[c])
		p.Categories[string(c)] = CategoryScore{Score: int(math.Round(score)), Band: band(score), PerKLines: round2(rate)}
		sum += score
		weakest = min(weakest, score)
	}
	rng := 100 * float64(len(idioms)) / float64(catalog)
	score := rangeWeight*rng + (1-rangeWeight)*sum/float64(len(applicable))
	score = min(score, weakest+weakestLead)
	p.Score, p.Range = int(math.Round(score)), int(math.Round(rng))
	p.Level = level(p.Score)
	switch {
	case lr.OwnedLines >= 10000:
		p.Confidence = "high"
	case lr.OwnedLines >= 2000:
		p.Confidence = "medium"
	default:
		p.Confidence = "low"
	}
	return p, idioms
}

func band(score float64) string {
	switch {
	case score >= 85:
		return "Strong"
	case score >= 70:
		return "Solid"
	case score >= 50:
		return "Developing"
	}
	return "Focus area"
}

// level classifies a 0–100 rating. The same levels apply to every rating, so a
// language, a framework, and a habit read the same way.
func level(score int) string {
	switch {
	case score >= 85:
		return "Expert"
	case score >= 70:
		return "Advanced"
	case score >= 40:
		return "Intermediate"
	}
	return "Beginner"
}

// experience rates sustained use of a framework or tool from the repositories
// and owned files that use it: ten repositories and 200 files rate 100. It
// measures how much, not how well; language scores measure how well.
func experience(repos, files int) int { return experienceAt(repos, files, 10, 200) }

// areaExperience rates a design area. An area pools many libraries, so it
// needs 25 repositories and 500 files to rate 100.
func areaExperience(repos, files int) int { return experienceAt(repos, files, 25, 500) }

// experienceAt grows with the logarithm of use, so the first projects count
// most; repositories weigh more than files, since one large project can
// reflect a team's choices.
func experienceAt(repos, files, fullRepos, fullFiles int) int {
	r := math.Min(1, math.Log2(1+float64(repos))/math.Log2(1+float64(fullRepos)))
	f := math.Min(1, math.Log2(1+float64(files))/math.Log2(1+float64(fullFiles)))
	return int(math.Round(60*r + 40*f))
}

// experienceConfidence grows with the number of repositories, since one
// project can reflect a team's choices more than the user's.
func experienceConfidence(repos int) string {
	switch {
	case repos >= 5:
		return "high"
	case repos >= 2:
		return "medium"
	}
	return "low"
}

// writeProficiency prints a language's score, its category bands, and the idioms
// it demonstrates.
func writeProficiency(b *strings.Builder, l Language) {
	if p := l.Proficiency; p != nil {
		fmt.Fprintf(b, "  Proficiency: %d/100 · %s (%s confidence) · idiom range %d%%\n", p.Score, p.Level, p.Confidence, p.Range)
		names := make([]string, 0, len(p.Categories))
		for c := range p.Categories {
			names = append(names, c)
		}
		sort.Strings(names)
		line := "   "
		for _, c := range names {
			cs := p.Categories[c]
			line += fmt.Sprintf(" %s %d (%s);", c, cs.Score, cs.Band)
		}
		fmt.Fprintf(b, "%s\n", line[:len(line)-1])
	}
	if len(l.Idioms) > 0 {
		fmt.Fprintf(b, "  Shows\n")
		for _, i := range l.Idioms {
			fmt.Fprintf(b, "    + %s: %d uses in %d files\n", i.Title, i.Uses, i.Files)
		}
	}
}
