package report

import (
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestBuildPlatforms(t *testing.T) {
	res := analysis.Result{Platforms: map[string]analysis.PlatformUse{
		"aws":        {Usage: analysis.Usage{Repos: 15, Files: 200}, Code: 150, Config: 50},
		"salesforce": {Usage: analysis.Usage{Repos: 1, Files: 3}, Config: 3},
		"gcp":        {}, // nothing counted
	}}
	got := BuildPlatforms(res)
	if len(got) != 2 || got[0].ID != "aws" || got[0].Score != 100 || got[0].Level != "Expert" || got[1].Title != "Salesforce" {
		t.Fatalf("platforms = %+v", got)
	}
	if e := got[0].evidence(); e != "15 repos: 150 files using its SDKs, 50 deploy or infrastructure files" {
		t.Errorf("evidence = %q", e)
	}

	r := Report{Platforms: got}
	Summarize(&r)
	var rated []Rating
	for _, x := range r.Summary.Ratings {
		if x.Area == "platform" {
			rated = append(rated, x)
		}
	}
	if len(rated) != 2 || rated[0].Name != "aws" || rated[0].Basis != "experience" {
		t.Errorf("platform ratings = %+v", rated)
	}
	if !strings.Contains(strings.Join(r.Summary.Strengths, "\n"), "Platform experience: AWS 100 (Expert") {
		t.Errorf("strengths = %q", r.Summary.Strengths)
	}
	var b strings.Builder
	writeSummary(&b, r.Summary, nil)
	if !strings.Contains(b.String(), "platform   AWS") {
		t.Errorf("ratings should name platforms by title:\n%s", b.String())
	}
}
