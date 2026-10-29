// Package secrets detects credentials committed in source and configuration
// files. Files that hold secrets by design (.env, key files, and similar) are
// excluded by the engine and never read; see analysis.IsProtected. Findings carry
// the rule and line only; matched values never leave this package.
package secrets

import (
	"bytes"
	"context"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/rburns629/urfluency/analysis"
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
	// varRef matches a variable reference such as $(DB_PASSWORD) in Makefiles and
	// Kubernetes manifests, or $PASSWORD and $$PASSWORD in shells and Makefiles.
	varRef = regexp.MustCompile(`\$\$?\(?[A-Za-z_]`)
	// commentPrefixes start comment lines, where generic matches are examples.
	commentPrefixes = []string{"#", "//", "--", "/*", "*", ";", "<!--"}
)

// Analyze reports credential findings by rule and line.
func (Scanner) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	var findings []analysis.Finding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		generic := !isTestPath(f.Path)
		lines := bytes.Split(f.Content, []byte("\n"))
		for i, line := range lines {
			text := string(line)
			rule := match(text, generic)
			if rule == "" || (rule == "sec.api-token" && firebaseWebKey(text, lines, i)) {
				continue
			}
			findings = append(findings, analysis.Finding{Rule: rule, Path: f.Path, Line: i + 1})
		}
	}
	return findings, nil
}

// firebaseKey matches the apiKey entry of a Firebase web config.
var firebaseKey = regexp.MustCompile(`\bapiKey["']?\s*:\s*["']AIza[0-9A-Za-z_-]{35}["']`)

// firebaseWebKey reports whether line i is the apiKey of a Firebase web config:
// an apiKey entry with authDomain and messagingSenderId or appId within ten
// lines. Firebase web keys ship to every browser by design; access is governed by
// Firebase Security Rules, so they are not secrets. Other Google keys are.
func firebaseWebKey(line string, lines [][]byte, i int) bool {
	if !firebaseKey.MatchString(line) {
		return false
	}
	window := bytes.Join(lines[max(0, i-10):min(len(lines), i+11)], []byte("\n"))
	return bytes.Contains(window, []byte("authDomain")) &&
		(bytes.Contains(window, []byte("messagingSenderId")) || bytes.Contains(window, []byte("appId")))
}

// match returns the first rule a line violates, or "". Generic checks (URL
// credentials and password assignments) are skipped in test files, where fixture
// passwords are expected, and on comment lines, where they are examples. Known
// token formats are reported everywhere.
func match(line string, generic bool) string {
	for _, p := range patterns {
		// AWS documents sample credentials such as AKIAIOSFODNN7EXAMPLE and
		// ...EXAMPLEKEY; they are not secrets.
		if m := p.re.FindString(line); m != "" && !strings.Contains(m, "EXAMPLE") {
			return p.rule
		}
	}
	if !generic || isComment(line) {
		return ""
	}
	if m := urlCredentials.FindStringSubmatch(line); m != nil && !isPlaceholder(m[1]+m[2]) && !strings.Contains(line, "localhost") {
		return "sec.credentials-in-url"
	}
	if m := assignment.FindStringSubmatch(line); m != nil && !isPlaceholder(m[1]) && !namesCredential(m[1]) && entropy(m[1]) >= 3.0 {
		return "sec.hardcoded-password"
	}
	return ""
}

// namesCredential reports whether a value names a credential rather than holding
// one, such as the autocomplete token "new-password" in a JSX ternary.
func namesCredential(v string) bool {
	lower := strings.ToLower(v)
	for _, word := range []string{"password", "passwd", "secret", "token"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func isComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, p := range commentPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func isPlaceholder(v string) bool {
	if varRef.MatchString(v) {
		return true
	}
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
