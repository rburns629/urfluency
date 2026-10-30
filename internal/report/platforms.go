package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// Platform is how much the user worked with one platform, such as AWS or
// Salesforce, from the SDKs their files import and the deploy and
// infrastructure files they committed to. It rates experience, not quality.
// Only counts are kept: no resource names, accounts, or settings.
type Platform struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Repos  int    `json:"repositories"`
	Files  int    `json:"files"`
	Code   int    `json:"code_files"`
	Config int    `json:"config_files"`
	Score  int    `json:"score"`
	Level  string `json:"level"`
}

// BuildPlatforms lists the platforms the user worked with, highest rated first.
func BuildPlatforms(res analysis.Result) []Platform {
	out := []Platform{}
	for _, p := range analysis.Platforms {
		u := res.Platforms[p.ID]
		if u.Files == 0 {
			continue
		}
		score := platformExperience(u.Repos, u.Files)
		out = append(out, Platform{ID: p.ID, Title: p.Title, Repos: u.Repos, Files: u.Files, Code: u.Code, Config: u.Config, Score: score, Level: level(score)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// platformExperience rates a platform: fifteen repositories and 200 files
// rate 100, the scale of a medium design area.
func platformExperience(repos, files int) int { return experienceAt(repos, files, 15, 200) }

// evidence says what a platform rating counts.
func (p Platform) evidence() string {
	var parts []string
	if p.Code > 0 {
		parts = append(parts, count(p.Code, "file")+" using its SDKs")
	}
	if p.Config > 0 {
		parts = append(parts, count(p.Config, "deploy or infrastructure file"))
	}
	return fmt.Sprintf("%s: %s", count(p.Repos, "repo"), strings.Join(parts, ", "))
}

// writePlatforms prints platforms as counts; the summary rates them.
func writePlatforms(b *strings.Builder, heading string, platforms []Platform) {
	if len(platforms) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n", heading)
	for _, p := range platforms {
		fmt.Fprintf(b, "  %-34s %s\n", p.Title, p.evidence())
	}
}

// platformTitle names a platform by its ID.
func platformTitle(id string) string {
	for _, p := range analysis.Platforms {
		if p.ID == id {
			return p.Title
		}
	}
	return id
}
