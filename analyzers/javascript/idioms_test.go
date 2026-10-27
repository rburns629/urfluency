package javascript

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func idioms(t *testing.T, a analysis.Analyzer, path, src string) map[string]int {
	t.Helper()
	findings, err := a.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
			if r, ok := rules.Lookup(f.Rule); !ok || r.Category != rules.Idiom {
				t.Errorf("%s is not an idiom in the catalog", f.Rule)
			}
		}
	}
	return got
}

func TestIdioms(t *testing.T) {
	js := `async function load(api, ids) {
  const { data } = await api.get("/users");
  const [first] = data;
  const results = await Promise.all(ids.map((id) => api.get(id)));
  return first?.name ?? results.length;
}
`
	got := idioms(t, JavaScript{}, "load.js", js)
	for rule, n := range map[string]int{
		"js.idiom.async-await": 2, "js.idiom.destructuring": 2, "js.idiom.concurrent-promises": 1,
		"js.idiom.optional-chaining": 1, "js.idiom.nullish-coalescing": 1,
	} {
		if got[rule] != n {
			t.Errorf("js %s = %d, want %d", rule, got[rule], n)
		}
	}

	tsSrc := `interface User { id: string; name?: string }
type Status = "active" | "disabled" | "pending";
function isUser(x: unknown): x is User { return typeof x === "object" && x !== null; }
function first<T>(items: T[]): T | undefined { return items[0]; }
type Patch = Partial<User>;
`
	got = idioms(t, TypeScript{}, "user.ts", tsSrc)
	for rule, n := range map[string]int{
		"ts.idiom.declared-types": 3, "ts.idiom.union-types": 2, "ts.idiom.type-guard": 1,
		"ts.idiom.unknown": 1, "ts.idiom.generics": 1, "ts.idiom.utility-types": 1,
	} {
		if got[rule] != n {
			t.Errorf("ts %s = %d, want %d", rule, got[rule], n)
		}
	}
}
