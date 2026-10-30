package vulns

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"
)

// osvRecord holds the fields of an OSV record (https://ossf.github.io/osv-schema/)
// that matching and reporting use.
type osvRecord struct {
	ID               string          `json:"id"`
	Aliases          []string        `json:"aliases"`
	Summary          string          `json:"summary"`
	Details          string          `json:"details"`
	Withdrawn        string          `json:"withdrawn"`
	Severity         []osvSeverity   `json:"severity"`
	Affected         []osvAffected   `json:"affected"`
	DatabaseSpecific json.RawMessage `json:"database_specific"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Ranges           []osvRange      `json:"ranges"`
	Versions         []string        `json:"versions"`
	Severity         []osvSeverity   `json:"severity"`
	DatabaseSpecific json.RawMessage `json:"database_specific"`
}

type osvRange struct {
	Type   string              `json:"type"`
	Events []map[string]string `json:"events"` // each holds one of introduced, fixed, last_affected, limit
}

// event is one range event: kind is introduced, fixed, last_affected, or limit.
type event struct {
	Kind, Version string
}

func (r osvRange) events() []event {
	var out []event
	for _, e := range r.Events {
		for _, kind := range []string{"introduced", "fixed", "last_affected", "limit"} {
			if v, ok := e[kind]; ok {
				out = append(out, event{kind, v})
			}
		}
	}
	return out
}

// summary returns the record's summary, or the first line of its details
// when it has none.
func (r osvRecord) summary() string {
	if r.Summary != "" {
		return r.Summary
	}
	s, _, _ := strings.Cut(strings.TrimSpace(r.Details), "\n")
	if runes := []rune(s); len(runes) > 160 {
		s = string(runes[:157]) + "..."
	}
	return s
}

// severity rates a record from its first CVSS v3 vector, falling back to the
// severity a database such as GitHub's records. CVSS v2 and v4 vectors are not
// scored.
func (r osvRecord) severity() (string, float64) {
	vectors := r.Severity
	for _, a := range r.Affected {
		vectors = append(vectors, a.Severity...)
	}
	for _, s := range vectors {
		if score, ok := cvss3(s.Score); ok {
			return rating(score), score
		}
	}
	if s := databaseSeverity(r.DatabaseSpecific); s != "" {
		return s, 0
	}
	for _, a := range r.Affected {
		if s := databaseSeverity(a.DatabaseSpecific); s != "" {
			return s, 0
		}
	}
	return "", 0
}

func databaseSeverity(raw json.RawMessage) string {
	var v struct {
		Severity any `json:"severity"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	s, _ := v.Severity.(string)
	switch strings.ToLower(s) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "moderate", "medium":
		return "medium"
	case "low":
		return "low"
	}
	return ""
}

func rating(score float64) string {
	switch {
	case score >= 9:
		return "critical"
	case score >= 7:
		return "high"
	case score >= 4:
		return "medium"
	case score > 0:
		return "low"
	}
	return ""
}

// cvss3 computes a CVSS v3.0 or v3.1 base score from its vector string, as the
// FIRST specification defines it.
func cvss3(vector string) (float64, bool) {
	parts := strings.Split(vector, "/")
	if len(parts) < 9 || (parts[0] != "CVSS:3.0" && parts[0] != "CVSS:3.1") {
		return 0, false
	}
	m := map[string]string{}
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, ":")
		if !ok {
			return 0, false
		}
		m[k] = v
	}
	changed := m["S"] == "C"
	if m["S"] != "C" && m["S"] != "U" {
		return 0, false
	}
	pr := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	if changed {
		pr = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	}
	cia := map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	weights := []struct {
		metric string
		values map[string]float64
	}{
		{"AV", map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}},
		{"AC", map[string]float64{"L": 0.77, "H": 0.44}},
		{"PR", pr},
		{"UI", map[string]float64{"N": 0.85, "R": 0.62}},
		{"C", cia}, {"I", cia}, {"A", cia},
	}
	w := make([]float64, len(weights))
	for i, mw := range weights {
		v, ok := mw.values[m[mw.metric]]
		if !ok {
			return 0, false
		}
		w[i] = v
	}
	iss := 1 - (1-w[4])*(1-w[5])*(1-w[6])
	impact := 6.42 * iss
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	}
	if impact <= 0 {
		return 0, true
	}
	exploitability := 8.22 * w[0] * w[1] * w[2] * w[3]
	total := impact + exploitability
	if changed {
		total *= 1.08
	}
	return roundUp(math.Min(total, 10)), true
}

// roundUp is CVSS v3.1's Roundup: the smallest one-decimal number not below x,
// computed in integers to avoid floating-point error.
func roundUp(x float64) float64 {
	i := int64(math.Round(x * 100000))
	if i%10000 == 0 {
		return float64(i) / 100000
	}
	return float64(i/10000+1) / 10
}

