package analysis

import (
	"testing"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

func TestToolOf(t *testing.T) {
	for p, want := range map[string]string{
		"infra/main.tf":                      "terraform",
		"infra/prod.tfvars":                  "terraform", // counted by path; contents stay closed
		"Dockerfile":                         "docker",
		"svc/Dockerfile.dev":                 "docker",
		"docker-compose.override.yml":        "docker",
		"charts/api/Chart.yaml":              "helm",
		".github/workflows/ci.yml":           "github-actions",
		"Jenkinsfile":                        "jenkins",
		"Makefile":                           "make",
		"web/package.json":                   "npm",
		"node_modules/left-pad/package.json": "",
		"go.mod":                             "go-modules",
		"requirements-dev.txt":               "pip",
		".golangci.yml":                      "golangci-lint",
		"web/vite.config.ts":                 "vite",
		"jest.config.js":                     "jest",
		"e2e/playwright.config.ts":           "playwright",
		".github/dependabot.yml":             "dependabot",
		"firebase.json":                      "firebase-hosting",
		"web/apphosting.yaml":                "app-hosting",
		".husky/pre-commit":                  "husky",
		"infra/Pulumi.yaml":                  "pulumi",
		"src/main.go":                        "",
		"docs/compose.md":                    "",
	} {
		if got := toolOf(p); got != want {
			t.Errorf("toolOf(%s) = %q, want %q", p, got, want)
		}
	}
}

func TestToolUsageAndGitHabits(t *testing.T) {
	jan, mar := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), time.Date(2025, 3, 9, 0, 0, 0, 0, time.UTC)
	commits := map[string]gitdata.Commit{
		"a": {Time: jan, Subject: "feat(infra): add the staging network", HasBody: true},
		"b": {Time: mar, Subject: "wip"},
		"c": {Time: mar, Subject: "Bump Terraform provider and pin the Go toolchain version for reproducible builds"},
	}
	added := map[string]map[string]int{
		"a": {"infra/main.tf": 40, "infra/vars.tf": 10},
		"b": {"infra/main.tf": 2, "src/app.go": 30},
		"c": {"infra/main.tf": 1, "go.mod": 1},
	}
	tools := toolUsage(added, commits, time.Time{})
	if tf := tools["terraform"]; tf.Commits != 3 || tf.Files != 2 || tf.Lines != 53 || !tf.Last.Equal(mar) {
		t.Errorf("terraform = %+v", tf)
	}
	if gm := tools["go-modules"]; gm.Commits != 1 || gm.Files != 1 {
		t.Errorf("go-modules = %+v", gm)
	}
	g := gitHabits(commits, time.Time{})
	want := GitHabits{Commits: 3, ShortSubject: 2, Descriptive: 2, Conventional: 1, WithBody: 1}
	if g != want {
		t.Errorf("git = %+v, want %+v", g, want)
	}
	if g := gitHabits(commits, mar); g.Commits != 2 {
		t.Errorf("since is not applied: %+v", g)
	}
}
