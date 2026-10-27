package java

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `package a;

import java.util.*;
import java.util.stream.Collectors;

record Point(int x, int y) {}

sealed interface Shape permits Circle {}

final class Circle implements Shape {
  <T extends Comparable<? super T>> T max(List<T> xs) {
    return xs.stream().filter(Objects::nonNull).max(Comparator.naturalOrder()).orElseThrow();
  }

  List<String> names(List<User> users) {
    var out = users.stream().map(User::name).filter(n -> !n.isBlank()).collect(Collectors.toList());
    users.stream().count();
    return out;
  }

  Optional<String> find(String key) {
    return Optional.ofNullable(key);
  }

  String read(Path p) throws IOException {
    try (var in = Files.newBufferedReader(p)) {
      return in.readLine();
    }
  }

  String describe(Object o, int k) {
    if (o instanceof String s) {
      return s;
    }
    if (o instanceof Integer) {
      return "int";
    }
    String size = switch (k) {
      case 0 -> "none";
      default -> "some";
    };
    switch (k) {
      case 1: return "one";
    }
    return size;
  }
}
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "src/main/java/a/Circle.java", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"java.idiom.records": 1, "java.idiom.sealed-types": 1, "java.idiom.bounded-generics": 2,
		"java.idiom.streams": 2, "java.idiom.lambdas": 3, "java.idiom.var": 2, "java.idiom.optional": 1,
		"java.idiom.try-with-resources": 1, "java.idiom.pattern-matching": 1, "java.idiom.switch-expression": 1,
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
	for _, r := range rules.ForLanguage("java") {
		if r.Category == rules.Idiom && want[r.ID] == 0 {
			t.Errorf("idiom %s is not exercised by this test", r.ID)
		}
	}
}
