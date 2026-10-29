package analysis

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

// ToolUse is how much the user worked with one tool, judged from the paths of
// files they committed to (or, without Git, files present). Contents are never
// read for this, so tool files that hold values, such as .tfvars, stay closed.
type ToolUse struct {
	Repos   int
	Files   int
	Commits int
	Lines   int       // lines added in the user's commits
	Last    time.Time // the user's latest commit to the tool's files
}

// tools identifies the tool behind a path. Kinds group tools in the report.
var tools = []struct {
	name, kind string
	match      func(p, base string) bool
}{
	{"terraform", "infrastructure", func(p, base string) bool { return hasExt(base, ".tf", ".tfvars", ".hcl") }},
	{"docker", "infrastructure", func(p, base string) bool {
		return base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".dockerfile") ||
			strings.HasPrefix(base, "docker-compose") || base == "compose.yml" || base == "compose.yaml"
	}},
	{"helm", "infrastructure", func(p, base string) bool { return base == "Chart.yaml" }},
	{"kustomize", "infrastructure", func(p, base string) bool { return base == "kustomization.yaml" || base == "kustomization.yml" }},
	{"serverless", "infrastructure", func(p, base string) bool { return base == "serverless.yml" || base == "serverless.yaml" }},
	{"pulumi", "infrastructure", func(p, base string) bool { return base == "Pulumi.yaml" || base == "Pulumi.yml" }},
	{"ansible", "infrastructure", func(p, base string) bool { return base == "ansible.cfg" }},
	{"nginx", "infrastructure", func(p, base string) bool { return base == "nginx.conf" || strings.HasSuffix(base, ".nginx.conf") }},
	{"github-actions", "ci", func(p, base string) bool {
		return strings.HasPrefix(p, ".github/workflows/") && hasExt(base, ".yml", ".yaml")
	}},
	{"dependabot", "ci", func(p, base string) bool { return p == ".github/dependabot.yml" || p == ".github/dependabot.yaml" }},
	{"renovate", "ci", func(p, base string) bool { return base == "renovate.json" || base == "renovate.json5" }},
	{"circleci", "ci", func(p, base string) bool { return strings.HasSuffix(p, ".circleci/config.yml") }},
	{"bitbucket-pipelines", "ci", func(p, base string) bool { return base == "bitbucket-pipelines.yml" }},
	{"gitlab-ci", "ci", func(p, base string) bool { return base == ".gitlab-ci.yml" }},
	{"jenkins", "ci", func(p, base string) bool { return base == "Jenkinsfile" }},
	{"cloud-build", "ci", func(p, base string) bool { return base == "cloudbuild.yaml" || base == "cloudbuild.yml" }},
	{"azure-pipelines", "ci", func(p, base string) bool { return base == "azure-pipelines.yml" }},
	{"make", "build", func(p, base string) bool { return base == "Makefile" || hasExt(base, ".mk") }},
	{"cmake", "build", func(p, base string) bool { return base == "CMakeLists.txt" || hasExt(base, ".cmake") }},
	{"bazel", "build", func(p, base string) bool {
		return base == "BUILD.bazel" || base == "WORKSPACE" || base == "MODULE.bazel"
	}},
	{"vite", "build", func(p, base string) bool { return strings.HasPrefix(base, "vite.config.") }},
	{"webpack", "build", func(p, base string) bool { return strings.HasPrefix(base, "webpack.config.") }},
	{"rollup", "build", func(p, base string) bool { return strings.HasPrefix(base, "rollup.config.") }},
	{"turborepo", "build", func(p, base string) bool { return base == "turbo.json" }},
	{"nx", "build", func(p, base string) bool { return base == "nx.json" }},
	{"babel", "build", func(p, base string) bool {
		return strings.HasPrefix(base, "babel.config.") || strings.HasPrefix(base, ".babelrc")
	}},
	{"tailwind", "build", func(p, base string) bool { return strings.HasPrefix(base, "tailwind.config.") }},
	{"gradle", "build", func(p, base string) bool { return strings.HasPrefix(base, "build.gradle") }},
	{"maven", "build", func(p, base string) bool { return base == "pom.xml" }},
	{"npm", "packages", func(p, base string) bool { return base == "package.json" }},
	{"cargo", "packages", func(p, base string) bool { return base == "Cargo.toml" }},
	{"dotnet", "packages", func(p, base string) bool { return hasExt(base, ".csproj", ".sln", ".fsproj", ".vbproj") }},
	{"swift-package-manager", "packages", func(p, base string) bool { return base == "Package.swift" }},
	{"cocoapods", "packages", func(p, base string) bool { return base == "Podfile" }},
	{"pub", "packages", func(p, base string) bool { return base == "pubspec.yaml" }},
	{"conan", "packages", func(p, base string) bool { return base == "conanfile.txt" || base == "conanfile.py" }},
	{"vcpkg", "packages", func(p, base string) bool { return base == "vcpkg.json" }},
	{"go-modules", "packages", func(p, base string) bool { return base == "go.mod" }},
	{"pip", "packages", func(p, base string) bool {
		return base == "pyproject.toml" || base == "Pipfile" || base == "setup.py" || (strings.HasPrefix(base, "requirements") && hasExt(base, ".txt"))
	}},
	{"pre-commit", "quality", func(p, base string) bool { return base == ".pre-commit-config.yaml" }},
	{"eslint", "quality", func(p, base string) bool {
		return strings.HasPrefix(base, ".eslintrc") || strings.HasPrefix(base, "eslint.config.")
	}},
	{"prettier", "quality", func(p, base string) bool {
		return strings.HasPrefix(base, ".prettierrc") || strings.HasPrefix(base, "prettier.config.")
	}},
	{"golangci-lint", "quality", func(p, base string) bool { return strings.HasPrefix(base, ".golangci.") }},
	{"biome", "quality", func(p, base string) bool { return base == "biome.json" || base == "biome.jsonc" }},
	{"ruff", "quality", func(p, base string) bool { return base == "ruff.toml" || base == ".ruff.toml" }},
	{"stylelint", "quality", func(p, base string) bool {
		return strings.HasPrefix(base, ".stylelintrc") || strings.HasPrefix(base, "stylelint.config.")
	}},
	{"husky", "quality", func(p, base string) bool { return strings.HasPrefix(p, ".husky/") || strings.Contains(p, "/.husky/") }},
	{"jest", "testing", func(p, base string) bool { return strings.HasPrefix(base, "jest.config.") }},
	{"vitest", "testing", func(p, base string) bool { return strings.HasPrefix(base, "vitest.config.") }},
	{"playwright", "testing", func(p, base string) bool { return strings.HasPrefix(base, "playwright.config.") }},
	{"cypress", "testing", func(p, base string) bool { return strings.HasPrefix(base, "cypress.config.") || base == "cypress.json" }},
	{"tox", "testing", func(p, base string) bool { return base == "tox.ini" }},
	{"firebase-hosting", "hosting", func(p, base string) bool { return base == "firebase.json" }},
	{"app-hosting", "hosting", func(p, base string) bool { return base == "apphosting.yaml" }},
	{"vercel", "hosting", func(p, base string) bool { return base == "vercel.json" }},
	{"netlify", "hosting", func(p, base string) bool { return base == "netlify.toml" }},
	{"fly-io", "hosting", func(p, base string) bool { return base == "fly.toml" }},
	{"heroku", "hosting", func(p, base string) bool { return base == "Procfile" }},
}

