package analysis

import (
	"testing"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

func TestWritten(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	commits := map[string]gitdata.Commit{
		"a": {Time: at("2026-01-15")},
		"b": {Time: at("2026-03-02")},
		"c": {Time: at("2025-06-01")}, // before the scan's start
	}
	added := map[string]map[string]int{
		"a": {"main.go": 100, "README.md": 40, "vendor/x/x.go": 500, "gen.go": 70},
		"b": {"main.go": 30, "util.go": 20},
		"c": {"main.go": 999},
	}
	tools := map[string][]string{"b": {"claude", "cursor"}}
	w := written(added, commits, tools, func(p string) bool { return p == "gen.go" }, at("2026-01-01"))
	if got := w.Months["2026-01"]; got != (WrittenLines{Other: 100}) {
		t.Errorf("January = %+v, want 100 human lines: prose, vendored, and excluded files do not count", got)
	}
	if got := w.Months["2026-03"]; got != (WrittenLines{Assisted: 50}) {
		t.Errorf("March = %+v, want 50 AI-assisted lines", got)
	}
	if len(w.Months) != 2 || len(w.Languages) != 1 || w.Languages["go"] != (WrittenLines{Assisted: 50, Other: 100}) {
		t.Errorf("months %v, languages %v", w.Months, w.Languages)
	}
	if w.Tools["claude"] != 50 || w.Tools["cursor"] != 50 {
		t.Errorf("tools = %v", w.Tools)
	}

	m := newWritten()
	m.merge(w)
	m.merge(w)
	if m.Months["2026-03"].Assisted != 100 || m.Languages["go"].Other != 200 || m.Tools["claude"] != 100 {
		t.Errorf("merged = %+v", m)
	}
}
