package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Vulnerabilities are known vulnerabilities in the scanned projects' locked
// dependencies and pinned runtimes. They are for the person who ran the scan:
// never part of the summary or an upload.
type Vulnerabilities struct {
	// Mode is "live" (osv.dev), "offline" (downloaded databases), or "" when
	// nothing could be checked.
	Mode          string               `json:"mode"`
	Checked       map[string]int       `json:"checked"`             // ecosystem: packages checked
	Unchecked     map[string]string    `json:"unchecked,omitempty"` // ecosystem: why not
	DatabaseDates map[string]time.Time `json:"database_dates,omitempty"`
	// Notes say what to do when checking was incomplete, such as how to
	// download databases for offline use.
	Notes        []string               `json:"notes,omitempty"`
	Dependencies []VulnerableDependency `json:"dependencies"`
	Runtimes     []VulnerableRuntime    `json:"runtimes"`
}

// VulnerableDependency is one locked package version with its advisories.
type VulnerableDependency struct {
	Ecosystem  string         `json:"ecosystem"`
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Lockfile   string         `json:"lockfile"` // relative to the scanned path
	Advisories []VulnAdvisory `json:"advisories"`
	Severity   string         `json:"severity"` // the most severe advisory's
}

// VulnAdvisory is one advisory affecting a dependency.
type VulnAdvisory struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Severity string   `json:"severity,omitempty"`
	Score    float64  `json:"cvss,omitempty"`
	Fixed    []string `json:"fixed,omitempty"`
	URL      string   `json:"url"`
}

// VulnerableRuntime is a pinned runtime version with known vulnerabilities,
// such as PHP below the PHP2Shell fixes.
type VulnerableRuntime struct {
	Runtime string   `json:"runtime"`
	Version string   `json:"version"`
	Where   string   `json:"where"` // path:line, relative to the scanned path
	IDs     []string `json:"ids"`
	Title   string   `json:"title"`
	Fixed   string   `json:"fixed,omitempty"` // "" when the line gets no fix
}

var severityRank = map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}