// ToolKind returns the group a tool belongs to, such as "infrastructure".
func ToolKind(name string) string {
	for _, t := range tools {
		if t.name == name {
			return t.kind
		}
	}
	return ""
}

// toolOf returns the tool a path belongs to, or "".
func toolOf(p string) string {
	if inSkippedDir(p) || slices.ContainsFunc(strings.Split(path.Dir(p), "/"), IsDependencyDir) {
		return ""
	}
	base := path.Base(p)
	for _, t := range tools {
		if t.match(p, base) {
			return t.name
		}
	}
	return ""
}

func hasExt(base string, exts ...string) bool {
	ext := strings.ToLower(path.Ext(base))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// toolUsage sums the user's commits to each tool's files.
func toolUsage(added map[string]map[string]int, commits map[string]gitdata.Commit, since time.Time) map[string]ToolUse {
	usage := map[string]ToolUse{}
	files := map[string]map[string]bool{}
	for hash, perFile := range added {
		c, ok := commits[hash]
		if !ok || c.Time.Before(since) {
			continue
		}
		counted := map[string]bool{}
		for p, n := range perFile {
			name := toolOf(p)
			if name == "" {
				continue
			}
			u := usage[name]
			u.Repos, u.Lines = 1, u.Lines+n
			if !counted[name] {
				counted[name] = true
				u.Commits++
			}
			if files[name] == nil {
				files[name] = map[string]bool{}
			}
			if !files[name][p] {
				files[name][p] = true
				u.Files++
			}
			if c.Time.After(u.Last) {
				u.Last = c.Time
			}
			usage[name] = u
		}
	}
	return usage
}

// GitHabits measures how the user writes commits. Subjects are measured, never
// kept.
type GitHabits struct {
	Commits      int
	ShortSubject int // subject of 72 characters or fewer
	Descriptive  int // subject of three or more words, not "wip"
	Conventional int // Conventional Commits form, such as "fix(api): ..."
	WithBody     int // message explains the change beyond its subject and trailers
}

var conventional = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^)]*\))?!?: \S`)

func gitHabits(commits map[string]gitdata.Commit, since time.Time) GitHabits {
	var g GitHabits
	for _, c := range commits {
		if c.Time.Before(since) {
			continue
		}
		g.Commits++
		s := strings.TrimSpace(c.Subject)
		if len([]rune(s)) <= 72 {
			g.ShortSubject++
		}
		if words := strings.Fields(s); len(words) >= 3 && !strings.EqualFold(words[0], "wip") {
			g.Descriptive++
		}
		if conventional.MatchString(s) {
			g.Conventional++
		}
		if c.HasBody {
			g.WithBody++
		}
	}
	return g
}
