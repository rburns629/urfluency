package dart

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
		if _, ok := rules.Lookup(f.Rule); !ok {
			t.Errorf("%s is not in the catalog", f.Rule)
		}
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "dynamic in declarations but not type arguments",
			path: "lib/a.dart",
			src: `dynamic config;
dynamic parse(dynamic raw) => raw;
Map<String, dynamic> toJson() => {};
List<dynamic> items = [];
Object? safe;
`,
			want: []string{"dart.dynamic-type:1", "dart.dynamic-type:2", "dart.dynamic-type:2"},
		},
		{
			name: "null assertions but not negation or inequality",
			path: "lib/a.dart",
			src: `void f(String? a, bool b) {
  final n = a!.length;
  a!.trim();
  final ok = !b && a != null;
}
`,
			want: []string{"dart.null-assertion:2", "dart.null-assertion:3"},
		},
		{
			name: "empty catch and on blocks, but not handled or commented ones",
			path: "lib/a.dart",
			src: `void f() {
  try {
    g();
  } catch (e) {}
  try {
    g();
  } on FormatException {
  }
  try {
    g();
  } on StateError catch (e) {
    log(e);
  }
  try {
    g();
  } catch (_) {
    // Optional cache; a miss is fine.
  }
  try {
    g();
  } finally {}
}
`,
			want: []string{"dart.empty-catch:4", "dart.empty-catch:7"},
		},
		{
			name: "print in lib code but not debugPrint or methods named print",
			path: "lib/src/service.dart",
			src: `void f(Printer p) {
  print('loaded');
  debugPrint('ok');
  p.print('page');
  items.forEach((i) => print(i));
}
`,
			want: []string{"dart.print-in-library:2", "dart.print-in-library:5"},
		},
		{
			name: "print is fine in main.dart, bin, and tests",
			path: "lib/main.dart",
			src:  "void main() { print('hi'); }\n",
			want: []string{},
		},
		{
			name: "print in a command-line script",
			path: "bin/tool.dart",
			src:  "void main() { print('hi'); }\n",
			want: []string{},
		},
		{
			name: "http URL literals except local hosts and interpolated hosts",
			path: "lib/api.dart",
			src: `final a = Uri.parse('http://api.example.com/v1');
final b = Uri.parse("https://api.example.com");
final c = Uri.parse('http://localhost:8080/x');
final d = Uri.parse('http://10.0.2.2:3000');
final e = Uri.parse('http://$host/x');
final f = Uri.tryParse(r'http://example.com');
final g = Uri.parse(base + '/x');
`,
			want: []string{"dart.http-url-literal:1", "dart.http-url-literal:6"},
		},
		{
			name: "setState after await without a mounted check",
			path: "lib/home.dart",
			src: `import 'package:flutter/material.dart';

class _HomeState extends State<Home> {
  Future<void> load() async {
    final data = await fetch();
    setState(() => items = data);
  }

  Future<void> guarded() async {
    final data = await fetch();
    if (!mounted) return;
    setState(() => items = data);
  }

  Future<void> contextGuard() async {
    await save();
    if (!context.mounted) return;
    setState(() {});
  }

  Future<void> guardedTooEarly() async {
    await a();
    if (!mounted) return;
    await b();
    setState(() {});
  }

  Future<void> beforeAwait() async {
    setState(() => loading = true);
    await fetch();
    if (mounted) setState(() => loading = false);
  }

  void notAsync() {
    fetch().then((v) => setState(() => items = v));
  }
}
`,
			want: []string{"dart.set-state-after-await:25", "dart.set-state-after-await:6"},
		},
		{
			name: "setState is only checked in Flutter files",
			path: "lib/home.dart",
			src: `Future<void> load() async {
  await fetch();
  setState(() {});
}
`,
			want: []string{},
		},
		{
			name: "controllers in State without dispose",
			path: "lib/form.dart",
			src: `import 'package:flutter/widgets.dart';

class _FormState extends State<FormPage> {
  final _name = TextEditingController();
  late final AnimationController _anim;

  @override
  void initState() {
    super.initState();
    _anim = AnimationController(vsync: this);
  }
}

class _DisposedState extends State<Other> with TickerProviderStateMixin {
  late final AnimationController _anim = AnimationController(vsync: this);

  @override
  void dispose() {
    _anim.dispose();
    super.dispose();
  }
}

class Holder {
  final c = ScrollController();
}
`,
			want: []string{"dart.missing-dispose:4"},
		},
		{
			name: "test files skip type, print, and URL rules",
			path: "test/widget_test.dart",
			src: `void main() {
  dynamic x = f()!;
  print(Uri.parse('http://example.com'));
}
`,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, tt.path, tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComplexity(t *testing.T) {
	// Each nested if adds 1 plus its depth: 1+2+3+4+5 = 15 is at the threshold,
	// and one more nested level exceeds it.
	nested := func(depth int) string {
		var b strings.Builder
		b.WriteString("void f(int a) {\n")
		for i := 0; i < depth; i++ {
			fmt.Fprintf(&b, "if (a > %d) {\n", i)
		}
		b.WriteString("g();\n")
		b.WriteString(strings.Repeat("}\n", depth))
		b.WriteString("}\n")
		return b.String()
	}
	if got := findings(t, "lib/a.dart", nested(5)); len(got) != 0 {
		t.Errorf("complexity 15 reported: %v", got)
	}
	if got := findings(t, "lib/a.dart", nested(6)); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:1"}) {
		t.Errorf("complexity 21 = %v, want one finding", got)
	}
	if got := findings(t, "test/a_test.dart", nested(6)); len(got) != 0 {
		t.Errorf("tests reported for complexity: %v", got)
	}
	// Scores per the grammar: an else-if is not counted again and does not nest;
	// a ternary inside a loop adds 2; a labeled jump adds 1; a lambda nests.
	scores := []struct {
		src  string
		want int
	}{
		// if, two else ifs, and else each add 1, none nested.
		{"void f(int a) {\n if (a == 1) {} else if (a == 2) {} else if (a == 3) {} else {}\n}\n", 4},
		// One sequence of && and one of || add 1 each.
		{"void f(bool a, bool b, bool c) {\n if (a && b || c) {}\n}\n", 3},
		{"void f(List<int> xs) {\n outer: for (final x in xs) { var y = x > 0 ? 1 : 2; break outer; }\n}\n", 4},
		{"void f(List<int> xs) {\n xs.forEach((x) { if (x > 0) { g(); } });\n}\n", 2},
		{"void f() {\n try { g(); } catch (e) { while (true) {} }\n}\n", 2},
	}
	for _, tt := range scores {
		tree, err := parsing.Parse(parsing.Dart, []byte(tt.src))
		if err != nil {
			t.Fatal(err)
		}
		fns := parsing.Cognitive(tree, Complexity)
		tree.Close()
		if len(fns) != 1 || fns[0].Complexity != tt.want {
			t.Errorf("Cognitive(%q) = %+v, want one function with %d", tt.src, fns, tt.want)
		}
	}
}

