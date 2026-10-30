// Package vulns finds a project's locked dependency versions and checks them
// against the OSV vulnerability database (osv.dev), either live through its
// API or from per-ecosystem databases downloaded for offline use.
//
// Only a package's ecosystem, name, and version ever leave the machine: never
// paths, repository names, or code. A check that cannot finish never reports a
// package as safe: Coverage says what was checked and why the rest was not.
package vulns

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Package is one locked dependency version.
type Package struct {
	Ecosystem, Name, Version string
	// Lockfile is the path, relative to the project root, of the file the
	// version came from. It is for the local report only and is never sent.
	Lockfile string
}

// Advisory is one OSV vulnerability affecting a package.
type Advisory struct {
	ID      string
	Aliases []string // other identifiers for the same issue, such as CVE and GHSA IDs
	Summary string
	// Severity is "critical", "high", "medium", "low", or "" when the advisory
	// rates neither a CVSS v3 vector nor a database severity.
	Severity string
	Score    float64 // CVSS v3 base score; 0 when there is no v3 vector
	Fixed    []string
	URL      string // https://osv.dev/vulnerability/<id>
}

// Finding is a package with the advisories that affect its version.
type Finding struct {
	Package    Package
	Advisories []Advisory
}

// Coverage says what a check could and could not do. A package is either
// counted in Checked or named in its ecosystem's Unchecked reason, never both.
type Coverage struct {
	Mode string // "live" or "offline"
	// Checked counts distinct name and version pairs checked per ecosystem.
	Checked map[string]int
	// Unchecked explains, per ecosystem, which packages were not checked and
	// why. Unchecked packages must not be reported as safe.
	Unchecked map[string]string
	// DatabaseDates holds each offline database's publish date.
	DatabaseDates map[string]time.Time
}

// Checker checks packages for known vulnerabilities. It never panics or
// aborts on failure: it returns what it checked and reports the rest in
// Coverage.Unchecked.
type Checker interface {
	Check(ctx context.Context, pkgs []Package) ([]Finding, Coverage)
}

// supported lists the ecosystems this package reads and matches, spelled as
// OSV spells them.
var supported = []string{"npm", "PyPI", "Go", "Packagist", "RubyGems", "crates.io", "NuGet", "Pub"}

func isSupported(ecosystem string) bool { return slices.Contains(supported, ecosystem) }

// Ecosystems returns the distinct ecosystems of pkgs, sorted.
func Ecosystems(pkgs []Package) []string {
	var out []string
	for _, p := range pkgs {
		if !slices.Contains(out, p.Ecosystem) {
			out = append(out, p.Ecosystem)
		}
	}
	sort.Strings(out)
	return out
}

// normalizeName returns the form a package name is matched under offline.
// PyPI names are case-insensitive with -, _, and . equivalent (PEP 503);
// Packagist and NuGet names are case-insensitive. Others are exact.
func normalizeName(ecosystem, name string) string {
	switch ecosystem {
	case "PyPI":
		var b strings.Builder
		sep := false
		for _, r := range strings.ToLower(name) {
			if r == '-' || r == '_' || r == '.' {
				sep = true
				continue
			}
			if sep && b.Len() > 0 {
				b.WriteByte('-')
			}
			sep = false
			b.WriteRune(r)
		}
		return b.String()
	case "Packagist", "NuGet":
		return strings.ToLower(name)
	}
	return name
}

// queryVersion returns the version as OSV records it. go.mod writes module
// versions with a v prefix; OSV's Go database does not.
func queryVersion(ecosystem, version string) string {
	version = strings.TrimSpace(version)
	if ecosystem == "Go" {
		return strings.TrimPrefix(version, "v")
	}
	return version
}

// unparseable is the Unchecked reason for a version its ecosystem's
// comparator does not understand.
const unparseable = "version not understood"

// checkable returns why a package cannot be checked, or "" if it can.
func checkable(p Package) string {
	if !isSupported(p.Ecosystem) {
		return "ecosystem not supported"
	}
	if _, ok := parseVersion(p.Ecosystem, queryVersion(p.Ecosystem, p.Version)); !ok {
		return unparseable
	}
	return ""
}

func advisoryURL(id string) string { return "https://osv.dev/vulnerability/" + id }

// unreachableReasons start the Unchecked reasons of a live check that could
// not reach OSV, as opposed to packages that could not be checked anywhere.
var unreachableReasons = []string{"OSV did not answer", "OSV query failed", "OSV answered"}

// Unreachable reports whether a live check left packages unchecked because
// OSV could not be reached or failed, so downloaded databases may help.
func (c Coverage) Unreachable() bool {
	for _, reason := range c.Unchecked {
		for _, r := range unreachableReasons {
			if strings.Contains(reason, r) {
				return true
			}
		}
	}
	return false
}

// tally accumulates Coverage while a check runs.
type tally struct {
	mode     string
	checked  map[string]map[string]bool     // ecosystem -> name@version
	problems map[string]map[string][]string // ecosystem -> reason -> name@version
	dates    map[string]time.Time
}

func newTally(mode string) *tally {
	return &tally{mode: mode, checked: map[string]map[string]bool{},
		problems: map[string]map[string][]string{}, dates: map[string]time.Time{}}
}

func label(p Package) string { return p.Name + "@" + p.Version }

func (t *tally) check(p Package) {
	if t.checked[p.Ecosystem] == nil {
		t.checked[p.Ecosystem] = map[string]bool{}
	}
	t.checked[p.Ecosystem][label(p)] = true
}

func (t *tally) skip(p Package, reason string) {
	if t.problems[p.Ecosystem] == nil {
		t.problems[p.Ecosystem] = map[string][]string{}
	}
	if l := label(p); !slices.Contains(t.problems[p.Ecosystem][reason], l) {
		t.problems[p.Ecosystem][reason] = append(t.problems[p.Ecosystem][reason], l)
	}
}

// maxNamed bounds how many packages an Unchecked reason names.
const maxNamed = 5

func (t *tally) coverage() Coverage {
	c := Coverage{Mode: t.mode, Checked: map[string]int{}, Unchecked: map[string]string{}, DatabaseDates: t.dates}
	for eco, set := range t.checked {
		c.Checked[eco] = len(set)
	}
	for eco, reasons := range t.problems {
		var parts []string
		for reason, labels := range reasons {
			sort.Strings(labels)
			s := fmt.Sprintf("%s (%d): %s", reason, len(labels), strings.Join(labels[:min(len(labels), maxNamed)], ", "))
			if len(labels) > maxNamed {
				s += fmt.Sprintf(" and %d more", len(labels)-maxNamed)
			}
			parts = append(parts, s)
		}
		sort.Strings(parts)
		c.Unchecked[eco] = strings.Join(parts, "; ")
	}
	return c
}

func sortFindings(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i].Package, fs[j].Package
		if a.Ecosystem != b.Ecosystem {
			return a.Ecosystem < b.Ecosystem
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Lockfile < b.Lockfile
	})
	for _, f := range fs {
		sort.Slice(f.Advisories, func(i, j int) bool { return f.Advisories[i].ID < f.Advisories[j].ID })
	}
}
