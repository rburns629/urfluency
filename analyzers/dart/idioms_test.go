package dart

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `sealed class Shape {}

final class Circle extends Shape {
  const Circle(this.r);
  final double r;
}

extension Blank on String {
  bool get isBlank => trim().isEmpty;
}

String describe(Shape s) => switch (s) {
      Circle(r: var r) => 'circle $r',
      _ => 'other',
    };

(int, String) pair() => (1, 'a');

void build({required String title, int? count}) {
  final label = count?.toString() ?? 'none';
  String? cached;
  cached ??= label;
  final (n, s) = pair();
  final items = [for (final i in [1, 2]) i, if (n > 0) n];
  if (items case [var first, ...]) {}
}

Future<void> load() async {
  await Future<void>.delayed(Duration.zero);
}

Stream<int> ticks() async* {
  yield 1;
}
`
	got := map[string]int{}
	fs, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "lib/app.dart", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"dart.idiom.sealed-class": 1, "dart.idiom.const-constructor": 1, "dart.idiom.extension-methods": 1,
		"dart.idiom.pattern-matching": 3, "dart.idiom.records": 2, "dart.idiom.required-named-parameters": 1,
		"dart.idiom.null-aware": 3, "dart.idiom.collection-control-flow": 2, "dart.idiom.async-await": 1,
		"dart.idiom.generator": 1,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
	for _, r := range rules.ForLanguage("dart") {
		if r.Category == rules.Idiom && want[r.ID] == 0 {
			t.Errorf("catalog idiom %s is not covered by this test", r.ID)
		}
	}
}
