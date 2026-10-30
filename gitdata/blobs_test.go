package gitdata_test

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/testutil"
)

func TestBlobsMatchShow(t *testing.T) {
	r := testutil.NewRepo(t)
	files := map[string]string{"a.go": "package a\n", "dir/b c.py": "x = 1\n", "empty.txt": "", "no-newline.js": "let x"}
	r.Commit("me@example.com", "", files)
	ctx := context.Background()
	repo := gitdata.Repo{Dir: r.Dir}
	head, err := repo.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := repo.OpenBlobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()

	var wg sync.WaitGroup
	for range 4 { // concurrent readers take turns
		for p := range files {
			wg.Go(func() {
				want, err := repo.Show(ctx, head, p)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := blobs.Show(head, p)
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("%s = %q, %v; want %q", p, got, err, want)
				}
			})
		}
	}
	wg.Wait()

	for _, p := range []string{"missing.go", "dir", "dir/no such file"} {
		if _, err := blobs.Show(head, p); err == nil {
			t.Errorf("%s: no error", p)
		}
	}
	// The stream stays in step after errors.
	if got, err := blobs.Show(head, "a.go"); err != nil || string(got) != files["a.go"] {
		t.Errorf("after errors: %q, %v", got, err)
	}
}
