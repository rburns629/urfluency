package analysis

import (
	"regexp"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/gitdata"
)

// MinCodeChange is the number of added code lines a commit needs before it is
// expected to come with tests; small fixes and renames are not.
const MinCodeChange = 10

// TestHabits counts the user's code-changing commits and those that also
// changed tests, split by whether the commit was marked AI-assisted.
type TestHabits struct {
	AssistedCommits, AssistedWithTests int
	OtherCommits, OtherWithTests       int
}

func testHabits(added map[string]map[string]int, commits map[string]gitdata.Commit, tools map[string][]string, since time.Time) TestHabits {
	var h TestHabits
	for hash, perPath := range added {
		c, ok := commits[hash]
		if !ok || c.Time.Before(since) {
			continue
		}
		code, tests := 0, false
		for p, n := range perPath {
			if inSkippedDir(p) || IsProtected(p) || ForPath(p) == nil || n == 0 {
				continue
			}
			if IsTestPath(p) {
				tests = true
			} else {
				code += n
			}
		}
		if code < MinCodeChange {
			continue
		}
		switch {
		case len(tools[hash]) > 0:
			h.AssistedCommits++
			if tests {
				h.AssistedWithTests++
			}
		default:
			h.OtherCommits++
			if tests {
				h.OtherWithTests++
			}
		}
	}
	return h
}

// CI practices, matched in CI configuration and in the Makefile and package.json
// scripts it may call.
var (
	ciFile = regexp.MustCompile(`^(\.github/workflows/[^/]+\.ya?ml|\.gitlab-ci\.yml|\.circleci/config\.yml|Jenkinsfile|azure-pipelines\.yml|bitbucket-pipelines\.yml|\.pre-commit-config\.yaml)$`)
	// Practices are checked in this order; each pattern names commands that run it.
	practicePatterns = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"tests", regexp.MustCompile(`\b(go test|pytest|npm (run )?test|yarn test|pnpm (run )?test|vitest|jest|cargo test|mvn test|gradle test|dotnet test)\b`)},
		{"lint", regexp.MustCompile(`\b(golangci-lint|staticcheck|go vet|eslint|ruff check|flake8|pylint|npm run lint|pnpm (run )?lint|yarn lint|clippy)\b`)},
		{"format", regexp.MustCompile(`\b(gofmt|goimports|prettier|black|ruff format|cargo fmt)\b`)},
		{"types", regexp.MustCompile(`\b(tsc|mypy|pyright|npm run (typecheck|type-check)|pnpm (run )?(typecheck|type-check))\b`)},
		{"security", regexp.MustCompile(`\b(govulncheck|npm audit|pip-audit|codeql|semgrep|gitleaks|trivy|snyk|osv-scanner)\b`)},
	}
)

// CIPractices records, for repositories where the user wrote part of the CI
// configuration, which practices that CI runs.
type CIPractices struct {
	Repos     int            // repositories where the user owns CI configuration lines
	Practices map[string]int // practice → repositories running it
}

// ciPractices inspects CI files the user owns lines in. buildScripts are the
// Makefile and package.json contents, which CI commands such as "make check"
// or "npm run check" delegate to.
func ciPractices(files []ownedFile, buildScripts []string) CIPractices {
	out := CIPractices{Practices: map[string]int{}}
	var config strings.Builder
	owned := false
	for _, f := range files {
		if !ciFile.MatchString(f.file.Path) {
			continue
		}
		config.Write(f.file.Content)
		config.WriteString("\n")
		if len(f.owned) > 0 {
			owned = true
		}
	}
	if !owned {
		return out
	}
	out.Repos = 1
	text := config.String() + "\n" + strings.Join(buildScripts, "\n")
	for _, p := range practicePatterns {
		if p.re.MatchString(text) {
			out.Practices[p.name] = 1
		}
	}
	return out
}

// isCIFile reports CI configuration paths; these are read even when no analyzer
// or scanner applies, so CI practices can be measured.
func isCIFile(p string) bool { return ciFile.MatchString(p) }

// buildScriptPaths are the top-level files CI commands commonly delegate to.
var buildScriptPaths = []string{"Makefile", "package.json"}

// PracticeNames lists the CI practices in report order.
func PracticeNames() []string {
	names := make([]string, 0, len(practicePatterns))
	for _, p := range practicePatterns {
		names = append(names, p.name)
	}
	return names
}
