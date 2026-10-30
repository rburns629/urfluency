// Package safety checks a repository for signs of malicious or privacy-invasive
// behavior before anyone runs it: install-time execution, remote code
// execution, data collection, credential access, obfuscation, persistence,
// supply-chain risks, and committed secrets.
//
// It is static analysis only. Nothing in the repository is executed, and a
// scan can find indicators but never prove a repository is safe; reports say
// so. The same engine runs in `urfluency check` and in the hosted scanner.
package safety

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/analyzers/secrets"
	"github.com/rburns629/urfluency/rules"
)

// Version identifies the rule set; reports carry it so results can be compared.
const Version = "0.1"

// Severity says how bad a behavior is if it is what it looks like.
type Severity string

const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
)

// Confidence says how likely a match is to be the behavior it names.
type Confidence string

const (
	Likely   Confidence = "high"
	Possible Confidence = "medium"
	Weak     Confidence = "low"
)

// Verdicts. A report never says "safe".
const (
	NoIndicators      = "No indicators found"
	ReviewRecommended = "Review recommended"
	HighRisk          = "High-risk indicators"
)

// Categories, in report order.
var Categories = []struct{ ID, Title string }{
	{"install", "Install-time execution"},
	{"remote-code", "Remote code execution"},
	{"data-collection", "Data collection"},
	{"credentials", "Credential access"},
	{"obfuscation", "Obfuscation"},
	{"persistence", "Persistence and abuse"},
	{"supply-chain", "Supply chain"},
	{"secrets", "Secrets"},
}

// Finding is one indicator.
type Finding struct {
	Rule       string     `json:"rule"`
	Category   string     `json:"category"`
	Title      string     `json:"title"`
	Severity   Severity   `json:"severity"`
	Confidence Confidence `json:"confidence"`
	Path       string     `json:"path"`
	Line       int        `json:"line,omitempty"`
	// Evidence is a short excerpt of the matched line. Secret findings never
	// carry evidence, so values are not reproduced.
	Evidence string `json:"evidence,omitempty"`
	Why      string `json:"why"`
}

// Report is the result of a scan.
type Report struct {
	Version   string    `json:"version"`
	Verdict   string    `json:"verdict"`
	Score     int       `json:"score"` // 0–100; 100 means nothing was found
	Files     int       `json:"files_checked"`
	Skipped   int       `json:"files_skipped"`
	Truncated bool      `json:"truncated,omitempty"` // the file limit was reached
	Findings  []Finding `json:"findings"`
	Counts    []Count   `json:"by_category"`
	Caveat    string    `json:"caveat"`
}

// Count is the number of findings in one category.
type Count struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Findings int    `json:"findings"`
}

// Limits bound a scan so a hostile repository cannot exhaust it.
type Limits struct {
	MaxFiles    int   // files read
	MaxFileSize int64 // bytes per file; larger files are skipped (binaries are still identified)
}

// DefaultLimits suit a typical repository.
var DefaultLimits = Limits{MaxFiles: 20000, MaxFileSize: 2 << 20}

// skipDirs are never read: version control metadata and installed
// dependencies, which are not the repository's own code. Committed
// dependencies under other names are still read.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true}

// Scan checks the files under dir.
func Scan(ctx context.Context, dir string, limits Limits) (Report, error) {
	r := Report{Version: Version, Findings: []Finding{}, Caveat: "A scan finds indicators of risky behavior. It cannot prove a repository is safe."}
	var files []analysis.File
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and devices are never followed
		}
		if len(files)+r.Skipped >= limits.MaxFiles {
			r.Truncated = true
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		content, err := readBounded(path, info.Size(), limits.MaxFileSize)
		if err != nil {
			r.Skipped++
			return nil
		}
		files = append(files, analysis.File{Path: rel, Content: content})
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return Report{}, err
	}
	r.Files = len(files)

	perFile := make([][]Finding, len(files))
	parallel(len(files), func(i int) { perFile[i] = checkFile(files[i]) })
	for _, found := range perFile {
		r.Findings = append(r.Findings, found...)
	}
	r.Findings = append(r.Findings, checkRepository(files)...)
	sec, err := secretFindings(ctx, files)
	if err != nil {
		return Report{}, err
	}
	r.Findings = append(r.Findings, sec...)
	for i := range r.Findings {
		r.Findings[i] = softenOutsideRuntime(r.Findings[i])
	}

	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if rank(a.Severity) != rank(b.Severity) {
			return rank(a.Severity) > rank(b.Severity)
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	r.Verdict, r.Score = judge(r.Findings)
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.Category]++
	}
	for _, c := range Categories {
		r.Counts = append(r.Counts, Count{Category: c.ID, Title: c.Title, Findings: counts[c.ID]})
	}
	return r, nil
}

