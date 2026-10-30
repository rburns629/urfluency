package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/safety"
	"github.com/rburns629/urfluency/vulns"
)

// Dependency checks look up each project's locked package versions in OSV.
// Live, only package names, versions, and ecosystems are sent to osv.dev;
// offline (--offline, or offline saved by init), the downloaded databases
// are used and nothing is sent.

// vulnsDir is where `urfluency vulns update` keeps the databases: <user cache
// dir>/urfluency/vulns, or URFLUENCY_CACHE_DIR/vulns when set.
func vulnsDir() (string, error) {
	if dir := os.Getenv("URFLUENCY_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "vulns"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "urfluency", "vulns"), nil
}

// newLiveChecker is the live checker; tests replace it.
var newLiveChecker = func() vulns.Checker { return vulns.Live(nil) }

// staleAfter is how old offline databases may get before reports say so.
const staleAfter = 7 * 24 * time.Hour

// offlineMode resolves --offline and --online against the saved default.
func offlineMode(offline, online bool, saved settings) (bool, error) {
	if offline && online {
		return false, fmt.Errorf("%w: --offline and --online cannot be used together", errUsage)
	}
	if online {
		return false, nil
	}
	return offline || saved.Offline, nil
}

// checkDependencies checks the projects' lockfiles and pinned runtimes. It
// never fails: what could not be checked is said in the result.
func checkDependencies(ctx context.Context, root string, projects []string, offline bool) *report.Vulnerabilities {
	v := &report.Vulnerabilities{Checked: map[string]int{}, Dependencies: []report.VulnerableDependency{}, Runtimes: []report.VulnerableRuntime{}}
	var pkgs []vulns.Package
	for _, dir := range projects {
		found, err := vulns.Lockfiles(dir)
		if err != nil && !errors.Is(err, vulns.ErrLimit) {
			v.Notes = append(v.Notes, fmt.Sprintf("Could not read the lockfiles in %s: %v", label(root, dir), err))
		} else if err != nil {
			v.Notes = append(v.Notes, fmt.Sprintf("%s is too large to search all its lockfiles; some dependencies were not checked.", label(root, dir)))
		}
		for _, p := range found {
			p.Lockfile = filepath.ToSlash(filepath.Join(label(root, dir), p.Lockfile))
			pkgs = append(pkgs, p)
		}
		for _, a := range safety.RuntimeAdvisories(safety.RuntimeFiles(dir)) {
			v.Runtimes = append(v.Runtimes, report.VulnerableRuntime{Runtime: a.Runtime, Version: a.Version,
				Where: fmt.Sprintf("%s:%d", filepath.ToSlash(filepath.Join(label(root, dir), a.Path)), a.Line), IDs: a.IDs, Title: a.Title, Fixed: a.Fixed})
		}
	}
	if len(pkgs) == 0 {
		v.Mode = "live"
		if offline {
			v.Mode = "offline"
		}
		return v
	}
	dir, _ := vulnsDir()
	var findings []vulns.Finding
	var cov vulns.Coverage
	if offline {
		findings, cov = vulns.Offline(dir).Check(ctx, pkgs)
	} else {
		findings, cov = newLiveChecker().Check(ctx, pkgs)
		// osv.dev could not be reached at all: use downloaded databases if
		// there are any, and say so.
		if len(cov.Checked) == 0 && cov.Unreachable() && hasDatabases(dir) {
			findings, cov = vulns.Offline(dir).Check(ctx, pkgs)
			v.Notes = append(v.Notes, "osv.dev could not be reached, so the downloaded databases were used instead.")
		}
	}
	v.Mode, v.Checked, v.Unchecked, v.DatabaseDates = cov.Mode, cov.Checked, cov.Unchecked, cov.DatabaseDates
	if len(cov.Checked) == 0 && !offline && cov.Unreachable() && v.Notes == nil {
		v.Mode = ""
	}
	for _, f := range findings {
		d := report.VulnerableDependency{Ecosystem: f.Package.Ecosystem, Name: f.Package.Name, Version: f.Package.Version, Lockfile: f.Package.Lockfile}
		for _, a := range f.Advisories {
			d.Advisories = append(d.Advisories, report.VulnAdvisory{ID: a.ID, Aliases: a.Aliases, Summary: a.Summary, Severity: a.Severity, Score: a.Score, Fixed: a.Fixed, URL: a.URL})
		}
		v.Dependencies = append(v.Dependencies, d)
	}
	v.Sort()
	v.Notes = append(v.Notes, dependencyNotes(v, offline, pkgs)...)
	return v
}

// dependencyNotes say how to finish an incomplete check.
func dependencyNotes(v *report.Vulnerabilities, offline bool, pkgs []vulns.Package) []string {
	var notes []string
	if len(v.Unchecked) > 0 {
		var missing []string
		for _, e := range vulns.Ecosystems(pkgs) {
			if _, ok := v.DatabaseDates[e]; offline && !ok {
				missing = append(missing, e)
			}
		}
		switch {
		case offline && len(missing) > 0:
			notes = append(notes, "To check offline, download the databases once: urfluency vulns update "+strings.Join(missing, " "))
		case !offline && v.Mode == "":
			notes = append(notes, "Could not reach osv.dev. Check your connection, or run urfluency vulns update once and scan with --offline.")
		}
	}
	if offline {
		for _, d := range v.DatabaseDates {
			if time.Since(d) > staleAfter {
				notes = append(notes, "Some databases are over a week old; run urfluency vulns update for current advisories.")
				break
			}
		}
	}
	return notes
}

func hasDatabases(dir string) bool {
	st, err := vulns.LocalStatus(dir)
	return err == nil && len(st) > 0
}

func label(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

// vulnsCmd manages the databases for offline dependency checks.
func vulnsCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: vulns needs update, status, or remove", errUsage)
	}
	dir, err := vulnsDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		return vulnsStatus(dir, stdout)
	case "remove":
		if err := vulns.Remove(dir); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "Removed the downloaded vulnerability databases.")
		return err
	case "update":
		return vulnsUpdate(ctx, dir, args[1:], stdin, stdout)
	}
	return fmt.Errorf("%w: unknown vulns command %q", errUsage, args[0])
}

