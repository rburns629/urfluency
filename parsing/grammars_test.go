package parsing

import "testing"

// TestGrammars checks every grammar loads and parses its language without errors.
func TestGrammars(t *testing.T) {
	cases := map[string]struct {
		g   Grammar
		src string
	}{
		"rust":       {Rust, "fn main() { let x: i32 = 1; println!(\"{}\", x); }"},
		"c":          {C, "#include <stdio.h>\nint main(void) { printf(\"hi\"); return 0; }"},
		"cpp":        {CPP, "#include <vector>\nint main() { std::vector<int> v{1}; for (auto x : v) {} }"},
		"csharp":     {CSharp, "using System;\nclass A { async Task M() { await Task.Delay(1); } }"},
		"java":       {Java, "class A { void m() { var x = java.util.List.of(1); } }"},
		"kotlin":     {Kotlin, "data class P(val x: Int)\nfun main() { val p = P(1) }"},
		"php":        {PHP, "<h1><?php echo htmlspecialchars($t); ?></h1>\n<?php\nfunction f(int $x): int { return $x + 1; }"},
		"ruby":       {Ruby, "class P\n  def initialize(x) = @x = x\n  def to_s = \"#{@x}\"\nend"},
		"swift":      {Swift, "struct P { let x: Int }\nfunc main() { let p = P(x: 1); print(p) }"},
		"dart":       {Dart, "class P { final int x; P(this.x); }\nvoid main() { final p = P(1); print(p.x); }"},
		"bash":       {Bash, "#!/bin/bash\nset -euo pipefail\nf() { local x=\"$1\"; [[ -n $x ]] && echo \"$x\"; }\nf \"$@\""},
		"powershell": {PowerShell, "function Get-X {\n  [CmdletBinding()]\n  param([string]$Name)\n  Write-Output $Name\n}"},
	}
	for name, c := range cases {
		tree, err := Parse(c.g, []byte(c.src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tree.Root().HasError() {
			t.Errorf("%s: parse has errors", name)
		}
		tree.Close()
	}
}

// TestSwiftScannerMemory exercises the Swift scanner paths with memory-safety
// fixes backported from upstream (third_party/tree-sitter-swift/README.md): its
// state, used by raw strings, and a compiler directive at the end of the input.
// Run under -asan (the nightly fuzz job), a regression fails.
func TestSwiftScannerMemory(t *testing.T) {
	for _, src := range []string{"let s = #\"raw \\#(x) \"q\"\"#\n", "#if", "#else", "#endif", "#"} {
		tree, err := Parse(Swift, []byte(src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		tree.Close()
	}
}