// Sort orders dependencies most severe first, then by name, and sets each
// dependency's severity from its advisories.
func (v *Vulnerabilities) Sort() {
	for i := range v.Dependencies {
		d := &v.Dependencies[i]
		sort.SliceStable(d.Advisories, func(a, b int) bool {
			return severityRank[d.Advisories[a].Severity] > severityRank[d.Advisories[b].Severity]
		})
		if len(d.Advisories) > 0 {
			d.Severity = d.Advisories[0].Severity
		}
	}
	sort.SliceStable(v.Dependencies, func(a, b int) bool {
		x, y := v.Dependencies[a], v.Dependencies[b]
		if severityRank[x.Severity] != severityRank[y.Severity] {
			return severityRank[x.Severity] > severityRank[y.Severity]
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Lockfile < y.Lockfile
	})
}

// counts returns how many dependencies and runtimes are vulnerable, and how
// many of those are critical or high.
func (v *Vulnerabilities) counts() (total, severe int) {
	for _, d := range v.Dependencies {
		total++
		if severityRank[d.Severity] >= 3 {
			severe++
		}
	}
	total += len(v.Runtimes)
	severe += len(v.Runtimes) // every runtime advisory here is critical
	return total, severe
}

// writeVulnerabilities prints the section: what was found, then what could
// not be checked and why.
func writeVulnerabilities(b *strings.Builder, v *Vulnerabilities) {
	if v == nil {
		return
	}
	how := "checked live against osv.dev; only package names and versions were sent"
	switch v.Mode {
	case "offline":
		how = "checked against the downloaded databases" + databaseAge(v.DatabaseDates) + "; nothing was sent"
	case "":
		how = "not checked"
	}
	fmt.Fprintf(b, "\nVULNERABLE DEPENDENCIES (%s; never uploaded)\n", how)
	checked := 0
	for _, n := range v.Checked {
		checked += n
	}
	total, _ := v.counts()
	if total == 0 && v.Mode != "" {
		fmt.Fprintf(b, "  None known in %s.\n", count(checked, "locked package"))
	}
	for _, r := range v.Runtimes {
		fix := "upgrade to " + r.Fixed + " or later"
		if r.Fixed == "" {
			fix = "past end of life, no fix on this line"
		}
		fmt.Fprintf(b, "  ! %s %s (%s): %s, %s; %s\n", displayLanguage(r.Runtime), r.Version, r.Where, r.Title, strings.Join(r.IDs, ", "), fix)
	}
	for _, d := range v.Dependencies {
		ids := make([]string, 0, len(d.Advisories))
		for _, a := range d.Advisories {
			ids = append(ids, advisoryName(a))
		}
		line := fmt.Sprintf("  ! %s %s (%s, %s): %s", d.Name, d.Version, d.Ecosystem, d.Lockfile, strings.Join(ids, ", "))
		if fix := upgradeTo(d); fix != "" {
			line += "; fixed in " + fix
		}
		b.WriteString(line + "\n")
	}
	if len(v.Unchecked) > 0 {
		ecos := make([]string, 0, len(v.Unchecked))
		for e := range v.Unchecked {
			ecos = append(ecos, e)
		}
		sort.Strings(ecos)
		b.WriteString("  Not checked:\n")
		for _, e := range ecos {
			fmt.Fprintf(b, "    %s: %s\n", e, v.Unchecked[e])
		}
	}
	for _, n := range v.Notes {
		fmt.Fprintf(b, "  %s\n", n)
	}
}

// advisoryName names an advisory by its CVE ID when it has one, with its
// severity.
func advisoryName(a VulnAdvisory) string {
	name := a.ID
	for _, alias := range a.Aliases {
		if strings.HasPrefix(alias, "CVE-") {
			name = alias
			break
		}
	}
	if a.Severity != "" {
		name += " " + a.Severity
	}
	return name
}

// upgradeTo is the lowest version that fixes every advisory of a dependency:
// for each advisory, its lowest fix above the current version, and the highest
// of those. It is "" when an advisory has no fix above the current version.
func upgradeTo(d VulnerableDependency) string {
	need := ""
	for _, a := range d.Advisories {
		lowest := ""
		for _, f := range a.Fixed {
			if dottedLess(d.Version, f) && (lowest == "" || dottedLess(f, lowest)) {
				lowest = f
			}
		}
		if lowest == "" {
			return ""
		}
		if need == "" || dottedLess(need, lowest) {
			need = lowest
		}
	}
	return need
}

// dottedLess compares dotted versions number by number, falling back to text.
func dottedLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var x, y int
		_, ex := fmt.Sscan(pa[i], &x)
		_, ey := fmt.Sscan(pb[i], &y)
		if ex != nil || ey != nil {
			return a < b
		}
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

// databaseAge describes the oldest offline database, warning past a week.
func databaseAge(dates map[string]time.Time) string {
	var oldest time.Time
	for _, d := range dates {
		if oldest.IsZero() || d.Before(oldest) {
			oldest = d
		}
	}
	if oldest.IsZero() {
		return ""
	}
	days := int(time.Since(oldest).Hours() / 24)
	if days > 7 {
		return fmt.Sprintf(" from %s, %d days old: run urfluency vulns update", oldest.Format("2006-01-02"), days)
	}
	return " from " + oldest.Format("2006-01-02")
}

// WriteVulnerabilities prints the vulnerable dependencies section on its own,
// as urfluency check does after its verdict.
func WriteVulnerabilities(w io.Writer, v *Vulnerabilities) error {
	var b strings.Builder
	writeVulnerabilities(&b, v)
	_, err := io.WriteString(w, b.String())
	return err
}

// vulnerabilityPointer is the last line of a text report with vulnerabilities,
// so they are not missed above the summary.
func vulnerabilityPointer(v *Vulnerabilities) string {
	if v == nil {
		return ""
	}
	total, severe := v.counts()
	switch {
	case total == 0:
		return ""
	case severe > 0:
		return fmt.Sprintf("\n! %s (%d critical or high): see VULNERABLE DEPENDENCIES above.\n", dependencies(total), severe)
	}
	return fmt.Sprintf("\n! %s: see VULNERABLE DEPENDENCIES above.\n", dependencies(total))
}

func dependencies(n int) string {
	if n == 1 {
		return "1 vulnerable dependency"
	}
	return fmt.Sprintf("%d vulnerable dependencies", n)
}
