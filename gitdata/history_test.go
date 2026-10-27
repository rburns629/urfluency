package gitdata_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestFixHistory(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "2025-01-01T00:00:00Z", map[string]string{"a.go": "one\ntwo\nthree\n", "notes.md": "x\n"})
	if err := os.WriteFile(filepath.Join(r.Dir, "a.go"), []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("other@example.com", "2025-01-10T00:00:00Z", "commit", "-qam", "fix: correct the second line")
	r.Git("me@example.com", "2025-01-12T00:00:00Z", "mv", "a.go", "b.go")
	r.Git("me@example.com", "2025-01-12T00:00:00Z", "commit", "-qm", "Rename a to b")
	repo := gitdata.Repo{Dir: r.Dir}
	ctx := context.Background()

	commits, err := repo.Commits(ctx, []string{"me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	byDay := map[string]string{}
	for hash, c := range commits {
		byDay[c.Time.UTC().Format("2006-01-02")] = hash
	}
	first, rename := byDay["2025-01-01"], byDay["2025-01-12"]
	if first == "" || rename == "" {
		t.Fatalf("commits = %+v", commits)
	}

	added, err := repo.AddedLines(ctx, []string{"me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added[first], map[string]int{"a.go": 3, "notes.md": 1}) || added[rename]["b.go"] != 0 {
		t.Errorf("added = %v; want 3 lines in a.go first and none for the rename", added)
	}

	fixes, err := repo.FixCommits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixes) != 1 || fixes[0].Time.UTC().Format("2006-01-02") != "2025-01-10" {
		t.Fatalf("fixes = %+v, want only the fix: commit", fixes)
	}
	fixed, err := repo.FixedLines(ctx, fixes[0].Hash, func(p string) bool { return p == "a.go" })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixed, map[string]int{first: 1}) {
		t.Errorf("fixed lines = %v, want the rewritten line traced to %s", fixed, first)
	}
}

func TestTouchedPathsFollowsRenames(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"old/a.go": "package a\n"})
	r.Commit("other@example.com", "", map[string]string{"theirs.go": "package b\n"})
	r.Git("other@example.com", "", "mv", "old/a.go", "a.go")
	r.Git("other@example.com", "", "commit", "-qm", "Move a")
	repo := gitdata.Repo{Dir: r.Dir}

	touched, err := repo.TouchedPaths(context.Background(), []string{"ME@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !touched["a.go"] || touched["theirs.go"] {
		t.Errorf("touched = %v, want a.go (renamed by someone else) and not theirs.go", touched)
	}
	none, err := repo.TouchedPaths(context.Background(), []string{"nobody@example.com"})
	if err != nil || len(none) != 0 {
		t.Errorf("touched for an author with no commits = %v, %v", none, err)
	}
}

func TestAuthorMatchingIsLiteralAndExact(t *testing.T) {
	r := testutil.NewRepo(t)
	noreply := "16638916+someone@users.noreply.github.com"
	r.Commit(noreply, "", map[string]string{"mine.go": "package a\n"})
	r.Commit("nome@example.com", "", map[string]string{"theirs.go": "package a\n"})
	repo := gitdata.Repo{Dir: r.Dir}
	ctx := context.Background()

	commits, err := repo.Commits(ctx, []string{noreply})
	if err != nil || len(commits) != 1 {
		t.Errorf("a noreply address with + matched %d commits (%v), want 1", len(commits), err)
	}
	touched, err := repo.TouchedPaths(ctx, []string{"ME@example.com"})
	if err != nil || len(touched) != 0 {
		t.Errorf("me@example.com must not match nome@example.com: %v %v", touched, err)
	}
	added, err := repo.AddedLines(ctx, []string{noreply})
	if err != nil || len(added) != 1 {
		t.Errorf("added lines for the noreply address: %v %v", added, err)
	}
}
