package vulns

import (
	"encoding/json"
	"strings"
)

type npmLock struct {
	Packages map[string]struct {
		Version  string `json:"version"`
		Name     string `json:"name"` // set when the folder name is an alias
		Resolved string `json:"resolved"`
		Link     bool   `json:"link"`
	} `json:"packages"`
	Dependencies map[string]npmDependency `json:"dependencies"`
}

type npmDependency struct {
	Version      string                   `json:"version"`
	Dependencies map[string]npmDependency `json:"dependencies"`
}

// parseNPMLock reads package-lock.json and npm-shrinkwrap.json. Versions 2
// and 3 list installed folders under "packages"; version 1 nests
// "dependencies". Workspace folders, links, and packages resolved from Git or
// local files are not registry packages and are skipped.
func parseNPMLock(content []byte) []Package {
	var lock npmLock
	if json.Unmarshal(content, &lock) != nil {
		return nil
	}
	var out []Package
	if len(lock.Packages) > 0 {
		for key, e := range lock.Packages {
			i := strings.LastIndex(key, "node_modules/")
			if key == "" || e.Link || i < 0 || (e.Resolved != "" && !isHTTP(e.Resolved)) {
				continue
			}
			name := key[i+len("node_modules/"):]
			if e.Name != "" {
				name = e.Name
			}
			out = append(out, Package{Ecosystem: "npm", Name: name, Version: e.Version})
		}
		return out
	}
	var walk func(map[string]npmDependency)
	walk = func(deps map[string]npmDependency) {
		for name, d := range deps {
			if n, v, ok := npmVersion(name, d.Version); ok {
				out = append(out, Package{Ecosystem: "npm", Name: n, Version: v})
			}
			walk(d.Dependencies)
		}
	}
	walk(lock.Dependencies)
	return out
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// npmVersion resolves a version 1 lockfile version, which may be an alias
// (npm:real-name@1.2.3) or a Git or file reference (skipped).
func npmVersion(name, v string) (string, string, bool) {
	if rest, ok := strings.CutPrefix(v, "npm:"); ok {
		if i := strings.LastIndex(rest, "@"); i > 0 {
			return rest[:i], rest[i+1:], true
		}
		return "", "", false
	}
	if strings.ContainsAny(v, ":/") {
		return "", "", false
	}
	return name, v, true
}

// parseYarnLock reads yarn.lock: version 1 (`version "1.2.3"`) and, since
// the layout is the same, Berry (`version: 1.2.3`). Each entry starts with an
// unindented header listing the ranges it satisfies; the first names the
// package. Entries from Git, files, links, workspaces, or patches are skipped.
func parseYarnLock(content []byte) []Package {
	var out []Package
	name := ""
	for _, line := range lines(content) {
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case indent(line) == 0:
			name = ""
			if strings.HasSuffix(line, ":") {
				name = yarnName(strings.TrimSuffix(line, ":"))
			}
		case indent(line) == 2 && name != "":
			t := strings.TrimSpace(line)
			v, ok := strings.CutPrefix(t, "version ")
			if !ok {
				v, ok = strings.CutPrefix(t, "version: ")
			}
			if ok {
				out = append(out, Package{Ecosystem: "npm", Name: name, Version: unquote(strings.TrimSpace(v))})
				name = ""
			}
		}
	}
	return out
}

// yarnName returns the registry package an entry header names, or "".
func yarnName(header string) string {
	// Version 1 quotes each range; Berry quotes the whole list.
	first, _, _ := strings.Cut(header, ",")
	first = strings.Trim(strings.TrimSpace(first), `"`)
	at := strings.Index(first[min(1, len(first)):], "@") + 1 // a scope's @ is not the separator
	if at <= 0 {
		return ""
	}
	name, rng := first[:at], first[at+1:]
	if rest, ok := strings.CutPrefix(rng, "npm:"); ok {
		// An alias (alias@npm:real@^1) names the real package.
		if i := strings.Index(rest[min(1, len(rest)):], "@") + 1; i > 0 {
			return rest[:i]
		}
		return name
	}
	if strings.ContainsAny(rng, ":/") {
		return ""
	}
	return name
}

// parsePNPMLock reads the package keys under "packages:" in pnpm-lock.yaml:
// /name@1.2.3 (version 6) or name@1.2.3 (version 9), with any peer suffix in
// parentheses removed. Version 5 keys (/name/1.2.3) and non-registry
// versions are skipped.
func parsePNPMLock(content []byte) []Package {
	var out []Package
	inPackages := false
	for _, line := range lines(content) {
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if indent(line) == 0 {
			inPackages = strings.TrimSpace(line) == "packages:"
			continue
		}
		if !inPackages || indent(line) != 2 {
			continue
		}
		key := strings.TrimSpace(line)
		key = strings.TrimSuffix(key, " {}")
		if !strings.HasSuffix(key, ":") {
			continue
		}
		key = strings.TrimPrefix(unquote(strings.TrimSuffix(key, ":")), "/")
		if i := strings.Index(key, "("); i >= 0 {
			key = key[:i]
		}
		at := strings.LastIndex(key, "@")
		if at <= 0 {
			continue
		}
		name, v := key[:at], key[at+1:]
		if v == "" || v[0] < '0' || v[0] > '9' {
			continue
		}
		out = append(out, Package{Ecosystem: "npm", Name: name, Version: v})
	}
	return out
}
