// Package fuzz feeds arbitrary bytes to every analyzer, as an attacker's
// repository could. The tree-sitter grammars are C, so CI runs this with
// AddressSanitizer (-asan), which turns a silent out-of-bounds read or write
// in a parser into a failure; plain runs catch crashes, hangs, and panics in
// the Go analyzers. The seeds also run as ordinary tests.
package fuzz

import (
	"context"
	"testing"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"

	_ "github.com/rburns629/urfluency/analyzers/c"
	_ "github.com/rburns629/urfluency/analyzers/cpp"
	_ "github.com/rburns629/urfluency/analyzers/csharp"
	_ "github.com/rburns629/urfluency/analyzers/dart"
	_ "github.com/rburns629/urfluency/analyzers/golang"
	_ "github.com/rburns629/urfluency/analyzers/java"
	_ "github.com/rburns629/urfluency/analyzers/javascript"
	_ "github.com/rburns629/urfluency/analyzers/kotlin"
	_ "github.com/rburns629/urfluency/analyzers/powershell"
	_ "github.com/rburns629/urfluency/analyzers/python"
	_ "github.com/rburns629/urfluency/analyzers/rust"
	_ "github.com/rburns629/urfluency/analyzers/secrets"
	_ "github.com/rburns629/urfluency/analyzers/shell"
	_ "github.com/rburns629/urfluency/analyzers/swift"
)

// seeds are small, valid-looking files in each language, with the
// constructs the hand-written grammar scanners handle: heredocs, raw and
// template strings, nested comments, and string interpolation.
var seeds = []string{
	"package a\nfunc f() { s := `raw\n${x}`; _ = s }\n",
	"def f(x):\n    '''doc'''\n    return f\"{x!r:>{w}}\" if x else None\n",
	"const f = async ({a, ...b}) => `t ${a ? `n ${b}` : ''}`; /* c */ <div key={i}>{x}</div>\n",
	"fn f<'a>(x: &'a str) -> String { r#\"raw \"q\" \"#.to_string() + x } /* /* nested */ */\n",
	"#include <x.h>\n#define M(a) do { a; } while (0)\nint f(void) { return R\"(raw)\"[0]; }\n",
	"class A { string S => $@\"v{x}\"; async Task M() { await Task.Delay(1); } }\n",
	"class A { String s = \"\"\"\n  text\n  \"\"\"; void m() { var x = switch (y) { case 1 -> 2; default -> 3; }; } }\n",
	"fun f(x: Int) = \"\"\"${x}\n\"\"\".trimIndent()\n",
	"let s = #\"\"\"\n raw \\#(x)\n\"\"\"#\nfunc f() async throws { try await g() }\n",
	"void main() { final s = '''${a}\n'''; print(\"$x ${y.z}\"); }\n",
	"#!/bin/bash\ncat <<-EOF\n\t$x ${y:-z} $(cmd \"$a\")\n\tEOF\ncase $1 in a|b) echo;; esac\n",
	"function F { [CmdletBinding()] param([string]$a) @\"\n$($a)\n\"@ | % { $_ } }\n",
}

// Some inputs make a grammar slow, which ParseBudget bounds by cancelling the
// parse; the fuzzer looks for crashes and for work nothing bounds. Fuzzing
// lowers the budget to a second, so a bounded slow input finishes well before
// hangAfter, and anything past it is a real hang. hangAfter itself fails the
// input before Go's fuzzer would kill the worker, about ten seconds in, and
// report only that it hung.
const (
	fuzzParseBudget = time.Second
	hangAfter       = 8 * time.Second
)

var extensions = []string{".go", ".py", ".js", ".ts", ".tsx", ".rs", ".c", ".cpp", ".cs", ".java", ".kt", ".swift", ".dart", ".sh", ".ps1"}

func FuzzAnalyzers(f *testing.F) {
	parsing.ParseBudget = fuzzParseBudget
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Every file is given to every language, since an attacker chooses
		// the extension and the content independently.
		for _, ext := range extensions {
			a := analysis.ForPath("f" + ext)
			if a == nil {
				t.Fatalf("no analyzer for %s", ext)
			}
			file := analysis.File{Path: "src/f" + ext, Content: data}
			if a.Skip(file) {
				continue
			}
			// Failing before Go's fuzzer kills the worker names the language,
			// so a hang can be traced.
			done := make(chan error, 1)
			go func() {
				_, err := a.Analyze(context.Background(), []analysis.File{file})
				if fd, ok := a.(analysis.FrameworkDetector); ok {
					fd.Frameworks(file)
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s: %v", ext, err)
				}
			case <-time.After(hangAfter):
				t.Fatalf("%s: analysis took over %s", ext, hangAfter)
			}
		}
	})
}
