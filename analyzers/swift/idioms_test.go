package swift

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `import SwiftUI

protocol Shape {
    func area() -> Double
}

extension Shape {
    func describe() -> String { "\(area())" }
}

extension Collection where Element: Equatable {}

extension User {}

struct User: Codable, Equatable {
    let name: String
}

enum Event {
    case tap(x: Int, y: Int)
    case close
}

enum Plain: String {
    case a
}

actor Counter {
    var n = 0
}

@MainActor
final class Model: ObservableObject {
    @Published var count = 0

    func load(id: Int?) async throws -> User {
        guard let id else { throw Failure() }
        let file = open(id)
        defer { file.close() }
        return try await fetch(id)
    }
}

@propertyWrapper
struct Clamped<Value: Comparable> {
    var wrappedValue: Value
}

func largest<T>(_ xs: [T]) -> T? where T: Comparable { xs.max() }

func identity<T>(_ x: T) -> T { x }

struct CounterView: View {
    @State private var taps = 0
    var body: some View { Text("\(taps)") }
}
`
	got := map[string]int{}
	fs, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "Sources/App/App.swift", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"swift.idiom.guard-early-exit":     1,
		"swift.idiom.value-type":           3, // User, Clamped, CounterView
		"swift.idiom.protocol-extension":   2, // Shape (declared here), Collection (standard)
		"swift.idiom.associated-values":    1,
		"swift.idiom.async-await":          1,
		"swift.idiom.codable":              1,
		"swift.idiom.constrained-generics": 2, // Clamped<Value: Comparable>, largest where
		"swift.idiom.defer":                1,
		"swift.idiom.actor-isolation":      2, // actor Counter, @MainActor
		"swift.idiom.property-wrapper":     3, // @Published, @propertyWrapper, @State
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
	for rule := range want {
		if _, ok := got[rule]; !ok {
			t.Errorf("%s was never found", rule)
		}
	}
}

// TestCatalog checks that every rule the analyzer reports is in the catalog.
func TestCatalog(t *testing.T) {
	ids := []string{"swift.force-unwrap", "swift.force-try", "swift.force-cast", "swift.implicitly-unwrapped-optional",
		"swift.empty-catch", "swift.http-url-literal", "swift.count-instead-of-is-empty", "clean.cognitive-complexity"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok {
			t.Errorf("%s missing from the catalog", id)
			continue
		}
		found := false
		for _, l := range r.Languages {
			found = found || l == "swift"
		}
		if !found {
			t.Errorf("%s does not list swift", id)
		}
	}
}
