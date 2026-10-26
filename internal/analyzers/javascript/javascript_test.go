package javascript

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/internal/analysis"
)

func findings(t *testing.T, a analysis.Analyzer, path, src string) []string {
	t.Helper()
	got, err := a.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestJavaScriptRules(t *testing.T) {
	src := `var a = 1;
let b = 2;
if (a == 1 || b != "2" || a == null || a === 1) {}
try { f(); } catch (e) {}
try { f(); } catch { /* optional cleanup */ }
items.forEach(async (x) => { await g(x); });
items.forEach((x) => g(x));
// @ts-ignore
`
	want := []string{
		"js.async-foreach:6", "js.empty-catch:4", "js.loose-equality:3", "js.loose-equality:3",
		"js.var-declaration:1", "ts.ts-ignore:8",
	}
	if got := findings(t, JavaScript{}, "src/a.js", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTypeScriptRules(t *testing.T) {
	src := `const a: any = b!.c as unknown as string;
function h(p: unknown): string { return p as string; }
const d = e as any;
`
	want := []string{"ts.double-assertion:1", "ts.explicit-any:1", "ts.explicit-any:3", "ts.non-null-assertion:1"}
	if got := findings(t, TypeScript{}, "src/a.ts", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTSXUsesTSXGrammar(t *testing.T) {
	src := `export const C = (p: any) => <div>{p.x!}</div>;
`
	want := []string{"ts.explicit-any:1", "ts.non-null-assertion:1"}
	if got := findings(t, TypeScript{}, "src/C.tsx", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSkip(t *testing.T) {
	long := strings.Repeat("a", 1200)
	cases := []struct {
		a       analysis.Analyzer
		path    string
		content string
		want    bool
	}{
		{TypeScript{}, "src/types.d.ts", "declare const x: any;", true},
		{TypeScript{}, "src/app.ts", "const x = 1;", false},
		{JavaScript{}, "dist/app.js", "var x;", true},
		{JavaScript{}, "src/vendor.min.js", "var x;", true},
		{JavaScript{}, "src/bundle.js", long, true},
		{JavaScript{}, "src/app.js", "const x = 1;", false},
	}
	for _, c := range cases {
		if got := c.a.Skip(analysis.File{Path: c.path, Content: []byte(c.content)}); got != c.want {
			t.Errorf("Skip(%s) = %v, want %v", c.path, got, c.want)
		}
	}
}