func TestSkip(t *testing.T) {
	cases := map[string]bool{
		"lib/user.dart":             false,
		"lib/user.g.dart":           true,
		"lib/user.freezed.dart":     true,
		"lib/router.gr.dart":        true,
		"lib/di.config.dart":        true,
		"test/repo_test.mocks.dart": true,
		"build/app/x.dart":          true,
		".dart_tool/build/x.dart":   true,
		"lib/generated.dart":        true, // by header
	}
	for p, want := range cases {
		content := []byte("class A {}\n")
		if p == "lib/generated.dart" {
			content = []byte("// GENERATED CODE - DO NOT MODIFY BY HAND\n\npart of 'user.dart';\n")
		}
		if got := (Analyzer{}).Skip(analysis.File{Path: p, Content: content}); got != want {
			t.Errorf("Skip(%s) = %v, want %v", p, got, want)
		}
	}
	if !(Analyzer{}).SkipPath("lib/user.g.dart") || (Analyzer{}).SkipPath("lib/user.dart") {
		t.Error("SkipPath does not match Skip for generated suffixes")
	}
}

func TestRegistered(t *testing.T) {
	if a := analysis.ForPath("lib/main.dart"); a == nil || a.Language() != "dart" {
		t.Fatalf("ForPath(.dart) = %v", a)
	}
}
