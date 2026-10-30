package vulns

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func pkgStrings(pkgs []Package) []string {
	var out []string
	for _, p := range pkgs {
		out = append(out, p.Ecosystem+" "+p.Name+" "+p.Version)
	}
	sort.Strings(out)
	return out
}

func TestParseLockfile(t *testing.T) {
	tests := []struct {
		fixture string
		want    []string
	}{
		{"npm-v1/package-lock.json", []string{
			"npm express 4.17.1", "npm lodash 4.17.20", "npm qs 6.11.0", "npm qs 6.7.0"}},
		{"npm-v3/package-lock.json", []string{
			"npm @babel/core 7.22.5", "npm lodash 4.17.21", "npm semver 6.3.0", "npm string-width 4.2.3"}},
		{"yarn-v1/yarn.lock", []string{
			"npm @babel/code-frame 7.22.5", "npm lodash 4.17.21", "npm semver 7.5.4"}},
		{"yarn-berry/yarn.lock", []string{"npm @types/node 20.4.5", "npm lodash 4.17.21"}},
		{"pnpm-v6/pnpm-lock.yaml", []string{
			"npm @types/react 18.2.15", "npm js-tokens 4.0.0", "npm react-dom 18.2.0"}},
		{"pnpm-v9/pnpm-lock.yaml", []string{"npm @vitejs/plugin-react 4.0.3", "npm vite 4.4.7"}},
		{"poetry/poetry.lock", []string{"PyPI Jinja2 2.11.3", "PyPI markupsafe 2.1.3", "PyPI requests 2.25.0"}},
		{"pipfile/Pipfile.lock", []string{"PyPI django 3.2.0", "PyPI pytest 7.4.0"}},
		{"requirements/requirements-dev.txt", []string{
			"PyPI Django 3.2.0", "PyPI requests 2.25.0", "PyPI six 1.16.0", "PyPI urllib3 1.26.4"}},
		{"gomod/go.mod", []string{
			"Go github.com/gin-gonic/gin v1.9.0", "Go github.com/new/mod v1.3.0",
			"Go github.com/quoted/mod v0.0.0-20210101000000-abcdef123456",
			"Go golang.org/x/net v0.17.0", "Go golang.org/x/text v0.3.8"}},
		{"composer/composer.lock", []string{
			"Packagist acme/internal dev-main", "Packagist guzzlehttp/guzzle 7.4.0",
			"Packagist phpunit/phpunit 9.6.0", "Packagist symfony/http-kernel 5.4.20"}},
		{"gemfile/Gemfile.lock", []string{
			"RubyGems actionpack 7.0.4", "RubyGems nokogiri 1.15.4", "RubyGems racc 1.7.1",
			"RubyGems rack 2.2.6.2", "RubyGems rails-html-sanitizer 1.4.3"}},
		{"cargo/Cargo.lock", []string{"crates.io regex 1.5.4", "crates.io smallvec 1.6.0"}},
		{"nuget/packages.lock.json", []string{"NuGet Newtonsoft.Json 12.0.1", "NuGet System.Text.Encodings.Web 4.5.0"}},
		{"pub/pubspec.lock", []string{"Pub async 2.11.0", "Pub http 0.13.6"}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("testdata", "lockfiles", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := ParseLockfile("app/"+filepath.Base(tt.fixture), content)
			if !ok {
				t.Fatal("not recognized as a lockfile")
			}
			if g := pkgStrings(got); !slices.Equal(g, tt.want) {
				t.Errorf("got\n  %s\nwant\n  %s", strings.Join(g, "\n  "), strings.Join(tt.want, "\n  "))
			}
			for _, p := range got {
				if p.Lockfile != "app/"+filepath.Base(tt.fixture) {
					t.Errorf("%s: Lockfile = %q", p.Name, p.Lockfile)
				}
			}
		})
	}
}

