// Package rust analyzes Rust source with tree-sitter. It parses files and never
// compiles or runs them.
package rust

import (
	"bytes"
	"context"
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func init() { analysis.Register(Analyzer{}) }

// Analyzer implements analysis.Analyzer for Rust.
type Analyzer struct{}

func (Analyzer) Language() string     { return "rust" }
func (Analyzer) Extensions() []string { return []string{".rs"} }

// SkipPath excludes Cargo build output, which holds generated code.
func (Analyzer) SkipPath(p string) bool { return parsing.InDir(p, "target") }

// Skip excludes build output and files marked as generated near the top.
func (a Analyzer) Skip(f analysis.File) bool {
	if a.SkipPath(f.Path) {
		return true
	}
	head := f.Content[:min(len(f.Content), 512)]
	return bytes.Contains(head, []byte("@generated")) || bytes.Contains(head, []byte("Code generated"))
}

// complexity names the Rust grammar's nodes for cognitive complexity.
var complexity = parsing.ComplexitySyntax{
	Functions:  set("function_item", "closure_expression"),
	Structures: set("if_expression", "for_expression", "while_expression", "loop_expression", "match_expression"),
	Hybrids:    set("else_clause"),
	ElseClause: "else_clause",
	Logical:    "binary_expression",
	LogicalOps: set("&&", "||"),
	Jumps:      set("break_expression", "continue_expression"),
	Label:      "label",
	Call:       "call_expression",
	Self:       []string{"self"},
}

// Analyze applies every Rust rule to each file.
func (Analyzer) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	var findings []analysis.Finding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tree, err := parsing.Parse(parsing.Rust, f.Content)
		if err != nil {
			continue
		}
		c := checker{tree: tree, path: f.Path, isTest: analysis.IsTestPath(f.Path) || testCrateFile(tree)}
		c.binary = c.isTest || binaryPath(f.Path)
		parsing.Walk(tree.Root(), c.visit)
		parsing.Walk(tree.Root(), c.visitIdioms)
		c.checkComplexity()
		tree.Close()
		findings = append(findings, c.findings...)
	}
	return findings, nil
}

// binaryPath reports whether a file belongs to a binary, example, benchmark, or
// build script, where panicking on failure is acceptable.
func binaryPath(p string) bool {
	base := path.Base(p)
	return base == "main.rs" || base == "build.rs" || parsing.InDir(p, "bin", "examples", "benches")
}

// testCrateFile reports whether the file starts with #![cfg(test)].
func testCrateFile(t *parsing.Tree) bool {
	for _, n := range parsing.NamedChildren(t.Root()) {
		if n.Kind() == "inner_attribute_item" && isTestAttribute(attributeText(t, n)) {
			return true
		}
	}
	return false
}

type checker struct {
	tree     *parsing.Tree
	path     string
	isTest   bool // the whole file is test code
	binary   bool // panicking is acceptable: tests, binaries, examples, build scripts
	findings []analysis.Finding
}

func (c *checker) report(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n)})
}

func (c *checker) visit(n *ts.Node) bool {
	switch n.Kind() {
	case "call_expression":
		c.checkUnwrap(n)
		c.checkCommandShell(n)
	case "macro_invocation":
		c.checkPanic(n)
	case "unsafe_block":
		if !c.inTestCode(n) && !c.hasSafetyComment(n) {
			c.report("rust.unsafe-without-safety-comment", n)
		}
	case "type_cast_expression":
		if !c.inTestCode(n) {
			c.checkCast(n)
		}
	case "function_item":
		if !c.inTestCode(n) {
			c.checkPtrArgs(n)
		}
	case "for_expression":
		c.checkIndexLoop(n)
	}
	return true
}

