package parsing

import (
	"reflect"
	"testing"
)

// Cases follow SonarSource's specification, including its worked examples
// sumOfPrimes (7) and getWords (1).
func TestCognitive(t *testing.T) {
	tests := []struct {
		name string
		g    Grammar
		syn  ComplexitySyntax
		src  string
		want []int
	}{
		{"sumOfPrimes", JavaScript, JavaScriptComplexity, `function sumOfPrimes(max) {
  let total = 0;
  OUT: for (let i = 1; i <= max; ++i) {
    for (let j = 2; j < i; ++j) {
      if (i % j === 0) { continue OUT; }
    }
    total += i;
  }
  return total;
}`, []int{7}},
		{"getWords", JavaScript, JavaScriptComplexity, `function getWords(n) {
  switch (n) { case 1: return "one"; case 2: return "a couple"; default: return "lots"; }
}`, []int{1}},
		{"else if chain counts once each", JavaScript, JavaScriptComplexity,
			`function f(a, b) { if (a) { x(); } else if (b) { y(); } else { z(); } }`, []int{3}},
		{"logical sequences", TypeScript, JavaScriptComplexity,
			`function f(a: boolean, b: boolean, c: boolean) { if (a && b && c) {} if (a && b || c) {} }`, []int{2 + 3}},
		{"nested arrow deepens nesting", TSX, JavaScriptComplexity,
			`const List = ({ items }) => { items.forEach((x) => { if (x) { log(x); } }); return null; }`, []int{2}},
		{"ternary nested in loop", JavaScript, JavaScriptComplexity,
			`function f(xs) { for (const x of xs) { const y = x ? 1 : 2; } }`, []int{1 + 2}},
		{"python", Python, PythonComplexity, `def f(xs, y, z):
    for x in xs:
        if x and y or z:
            pass
        elif x:
            pass
        else:
            pass
    return [v for v in xs if v]
`, []int{1 + 2 + 1 + 1 + 1 + 1}},
		// Cases below are adapted from codemetrics' tests (MIT), as a cross-check.
		{"python nested", Python, PythonComplexity, "def branchy(x):\n    if x > 0:\n        for i in range(x):\n            if i % 2 == 0:\n                return i\n    return 0\n", []int{6}},
		{"python elif chain", Python, PythonComplexity, "def f(n):\n    if n == 1:\n        return 1\n    elif n == 2:\n        return 2\n    elif n == 3:\n        return 3\n    elif n == 4:\n        return 4\n    return 0\n", []int{4}},
		{"python bool", Python, PythonComplexity, "def f(a, b, c):\n    if a and b and c:\n        return 1\n    return 0\n", []int{2}},
		{"js nested", JavaScript, JavaScriptComplexity, "function branchy(x) {\n  if (x > 0) {\n    for (let i = 0; i < x; i++) {\n      if (i % 2 === 0) { return i; }\n    }\n  }\n  return 0;\n}\n", []int{6}},
		{"js for-of", JavaScript, JavaScriptComplexity, "function f(xs) {\n  for (const x of xs) {\n    if (x > 0) { return x; }\n  }\n  return 0;\n}\n", []int{3}},
		{"ts switch", TypeScript, JavaScriptComplexity, "function f(n: number): string {\n  switch (n) {\n    case 1: return \"one\";\n    default: return \"lots\";\n  }\n}\n", []int{1}},
		{"recursion", JavaScript, JavaScriptComplexity, "function fib(n) { if (n < 2) { return n; } return fib(n - 1) + fib(n - 2); }", []int{3}},
		{"python method recursion", Python, PythonComplexity, "class T:\n    def fib(self, n):\n        if n < 2:\n            return n\n        return self.fib(n - 1) + self.fib(n - 2)\n", []int{3}},
		{"python lambda and try", Python, PythonComplexity, `def g(items):
    try:
        return sorted(items, key=lambda i: i.a if i else 0)
    except ValueError:
        return []
`, []int{1 + 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := Parse(tt.g, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			defer tree.Close()
			var got []int
			for _, fn := range Cognitive(tree, tt.syn) {
				got = append(got, fn.Complexity)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("complexity = %v, want %v", got, tt.want)
			}
		})
	}
}