// errEventVersion means an advisory's range uses a version the comparator
// cannot parse; the package is then reported unchecked rather than safe.
var errEventVersion = errors.New("an advisory's version range could not be evaluated")

// inRange evaluates one range's events against v, following the OSV schema's
// algorithm: walk the events in version order, entering the affected state at
// each introduced and leaving it at each fixed, after each last_affected, and
// at each limit.
func inRange(v version, parse parser, events []event) (bool, error) {
	type parsed struct {
		kind string
		v    version // nil for introduced "0", which precedes every version
	}
	evs := make([]parsed, 0, len(events))
	for _, e := range events {
		if e.Kind == "introduced" && e.Version == "0" {
			evs = append(evs, parsed{kind: e.Kind})
			continue
		}
		pv, ok := parse(e.Version)
		if !ok {
			return false, errEventVersion
		}
		evs = append(evs, parsed{e.Kind, pv})
	}
	sort.SliceStable(evs, func(i, j int) bool {
		switch {
		case evs[i].v == nil:
			return evs[j].v != nil
		case evs[j].v == nil:
			return false
		}
		return evs[i].v.compare(evs[j].v) < 0
	})
	affected := false
	for _, e := range evs {
		switch e.kind {
		case "introduced":
			if e.v == nil || v.compare(e.v) >= 0 {
				affected = true
			}
		case "fixed", "limit":
			if v.compare(e.v) >= 0 {
				affected = false
			}
		case "last_affected":
			if v.compare(e.v) > 0 {
				affected = false
			}
		}
	}
	return affected, nil
}

// rangeParser returns the comparator for a range type; GIT and unknown range
// types return nil and are skipped.
func rangeParser(rangeType, ecosystem string) parser {
	switch rangeType {
	case "SEMVER":
		return parseSemver
	case "ECOSYSTEM":
		return ecosystemParsers[ecosystem]
	}
	return nil
}

// affects reports whether one affected entry covers rawVersion: whether it is
// one of the enumerated versions (equal strings or equal parsed versions) or
// falls in a SEMVER or ECOSYSTEM range. As in the OSV schema, an entry affects
// the union of both: the enumerated list is a snapshot of the versions known
// when the advisory was written, so a later release can be in a range but not
// in the list.
func affects(ecosystem, rawVersion string, versions []string, ranges []indexRange) (bool, error) {
	if len(versions) > 0 {
		v, ok := parseVersion(ecosystem, rawVersion)
		for _, s := range versions {
			if s == rawVersion {
				return true, nil
			}
			if ok {
				if w, wok := parseVersion(ecosystem, s); wok && v.compare(w) == 0 {
					return true, nil
				}
			}
		}
	}
	var firstErr error
	for _, r := range ranges {
		parse := rangeParser(r.Type, ecosystem)
		if parse == nil {
			continue
		}
		v, ok := parse(rawVersion)
		if !ok {
			return false, errors.New(unparseable)
		}
		hit, err := inRange(v, parse, r.Events)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if hit {
			return true, nil
		}
	}
	return false, firstErr
}

// fixedVersions returns the fixed events of ranges, distinct and in version
// order where they parse.
func fixedVersions(ecosystem string, ranges []indexRange) []string {
	var out []string
	for _, r := range ranges {
		if rangeParser(r.Type, ecosystem) == nil {
			continue
		}
		for _, e := range r.Events {
			if e.Kind == "fixed" && !slices.Contains(out, e.Version) {
				out = append(out, e.Version)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, aok := parseVersion(ecosystem, out[i])
		b, bok := parseVersion(ecosystem, out[j])
		if aok && bok {
			return a.compare(b) < 0
		}
		return out[i] < out[j]
	})
	return out
}

// indexRange is a range as matching needs it, in both live and offline mode.
type indexRange struct {
	Type   string
	Events []event
}

// affectedEntry is one affected package entry of an advisory, reduced to what
// matching needs.
type affectedEntry struct {
	Versions []string
	Ranges   []indexRange
}

// entriesFor returns the record's affected entries for one package, without
// GIT ranges.
func (r osvRecord) entriesFor(ecosystem, name string) []affectedEntry {
	var out []affectedEntry
	for _, a := range r.Affected {
		if a.Package.Ecosystem != ecosystem || normalizeName(ecosystem, a.Package.Name) != normalizeName(ecosystem, name) {
			continue
		}
		e := affectedEntry{Versions: a.Versions}
		for _, rg := range a.Ranges {
			if rangeParser(rg.Type, ecosystem) != nil {
				e.Ranges = append(e.Ranges, indexRange{Type: rg.Type, Events: rg.events()})
			}
		}
		if len(e.Versions) > 0 || len(e.Ranges) > 0 {
			out = append(out, e)
		}
	}
	return out
}