func vulnsStatus(dir string, stdout io.Writer) error {
	st, err := vulns.LocalStatus(dir)
	if err != nil {
		return err
	}
	if len(st) == 0 {
		_, err := fmt.Fprintln(stdout, "No vulnerability databases downloaded. Scans check live against osv.dev; for offline checks, run urfluency vulns update.")
		return err
	}
	fmt.Fprintf(stdout, "Vulnerability databases in %s:\n", tildePath(dir))
	for _, s := range st {
		age := ""
		if time.Since(s.Published) > staleAfter {
			age = " (over a week old: run urfluency vulns update)"
		}
		fmt.Fprintf(stdout, "  %-10s %6.1f MB  advisories from %s%s\n", s.Ecosystem, float64(s.Bytes)/(1<<20), s.Published.Format("2006-01-02"), age)
	}
	return nil
}

// allEcosystems are the ecosystems OSV publishes that urFluency reads
// lockfiles for.
var allEcosystems = []string{"npm", "PyPI", "Go", "Packagist", "RubyGems", "crates.io", "NuGet", "Pub"}

func vulnsUpdate(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("vulns update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "")
	yes := fs.Bool("yes", false, "")
	var only stringList
	fs.Var(&only, "ecosystem", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	var ecosystems []string
	switch {
	case *all:
		ecosystems = allEcosystems
	case len(only) > 0:
		for _, e := range splitList(only) {
			i := slices.IndexFunc(allEcosystems, func(x string) bool { return strings.EqualFold(x, e) })
			if i < 0 {
				return fmt.Errorf("%w: unknown ecosystem %q; use one of %s", errUsage, e, strings.Join(allEcosystems, ", "))
			}
			ecosystems = append(ecosystems, allEcosystems[i])
		}
	default:
		// The ecosystems the projects at the given paths use, or every
		// database already downloaded.
		for _, p := range fs.Args() {
			pkgs, _ := vulns.Lockfiles(p)
			ecosystems = append(ecosystems, vulns.Ecosystems(pkgs)...)
		}
		if fs.NArg() == 0 {
			st, _ := vulns.LocalStatus(dir)
			for _, s := range st {
				ecosystems = append(ecosystems, s.Ecosystem)
			}
		}
		if len(ecosystems) == 0 {
			return fmt.Errorf("%w: name projects to download databases for (urfluency vulns update ~/Projects), or use --all or --ecosystem", errUsage)
		}
	}
	slices.Sort(ecosystems)
	ecosystems = slices.Compact(ecosystems)

	remote, err := vulns.Remote(ctx, nil, ecosystems)
	if err != nil {
		return fmt.Errorf("could not reach the OSV databases: %w", err)
	}
	local := map[string]time.Time{}
	if st, err := vulns.LocalStatus(dir); err == nil {
		for _, s := range st {
			local[s.Ecosystem] = s.Published
		}
	}
	var need []string
	var size int64
	for _, e := range ecosystems {
		r := remote[e]
		if have, ok := local[e]; ok && !r.Published.After(have) {
			fmt.Fprintf(stdout, "  %-10s up to date (%s)\n", e, have.Format("2006-01-02"))
			continue
		}
		mb := "size unknown"
		if r.Bytes >= 0 {
			mb = fmt.Sprintf("%.0f MB", float64(r.Bytes)/(1<<20))
			size += r.Bytes
		}
		fmt.Fprintf(stdout, "  %-10s %s, advisories from %s\n", e, mb, r.Published.Format("2006-01-02"))
		need = append(need, e)
	}
	if len(need) == 0 {
		_, err := fmt.Fprintln(stdout, "All databases are up to date.")
		return err
	}
	if !*yes {
		fmt.Fprintf(stdout, "Download %.0f MB to %s? [Y/n] ", float64(size)/(1<<20), tildePath(dir))
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "" && a != "y" && a != "yes" {
			_, err := fmt.Fprintln(stdout, "Nothing downloaded.")
			return err
		}
	}
	updated, err := vulns.Update(ctx, nil, dir, need, func(e string, done, total int64) {})
	for _, e := range updated {
		fmt.Fprintf(stdout, "  %-10s updated\n", e)
	}
	if err != nil {
		return fmt.Errorf("update the databases: %w", err)
	}
	_, err = fmt.Fprintln(stdout, "Scan with --offline to check against these, with nothing sent.")
	return err
}

// splitList splits repeated or comma-separated values.
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// checkVulnDatabases reports, for doctor, how dependency checks will run and
// how old the offline databases are.
func checkVulnDatabases(b *strings.Builder) {
	saved, _ := loadSettings()
	dir, err := vulnsDir()
	if err != nil {
		return
	}
	st, _ := vulns.LocalStatus(dir)
	switch {
	case len(st) == 0 && saved.Offline:
		b.WriteString("  [missing] Vulnerability databases: offline is your default, but none are downloaded\n            Run: urfluency vulns update ~/Projects\n")
		return
	case len(st) == 0:
		b.WriteString("  [ok]      Dependency checks: live against osv.dev (no offline databases)\n")
		return
	}
	oldest := st[0].Published
	names := make([]string, 0, len(st))
	for _, s := range st {
		names = append(names, s.Ecosystem)
		if s.Published.Before(oldest) {
			oldest = s.Published
		}
	}
	status, mode := "[ok]     ", "live, with offline databases as a fallback"
	if saved.Offline {
		mode = "offline by default"
	}
	if time.Since(oldest) > staleAfter {
		status = "[old]    "
	}
	fmt.Fprintf(b, "  %s Dependency checks: %s; databases for %s, oldest from %s\n", status, mode, strings.Join(names, ", "), oldest.Format("2006-01-02"))
	if status == "[old]    " {
		b.WriteString("            Run: urfluency vulns update\n")
	}
}
