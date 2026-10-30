package vulns

import (
	"encoding/json"
	"regexp"
	"strings"
)

// parsePoetryLock reads the name and version of each [[package]] in
// poetry.lock, skipping packages whose [package.source] is Git, a directory,
// a file, or a URL rather than an index.
func parsePoetryLock(content []byte) []Package {
	var out []Package
	var name, ver, source, section string
	inPackage := false
	flush := func() {
		switch source {
		case "git", "directory", "file", "url":
		default:
			if inPackage {
				out = append(out, Package{Ecosystem: "PyPI", Name: name, Version: ver})
			}
		}
		name, ver, source, section, inPackage = "", "", "", "", false
	}
	for _, line := range lines(content) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			switch {
			case t == "[[package]]":
				flush()
				inPackage = true
			case strings.HasPrefix(t, "[package."):
				section = t
			default:
				flush()
			}
			continue
		}
		k, v, ok := tomlString(t)
		if !ok || !inPackage {
			continue
		}
		switch {
		case section == "" && k == "name":
			name = v
		case section == "" && k == "version":
			ver = v
		case section == "[package.source]" && k == "type":
			source = v
		}
	}
	flush()
	return out
}

// parsePipfileLock reads Pipfile.lock's default and develop packages pinned
// with ==; Git and editable entries carry no version and are skipped.
func parsePipfileLock(content []byte) []Package {
	type entry struct {
		Version string `json:"version"`
	}
	var lock struct {
		Default map[string]entry `json:"default"`
		Develop map[string]entry `json:"develop"`
	}
	if json.Unmarshal(content, &lock) != nil {
		return nil
	}
	var out []Package
	for _, group := range []map[string]entry{lock.Default, lock.Develop} {
		for name, e := range group {
			if v, ok := strings.CutPrefix(e.Version, "=="); ok && !strings.HasPrefix(v, "=") {
				out = append(out, Package{Ecosystem: "PyPI", Name: name, Version: v})
			}
		}
	}
	return out
}

// requirementRe matches a requirement pinned to one exact version:
// name[extras]==version. Ranges, === (arbitrary equality), and wildcards do
// not lock a version and do not match.
var requirementRe = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(?:\[[^\]]*\])?\s*==\s*([A-Za-z0-9!+._-]+)$`)

// parseRequirements reads requirements*.txt lines pinned with ==. Options
// (-r, -e, --hash), URLs, and environment markers after ; are ignored; lines
// continued with a backslash are joined first.
func parseRequirements(content []byte) []Package {
	var out []Package
	joined := strings.ReplaceAll(strings.ReplaceAll(string(content), "\r\n", "\n"), "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") || strings.Contains(line, "://") {
			continue
		}
		if i := strings.Index(line, ";"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, " --"); i >= 0 {
			line = line[:i]
		}
		if m := requirementRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !strings.Contains(m[2], "*") {
			out = append(out, Package{Ecosystem: "PyPI", Name: m[1], Version: m[2]})
		}
	}
	return out
}
