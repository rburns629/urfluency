package vulns

import (
	"regexp"
	"slices"
	"strings"
)

// version is a parsed version. compare is only called between versions from
// the same parser.
type version interface {
	compare(other version) int
}

type parser func(string) (version, bool)

// ecosystemParsers order versions the way each registry does, for OSV
// ECOSYSTEM ranges. SEMVER ranges use parseSemver whatever the ecosystem.
var ecosystemParsers = map[string]parser{
	"npm":       parseSemver,
	"crates.io": parseSemver,
	"Pub":       parseSemver,
	"Go":        parseSemver, // optional v; pseudo-versions are semver prereleases
	"NuGet":     parseNuGet,
	"Packagist": parseComposer,
	"RubyGems":  parseGem,
	"PyPI":      parsePEP440,
}

func parseVersion(ecosystem, s string) (version, bool) {
	p := ecosystemParsers[ecosystem]
	if p == nil {
		return nil, false
	}
	return p(s)
}

// cmpNum compares two strings of decimal digits by value without converting
// them, so numbers of any length compare correctly.
func cmpNum(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// semver is a Semantic Versioning 2.0 version, optionally with more or fewer
// numeric parts (NuGet) and case-insensitive prerelease labels (NuGet).
type semver struct {
	core     [4]string // missing parts are "0"
	pre      []string
	foldCase bool
}

// parseSemver accepts MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD] with an optional
// v prefix, as npm, crates.io, Pub, Go, and OSV SEMVER ranges use.
func parseSemver(s string) (version, bool) { return parseSemverish(s, 3, 3, false) }

// parseNuGet accepts NuGet's one to four numeric parts with case-insensitive
// prerelease labels; 1.0 and 1.0.0.0 are equal.
func parseNuGet(s string) (version, bool) { return parseSemverish(s, 1, 4, true) }

func parseSemverish(s string, minParts, maxParts int, foldCase bool) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	s, _, _ = strings.Cut(s, "+") // build metadata does not affect precedence
	s, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) < minParts || len(parts) > maxParts {
		return nil, false
	}
	v := semver{core: [4]string{"0", "0", "0", "0"}, foldCase: foldCase}
	for i, p := range parts {
		if !isDigits(p) {
			return nil, false
		}
		v.core[i] = p
	}
	if hasPre {
		for _, id := range strings.Split(pre, ".") {
			if id == "" || strings.Trim(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
				return nil, false
			}
			v.pre = append(v.pre, id)
		}
	}
	return v, true
}

