package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/vulns"
)

// fakeChecker finds every package of one name vulnerable.
type fakeChecker struct {
	name string
	seen *[]vulns.Package
}

func (f fakeChecker) Check(_ context.Context, pkgs []vulns.Package) ([]vulns.Finding, vulns.Coverage) {
	*f.seen = append(*f.seen, pkgs...)
	cov := vulns.Coverage{Mode: "live", Checked: map[string]int{}}
	var out []vulns.Finding
	for _, p := range pkgs {
		cov.Checked[p.Ecosystem]++
		if p.Name == f.name {
			out = append(out, vulns.Finding{Package: p, Advisories: []vulns.Advisory{{ID: "GHSA-test", Aliases: []string{"CVE-2021-23337"}, Severity: "high", Fixed: []string{"4.17.21"}, URL: "https://osv.dev/vulnerability/GHSA-test"}}})
		}
	}
	return out, cov
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckDependencies(t *testing.T) {
	root := writeProject(t, map[string]string{
		"web/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"name":"web"},"node_modules/lodash":{"version":"4.17.20"},"node_modules/express":{"version":"5.1.0"}}}`,
		"api/Dockerfile":        "FROM php:8.3.30-fpm\n",
	})
	var seen []vulns.Package
	defer func(f func() vulns.Checker) { newLiveChecker = f }(newLiveChecker)
	newLiveChecker = func() vulns.Checker { return fakeChecker{name: "lodash", seen: &seen} }

	v := checkDependencies(context.Background(), root, []string{filepath.Join(root, "web"), filepath.Join(root, "api")}, false)
	if v.Mode != "live" || v.Checked["npm"] != 2 || len(v.Dependencies) != 1 || v.Dependencies[0].Lockfile != "web/package-lock.json" || v.Dependencies[0].Severity != "high" {
		t.Fatalf("dependencies = %+v", v)
	}
	if len(v.Runtimes) != 1 || v.Runtimes[0].Where != "api/Dockerfile:1" || v.Runtimes[0].Fixed != "8.3.36" {
		t.Errorf("runtimes = %+v", v.Runtimes)
	}
	var text bytes.Buffer
	if err := report.WriteVulnerabilities(&text, v); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"checked live against osv.dev", "lodash 4.17.20 (npm, web/package-lock.json): CVE-2021-23337 high; fixed in 4.17.21", "PHP 8.3.30 (api/Dockerfile:1)"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, text.String())
		}
	}
}

// Offline with no databases sends nothing and says how to download them.
func TestOfflineWithoutDatabases(t *testing.T) {
	root := writeProject(t, map[string]string{"composer.lock": `{"packages":[{"name":"laravel/framework","version":"v11.2.0"}]}`})
	var seen []vulns.Package
	defer func(f func() vulns.Checker) { newLiveChecker = f }(newLiveChecker)
	newLiveChecker = func() vulns.Checker { return fakeChecker{seen: &seen} }

	v := checkDependencies(context.Background(), root, []string{root}, true)
	if len(seen) != 0 {
		t.Errorf("offline sent %d packages to the live checker", len(seen))
	}
	if v.Unchecked["Packagist"] == "" || len(v.Notes) == 0 || !strings.Contains(strings.Join(v.Notes, " "), "urfluency vulns update Packagist") {
		t.Errorf("offline without databases = %+v", v)
	}
}

// Live with no network says the check could not run; it never reads as clean.
func TestLiveUnreachable(t *testing.T) {
	root := writeProject(t, map[string]string{"Gemfile.lock": "GEM\n  remote: https://rubygems.org/\n  specs:\n    rails (8.0.1)\n\nPLATFORMS\n  ruby\n"})
	v := checkDependencies(context.Background(), root, []string{root}, false)
	var text bytes.Buffer
	_ = report.WriteVulnerabilities(&text, v)
	if strings.Contains(text.String(), "None known") || !strings.Contains(text.String(), "Could not reach osv.dev") {
		t.Errorf("unreachable report:\n%s", text.String())
	}
}

// Vulnerabilities never reach an upload.
func TestUploadsLeaveOutVulnerabilities(t *testing.T) {
	r := report.Build(analysis.Result{}, time.Now().AddDate(-1, 0, 0), false)
	r.Combined = report.BuildCombined(analysis.Result{}, analysis.Result{})
	report.Summarize(&r)
	r.Vulnerabilities = &report.Vulnerabilities{Dependencies: []report.VulnerableDependency{{Name: "lodash", Version: "4.17.20"}}}
	p, err := buildPayload(r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if bytes.Contains(b, []byte("lodash")) || bytes.Contains(b, []byte("vulnerab")) {
		t.Errorf("payload carries vulnerabilities: %s", b)
	}
}

func TestOfflineMode(t *testing.T) {
	if _, err := offlineMode(true, true, settings{}); err == nil {
		t.Error("--offline with --online accepted")
	}
	for _, c := range []struct {
		offline, online, saved, want bool
	}{{false, false, false, false}, {true, false, false, true}, {false, false, true, true}, {false, true, true, false}} {
		if got, _ := offlineMode(c.offline, c.online, settings{Offline: c.saved}); got != c.want {
			t.Errorf("offlineMode(%v, %v, saved %v) = %v", c.offline, c.online, c.saved, got)
		}
	}
}

// Packages that cannot be checked anywhere, such as Composer branches, are
// not a network failure: no fallback, and no "could not reach" note.
func TestUncheckableIsNotUnreachable(t *testing.T) {
	root := writeProject(t, map[string]string{"composer.lock": `{"packages":[{"name":"acme/lib","version":"dev-main"}]}`})
	defer func(f func() vulns.Checker) { newLiveChecker = f }(newLiveChecker)
	newLiveChecker = func() vulns.Checker { return uncheckable{} }
	v := checkDependencies(context.Background(), root, []string{root}, false)
	var text bytes.Buffer
	_ = report.WriteVulnerabilities(&text, v)
	if v.Mode != "live" || strings.Contains(text.String(), "Could not reach") || !strings.Contains(text.String(), "version not understood") {
		t.Errorf("uncheckable packages reported as:\n%s", text.String())
	}
}

// uncheckable answers as the live checker does for versions no comparator
// understands: unchecked before any request is made.
type uncheckable struct{}

func (uncheckable) Check(_ context.Context, pkgs []vulns.Package) ([]vulns.Finding, vulns.Coverage) {
	cov := vulns.Coverage{Mode: "live", Checked: map[string]int{}, Unchecked: map[string]string{}}
	for _, e := range vulns.Ecosystems(pkgs) {
		cov.Unchecked[e] = "version not understood (1): acme/lib@dev-main"
	}
	return nil, cov
}
