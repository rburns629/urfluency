package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCognitiveGo(t *testing.T) {
	tests := []struct {
		name, src string
		want      int
	}{
		{"sumOfPrimes", `func sumOfPrimes(max int) int {
	total := 0
OUT:
	for i := 1; i <= max; i++ {
		for j := 2; j < i; j++ {
			if i%j == 0 {
				continue OUT
			}
		}
		total += i
	}
	return total
}`, 7},
		{"getWords", `func getWords(n int) string {
	switch n {
	case 1:
		return "one"
	default:
		return "lots"
	}
}`, 1},
		{"else if chain and logical sequences", `func f(a, b, c bool) {
	if a && b || c {
	} else if b {
	} else {
	}
}`, 1 + 2 + 1 + 1},
		// Cases below are adapted from codemetrics' tests (MIT), as a cross-check.
		{"flat ifs", "func f(a, b bool) int {\n\tif a {\n\t\treturn 1\n\t}\n\tif b {\n\t\treturn 2\n\t}\n\treturn 0\n}", 2},
		{"nested if", "func f(a, b bool) int {\n\tif a {\n\t\tif b {\n\t\t\treturn 1\n\t\t}\n\t}\n\treturn 0\n}", 3},
		{"and run", "func f(a, b, c, d bool) bool { return a && b && c && d }", 1},
		{"paren bool", "func f(a, b, c bool) bool { return a && (b || c) }", 2},
		{"recursion", "func fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-1) + fib(n-2)\n}", 3},
		{"method recursion", "type T struct{ n int }\nfunc (t *T) fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn t.fib(n-1) + t.fib(n-2)\n}", 3},
		{"func lit nesting", "func f(items []int) {\n\tfor range items {\n\t\tg := func(x int) {\n\t\t\tif x > 0 {\n\t\t\t\tprintln(x)\n\t\t\t}\n\t\t}\n\t\tg(1)\n\t}\n}", 4},
		{"closure deepens nesting", `func f(xs []int) {
	go func() {
		for range xs {
			if true {
			}
		}
	}()
}`, 2 + 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "a.go", "package a\n"+tt.src, 0)
			if err != nil {
				t.Fatal(err)
			}
			var fn *ast.FuncDecl
			for _, d := range file.Decls {
				if f, ok := d.(*ast.FuncDecl); ok {
					fn = f
				}
			}
			cc := newCognitive(fn)
			cc.block(fn.Body.List, 0)
			if cc.total != tt.want {
				t.Errorf("complexity = %d, want %d", cc.total, tt.want)
			}
		})
	}
}
