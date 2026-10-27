package clang

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
	"github.com/rburns629/urfluency/rules"
)

func findings(t *testing.T, path, src string) []string {
	t.Helper()
	got, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		if f.Positive {
			continue // idioms are tested separately
		}
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "unsafe string functions",
			src: `void f(char *dst, const char *src, va_list ap) {
	gets(dst);
	strcpy(dst, src);
	strcat(dst, src);
	sprintf(dst, "%s", src);
	vsprintf(dst, "%s", ap);
}
`,
			want: []string{"c.unsafe-string-function:2", "c.unsafe-string-function:3", "c.unsafe-string-function:4",
				"c.unsafe-string-function:5", "c.unsafe-string-function:6"},
		},
		{
			name: "bounded string functions and literal copies are fine",
			src: `void f(char *dst, size_t n, const char *src) {
	snprintf(dst, n, "%s", src);
	strlcpy(dst, src, n);
	fgets(dst, n, stdin);
	strcpy(dst, "fixed");
	s.strcpy(dst, src);
}
`,
			want: []string{},
		},
		{
			name: "non-literal format strings",
			src: `void f(const char *msg, char *buf, size_t n) {
	printf(msg);
	fprintf(stderr, msg);
	snprintf(buf, n, msg);
	syslog(LOG_ERR, buf);
}
`,
			want: []string{"c.format-string-not-literal:2", "c.format-string-not-literal:3",
				"c.format-string-not-literal:4", "c.format-string-not-literal:5"},
		},
		{
			name: "literal, macro, translated, and forwarded formats are fine",
			src: `void f(const char *msg, const char *fmt, va_list ap, uint64_t v) {
	printf("%s", msg);
	printf("%" PRIu64 "\n", v);
	printf(USAGE);
	printf(_("hello\n"));
	fprintf(stderr, fmt, v);
	vfprintf(stderr, fmt, ap);
}
`,
			want: []string{},
		},
		{
			name: "system and popen with built commands",
			src: `void f(const char *cmd) {
	system(cmd);
	FILE *p = popen(cmd, "r");
	system("ls -l");
	system(DEFAULT_CMD);
	popen("date", "r");
}
`,
			want: []string{"c.system-call:2", "c.system-call:3"},
		},
		{
			name: "void pointer arithmetic",
			src: `void f(void *buf, const void *in, void **pp, size_t n) {
	void *end = buf;
	char *c = buf;
	buf++;
	end += n;
	memcpy(buf + 1, in, n);
	pp++;
	c++;
	c += n;
	memcpy((unsigned char *)buf + 1, in, n);
}
`,
			want: []string{"c.void-pointer-arithmetic:4", "c.void-pointer-arithmetic:5", "c.void-pointer-arithmetic:6"},
		},
		{
			name: "names redeclared with another type are not guessed",
			src: `void f(void) {
	{ void *p = 0; (void)p; }
	{ char *p = 0; p++; }
}
`,
			want: []string{},
		},
		{
			name: "empty parameter lists in prototypes",
			src: `int f();
int g(void);
int h(int a);
int i( );
int main() { return 0; }
`,
			want: []string{"c.empty-parameter-list:1", "c.empty-parameter-list:4"},
		},
		{
			name: "assignment in condition",
			src: `void f(int x, int y, struct node *p) {
	if (x = y) {}
	while (p = p->next) {}
	do {} while (x = 0);
	if ((x = y)) {}
	if (x == y) {}
	while ((x = getchar()) != EOF) {}
}
`,
			want: []string{"c.assignment-in-condition:2", "c.assignment-in-condition:3", "c.assignment-in-condition:4"},
		},
		{
			name: "realloc assigned to its own argument",
			src: `void f(char *p, char **q, size_t n) {
	p = realloc(p, n);
	*q = (char *)realloc(*q, n);
	char *tmp = realloc(p, n);
	if (tmp) p = tmp;
}
`,
			want: []string{"c.realloc-self-assign:2", "c.realloc-self-assign:3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, "src/a.c", tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	for _, r := range rules.ForLanguage("c") {
		if !strings.HasPrefix(r.ID, "c.") && r.ID != "clean.cognitive-complexity" {
			t.Errorf("unexpected C rule %s", r.ID)
		}
	}
	for _, id := range []string{"c.unsafe-string-function", "c.format-string-not-literal", "c.system-call",
		"c.void-pointer-arithmetic", "c.empty-parameter-list", "c.assignment-in-condition", "c.realloc-self-assign"} {
		if r, ok := rules.Lookup(id); !ok || r.Category == rules.Idiom {
			t.Errorf("%s is not a rule in the catalog", id)
		}
	}
}

