// Package javascript analyzes JavaScript and TypeScript source with tree-sitter.
// It parses files and never runs them or their build tooling.
package javascript

import (
	"bytes"
	"context"
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/parsing"
)

func init() {
	analysis.Register(JavaScript{})
	analysis.Register(TypeScript{})
}

// JavaScript analyzes .js, .jsx, .mjs, and .cjs files.
type JavaScript struct{}

func (JavaScript) Language() string     { return "javascript" }
func (JavaScript) Extensions() []string { return []string{".js", ".jsx", ".mjs", ".cjs"} }
func (JavaScript) Skip(f analysis.File) bool {
	return skipBundled(f)
}
func (JavaScript) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	return analyze(ctx, files, false)
}

// TypeScript analyzes .ts, .tsx, .mts, and .cts files, excluding declaration files.
type TypeScript struct{}

func (TypeScript) Language() string     { return "typescript" }
func (TypeScript) Extensions() []string { return []string{".ts", ".tsx", ".mts", ".cts"} }
func (TypeScript) Skip(f analysis.File) bool {
	return strings.HasSuffix(f.Path, ".d.ts") || skipBundled(f)
}
func (TypeScript) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	return analyze(ctx, files, true)
}

// skipBundled excludes build output and minified files, which are not hand-written.
func skipBundled(f analysis.File) bool {
	if parsing.InDir(f.Path, "dist", "build", "out", ".next", "coverage") || strings.Contains(path.Base(f.Path), ".min.") {
		return true
	}
	for _, line := range bytes.Split(f.Content, []byte("\n")) {
		if len(line) > 1000 {
			return true
		}
	}
	return false
}

func grammarFor(p string, typescript bool) parsing.Grammar {
	switch {
	case strings.HasSuffix(p, ".tsx"):
		return parsing.TSX
	case typescript:
		return parsing.TypeScript
	default:
		return parsing.JavaScript
	}
}

func analyze(ctx context.Context, files []analysis.File, typescript bool) ([]analysis.Finding, error) {
	var findings []analysis.Finding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tree, err := parsing.Parse(grammarFor(f.Path, typescript), f.Content)
		if err != nil {
			continue
		}
		c := checker{tree: tree, path: f.Path, typescript: typescript, frameworks: frameworksOf(tree, f.Path)}
		parsing.Walk(tree.Root(), c.visit)
		if !analysis.IsTestPath(f.Path) {
			for _, fn := range parsing.Cognitive(tree, parsing.JavaScriptComplexity) {
				if fn.Complexity > parsing.CognitiveThreshold {
					findings = append(findings, analysis.Finding{Rule: "clean.cognitive-complexity", Path: f.Path, Line: fn.Line})
				}
			}
		}
		tree.Close()
		findings = append(findings, c.findings...)
	}
	return findings, nil
}

type checker struct {
	tree       *parsing.Tree
	path       string
	typescript bool
	frameworks map[string]bool
	findings   []analysis.Finding
}

func (c *checker) report(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n)})
}

func (c *checker) visit(n *ts.Node) bool {
	switch n.Kind() {
	case "binary_expression":
		if parsing.HasToken(n, "==", "!=") && !isNull(n.ChildByFieldName("left")) && !isNull(n.ChildByFieldName("right")) {
			c.report("js.loose-equality", n)
		}
	case "variable_declaration":
		c.report("js.var-declaration", n)
	case "catch_clause":
		if body := n.ChildByFieldName("body"); body != nil && body.NamedChildCount() == 0 {
			c.report("js.empty-catch", n)
		}
	case "call_expression":
		c.checkAsyncForEach(n)
		c.checkCallSecurity(n)
	case "comment":
		text := c.tree.Text(n)
		if strings.Contains(text, "@ts-ignore") || strings.Contains(text, "@ts-nocheck") {
			c.report("ts.ts-ignore", n)
		}
	}
	c.checkNodeSecurity(n)
	if c.typescript {
		switch n.Kind() {
		case "predefined_type":
			if c.tree.Text(n) == "any" {
				c.report("ts.explicit-any", n)
			}
		case "non_null_expression":
			c.report("ts.non-null-assertion", n)
		case "as_expression":
			if inner := n.NamedChild(0); inner != nil && inner.Kind() == "as_expression" {
				if target := inner.NamedChild(1); target != nil && (c.tree.Text(target) == "unknown" || c.tree.Text(target) == "any") {
					c.report("ts.double-assertion", n)
				}
			}
		}
	}
	return true
}

func isNull(n *ts.Node) bool {
	return n != nil && (n.Kind() == "null" || n.Kind() == "undefined")
}

// checkAsyncForEach flags xs.forEach(async ...) calls.
func (c *checker) checkAsyncForEach(call *ts.Node) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" {
		return
	}
	prop := fn.ChildByFieldName("property")
	args := call.ChildByFieldName("arguments")
	if prop == nil || args == nil || c.tree.Text(prop) != "forEach" || args.NamedChildCount() == 0 {
		return
	}
	callback := args.NamedChild(0)
	switch callback.Kind() {
	case "arrow_function", "function_expression", "function":
		if parsing.HasToken(callback, "async") {
			c.report("js.async-foreach", call)
		}
	}
}
