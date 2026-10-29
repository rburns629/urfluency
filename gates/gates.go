// Package gates names the quality gates of clean-agent-code's QUALITY_GATES.md
// and recognizes commands that run them, in CI configuration and in the commands
// coding agents run.
package gates

import "regexp"

// Gates, in report order.
const (
	Format   = "format"
	Lint     = "lint"
	Types    = "types"
	Build    = "build"
	Tests    = "tests"
	Security = "security" // dependency vulnerability, secret, and security scans
	Other    = "other"    // a check that delegates to an unknown target, such as "make check"
)

// Mandatory gates apply to every change where the language supports them;
// Security is conditional on the change.
var Mandatory = []string{Format, Lint, Types, Build, Tests}

// Named lists every gate except Other.
var Named = []string{Format, Lint, Types, Build, Tests, Security}

// all is Named plus Other, the order of Set bits.
var all = append(append([]string{}, Named...), Other)

// Set is a compact, comparable set of gates.
type Set uint8

// With returns the set plus the named gates.
func (s Set) With(names ...string) Set {
	for _, n := range names {
		for i, g := range all {
			if g == n {
				s |= 1 << i
			}
		}
	}
	return s
}

// Has reports whether the set includes a gate.
func (s Set) Has(name string) bool {
	for i, g := range all {
		if g == name {
			return s&(1<<i) != 0
		}
	}
	return false
}

// Standard is the clean-agent-code document that defines the gates.
const Standard = "https://github.com/rburns629/clean-agent-code/blob/main/standards/QUALITY_GATES.md"

var patterns = []struct {
	gate string
	re   *regexp.Regexp
}{
	{Format, regexp.MustCompile(`\b(gofmt|goimports|prettier|black|ruff format|cargo fmt|rustfmt|clang-format|dotnet format|google-java-format|spotless(Apply|Check)|ktfmt|swiftformat|swift-format|dart format|flutter format|shfmt)\b`)},
	{Lint, regexp.MustCompile(`\b(golangci-lint|staticcheck|go vet|eslint|ruff check|flake8|pylint|clippy|clang-tidy|cppcheck|checkstyle|spotbugs|pmd|ktlint|detekt|swiftlint|dart analyze|flutter analyze|shellcheck|Invoke-ScriptAnalyzer|(npm|pnpm|yarn|bun)( run)? lint|make lint)\b`)},
	{Types, regexp.MustCompile(`\b(tsc|mypy|pyright|cargo check|(npm|pnpm|yarn|bun)( run)? (typecheck|type-check))\b`)},
	{Build, regexp.MustCompile(`\b(go build|cargo build|cmake --build|msbuild|mvn (package|compile|verify|install)|(gradle|gradlew) (build|assemble)|dotnet build|swift build|xcodebuild( build)?|flutter build|(npm|pnpm|yarn|bun)( run)? build|make build)\b`)},
	{Tests, regexp.MustCompile(`\b(go test|pytest|python -m unittest|vitest|jest|cargo (test|nextest)|ctest|mvn (test|verify)|(gradle|gradlew) (test|check)|dotnet test|swift test|xcodebuild test|flutter test|dart test|bats|Invoke-Pester|(npm|pnpm|yarn|bun)( run)? test|make test)\b`)},
	{Security, regexp.MustCompile(`\b(govulncheck|npm audit|pnpm audit|yarn audit|pip-audit|cargo (audit|deny)|dependency-check|dotnet list package --vulnerable|codeql|semgrep|gitleaks|trivy|snyk|osv-scanner)\b`)},
}

// delegated targets run some checks without naming which.
var delegated = regexp.MustCompile(`\b(make|just|task) (check|verify|ci)\b|\b(npm|pnpm|yarn)( run)? (check|verify|ci)\b`)

// Run returns the gates that text runs, in Named order, with Other when it only
// delegates to an unknown check target.
func Run(text string) []string {
	var out []string
	for _, p := range patterns {
		if p.re.MatchString(text) {
			out = append(out, p.gate)
		}
	}
	if len(out) == 0 && delegated.MatchString(text) {
		out = append(out, Other)
	}
	return out
}

// compiled languages type-check when they build.
var compiled = map[string]bool{"go": true, "rust": true, "c": true, "cpp": true, "csharp": true, "java": true,
	"kotlin": true, "swift": true, "dart": true}

// Applies reports whether a mandatory gate applies to a language: type checking
// and build apply where the language has them (compiled languages and
// TypeScript); format, lint, and tests apply everywhere.
func Applies(gate, language string) bool {
	switch gate {
	case Types, Build:
		return compiled[language] || language == "typescript"
	}
	return true
}

// Implied returns the gates a language satisfies through others: building a
// compiled language type-checks it, and its test and lint tools compile it (go
// test, go vet, cargo test, clippy, mvn test, and so on).
func Implied(language string, ran map[string]bool) map[string]bool {
	out := map[string]bool{}
	for g := range ran {
		out[g] = true
	}
	if compiled[language] && (ran[Tests] || ran[Build] || ran[Lint]) {
		out[Types], out[Build] = true, true
	}
	return out
}