// checkComplexity reports functions over the threshold outside test code.
func (c *checker) checkComplexity() {
	if c.isTest {
		return
	}
	var tests [][2]int
	parsing.Walk(c.tree.Root(), func(n *ts.Node) bool {
		if (n.Kind() == "mod_item" || n.Kind() == "function_item") && c.hasTestAttribute(n) {
			tests = append(tests, [2]int{parsing.Line(n), int(n.EndPosition().Row) + 1})
			return false
		}
		return true
	})
	for _, fn := range parsing.Cognitive(c.tree, complexity) {
		if fn.Complexity > parsing.CognitiveThreshold && !within(fn.Line, tests) {
			c.findings = append(c.findings, analysis.Finding{Rule: "clean.cognitive-complexity", Path: c.path, Line: fn.Line})
		}
	}
}

func within(line int, ranges [][2]int) bool {
	for _, r := range ranges {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

// inTestCode reports whether a node is in a test file, a #[cfg(test)] module, or
// a #[test] function.
func (c *checker) inTestCode(n *ts.Node) bool {
	if c.isTest {
		return true
	}
	for p := n; p != nil; p = p.Parent() {
		if (p.Kind() == "mod_item" || p.Kind() == "function_item") && c.hasTestAttribute(p) {
			return true
		}
	}
	return false
}

// mayPanic reports whether panicking at n is acceptable: test code, binaries,
// fn main, or a function whose docs have a # Panics section.
func (c *checker) mayPanic(n *ts.Node, documented bool) bool {
	if c.binary || c.inTestCode(n) {
		return true
	}
	fn := enclosingFunction(n)
	if fn == nil {
		return false
	}
	if name := fn.ChildByFieldName("name"); name != nil && c.tree.Text(name) == "main" {
		return true
	}
	return documented && c.documentsPanics(fn)
}

func enclosingFunction(n *ts.Node) *ts.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_item" {
			return p
		}
	}
	return nil
}

// preceding returns the attributes and comments directly above an item.
func preceding(n *ts.Node) []*ts.Node {
	var out []*ts.Node
	for p := n.PrevNamedSibling(); p != nil; p = p.PrevNamedSibling() {
		switch p.Kind() {
		case "attribute_item", "line_comment", "block_comment":
			out = append(out, p)
		default:
			return out
		}
	}
	return out
}

func (c *checker) hasTestAttribute(item *ts.Node) bool {
	for _, p := range preceding(item) {
		if p.Kind() == "attribute_item" && isTestAttribute(attributeText(c.tree, p)) {
			return true
		}
	}
	return false
}

func (c *checker) documentsPanics(fn *ts.Node) bool {
	for _, p := range preceding(fn) {
		if p.Kind() != "attribute_item" && strings.Contains(c.tree.Text(p), "# Panics") {
			return true
		}
	}
	return false
}

// attributeText returns the attribute inside #[...] or #![...], such as cfg(test).
func attributeText(t *parsing.Tree, item *ts.Node) string {
	for _, child := range parsing.NamedChildren(item) {
		if child.Kind() == "attribute" {
			return t.Text(child)
		}
	}
	return ""
}

// isTestAttribute matches #[test], #[tokio::test], and #[cfg(test)] or
// #[cfg(all(test, ...))], but not #[cfg(not(test))].
func isTestAttribute(attr string) bool {
	if attr == "test" || strings.HasSuffix(attr, "::test") {
		return true
	}
	if !strings.HasPrefix(attr, "cfg(") || strings.Contains(attr, "not(") {
		return false
	}
	for _, word := range strings.FieldsFunc(attr, func(r rune) bool { return !isIdentRune(r) }) {
		if word == "test" {
			return true
		}
	}
	return false
}

func isIdentRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// method splits a method call such as v.iter() or v.collect::<T>() into its
// name, receiver, and arguments. It returns "" for other calls.
func method(t *parsing.Tree, call *ts.Node) (name string, receiver, args *ts.Node) {
	fn := call.ChildByFieldName("function")
	if fn != nil && fn.Kind() == "generic_function" {
		fn = fn.ChildByFieldName("function")
	}
	if fn == nil || fn.Kind() != "field_expression" {
		return "", nil, nil
	}
	field := fn.ChildByFieldName("field")
	if field == nil {
		return "", nil, nil
	}
	return t.Text(field), fn.ChildByFieldName("value"), call.ChildByFieldName("arguments")
}

func set(kinds ...string) map[string]bool {
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}
