package cpp

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/rburns629/urfluency/analysis"
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
		path string
		src  string
		want []string
	}{
		{
			name: "raw new and delete, but not new handed to a smart pointer or placement new",
			path: "src/a.cpp",
			src: `void f() {
  int* p = new int(3);
  delete p;
  delete[] q;
  std::unique_ptr<int> a(new int);
  std::shared_ptr<int> b{new int};
  auto c = std::unique_ptr<int>(new int);
  auto d = std::unique_ptr<int>{new int};
  a.reset(new int);
  auto e = new (buf) T();
  auto g = std::make_unique<int>(1);
}
`,
			want: []string{"cpp.raw-new-delete:2", "cpp.raw-new-delete:3", "cpp.raw-new-delete:4"},
		},
		{
			name: "new in a Qt file is parent-owned, but delete is still reported",
			path: "src/window.cpp",
			src: `#include <QWidget>
void f(QWidget* parent) {
  auto* w = new QLabel(parent);
  delete w;
}
`,
			want: []string{"cpp.raw-new-delete:4"},
		},
		{
			name: "C-style casts, but not void casts, named casts, or parenthesized operands",
			path: "src/a.cpp",
			src: `void f() {
  int x = (int)3.5;
  auto p = (Foo*)ptr;
  g((T)x);
  (void)x;
  int k = static_cast<int>(1.0);
  int y = (a) - b;
}
`,
			want: []string{"cpp.c-style-cast:2", "cpp.c-style-cast:3", "cpp.c-style-cast:4"},
		},
		{
			name: "using namespace at header scope",
			path: "include/a.hpp",
			src: `using namespace std;
namespace n { using namespace boost; }
using std::string;
inline void f() { using namespace std::chrono; }
`,
			want: []string{"cpp.using-namespace-in-header:1", "cpp.using-namespace-in-header:2"},
		},
		{
			name: "using namespace in a source file is not reported",
			path: "src/a.cpp",
			src:  "using namespace std;\n",
			want: []string{},
		},
		{
			name: "unbounded C string functions, but not bounded ones or member calls",
			path: "src/a.cpp",
			src: `void f() {
  strcpy(a, b);
  std::strcat(a, b);
  ::sprintf(a, "%d", 1);
  gets(buf);
  snprintf(a, sizeof a, "%d", 1);
  strncpy(a, b, 3);
  obj.strcpy(a, b);
}
`,
			want: []string{"cpp.unsafe-c-string:2", "cpp.unsafe-c-string:3", "cpp.unsafe-c-string:4", "cpp.unsafe-c-string:5"},
		},
		{
			name: "system and popen with built commands, but not literals",
			path: "src/a.cpp",
			src: `void f() {
  system(cmd.c_str());
  std::system(cmd);
  popen(cmd, "r");
  system("ls");
  system("a" "b");
  system(R"(ls -l)");
  runner.system(cmd);
}
`,
			want: []string{"cpp.system-call:2", "cpp.system-call:3", "cpp.system-call:4"},
		},
		{
			name: "catch by value and empty catch",
			path: "src/a.cpp",
			src: `void f() {
  try { g(); } catch (std::exception e) { log(e); }
  try { g(); } catch (MyError) { log(); }
  try { g(); } catch (const std::exception& e) {}
  try { g(); } catch (int code) { log(code); }
  try { g(); } catch (const char* msg) { log(msg); }
  try { g(); } catch (...) { /* best effort */ }
  try { g(); } catch (...) { throw; }
}
`,
			want: []string{"cpp.catch-by-value:2", "cpp.catch-by-value:3", "cpp.empty-catch:4"},
		},
		{
			name: "missing override against a base defined in the same file",
			path: "src/shapes.hpp",
			src: `class Shape {
public:
  virtual ~Shape() = default;
  virtual double area() const = 0;
  virtual void scale(double f);
  void name();
};
class Square : public Shape {
public:
  ~Square();
  double area() const;
  void scale(double f) override;
  void name();
  void scale(double f, double g);
};
class Big final : public ns::Square {
  virtual double area() const;
};
class External : public Unknown {
  void run();
};
`,
			want: []string{"cpp.missing-override:11", "cpp.missing-override:17"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, tt.path, tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"cpp.raw-new-delete", "cpp.c-style-cast", "cpp.using-namespace-in-header", "cpp.unsafe-c-string",
		"cpp.catch-by-value", "cpp.missing-override", "cpp.empty-catch", "cpp.system-call", "clean.cognitive-complexity"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok || !containsString(r.Languages, "cpp") || r.Category == rules.Idiom {
			t.Errorf("%s is not a cpp rule in the catalog: %+v", id, r)
		}
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestComplexity(t *testing.T) {
	// Each nested if adds 1 plus its nesting: 1+2+3+4+5+6 = 21, over the threshold.
	deep := `int deep(int a) {
  if (a) { if (a) { if (a) { if (a) { if (a) { if (a) { return 1; } } } } } }
  return 0;
}
`
	simple := `int simple(int a) {
  if (a > 0 && a < 10) { return 1; }
  for (auto x : xs) { if (x) { continue; } }
  return a ? 1 : 0;
}
`
	if got := findings(t, "src/a.cpp", deep+simple); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:1"}) {
		t.Errorf("findings = %v, want one complexity finding on line 1", got)
	}
	if got := findings(t, "src/a_unittest.cc", deep); len(got) != 0 {
		t.Errorf("test file findings = %v, want none", got)
	}
}

func TestSkip(t *testing.T) {
	a := Analyzer{}
	tests := []struct {
		path, content string
		want          bool
	}{
		{"src/widget.cpp", "#include <vector>\n", false},
		{"build/src/widget.cpp", "", true},
		{"cmake-build-debug/main.cpp", "", true},
		{"out/gen.cc", "", true},
		{"bazel-bin/x.cc", "", true},
		{"src/moc_widget.cpp", "", true},
		{"src/qrc_resources.cpp", "", true},
		{"proto/user.pb.cc", "", true},
		{"proto/user.grpc.pb.cc", "", true},
		{"src/table.cpp", "// Generated by the protocol buffer compiler.  DO NOT EDIT!\n", true},
		{"src/lexer.cpp", "/* This file was automatically generated by flex */\n", true},
		{"src/builder.cpp", "// builds the output\n", false},
		{"src/outline.cpp", "", false},
	}
	for _, tt := range tests {
		if got := a.Skip(analysis.File{Path: tt.path, Content: []byte(tt.content)}); got != tt.want {
			t.Errorf("Skip(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
	if !a.SkipPath("build/a.cpp") || a.SkipPath("src/build.cpp") {
		t.Error("SkipPath should skip build directories but not files named build")
	}
}

func TestExtensions(t *testing.T) {
	for _, ext := range []string{".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx"} {
		if a := analysis.ForPath("src/x" + ext); a == nil || a.Language() != "cpp" {
			t.Errorf("ForPath(%s) is not the cpp analyzer", ext)
		}
	}
	if containsString(Analyzer{}.Extensions(), ".h") {
		t.Error("cpp must not claim .h, which belongs to the C analyzer")
	}
}
