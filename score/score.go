// Package score rates a whole repository, every author's code, for repository
// badges. It runs the same analysis and scoring as a personal scan, with
// authorship set to everyone, and returns the same payload an upload sends:
// the report's summary and combined result, without locations or names.
package score

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/upload"

	// Registered analyzers.
	_ "github.com/rburns629/urfluency/analyzers/c"
	_ "github.com/rburns629/urfluency/analyzers/cpp"
	_ "github.com/rburns629/urfluency/analyzers/csharp"
	_ "github.com/rburns629/urfluency/analyzers/dart"
	_ "github.com/rburns629/urfluency/analyzers/golang"
	_ "github.com/rburns629/urfluency/analyzers/java"
	_ "github.com/rburns629/urfluency/analyzers/javascript"
	_ "github.com/rburns629/urfluency/analyzers/kotlin"
	_ "github.com/rburns629/urfluency/analyzers/php"
	_ "github.com/rburns629/urfluency/analyzers/python"
	_ "github.com/rburns629/urfluency/analyzers/ruby"
	_ "github.com/rburns629/urfluency/analyzers/rust"
	_ "github.com/rburns629/urfluency/analyzers/secrets"
	_ "github.com/rburns629/urfluency/analyzers/swift"
)

// ErrNoHistory means the repository has no commits to score.
var ErrNoHistory = errors.New("the repository has no commits")

// Repo scores the Git repository at dir: its files at HEAD, and its history
// since since for the practice ratings, which need the commits in that window
// (a clone with --depth=1 rates code but not commit messages or testing).
// tool is the version of the program scoring it. The payload claims the
// self-reported tier; a service assigns the tier it can verify itself.
func Repo(ctx context.Context, dir string, since time.Time, tool string) (upload.Payload, error) {
	res, err := analysis.Scan(ctx, gitdata.Repo{Dir: dir}, analysis.Options{Emails: gitdata.Everyone, Since: since})
	if errors.Is(err, gitdata.ErrNoCommits) {
		return upload.Payload{}, ErrNoHistory
	}
	if err != nil {
		return upload.Payload{}, err
	}
	res = analysis.Merge([]string{"."}, []analysis.Result{res})
	r := report.Build(res, since, false)
	r.Combined = report.BuildCombined(res, analysis.Result{})
	if res.OwnedLines > 0 {
		r.Practices = report.BuildPractices(res)
	}
	report.Summarize(&r)
	summary, err := json.Marshal(r.Summary)
	if err != nil {
		return upload.Payload{}, err
	}
	combined, err := json.Marshal(r.Combined)
	if err != nil {
		return upload.Payload{}, err
	}
	p := upload.Payload{Format: upload.Format, Tool: tool, Report: r.Format, Catalog: r.Catalog, Model: report.ProficiencyModel,
		Tier: upload.SelfReported, Since: r.Since, Summary: summary, Combined: combined}
	return p, upload.Validate(p)
}