func (a semver) compare(other version) int {
	b, ok := other.(semver)
	if !ok {
		return 0
	}
	for i := range a.core {
		if c := cmpNum(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	// A version without a prerelease ranks above any prerelease of it.
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if a.foldCase {
			x, y = strings.ToLower(x), strings.ToLower(y)
		}
		xn, yn := isDigits(x), isDigits(y)
		var c int
		switch {
		case xn && yn:
			c = cmpNum(x, y)
		case xn:
			c = -1 // numeric identifiers rank below alphanumeric ones
		case yn:
			c = 1
		default:
			c = strings.Compare(x, y)
		}
		if c != 0 {
			return c
		}
	}
	return compareInts(len(a.pre), len(b.pre))
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// composer is a Composer (Packagist) version: up to four numeric parts, an
// optional stability suffix with a number, and an optional -dev marker.
// Branch names such as dev-main do not parse.
type composer struct {
	nums      [4]string
	stability int // composerStability rank
	stabNum   string
	dev       bool
}

var composerRe = regexp.MustCompile(`(?i)^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:\.(\d+))?` +
	`(?:[._-]?(stable|beta|b|rc|alpha|a|patch|pl|p)(?:[.-]?(\d+))?)?(?:[.-]?(dev))?$`)

// composerStability ranks stability suffixes as Composer orders them.
var composerStability = map[string]int{"alpha": 1, "a": 1, "beta": 2, "b": 2, "rc": 3, "": 4, "stable": 4, "patch": 5, "pl": 5, "p": 5}

func parseComposer(s string) (version, bool) {
	m := composerRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return nil, false
	}
	v := composer{nums: [4]string{"0", "0", "0", "0"}, stabNum: m[6], dev: m[7] != ""}
	for i := range 4 {
		if m[i+1] != "" {
			v.nums[i] = m[i+1]
		}
	}
	v.stability = composerStability[strings.ToLower(m[5])]
	if m[5] == "" && v.dev {
		v.stability = 0 // 1.0.0-dev precedes 1.0.0-alpha
	}
	return v, true
}

func (a composer) compare(other version) int {
	b, ok := other.(composer)
	if !ok {
		return 0
	}
	for i := range a.nums {
		if c := cmpNum(a.nums[i], b.nums[i]); c != 0 {
			return c
		}
	}
	if c := compareInts(a.stability, b.stability); c != 0 {
		return c
	}
	if c := cmpNum(a.stabNum, b.stabNum); c != 0 {
		return c
	}
	switch {
	case a.dev == b.dev:
		return 0
	case a.dev:
		return -1
	}
	return 1
}

// gem is a RubyGems Gem::Version: segments split on dots into numbers and
// letter runs; a segment with letters makes the version a prerelease.
type gem struct {
	segs []gemSeg // canonical: trailing zeros dropped from the release and prerelease parts
}

type gemSeg struct {
	s     string
	isStr bool
}

var (
	gemValid = regexp.MustCompile(`^[0-9]+(?:\.[0-9a-zA-Z]+)*(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	gemScan  = regexp.MustCompile(`[0-9]+|[a-zA-Z]+`)
)

func parseGem(s string) (version, bool) {
	s = strings.TrimSpace(s)
	if !gemValid.MatchString(s) {
		return nil, false
	}
	s = strings.ReplaceAll(s, "-", ".pre.") // as Gem::Version does
	var segs []gemSeg
	for _, t := range gemScan.FindAllString(s, -1) {
		segs = append(segs, gemSeg{s: t, isStr: !isDigits(t)})
	}
	// Gem::Version#canonical_segments: drop trailing zeros from the numeric
	// part and from the prerelease part separately.
	split := len(segs)
	for i, sg := range segs {
		if sg.isStr {
			split = i
			break
		}
	}
	trim := func(ss []gemSeg) []gemSeg {
		for len(ss) > 0 && !ss[len(ss)-1].isStr && cmpNum(ss[len(ss)-1].s, "0") == 0 {
			ss = ss[:len(ss)-1]
		}
		return ss
	}
	canon := append(trim(slices.Clone(segs[:split])), trim(slices.Clone(segs[split:]))...)
	return gem{segs: canon}, true
}

func (a gem) compare(other version) int {
	b, ok := other.(gem)
	if !ok {
		return 0
	}
	zero := gemSeg{s: "0"}
	for i := 0; i < max(len(a.segs), len(b.segs)); i++ {
		x, y := zero, zero
		if i < len(a.segs) {
			x = a.segs[i]
		}
		if i < len(b.segs) {
			y = b.segs[i]
		}
		switch {
		case x.isStr && y.isStr:
			if c := strings.Compare(x.s, y.s); c != 0 {
				return c
			}
		case x.isStr:
			return -1 // a letter segment ranks below any number
		case y.isStr:
			return 1
		default:
			if c := cmpNum(x.s, y.s); c != 0 {
				return c
			}
		}
	}
	return 0
}

// pep440 is a Python version (PEP 440): [N!]N(.N)*[{a|b|rc}N][.postN][.devN][+local].
type pep440 struct {
	epoch   string
	release []string // trailing zeros dropped
	preKind int      // 0 a, 1 b, 2 rc
	preNum  string
	hasPre  bool
	postNum string
	hasPost bool
	devNum  string
	hasDev  bool
	local   []string
}

// pep440Re is the pattern from the packaging library, which normalizes
// spellings such as 1.0-alpha.1, 1.0c1, 1.0-1 (a post-release), and 1.0.post.
var pep440Re = regexp.MustCompile(`^v?(?:(\d+)!)?(\d+(?:\.\d+)*)` +
	`(?:[-_.]?(alpha|a|beta|b|preview|pre|c|rc)[-_.]?(\d+)?)?` +
	`(?:-(\d+)|[-_.]?(post|rev|r)[-_.]?(\d+)?)?` +
	`(?:[-_.]?(dev)[-_.]?(\d+)?)?` +
	`(?:\+([a-z0-9]+(?:[-_.][a-z0-9]+)*))?$`)

func parsePEP440(s string) (version, bool) {
	m := pep440Re.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return nil, false
	}
	v := pep440{epoch: m[1]}
	v.release = strings.Split(m[2], ".")
	for len(v.release) > 1 && cmpNum(v.release[len(v.release)-1], "0") == 0 {
		v.release = v.release[:len(v.release)-1]
	}
	if m[3] != "" {
		v.hasPre, v.preNum = true, m[4]
		switch m[3] {
		case "alpha", "a":
			v.preKind = 0
		case "beta", "b":
			v.preKind = 1
		default:
			v.preKind = 2
		}
	}
	switch {
	case m[5] != "":
		v.hasPost, v.postNum = true, m[5]
	case m[6] != "":
		v.hasPost, v.postNum = true, m[7]
	}
	if m[8] != "" {
		v.hasDev, v.devNum = true, m[9]
	}
	if m[10] != "" {
		v.local = strings.FieldsFunc(m[10], func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	}
	return v, true
}

// compareOptional compares an optional number, ordering a missing one below
// (missing = -1) or above (missing = 1) every present one.
func compareOptional(aHas, bHas bool, missing int, a, b string) int {
	switch {
	case aHas && bHas:
		return cmpNum(a, b)
	case aHas:
		return -missing
	case bHas:
		return missing
	}
	return 0
}

func (a pep440) compare(other version) int {
	b, ok := other.(pep440)
	if !ok {
		return 0
	}
	if c := cmpNum(a.epoch, b.epoch); c != 0 {
		return c
	}
	for i := 0; i < max(len(a.release), len(b.release)); i++ {
		x, y := "0", "0"
		if i < len(a.release) {
			x = a.release[i]
		}
		if i < len(b.release) {
			y = b.release[i]
		}
		if c := cmpNum(x, y); c != 0 {
			return c
		}
	}
	if c := compareInts(a.preRank(), b.preRank()); c != 0 {
		return c
	}
	if a.hasPre && b.hasPre {
		if c := compareInts(a.preKind, b.preKind); c != 0 {
			return c
		}
		if c := cmpNum(a.preNum, b.preNum); c != 0 {
			return c
		}
	}
	if c := compareOptional(a.hasPost, b.hasPost, -1, a.postNum, b.postNum); c != 0 {
		return c
	}
	if c := compareOptional(a.hasDev, b.hasDev, 1, a.devNum, b.devNum); c != 0 {
		return c
	}
	return compareLocal(a.local, b.local)
}

// preRank places 1.0.dev0 before 1.0a1 and a final release after both.
func (v pep440) preRank() int {
	switch {
	case v.hasPre:
		return 0
	case !v.hasPost && v.hasDev:
		return -1
	}
	return 1
}

// compareLocal orders local labels: none first, then segment by segment with
// numbers above strings.
func compareLocal(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		xn, yn := isDigits(x), isDigits(y)
		var c int
		switch {
		case xn && yn:
			c = cmpNum(x, y)
		case xn:
			c = 1
		case yn:
			c = -1
		default:
			c = strings.Compare(x, y)
		}
		if c != 0 {
			return c
		}
	}
	return compareInts(len(a), len(b))
}
