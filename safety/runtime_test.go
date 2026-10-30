package safety

import (
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestPHPFixed(t *testing.T) {
	for v, want := range map[string]string{
		"8.3.30": "8.3.36", "8.3.36": "", "8.3.40": "", "8.4.0": "8.4.27", "8.5.11": "8.5.12", "8.2.34": "8.2.35",
		"8.1.29": "eol", "7.4.33": "eol", "9.0.0": "", "8.6.1": "",
	} {
		fixed, vulnerable := phpFixed(v)
		got := fixed
		if vulnerable && fixed == "" {
			got = "eol"
		}
		if !vulnerable {
			got = ""
		}
		if got != want {
			t.Errorf("phpFixed(%s) = %q %v, want %q", v, fixed, vulnerable, want)
		}
	}
}

func TestRuntimeAdvisories(t *testing.T) {
	files := []analysis.File{
		{Path: "Dockerfile", Content: []byte("FROM composer:2 AS deps\nFROM php:8.3.30-fpm-alpine AS app\nFROM php:8.4\n")},
		{Path: "docker/web.Dockerfile", Content: []byte("FROM docker.io/library/php:8.1.2-apache\n")},
		{Path: ".tool-versions", Content: []byte("nodejs 22.1.0\nphp 8.4.27\n")},
		{Path: "api/.php-version", Content: []byte("8.2.20\n")},
		{Path: "composer.json", Content: []byte(`{"require":{"php":"^8.3"},"config":{"platform":{"php":"8.5.1"}}}`)},
	}
	got := map[string]RuntimeAdvisory{}
	for _, a := range RuntimeAdvisories(files) {
		got[a.Path] = a
	}
	want := map[string]struct {
		version, fixed string
		line           int
	}{
		"Dockerfile": {"8.3.30", "8.3.36", 2}, "docker/web.Dockerfile": {"8.1.2", "", 1},
		"api/.php-version": {"8.2.20", "8.2.35", 1}, "composer.json": {"8.5.1", "8.5.12", 1},
	}
	if len(got) != len(want) {
		t.Errorf("got %d advisories, want %d: %+v", len(got), len(want), got)
	}
	for p, w := range want {
		a, ok := got[p]
		if !ok || a.Version != w.version || a.Fixed != w.fixed || a.Line != w.line {
			t.Errorf("%s = %+v, want %+v", p, a, w)
		}
	}
	// The floating php:8.4 tag and the fixed 8.4.27 are not reported.
	if _, ok := got[".tool-versions"]; ok {
		t.Error("fixed version reported")
	}
}
