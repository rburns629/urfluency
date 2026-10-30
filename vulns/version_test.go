package vulns

import (
	"errors"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		ecosystem, a, b string
		want            int
	}{
		// npm and semver: prereleases rank below their release.
		{"npm", "1.0.0-alpha", "1.0.0", -1},
		{"npm", "1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"npm", "1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"npm", "1.0.0-alpha.2", "1.0.0-alpha.10", -1},
		{"npm", "1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"npm", "1.0.0+build.5", "1.0.0", 0},
		{"npm", "1.10.0", "1.9.9", 1},
		{"npm", "2.0.0", "10.0.0", -1},
		{"crates.io", "0.3.0-pre", "0.3.0", -1},
		{"Pub", "2.11.0", "2.11.0", 0},
		// Go: v prefix optional; pseudo-versions are prereleases.
		{"Go", "v1.2.3", "1.2.3", 0},
		{"Go", "0.0.0-20210101000000-abcdef123456", "0.0.1", -1},
		{"Go", "0.0.0-20210101000000-abcdef123456", "0.0.0-20220101000000-abcdef123456", -1},
		{"Go", "1.2.4-0.20210101000000-abcdef123456", "1.2.3", 1},
		{"Go", "2.0.0+incompatible", "2.0.0", 0},
		// NuGet: one to four parts, case-insensitive prereleases.
		{"NuGet", "1.0", "1.0.0.0", 0},
		{"NuGet", "1.0.0-BETA", "1.0.0-beta", 0},
		{"NuGet", "4.5.0", "4.5.0.1", -1},
		// Composer: v prefix, stability suffixes.
		{"Packagist", "v5.4.20", "5.4.20", 0},
		{"Packagist", "1.0.0-beta1", "1.0.0", -1},
		{"Packagist", "1.0.0-alpha2", "1.0.0-beta1", -1},
		{"Packagist", "1.0.0-RC1", "1.0.0-beta3", 1},
		{"Packagist", "1.0.0-dev", "1.0.0-alpha", -1},
		{"Packagist", "1.0.0-p1", "1.0.0", 1},
		{"Packagist", "2.0", "2.0.0.0", 0},
		// RubyGems: letter segments make prereleases.
		{"RubyGems", "1.0.a", "1.0", -1},
		{"RubyGems", "1.0.0.pre", "1.0.0", -1},
		{"RubyGems", "1.0.0.rc1", "1.0.0.beta2", 1},
		{"RubyGems", "1.0", "1.0.0", 0},
		{"RubyGems", "1.0.b1", "1.0.a.2", 1},
		{"RubyGems", "2.2.6.2", "2.2.6", 1},
		{"RubyGems", "1.0.0-rc1", "1.0.0.pre.rc1", 0},
		// PyPI: PEP 440.
		{"PyPI", "1.0.post1", "1.0", 1},
		{"PyPI", "1.0rc1", "1.0", -1},
		{"PyPI", "1.0a1", "1.0b1", -1},
		{"PyPI", "1.0b2", "1.0rc1", -1},
		{"PyPI", "1.0.dev1", "1.0a1", -1},
		{"PyPI", "1.0a1.dev1", "1.0a1", -1},
		{"PyPI", "1.0.post1.dev1", "1.0.post1", -1},
		{"PyPI", "1.0.post1.dev1", "1.0", 1},
		{"PyPI", "1.0", "1.0.0", 0},
		{"PyPI", "1.0-1", "1.0.post1", 0},
		{"PyPI", "1.0-alpha.1", "1.0a1", 0},
		{"PyPI", "1.0c1", "1.0rc1", 0},
		{"PyPI", "1!0.1", "2.0", 1},
		{"PyPI", "1.0+local.1", "1.0", 1},
		{"PyPI", "1.0+abc", "1.0+1", -1},
		{"PyPI", "2.10", "2.9", 1},
		{"PyPI", "V1.0", "1.0", 0},
	}
	for _, tt := range tests {
		a, aok := parseVersion(tt.ecosystem, tt.a)
		b, bok := parseVersion(tt.ecosystem, tt.b)
		if !aok || !bok {
			t.Errorf("%s: %q or %q did not parse", tt.ecosystem, tt.a, tt.b)
			continue
		}
		if got := a.compare(b); got != tt.want {
			t.Errorf("%s: compare(%q, %q) = %d, want %d", tt.ecosystem, tt.a, tt.b, got, tt.want)
		}
		if got := b.compare(a); got != -tt.want {
			t.Errorf("%s: compare(%q, %q) = %d, want %d", tt.ecosystem, tt.b, tt.a, got, -tt.want)
		}
	}
}

