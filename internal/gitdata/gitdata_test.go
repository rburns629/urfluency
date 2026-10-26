package gitdata_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rburns629/urfluency/internal/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestOwnedLines(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("other@example.com", "2025-01-01T00:00:00Z", map[string]string{"a.go": "l1\nl2\nl3\n"})
	r.Commit("me@example.com", "2026-06-01T00:00:00Z", map[string]string{"a.go": "l1\nmine\nl3\nmine too\n"})
	repo := gitdata.Repo{Dir: r.Dir}
	ctx := context.Background()
	head, err := repo.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.OwnedLines(ctx, head, "a.go", []string{"ME@example.com"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]bool{2: true, 4: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("owned lines = %v, want %v", got, want)
	}

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	got, err = repo.OwnedLines(ctx, head, "a.go", []string{"me@example.com"}, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("lines before since should not count, got %v", got)
	}
}

func TestTrackedFilesAndShow(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"dir/b.go": "package b\n", "a.txt": "x"})
	repo := gitdata.Repo{Dir: r.Dir}
	ctx := context.Background()
	files, err := repo.TrackedFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "dir/b.go"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
	content, err := repo.Show(ctx, "HEAD", "dir/b.go")
	if err != nil || string(content) != "package b\n" {
		t.Errorf("show = %q, %v", content, err)
	}
}

func TestTrackedFilesListsOnlyCommittedRegularFiles(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("me@example.com", "", map[string]string{"a.go": "package a\n"})
	if err := os.Symlink("a.go", filepath.Join(r.Dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	// A submodule gitlink cannot be blamed; a staged file is not at HEAD.
	r.Git("me@example.com", "", "add", "link.go")
	r.Git("me@example.com", "", "update-index", "--add", "--cacheinfo", "160000,0f5e980e898054d8b769dd17b1db0c1c429e5e37,sub")
	r.Git("me@example.com", "", "commit", "-q", "-m", "link and submodule")
	if err := os.WriteFile(filepath.Join(r.Dir, "staged.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("me@example.com", "", "add", "staged.go")

	files, err := gitdata.Repo{Dir: r.Dir}.TrackedFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}
