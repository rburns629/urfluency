package vulns

import (
	"encoding/json"
	"strconv"
	"strings"
)

// parseGoMod reads go.mod require directives, single and in blocks,
// including // indirect ones, which are compiled in too. A replace with a
// module target substitutes that module and version; a replace with a local
// directory drops the requirement, as no published version is built.
func parseGoMod(content []byte) []Package {
	var requires []Package
	replaces := map[string][]goReplace{}
	handle := func(verb string, args []string) {
		switch verb {
		case "require":
			if len(args) >= 2 {
				requires = append(requires, Package{Ecosystem: "Go", Name: goUnquote(args[0]), Version: args[1]})
			}
		case "replace":
			arrow := -1
			for i, a := range args {
				if a == "=>" {
					arrow = i
				}
			}
			if arrow < 1 || arrow > 2 || len(args)-arrow-1 < 1 {
				return
			}
			r := goReplace{newPath: goUnquote(args[arrow+1])}
			if arrow == 2 {
				r.oldVersion = args[1]
			}
			if len(args)-arrow-1 >= 2 {
				r.newVersion = args[arrow+2]
			}
			old := goUnquote(args[0])
			replaces[old] = append(replaces[old], r)
		}
	}
	block := ""
	for _, line := range lines(content) {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
		case block != "":
			if f[0] == ")" {
				block = ""
			} else {
				handle(block, f)
			}
		case len(f) == 2 && f[1] == "(":
			block = f[0]
		default:
			handle(f[0], f[1:])
		}
	}
	var out []Package
	for _, p := range requires {
		r, ok := goReplacement(replaces[p.Name], p.Version)
		switch {
		case !ok:
			out = append(out, p)
		case r.newVersion != "":
			out = append(out, Package{Ecosystem: "Go", Name: r.newPath, Version: r.newVersion})
		}
	}
	return out
}

// goReplace is one replace directive; oldVersion "" applies to every
// version, and newVersion "" means a local directory.
type goReplace struct {
	oldVersion, newPath, newVersion string
}

// goReplacement picks the replace that applies to a version: one naming that
// version wins over one for all versions.
func goReplacement(rs []goReplace, version string) (goReplace, bool) {
	var all *goReplace
	for i, r := range rs {
		if r.oldVersion == version {
			return r, true
		}
		if r.oldVersion == "" {
			all = &rs[i]
		}
	}
	if all != nil {
		return *all, true
	}
	return goReplace{}, false
}

func goUnquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// parseComposerLock reads composer.lock's packages and packages-dev,
// dropping the v prefix Composer keeps from tags. Branch versions such as
// dev-main stay as written and are reported unchecked.
func parseComposerLock(content []byte) []Package {
	type entry struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	var lock struct {
		Packages    []entry `json:"packages"`
		PackagesDev []entry `json:"packages-dev"`
	}
	if json.Unmarshal(content, &lock) != nil {
		return nil
	}
	var out []Package
	for _, e := range append(lock.Packages, lock.PackagesDev...) {
		v := e.Version
		if len(v) > 1 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
			v = v[1:]
		}
		out = append(out, Package{Ecosystem: "Packagist", Name: e.Name, Version: v})
	}
	return out
}

// gemPlatforms start the platform suffixes Bundler appends to native gems'
// versions (nokogiri (1.15.4-x86_64-linux)).
var gemPlatforms = []string{"x86", "x64", "i386", "i686", "universal", "arm", "aarch64", "java", "jruby",
	"mswin", "mingw", "darwin", "linux", "ruby", "wasm", "ppc", "powerpc", "s390", "sparc", "riscv"}

// parseGemfileLock reads the specs of Gemfile.lock's GEM sections: lines of
// exactly four spaces, "name (version)". GIT and PATH sections are not
// registry gems and are skipped, as are the indented dependency lines.
func parseGemfileLock(content []byte) []Package {
	var out []Package
	section, inSpecs := "", false
	for _, line := range lines(content) {
		switch {
		case strings.TrimSpace(line) == "":
			section, inSpecs = "", false
		case indent(line) == 0:
			section, inSpecs = strings.TrimSpace(line), false
		case section != "GEM":
		case line == "  specs:":
			inSpecs = true
		case inSpecs && indent(line) == 4:
			t := line[4:]
			open := strings.Index(t, " (")
			if open < 0 || !strings.HasSuffix(t, ")") {
				continue
			}
			out = append(out, Package{Ecosystem: "RubyGems", Name: t[:open], Version: stripGemPlatform(t[open+2 : len(t)-1])})
		}
	}
	return out
}

func stripGemPlatform(v string) string {
	i := strings.Index(v, "-")
	if i <= 0 {
		return v
	}
	for _, p := range gemPlatforms {
		if strings.HasPrefix(v[i+1:], p) {
			return v[:i]
		}
	}
	return v
}

// cratesIOSources are the crates.io index in its Git and sparse forms. Crates
// from other registries are skipped: a same-named crates.io advisory would not
// apply to them.
var cratesIOSources = map[string]bool{
	"registry+https://github.com/rust-lang/crates.io-index": true,
	"sparse+https://index.crates.io/":                       true,
}

// parseCargoLock reads each [[package]] of Cargo.lock that comes from
// crates.io. Workspace members have no source and Git crates a git+ source.
func parseCargoLock(content []byte) []Package {
	var out []Package
	var name, ver, source string
	inPackage := false
	flush := func() {
		if inPackage && cratesIOSources[source] {
			out = append(out, Package{Ecosystem: "crates.io", Name: name, Version: ver})
		}
		name, ver, source, inPackage = "", "", "", false
	}
	for _, line := range lines(content) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			flush()
			inPackage = t == "[[package]]"
			continue
		}
		switch k, v, _ := tomlString(t); k {
		case "name":
			name = v
		case "version":
			ver = v
		case "source":
			source = v
		}
	}
	flush()
	return out
}

// parseNuGetLock reads packages.lock.json: resolved versions per target
// framework, skipping project references.
func parseNuGetLock(content []byte) []Package {
	var lock struct {
		Dependencies map[string]map[string]struct {
			Type     string `json:"type"`
			Resolved string `json:"resolved"`
		} `json:"dependencies"`
	}
	if json.Unmarshal(content, &lock) != nil {
		return nil
	}
	var out []Package
	for _, deps := range lock.Dependencies {
		for name, d := range deps {
			if d.Type != "Project" && d.Resolved != "" {
				out = append(out, Package{Ecosystem: "NuGet", Name: name, Version: d.Resolved})
			}
		}
	}
	return out
}

// parsePubspecLock reads the hosted packages of pubspec.lock: two-space
// package names under "packages:" with four-space source and version fields.
// SDK, Git, and path packages are skipped.
func parsePubspecLock(content []byte) []Package {
	var out []Package
	var name, ver, source string
	inPackages := false
	flush := func() {
		if name != "" && source == "hosted" {
			out = append(out, Package{Ecosystem: "Pub", Name: name, Version: ver})
		}
		name, ver, source = "", "", ""
	}
	for _, line := range lines(content) {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
		case indent(line) == 0:
			flush()
			inPackages = t == "packages:"
		case !inPackages:
		case indent(line) == 2 && strings.HasSuffix(t, ":"):
			flush()
			name = unquote(strings.TrimSuffix(t, ":"))
		case indent(line) == 4:
			k, v, ok := strings.Cut(t, ":")
			if !ok {
				continue
			}
			switch k {
			case "source":
				source = unquote(strings.TrimSpace(v))
			case "version":
				ver = unquote(strings.TrimSpace(v))
			}
		}
	}
	flush()
	return out
}