func TestComplexity(t *testing.T) {
	// if (+1), for (+2), if (+3), && and || (+2), else (+1), while (+4),
	// switch (+2), ternary (+3), goto (+1), do (+1): 20.
	src := `int f(int a, int b) {
	if (a) {
		for (int i = 0; i < a; i++) {
			if (i && b || a) {
				b++;
			} else {
				while (b) b--;
			}
		}
		switch (a) { case 1: b = a ? 1 : 2; break; }
	}
	goto done;
	do { a--; } while (a);
done:
	return b;
}
int g(int a) { return a ? 1 : 0; }
`
	tree, err := parsing.Parse(parsing.C, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	scores := parsing.Cognitive(tree, Complexity)
	if wantScores := []parsing.FunctionComplexity{{Line: 1, Complexity: 20}, {Line: 17, Complexity: 1}}; !reflect.DeepEqual(scores, wantScores) {
		t.Errorf("Cognitive = %v, want %v", scores, wantScores)
	}
	want := []string{"clean.cognitive-complexity:1"}
	if got := findings(t, "src/a.c", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := findings(t, "tests/test_a.c", src); len(got) != 0 {
		t.Errorf("test files report complexity: %v", got)
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path    string
		content string
		want    bool
	}{
		{"src/a.c", "int x;\n", false},
		{"include/a.h", "#ifndef A_H\n#define A_H\n/* a class of errors */\nint f(void);\n#endif\n", false},
		{"include/a.h", "#pragma once\nnamespace app {\nint f();\n}\n", true},
		{"include/b.h", "#pragma once\nclass Widget {};\n", true},
		{"include/c.h", "#pragma once\ntemplate <typename T> T id(T t);\n", true},
		{"include/d.h", "#include <string>\nstd::string name();\n", true},
		{"include/e.h", "extern \"C++\" {\n}\n", true},
		{"src/a.c", "/* Generated by flex. */\nint x;\n", true},
		{"src/b.c", "// Code generated. DO NOT EDIT.\nint x;\n", true},
		{"build/a.c", "int x;\n", true},
		{"cmake-build-debug/a.c", "int x;\n", true},
	}
	for _, tt := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tt.path, Content: []byte(tt.content)}); got != tt.want {
			t.Errorf("Skip(%s, %q) = %v, want %v", tt.path, tt.content, got, tt.want)
		}
	}
}

func TestSkipPath(t *testing.T) {
	tests := map[string]bool{
		"build/main.c":                 true,
		"out/obj/main.c":               true,
		"cmake-build-release/x/main.c": true,
		"src/main.c":                   false,
		"src/build.c":                  false,
		"src/output/main.c":            false,
	}
	for p, want := range tests {
		if got := (Analyzer{}).SkipPath(p); got != want {
			t.Errorf("SkipPath(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestFrameworks(t *testing.T) {
	src := `#include <stdio.h>
#include <unistd.h>
#include <gtk/gtk.h>
#include <glib.h>
#include "SDL2/SDL.h"
#include <openssl/ssl.h>
#include <curl/curl.h>
#include <sqlite3.h>
#ifdef USE_RTOS
#include "FreeRTOS.h"
#endif
#include <zephyr/kernel.h>
#include <openssl/evp.h>
`
	want := []string{"freertos", "glib", "gtk", "libcurl", "openssl", "sdl", "sqlite3", "zephyr"}
	if got := (Analyzer{}).Frameworks(analysis.File{Path: "main.c", Content: []byte(src)}); !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks = %v, want %v", got, want)
	}
	if got := (Analyzer{}).Frameworks(analysis.File{Path: "main.c", Content: []byte("#include <stdlib.h>\n")}); len(got) != 0 {
		t.Errorf("Frameworks = %v, want none", got)
	}
}

func TestRegistered(t *testing.T) {
	if a := analysis.ForPath("src/main.c"); a == nil || a.Language() != "c" {
		t.Errorf("ForPath(.c) = %v, want the C analyzer", a)
	}
}
