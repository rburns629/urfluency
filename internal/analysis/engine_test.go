package analysis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/analysis"
	_ "github.com/rburns629/urfluency/internal/analyzers/golang"
	_ "github.com/rburns629/urfluency/internal/analyzers/secrets"
	"github.com/rburns629/urfluency/internal/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

const theirs = `package a

func theirs() { panic("not mine") }
`

const mine = `package a

func theirs() { panic("not mine") }

func mine() { panic("mine") }
`

func TestScanKeepsOnlyOwnedFindings(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("other@example.com", "", map[string]string{"a/a.go": theirs, "vendor/v/v.go": "package v\nfunc f() { panic(1) }\n"})
	r.Commit("me@example.com", "", map[string]string{"a/a.go": mine, "vendor/v/v.go": "package v\nfunc f() { panic(2) }\n"})

	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir},
		analysis.Options{Emails: []string{"me@example.com"}, Since: time.Time{}})
	if err != nil {
		t.Fatal(err)
	}
	// Analyzers come first, then scanners (secrets).
	if len(res.Languages) != 2 || res.Languages[0].Language != "go" || res.Languages[1].Language != "secrets" {
		t.Fatalf("languages = %+v", res.Languages)
	}
	lr := res.Languages[0]
	if lr.Files != 1 {
		t.Errorf("files = %d, want 1 (vendor skipped)", lr.Files)
	}
	if len(lr.Findings) != 1 || lr.Findings[0].Line != 5 || lr.Findings[0].Rule != "go.panic-outside-main" {
		t.Errorf("findings = %+v, want one panic on line 5", lr.Findings)
	}
}

func TestScannersRunOnOwnedLinesOfAnyTextFile(t *testing.T) {
	// The fake token is assembled at runtime; see the secrets package tests.
	token := "gh" + "p_" + strings.Repeat("z", 36)
	r := testutil.NewRepo(t)
	r.Commit("other@example.com", "", map[string]string{"deploy/config.yaml": "a: 1\ntoken: " + token + "\n", "logo.png": "\x89PNG\x00"})
	r.Commit("me@example.com", "", map[string]string{"deploy/config.yaml": "a: 1\ntoken: " + token + "\nmine: " + token + "\n"})

	res, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: r.Dir},
		analysis.Options{Emails: []string{"me@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	var secrets *analysis.LanguageResult
	for i := range res.Languages {
		if res.Languages[i].Language == "secrets" {
			secrets = &res.Languages[i]
		}
	}
	if secrets == nil || len(secrets.Findings) != 1 || secrets.Findings[0].Line != 3 {
		t.Fatalf("want one owned token finding on line 3, got %+v", res.Languages)
	}
}

func TestScanRequiresEmails(t *testing.T) {
	if _, err := analysis.Scan(context.Background(), gitdata.Repo{Dir: t.TempDir()}, analysis.Options{}); err == nil {
		t.Error("expected an error without emails")
	}
}