// readBounded reads a file, or only its first bytes when it is over the limit,
// enough to recognize executables.
func readBounded(path string, size, limit int64) ([]byte, error) {
	if size <= limit {
		return os.ReadFile(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	if isExecutable(head[:n]) {
		return head[:n], nil
	}
	return nil, fmt.Errorf("%s: over %d bytes", path, limit)
}

// buildOnly reports files that run in tests, CI, or container builds rather
// than on the machine of someone installing or running the project.
func buildOnly(p string) (string, bool) {
	base := path.Base(p)
	switch {
	case analysis.IsTestPath(p) || strings.Contains("/"+p, "/testdata/") || strings.Contains("/"+p, "/fixtures/"):
		return "tests", true
	case strings.HasPrefix(p, ".github/workflows/") || base == ".gitlab-ci.yml" || strings.HasPrefix(p, ".circleci/") || base == "azure-pipelines.yml":
		return "CI", true
	case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".dockerfile"):
		return "container builds", true
	}
	return "", false
}

// softenOutsideRuntime lowers a finding in tests, CI, or container builds by one
// severity and to low confidence: it is still reported, but it does not run
// when someone installs or uses the project.
func softenOutsideRuntime(f Finding) Finding {
	where, ok := buildOnly(f.Path)
	if !ok || f.Rule == "safety.executable" {
		return f
	}
	switch f.Severity {
	case High:
		f.Severity = Medium
	case Medium:
		f.Severity = Low
	}
	f.Confidence = Weak
	f.Why = "In " + where + ", which do not run on install; " + strings.ToLower(f.Why[:1]) + f.Why[1:]
	return f
}

func rank(s Severity) int {
	switch s {
	case High:
		return 3
	case Medium:
		return 2
	}
	return 1
}

func weight(c Confidence) float64 {
	switch c {
	case Likely:
		return 1
	case Possible:
		return 0.6
	}
	return 0.3
}

// note reports a finding too minor to change the verdict: low severity with
// low confidence. Notes are still listed.
func note(f Finding) bool { return f.Severity == Low && f.Confidence == Weak }

// judge turns findings into a verdict and a 0–100 score. Any likely or
// possible high-severity finding is high risk; any other finding that is not
// a note means review. The score falls with each finding by severity and
// confidence; a rule counts at most three times, and notes cost at most ten
// points in all, so a large project with many harmless notes stays high.
func judge(findings []Finding) (string, int) {
	penalty := map[Severity]float64{High: 35, Medium: 12, Low: 4}
	perRule := map[string]int{}
	total, notes := 0.0, 0.0
	verdict := NoIndicators
	for _, f := range findings {
		switch {
		case f.Severity == High && f.Confidence != Weak:
			verdict = HighRisk
		case !note(f) && verdict == NoIndicators:
			verdict = ReviewRecommended
		}
		if perRule[f.Rule] < 3 {
			perRule[f.Rule]++
			if note(f) {
				notes += penalty[f.Severity] * weight(f.Confidence)
			} else {
				total += penalty[f.Severity] * weight(f.Confidence)
			}
		}
	}
	total += math.Min(notes, 10)
	score := int(math.Round(math.Max(0, 100-total)))
	if verdict == HighRisk {
		score = min(score, 40)
	}
	return verdict, score
}

// secretFindings reuses the CLI's credential scanner; values are never kept.
func secretFindings(ctx context.Context, files []analysis.File) ([]Finding, error) {
	sc := secrets.Scanner{}
	var candidates []analysis.File
	for _, f := range files {
		if sc.Applies(f.Path) && !isBinary(f.Content) {
			candidates = append(candidates, f)
		}
	}
	// One file per call, in parallel; the results keep the files' order.
	perFile := make([][]analysis.Finding, len(candidates))
	errs := make([]error, len(candidates))
	parallel(len(candidates), func(i int) {
		perFile[i], errs[i] = sc.Analyze(ctx, candidates[i:i+1])
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var out []Finding
	for _, f := range slices.Concat(perFile...) {
		title := "Committed secret"
		why := "A credential committed to a public repository can be used by anyone who finds it."
		if rule, ok := rules.Lookup(f.Rule); ok {
			title, why = rule.Title, rule.Why
		}
		sev, conf := Medium, Possible
		if isDoc(f.Path) {
			// Documentation shows example credentials far more often than real ones.
			sev, conf = Low, Weak
		}
		out = append(out, Finding{Rule: f.Rule, Category: "secrets", Title: title, Severity: sev, Confidence: conf, Path: f.Path, Line: f.Line, Why: why})
	}
	return out, nil
}

func isBinary(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// isExecutable recognizes ELF, Mach-O, and Windows PE files.
func isExecutable(b []byte) bool {
	switch {
	case bytes.HasPrefix(b, []byte("\x7fELF")):
		return true
	case bytes.HasPrefix(b, []byte("MZ")) && len(b) > 64:
		return true
	case len(b) >= 4:
		m := string(b[:4])
		return m == "\xfe\xed\xfa\xce" || m == "\xfe\xed\xfa\xcf" || m == "\xce\xfa\xed\xfe" || m == "\xcf\xfa\xed\xfe" || m == "\xca\xfe\xba\xbe"
	}
	return false
}

func excerpt(line string) string {
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return line
}

// parallel calls fn(i) for each i in [0, n) on every core Go uses.
func parallel(n int, fn func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				fn(i)
			}
		})
	}
	wg.Wait()
}
