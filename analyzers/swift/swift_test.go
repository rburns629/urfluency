package swift

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
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
			name: "force unwrap but not optional chaining, != or literal URLs",
			path: "Sources/App/A.swift",
			src: `let a = b!
let c = d!.e
let f = g?.h
let i = j != k
let l = !m
let u = URL(string: "https://example.com")!
let v = URL(string: "https://\(host)")!
let w = obj?.prop!
`,
			want: []string{"swift.force-unwrap:1", "swift.force-unwrap:2", "swift.force-unwrap:7", "swift.force-unwrap:8"},
		},
		{
			name: "force try but not try or try?",
			path: "Sources/App/A.swift",
			src: `let a = try! load()
let b = try? load()
func f() throws { try load() }
`,
			want: []string{"swift.force-try:1"},
		},
		{
			name: "force cast but not as? or as",
			path: "Sources/App/A.swift",
			src: `let a = b as! Int
let c = d as? Int
let e = 1 as Double
`,
			want: []string{"swift.force-cast:1"},
		},
		{
			name: "implicitly unwrapped optionals but not outlets or optionals",
			path: "Sources/App/A.swift",
			src: `var a: String!
let b: Int! = nil
@IBOutlet weak var label: UILabel!
var c: String?
func f(x: Int!, y: Int?) {}
`,
			want: []string{"swift.implicitly-unwrapped-optional:1", "swift.implicitly-unwrapped-optional:2", "swift.implicitly-unwrapped-optional:5"},
		},
		{
			name: "crash operators are accepted in tests",
			path: "Tests/AppTests/LoginTests.swift",
			src: `var sut: Login!
let a = b!
let c = try! load()
let d = e as! Int
let u = URL(string: "http://api.example.com")
`,
			want: []string{},
		},
		{
			name: "empty catch, including comment-only, but not handled",
			path: "Sources/App/A.swift",
			src: `do { try x() } catch { }
do {
  try x()
} catch {
  // ignore
}
do { try x() } catch let e as Failure { log(e) }
do { try x() } catch { throw Wrapped(error) }
`,
			want: []string{"swift.empty-catch:1", "swift.empty-catch:4"},
		},
		{
			name: "http URL literals but not https, local hosts, or interpolated hosts",
			path: "Sources/App/A.swift",
			src: `let a = URL(string: "http://api.example.com/v1")
let b = URLComponents(string: "http://example.com:8080/x")
let c = URL(string: "https://api.example.com")
let d = URL(string: "http://localhost:8080/")
let e = URL(string: "http://127.0.0.1/x")
let f = URL(string: "http://printer.local/status")
let g = URL(string: "http://\(host)/x")
let h = URL(string: path)
let i = URL(fileURLWithPath: "http://x")
let j = "http://example.com"
`,
			want: []string{"swift.http-url-literal:1", "swift.http-url-literal:2"},
		},
		{
			name: "count compared with zero",
			path: "Sources/App/A.swift",
			src: `let a = items.count == 0
let b = items.count != 0
let c = 0 == name.count
let d = items.count > 0
let e = items.count > 1
let f = items.isEmpty
let g = count == 0
let h = items.count == limit
`,
			want: []string{"swift.count-instead-of-is-empty:1", "swift.count-instead-of-is-empty:2",
				"swift.count-instead-of-is-empty:3", "swift.count-instead-of-is-empty:4"},
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

func TestCognitiveComplexity(t *testing.T) {
	// Nested branches: if(1) for(2) if(3) while(4) if(5) is 1+2+3+4+5 = 15, and
	// the else-if and else add 2 more, over the threshold of 15.
	complex := `func f(a: Bool, xs: [Int]) {
    if a {
        for x in xs {
            if x > 0 {
                while a {
                    if x > 1 {
                        print(x)
                    }
                }
            }
        }
    } else if xs.isEmpty {
        print(1)
    } else {
        print(2)
    }
}
`
	simple := `func g(a: Bool) -> Int {
    guard a else { return 0 }
    if a { return 1 } else if !a { return 2 } else { return 3 }
}
`
	if got := findings(t, "Sources/App/A.swift", complex); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:1"}) {
		t.Errorf("complex function: got %v", got)
	}
	if got := findings(t, "Sources/App/B.swift", simple); len(got) != 0 {
		t.Errorf("simple function: got %v", got)
	}
	if got := findings(t, "Tests/AppTests/ATests.swift", complex); len(got) != 0 {
		t.Errorf("test file: got %v", got)
	}
}

func TestSkip(t *testing.T) {
	a := Analyzer{}
	paths := map[string]bool{
		".build/checkouts/swift-nio/Sources/NIO/A.swift": true,
		"DerivedData/App/Build/A.swift":                  true,
		"Pods/Alamofire/Source/Session.swift":            true,
		"Carthage/Checkouts/Lib/A.swift":                 true,
		"App/R.generated.swift":                          true,
		"Sources/App/Login.swift":                        false,
		"App/Build.swift":                                false,
	}
	for p, want := range paths {
		if got := a.SkipPath(p); got != want {
			t.Errorf("SkipPath(%q) = %v, want %v", p, got, want)
		}
	}
	contents := map[string]bool{
		"// swiftlint:all\n// Generated using SwiftGen — https://github.com/SwiftGen/SwiftGen\n": true,
		"// DO NOT EDIT.\n// swift-format-ignore-file\n":                                         true,
		"// @generated\n// This file was automatically generated and should not be edited.\n":    true,
		"import Foundation\n\nstruct User { let name: String }\n":                                false,
	}
	for src, want := range contents {
		if got := a.Skip(analysis.File{Path: "Sources/App/A.swift", Content: []byte(src)}); got != want {
			t.Errorf("Skip(%q) = %v, want %v", strings.SplitN(src, "\n", 2)[0], got, want)
		}
	}
}
