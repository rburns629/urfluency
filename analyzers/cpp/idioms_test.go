package cpp

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `#include <memory>
#include <mutex>
#include <optional>

template <typename T>
concept Shape = requires(T t) { t.area(); };

class Base {
public:
  virtual ~Base() = default;
  virtual int size() const = 0;
};

class Impl final : public Base {
public:
  int size() const override { return n_; }
  Impl(Impl&& other) noexcept;
private:
  int n_ = 0;
};

constexpr int kLimit = 10;

std::optional<int> find(const std::map<int, int>& m, std::mutex& mu) {
  std::lock_guard<std::mutex> guard(mu);
  for (const auto& [k, v] : m) {
    if constexpr (kLimit > 0) {
      auto ok = [&](int x) { return x == v; };
      if (ok(k)) return k;
    }
  }
  return std::nullopt;
}

std::unique_ptr<Base> make() {
  auto p = std::make_unique<Impl>();
  return std::move(p);
}
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "src/app.cpp", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	// override counts the final class as well as the override method.
	want := map[string]int{
		"cpp.idiom.templates": 1, "cpp.idiom.override": 2, "cpp.idiom.move-semantics": 2, "cpp.idiom.constexpr": 2,
		"cpp.idiom.vocabulary-types": 1, "cpp.idiom.raii-lock": 1, "cpp.idiom.range-for": 1, "cpp.idiom.structured-bindings": 1,
		"cpp.idiom.lambda": 1, "cpp.idiom.smart-pointers": 2,
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
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
	}
	for _, r := range rules.ForLanguage("cpp") {
		if r.Category == rules.Idiom {
			if _, ok := want[r.ID]; !ok {
				t.Errorf("catalog idiom %s is not covered by this test", r.ID)
			}
		}
	}
}
