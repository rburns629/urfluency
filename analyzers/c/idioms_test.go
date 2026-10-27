package clang

import (
	"context"
	"reflect"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func idioms(t *testing.T, path, src string) map[string]int {
	t.Helper()
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	for rule := range got {
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
	return got
}

func TestIdioms(t *testing.T) {
	src := `#include <stdint.h>
#include <stdlib.h>

enum color { RED, GREEN };
enum color c;

struct point { int32_t x; int32_t y; };

_Static_assert(sizeof(struct point) == 8, "packed");

static int parse(const char *s, char *const out, size_t n) {
	snprintf(out, n, "%s", s);
	return 0;
}

int load(const char *path) {
	struct point p = { .x = 1, .y = 2 };
	int a[2] = { 1, 2 };
	int *buf = malloc(4 * sizeof *buf);
	int *more = calloc(4, sizeof(int));
	char *s = malloc(sizeof(s[0]) * 8);
	if (!buf) goto cleanup;
	if (!more) goto fail;
	free(s);
cleanup:
	free(buf);
retry:
	free(more);
fail:
	return 0;
}

void loop(char *s) {
again:
	if (*s++) goto again;
}
`
	want := map[string]int{
		"c.idiom.fixed-width-types": 2, "c.idiom.enum-constants": 1, "c.idiom.static-assert": 1,
		"c.idiom.static-function": 1, "c.idiom.const-pointer-param": 2, "c.idiom.bounded-string": 1,
		"c.idiom.designated-initializer": 1, "c.idiom.sizeof-object": 2, "c.idiom.goto-cleanup": 2,
	}
	if got := idioms(t, "src/load.c", src); !reflect.DeepEqual(got, want) {
		t.Errorf("idioms = %v, want %v", got, want)
	}
}

func TestIncludeGuard(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want int
	}{
		{"ifndef guard", "a.h", "/* a */\n#ifndef A_H\n#define A_H\nint f(void);\n#endif\n", 1},
		{"pragma once", "a.h", "#pragma once\nint f(void);\n", 1},
		{"mismatched define", "a.h", "#ifndef A_H\n#define B_H\nint f(void);\n#endif\n", 0},
		{"no guard", "a.h", "int f(void);\n", 0},
		{"source files are not headers", "a.c", "#pragma once\nint f(void);\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := idioms(t, tt.path, tt.src)["c.idiom.include-guard"]; got != tt.want {
				t.Errorf("include guards = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEveryIdiomIsCataloged(t *testing.T) {
	n := 0
	for _, r := range rules.ForLanguage("c") {
		if r.Category == rules.Idiom {
			n++
		}
	}
	if n != 10 {
		t.Errorf("C idioms in catalog = %d, want 10", n)
	}
}
