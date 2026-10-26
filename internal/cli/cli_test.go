package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	tests := map[string]string{"90d": "2026-07-28", "2026-01-31": "2026-01-31"}
	for in, want := range tests {
		got, err := parseSince(in, now)
		if err != nil || got.Format("2006-01-02") != want {
			t.Errorf("parseSince(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"0d", "-3d", "soon", "2026-13-01"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) accepted", bad)
		}
	}
}

func TestScanJSON(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a.go": "package a\n\nfunc f() { panic(1) }\n"})
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--locations", r.Dir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	var goLang *report.Language
	for i := range rep.Languages {
		if rep.Languages[i].Language == "go" {
			goLang = &rep.Languages[i]
		}
	}
	if goLang == nil || len(goLang.Weaknesses) != 1 || goLang.Weaknesses[0].Locations[0] != "a.go:3" {
		t.Errorf("report = %+v", rep)
	}
}

func TestUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"bogus"}, &out, &errOut); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("stderr = %q", errOut.String())
	}
}
