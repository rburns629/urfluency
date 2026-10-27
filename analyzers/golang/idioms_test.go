package golang

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

// positives returns the idiom rules found in the source.
func positives(t *testing.T, a analysis.Analyzer, path, src string) map[string]int {
	t.Helper()
	findings, err := a.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	return got
}

func TestIdioms(t *testing.T) {
	src := `package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"
)

type NotFound struct{ Key string }

func (e NotFound) Error() string { return "not found: " + e.Key }

type Getter interface{ Get(ctx context.Context, key string) (string, error) }

func First[T any](items []T) T { return items[0] }

func Load(ctx context.Context, path string, mu *sync.Mutex) error {
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	g, ctx := errgroup.WithContext(ctx)
	_ = g
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}
`
	got := positives(t, Analyzer{}, "store.go", src)
	want := map[string]int{
		"go.idiom.custom-error-type": 1, "go.idiom.small-interface": 1, "go.idiom.generics": 1,
		"go.idiom.context-first": 1, "go.idiom.defer-cleanup": 3, "go.idiom.error-wrapping": 1,
		"go.idiom.error-inspection": 1, "go.idiom.structured-concurrency": 1, "go.idiom.cancellation": 1,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}

	test := `package store

import "testing"

func TestFirst(t *testing.T) {
	for _, tc := range []struct{ in []int }{{[]int{1}}} {
		t.Run("case", func(t *testing.T) { _ = tc })
	}
}
`
	if got := positives(t, Analyzer{}, "store_test.go", test); got["go.idiom.table-driven-test"] != 1 {
		t.Errorf("table-driven test not found: %v", got)
	}
	// Every idiom the analyzer reports is in the catalog.
	for rule := range got {
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
}