func TestUnparseableVersions(t *testing.T) {
	tests := []struct{ ecosystem, v string }{
		{"npm", "1.2"},
		{"npm", "latest"},
		{"npm", "1.0.0-"},
		{"Go", "master"},
		{"crates.io", "1.0.0.0"},
		{"NuGet", "1.2.3.4.5"},
		{"Packagist", "dev-main"},
		{"Packagist", "1.x-dev"},
		{"RubyGems", "1.0 beta"},
		{"RubyGems", ""},
		{"PyPI", "1.0-foo"},
		{"PyPI", "latest"},
		{"Unknown", "1.0.0"},
	}
	for _, tt := range tests {
		if _, ok := parseVersion(tt.ecosystem, tt.v); ok {
			t.Errorf("%s %q parsed", tt.ecosystem, tt.v)
		}
	}
}

func ev(pairs ...string) []event {
	var out []event
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, event{pairs[i], pairs[i+1]})
	}
	return out
}

func TestAffects(t *testing.T) {
	tests := []struct {
		name, ecosystem, version string
		versions                 []string
		ranges                   []indexRange
		want                     bool
		err                      error
	}{
		{"npm prerelease before fix", "npm", "1.0.0-rc.1", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "fixed", "1.0.0")}}, true, nil},
		{"npm fixed release", "npm", "1.0.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "fixed", "1.0.0")}}, false, nil},
		{"npm prerelease of next major", "npm", "2.0.0-beta.1", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "fixed", "1.0.0")}}, false, nil},
		{"two intervals, between", "npm", "1.7.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "1.0.0", "fixed", "1.5.0", "introduced", "2.0.0", "fixed", "2.5.0")}}, false, nil},
		{"two intervals, second", "npm", "2.1.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "1.0.0", "fixed", "1.5.0", "introduced", "2.0.0", "fixed", "2.5.0")}}, true, nil},
		{"events out of order", "npm", "2.1.0", nil,
			[]indexRange{{"SEMVER", ev("fixed", "2.5.0", "introduced", "2.0.0", "fixed", "1.5.0", "introduced", "1.0.0")}}, true, nil},
		{"before introduced", "npm", "0.9.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "1.0.0")}}, false, nil},
		{"last_affected inclusive", "crates.io", "1.2.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "last_affected", "1.2.0")}}, true, nil},
		{"after last_affected", "crates.io", "1.2.1", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "last_affected", "1.2.0")}}, false, nil},
		{"limit", "npm", "3.0.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "limit", "3.0.0")}}, false, nil},
		{"Go v prefix and pseudo-version", "Go", "0.0.0-20210101000000-abcdef123456", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "fixed", "0.0.0-20220101000000-abcdef123456")}}, true, nil},
		{"PyPI post-release is after fix", "PyPI", "2.0.post1", nil,
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0", "fixed", "2.0")}}, false, nil},
		{"PyPI rc before fix", "PyPI", "2.0rc1", nil,
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0", "fixed", "2.0")}}, true, nil},
		{"Gem prerelease before fix", "RubyGems", "1.0.a", nil,
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0", "fixed", "1.0")}}, true, nil},
		{"Composer v prefix", "Packagist", "v5.4.19", nil,
			[]indexRange{{"ECOSYSTEM", ev("introduced", "5.0.0", "fixed", "5.4.20")}}, true, nil},
		{"NuGet four parts", "NuGet", "4.5.0", nil,
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0", "fixed", "4.5.1")}}, true, nil},
		{"enumerated versions match exactly", "PyPI", "1.0", []string{"1.0.0", "1.1"},
			[]indexRange{{"ECOSYSTEM", ev("introduced", "5.0")}}, true, nil},
		// A release after the advisory's snapshot of versions is still in its
		// unfixed range: the entry affects the union of both.
		{"in the range but not the enumerated snapshot", "PyPI", "1.2", []string{"1.0", "1.1"},
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0")}}, true, nil},
		{"in neither the enumerated versions nor the range", "PyPI", "2.1", []string{"1.0", "1.1"},
			[]indexRange{{"ECOSYSTEM", ev("introduced", "0", "fixed", "2.0")}}, false, nil},
		{"GIT ranges skipped", "npm", "1.0.0", nil,
			[]indexRange{{"GIT", ev("introduced", "0", "fixed", "abcdef")}}, false, nil},
		{"unparseable event version", "npm", "1.0.0", nil,
			[]indexRange{{"SEMVER", ev("introduced", "0", "fixed", "next")}}, false, errEventVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := affects(tt.ecosystem, queryVersion(tt.ecosystem, tt.version), tt.versions, tt.ranges)
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Errorf("affects = %v, %v; want %v, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestCVSS3(t *testing.T) {
	tests := []struct {
		vector string
		score  float64
		rating string
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, "critical"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1, "medium"},
		{"CVSS:3.0/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N", 5.9, "medium"},
		{"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H", 9.9, "critical"},
		{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 7.8, "high"},
		{"CVSS:3.1/AV:P/AC:H/PR:H/UI:R/S:U/C:L/I:N/A:N", 1.6, "low"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0, ""},
	}
	for _, tt := range tests {
		score, ok := cvss3(tt.vector)
		if !ok || score != tt.score || rating(score) != tt.rating {
			t.Errorf("%s = %v (%v), %s; want %v, %s", tt.vector, score, ok, rating(score), tt.score, tt.rating)
		}
	}
	for _, bad := range []string{"", "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N", "CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "AV:N/AC:L/Au:N/C:P/I:P/A:P"} {
		if _, ok := cvss3(bad); ok {
			t.Errorf("%q scored", bad)
		}
	}
}

func TestRecordSeverity(t *testing.T) {
	tests := []struct {
		name   string
		rec    osvRecord
		rating string
		score  float64
	}{
		{"CVSS v3", osvRecord{Severity: []osvSeverity{{"CVSS_V3", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}}}, "critical", 9.8},
		{"GitHub moderate", osvRecord{DatabaseSpecific: []byte(`{"severity":"MODERATE"}`)}, "medium", 0},
		{"v4 falls back to database", osvRecord{Severity: []osvSeverity{{"CVSS_V4", "CVSS:4.0/AV:N"}}, DatabaseSpecific: []byte(`{"severity":"HIGH"}`)}, "high", 0},
		{"non-string severity", osvRecord{DatabaseSpecific: []byte(`{"severity":3}`)}, "", 0},
		{"none", osvRecord{}, "", 0},
	}
	for _, tt := range tests {
		if r, s := tt.rec.severity(); r != tt.rating || s != tt.score {
			t.Errorf("%s: %s %v, want %s %v", tt.name, r, s, tt.rating, tt.score)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct{ ecosystem, in, want string }{
		{"PyPI", "Jinja2", "jinja2"},
		{"PyPI", "zope.interface", "zope-interface"},
		{"PyPI", "Foo__Bar-.baz", "foo-bar-baz"},
		{"NuGet", "Newtonsoft.Json", "newtonsoft.json"},
		{"Packagist", "Symfony/Http-Kernel", "symfony/http-kernel"},
		{"npm", "Express", "Express"},
	}
	for _, tt := range tests {
		if got := normalizeName(tt.ecosystem, tt.in); got != tt.want {
			t.Errorf("normalizeName(%s, %q) = %q, want %q", tt.ecosystem, tt.in, got, tt.want)
		}
	}
}
