// Package secrets detects committed credentials in any text file. Findings carry
// the rule and line only; matched values never leave this package.
package secrets

import (
	"bytes"
	"context"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/rburns629/urfluency/internal/analysis"
)

func init() { analysis.RegisterScanner(Scanner{}) }

// Scanner implements analysis.Scanner for credentials.
type Scanner struct{}

func (Scanner) Name() string { return "secrets" }

// skipNames are dependency lockfiles, whose hashes look like secrets.
var skipNames = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "go.sum": true,
	"poetry.lock": true, "Pipfile.lock": true, "Cargo.lock": true, "composer.lock": true, "Gemfile.lock": true,
}

var skipExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".svg": true, ".webp": true,
	".pdf": true, ".zip": true, ".gz": true, ".woff": true, ".woff2": true, ".ttf": true, ".mp4": true,
	".lock": true, ".sum": true,
}

// Applies reports whether a file is a candidate; minified bundles are excluded.
func (Scanner) Applies(p string) bool {
	base := path.Base(p)
	if skipNames[base] || strings.Contains(base, ".min.") {
		return false
	}
	return !skipExts[strings.ToLower(path.Ext(base))]
}

type pattern struct {
	rule string
	re   *regexp.Regexp
}

// patterns match well-known credential formats. They are anchored on fixed
// prefixes, so they apply in every file, including tests.
var patterns = []pattern{
	{"sec.private-key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`)},
	{"sec.cloud-credential", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"sec.cloud-credential", regexp.MustCompile(`(?i)aws_secret_access_key["']?\s*[:=]\s*["']?[A-Za-z0-9/+]{40}\b`)},
	{"sec.cloud-credential", regexp.MustCompile(`AccountKey=[A-Za-z0-9+/=]{80,}`)},
	{"sec.api-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36}\b|\bgithub_pat_[A-Za-z0-9_]{60,}`)},
	{"sec.api-token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"sec.api-token", regexp.MustCompile(`\b[rs]k_live_[A-Za-z0-9]{20,}`)},
	{"sec.api-token", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"sec.api-token", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}|\bsk-(?:proj-)?[A-Za-z0-9_-]{40,}`)},
	{"sec.api-token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{"sec.api-token", regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b`)},
}

var (
	urlCredentials = regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://([^/\s:@'"]+):([^/\s@'"]{3,})@`)
	assignment     = regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|private[_-]?key)\b["']?\s*[:=]\s*["']([^"'\s]{8,})["']`)
	// placeholders mark template or example values; "{" covers ${X}, {{x}}, and
	// Python {name} interpolation.
	placeholders = []string{"example", "sample", "changeme", "change_me", "your", "xxx", "placeholder",
		"dummy", "fake", "redacted", "<", "{", "%s", "process.env", "os.environ", "getenv", "****"}
)

// Analyze reports credential findings by rule and line.
func (Scanner) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	var findings []analysis.Finding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		generic := !isTestPath(f.Path)
		for i, line := range bytes.Split(f.Content, []byte("\n")) {
			text := string(line)
			if rule := match(text, generic); rule != "" {
				findings = append(findings, analysis.Finding{Rule: rule, Path: f.Path, Line: i + 1})
			}
		}
	}
	return findings, nil
}

// match returns the first rule a line violates, or "". Generic checks (URL
// credentials and password assignments) are skipped in test files, where
// fixture passwords are expected.
func match(line string, generic bool) string {
	for _, p := range patterns {
		if p.re.MatchString(line) {
			return p.rule
		}
	}
	if !generic {
		return ""
	}
	if m := urlCredentials.FindStringSubmatch(line); m != nil && !isPlaceholder(m[1]+m[2]) && !strings.Contains(line, "localhost") {
		return "sec.credentials-in-url"
	}
	if m := assignment.FindStringSubmatch(line); m != nil && !isPlaceholder(m[1]) && entropy(m[1]) >= 3.0 {
		return "sec.hardcoded-password"
	}
	return ""
}

func isPlaceholder(v string) bool {
	lower := strings.ToLower(v)
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// entropy is the Shannon entropy in bits per character; random secrets score
// higher than words.
func entropy(s string) float64 {
	counts := map[rune]float64{}
	for _, r := range s {
		counts[r]++
	}
	var h float64
	n := float64(len([]rune(s)))
	for _, c := range counts {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

func isTestPath(p string) bool {
	base := path.Base(p)
	return strings.Contains(p, "/test/") || strings.Contains(p, "/tests/") || strings.HasPrefix(p, "test/") ||
		strings.HasPrefix(p, "tests/") || strings.Contains(base, "_test.") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_")
}
