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
