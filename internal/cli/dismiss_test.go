package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/internal/testutil"
)

// secretFindings scans dir and returns how many sec.hardcoded-password
// findings it reports, the dismissed count, and the summary's caveats.
func secretFindings(t *testing.T, dir string) (int, int, []string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--author", "me@example.com", "--json", "--since", "all", dir}, &out, &errOut); code != 0 {
		t.Fatalf("scan exit %d: %s", code, errOut.String())
	}
	var rep struct {
		Dismissed int `json:"findings_dismissed"`
		Languages []struct {
			Weaknesses []struct {
				Rule  string `json:"id"`
				Count int    `json:"count"`
			} `json:"weaknesses"`
		} `json:"languages"`
		Summary struct {
			Caveats []string `json:"caveats"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range rep.Languages {
		for _, w := range l.Weaknesses {
			if w.Rule == "sec.hardcoded-password" {
				n += max(w.Count, 1)
			}
		}
	}
	return n, rep.Dismissed, rep.Summary.Caveats
}

// A dismissed finding leaves later scans, which say how many were dismissed;
// it comes back when the line changes or the dismissal is undone.
func TestDismiss(t *testing.T) {
	t.Setenv("URFLUENCY_CONFIG_DIR", t.TempDir())
	r := testutil.NewRepo(t)
	src := func(v string) string { return "package a\n\nconst apiKey = \"" + v + "\"\n" }
	r.Commit("me@example.com", "", map[string]string{"a.go": src("Zq8vLm2pR7xTb4")})
	if n, _, _ := secretFindings(t, r.Dir); n != 1 {
		t.Fatalf("before dismissing: %d findings, want 1", n)
	}
	var out, errOut bytes.Buffer
	at := filepath.Join(r.Dir, "a.go") + ":3"
	// The reason may follow the arguments, as the README shows it.
	if code := Run(context.Background(), []string{"dismiss", "sec.hardcoded-password", at, "--reason", "SDK placeholder"}, &out, &errOut); code != 0 {
		t.Fatalf("dismiss exit %d: %s", code, errOut.String())
	}
	n, dismissed, caveats := secretFindings(t, r.Dir)
	if n != 0 || dismissed != 1 || !strings.Contains(strings.Join(caveats, " "), "1 finding you dismissed") {
		t.Errorf("after dismissing: %d findings, %d dismissed, caveats %q", n, dismissed, caveats)
	}
	out.Reset()
	Run(context.Background(), []string{"dismiss", "--list"}, &out, &errOut)
	if !strings.Contains(out.String(), "sec.hardcoded-password  SDK placeholder") || strings.Contains(out.String(), "Zq8v") || strings.Contains(out.String(), "a.go") {
		t.Errorf("list = %q; want the rule and reason, never the value or path", out.String())
	}
	// A changed line is a different finding.
	r.Commit("me@example.com", "", map[string]string{"a.go": src("Yt5nWq9sK3pLv7")})
	if n, _, _ := secretFindings(t, r.Dir); n != 1 {
		t.Errorf("after the line changed: %d findings, want 1", n)
	}
	r.Commit("me@example.com", "", map[string]string{"a.go": src("Zq8vLm2pR7xTb4")})
	if code := Run(context.Background(), []string{"dismiss", "sec.hardcoded-password", "--undo", at}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit %d: %s", code, errOut.String())
	}
	if n, _, _ := secretFindings(t, r.Dir); n != 1 {
		t.Errorf("after undo: %d findings, want 1", n)
	}
	if code := Run(context.Background(), []string{"dismiss", "sec.no-such-rule", at}, &out, &errOut); code != 2 {
		t.Errorf("an unknown rule exit = %d, want 2", code)
	}
}

// A committed secret counts only while it is still in the working copy: one
// removed locally is gone from the user's files, committed or not.
func TestSecretsRemovedLocallyDoNotCount(t *testing.T) {
	t.Setenv("URFLUENCY_CONFIG_DIR", t.TempDir())
	r := testutil.NewRepo(t)
	withSecret := "package a\n\nconst apiKey = \"Zq8vLm2pR7xTb4\"\n"
	r.Commit("me@example.com", "", map[string]string{"a.go": withSecret})
	if n, _, _ := secretFindings(t, r.Dir); n != 1 {
		t.Fatalf("committed and present: %d findings, want 1", n)
	}
	file := filepath.Join(r.Dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n\nvar apiKey = os.Getenv(\"API_KEY\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := secretFindings(t, r.Dir); n != 0 {
		t.Errorf("removed locally, not committed: %d findings, want 0", n)
	}
	if err := os.WriteFile(file, []byte(withSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := secretFindings(t, r.Dir); n != 1 {
		t.Errorf("restored: %d findings, want 1", n)
	}
}
