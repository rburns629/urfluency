package safety

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// RuntimeAdvisory is a known vulnerability in a language runtime a project
// pins, such as the PHP version in its Dockerfile. Dependency databases cover
// packages, not the runtimes they run on, so these are checked here, offline.
// They are reported with vulnerable dependencies, apart from the verdict: an
// outdated runtime is a risk to fix, not a sign of malicious code.
type RuntimeAdvisory struct {
	Runtime string   // "php"
	Version string   // as pinned
	Path    string   // where it is pinned
	Line    int      // 1-based
	IDs     []string // CVE IDs
	Title   string
	Fixed   string // the release that fixes it on this line; "" when the line gets no fix
}

// phpFixes are the first PHP releases fixing PHP2Shell (CVE-2026-109618 and
// CVE-2026-109619), released 2026-10-21. Lines older than 8.2 are end of life
// and get no fix.
var phpFixes = map[string][3]int{"8.2": {8, 2, 35}, "8.3": {8, 3, 36}, "8.4": {8, 4, 27}, "8.5": {8, 5, 12}}

var phpIDs = []string{"CVE-2026-109618", "CVE-2026-109619"}

var (
	// dockerPHP matches a php base image pinned to an exact version, such as
	// FROM php:8.3.30-fpm-alpine or FROM docker.io/library/php:8.2.10 AS app.
	dockerPHP = regexp.MustCompile(`(?im)^\s*FROM\s+(?:--platform=\S+\s+)?(?:[\w.-]+(?::\d+)?/)?(?:library/)?php:(\d+\.\d+\.\d+)\b`)
	// toolVersionsPHP matches asdf and mise's .tool-versions entry.
	toolVersionsPHP = regexp.MustCompile(`(?m)^\s*php\s+(\d+\.\d+\.\d+)\b`)
	exactVersion    = regexp.MustCompile(`^\s*v?(\d+\.\d+\.\d+)\s*$`)
)

// RuntimeAdvisories finds runtime versions pinned in files that have known
// vulnerabilities. Floating pins such as php:8.3 are left out: a rebuild picks
// up the fix.
func RuntimeAdvisories(files []analysis.File) []RuntimeAdvisory {
	var out []RuntimeAdvisory
	for _, f := range files {
		base := path.Base(f.Path)
		switch {
		case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(strings.ToLower(base), ".dockerfile"):
			for _, m := range dockerPHP.FindAllSubmatchIndex(f.Content, -1) {
				out = appendPHP(out, string(f.Content[m[2]:m[3]]), f.Path, lineAt(f.Content, m[0]))
			}
		case base == ".tool-versions":
			if m := toolVersionsPHP.FindSubmatchIndex(f.Content); m != nil {
				out = appendPHP(out, string(f.Content[m[2]:m[3]]), f.Path, lineAt(f.Content, m[0]))
			}
		case base == ".php-version":
			if m := exactVersion.FindSubmatch(f.Content); m != nil {
				out = appendPHP(out, string(m[1]), f.Path, 1)
			}
		case base == "composer.json":
			// config.platform.php is the version Composer resolves for, and
			// usually the one deployed.
			var c struct {
				Config struct {
					Platform map[string]string `json:"platform"`
				} `json:"config"`
			}
			if json.Unmarshal(f.Content, &c) == nil {
				if m := exactVersion.FindStringSubmatch(c.Config.Platform["php"]); m != nil {
					out = appendPHP(out, m[1], f.Path, lineOf(f.Content, `"php"`))
				}
			}
		}
	}
	return out
}

func appendPHP(out []RuntimeAdvisory, version, p string, line int) []RuntimeAdvisory {
	fixed, vulnerable := phpFixed(version)
	if !vulnerable {
		return out
	}
	title := "PHP2Shell: remote code execution in the PHP interpreter"
	if fixed == "" {
		title = "PHP version past end of life, without the PHP2Shell fixes"
	}
	return append(out, RuntimeAdvisory{Runtime: "php", Version: version, Path: p, Line: line, IDs: phpIDs, Title: title, Fixed: fixed})
}

// phpFixed reports whether an exact PHP version is affected by PHP2Shell, and
// the release on its line that fixes it ("" for end-of-life lines).
func phpFixed(version string) (string, bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", false
		}
		v[i] = n
	}
	fix, supported := phpFixes[fmt.Sprintf("%d.%d", v[0], v[1])]
	switch {
	case !supported && (v[0] < 8 || v[0] == 8 && v[1] < 2):
		return "", true // end of life: no fix on this line
	case !supported:
		return "", false // a newer line than this check knows
	case v[2] < fix[2]:
		return fmt.Sprintf("%d.%d.%d", fix[0], fix[1], fix[2]), true
	}
	return "", false
}

// RuntimeFiles reads the small files that pin a runtime version: Dockerfiles,
// .tool-versions, .php-version, and composer.json, skipping dependency, hidden,
// test data, and protected folders, for RuntimeAdvisories.
func RuntimeFiles(root string) []analysis.File {
	var out []analysis.File
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || analysis.IsDependencyDir(name) || analysis.IsTestDataDir(name) || analysis.IsProtectedDir(name)) {
				return filepath.SkipDir
			}
			return nil
		}
		lower := strings.ToLower(name)
		if !d.Type().IsRegular() || len(out) >= 200 ||
			!(name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || strings.HasSuffix(lower, ".dockerfile") ||
				name == ".tool-versions" || name == ".php-version" || name == "composer.json") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if analysis.IsProtected(filepath.ToSlash(rel)) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 1<<20 {
			return nil
		}
		if content, err := os.ReadFile(p); err == nil {
			out = append(out, analysis.File{Path: filepath.ToSlash(rel), Content: content})
		}
		return nil
	})
	return out
}