func TestParseLockfileEdgeCases(t *testing.T) {
	tests := []struct {
		name, path, content string
		known               bool
		want                []string
	}{
		{"unknown file", "package.json", `{"dependencies":{"a":"1.0.0"}}`, false, nil},
		{"malformed JSON", "package-lock.json", `{"packages":`, true, nil},
		{"duplicates collapse", "requirements.txt", "six==1.16.0\nsix==1.16.0\n", true, []string{"PyPI six 1.16.0"}},
		{"CRLF line endings", "Gemfile.lock", "GEM\r\n  remote: https://rubygems.org/\r\n  specs:\r\n    rack (2.2.6)\r\n\r\n", true, []string{"RubyGems rack 2.2.6"}},
		{"Windows path", `web\app\yarn.lock`, "lodash@^4:\n  version \"4.17.21\"\n", true, []string{"npm lodash 4.17.21"}},
		{"java gem platform", "Gemfile.lock", "GEM\n  specs:\n    jruby-openssl (0.14.1-java)\n", true, []string{"RubyGems jruby-openssl 0.14.1"}},
		{"go.mod local replace by version", "go.mod", "require a.com/m v1.0.0\nreplace a.com/m v1.0.0 => ./m\n", true, nil},
		{"go.mod replace of another version", "go.mod", "require a.com/m v1.0.0\nreplace a.com/m v0.9.0 => ./m\n", true, []string{"Go a.com/m v1.0.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseLockfile(tt.path, []byte(tt.content))
			if ok != tt.known {
				t.Fatalf("known = %v, want %v", ok, tt.known)
			}
			if g := pkgStrings(got); !slices.Equal(g, tt.want) {
				t.Errorf("got %q, want %q", g, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLockfilesWalk(t *testing.T) {
	root := t.TempDir()
	req := "six==1.16.0\n"
	for _, p := range []string{
		"requirements.txt", "services/api/requirements.txt",
		"node_modules/x/requirements.txt", ".git/requirements.txt", "vendor/requirements.txt",
		".venv/requirements.txt", "venv/requirements.txt", "__pycache__/requirements.txt",
		"target/requirements.txt", "build/requirements.txt", "dist/requirements.txt",
		"secrets/requirements.txt", ".aws/requirements.txt", "credentials/requirements.txt",
	} {
		writeFile(t, filepath.Join(root, p), req)
	}
	writeFile(t, filepath.Join(root, "README.md"), "six==1.0\n")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "requirements.txt"), "evil==6.6.6\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "requirements.txt"), filepath.Join(root, "requirements-link.txt")); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Lockfiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, p := range got {
		files = append(files, p.Lockfile)
		if p.Name != "six" {
			t.Errorf("read %s from %s", p.Name, p.Lockfile)
		}
	}
	sort.Strings(files)
	if want := []string{"requirements.txt", "services/api/requirements.txt"}; !slices.Equal(files, want) {
		t.Errorf("lockfiles = %q, want %q", files, want)
	}
}

func TestLockfilesDirectoryLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "go.mod"), "require a.com/m v1.0.0\n") // walked before the d folders
	for i := range maxDirs {
		if err := os.Mkdir(filepath.Join(root, "d"+strconv.Itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Lockfiles(root)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	if len(got) != 1 {
		t.Errorf("packages found before the limit = %d, want 1", len(got))
	}
}

func TestLockfilesMissingRoot(t *testing.T) {
	if _, err := Lockfiles(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("no error for a missing root")
	}
}

// Many lockfiles that each pass the per-file size check still stop the walk
// once their packages pass the total, so a hostile repository cannot exhaust
// a scan's memory.
func TestLockfilesPackageLimit(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("GEM\n  remote: https://rubygems.org/\n  specs:\n")
	for i := range 30_000 {
		fmt.Fprintf(&b, "    gem%d (1.0.%d)\n", i, i)
	}
	for _, dir := range []string{"a", "b", "c", "d"} {
		writeFile(t, filepath.Join(root, dir, "Gemfile.lock"), b.String())
	}
	got, err := Lockfiles(root)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	if len(got) == 0 || len(got) > maxPackages {
		t.Errorf("kept %d packages, want some and at most %d", len(got), maxPackages)
	}
}
