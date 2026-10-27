package cpp

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// idiomTypes are standard library type names that show a deliberate idiom.
var idiomTypes = map[string]string{
	"unique_ptr": "cpp.idiom.smart-pointers", "shared_ptr": "cpp.idiom.smart-pointers", "weak_ptr": "cpp.idiom.smart-pointers",
	"optional": "cpp.idiom.vocabulary-types", "variant": "cpp.idiom.vocabulary-types", "expected": "cpp.idiom.vocabulary-types",
	"lock_guard": "cpp.idiom.raii-lock", "scoped_lock": "cpp.idiom.raii-lock", "unique_lock": "cpp.idiom.raii-lock",
}

// visitIdioms finds the modern C++ idioms that show fluency: each is a
// deliberate choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "type_identifier":
		if rule, ok := idiomTypes[c.tree.Text(n)]; ok {
			c.idiom(rule, n)
		}
	case "call_expression":
		switch c.callee(n) {
		case "std::make_unique", "std::make_shared":
			c.idiom("cpp.idiom.smart-pointers", n)
		case "std::move", "std::forward":
			c.idiom("cpp.idiom.move-semantics", n)
		}
	case "parameter_declaration":
		if d := n.ChildByFieldName("declarator"); d != nil && d.Kind() == "reference_declarator" && parsing.HasToken(d, "&&") {
			c.idiom("cpp.idiom.move-semantics", n)
		}
	case "for_range_loop":
		c.idiom("cpp.idiom.range-for", n)
	case "type_qualifier":
		switch c.tree.Text(n) {
		case "constexpr", "consteval", "constinit":
			c.idiom("cpp.idiom.constexpr", n)
		}
	case "if_statement":
		if parsing.HasToken(n, "constexpr") {
			c.idiom("cpp.idiom.constexpr", n)
		}
	case "lambda_expression":
		c.idiom("cpp.idiom.lambda", n)
	case "virtual_specifier":
		c.idiom("cpp.idiom.override", n)
	case "template_declaration":
		c.idiom("cpp.idiom.templates", n)
	case "structured_binding_declarator":
		c.idiom("cpp.idiom.structured-bindings", n)
	}
	return true
}

// callee returns a call's function text without template arguments, such as
// std::make_unique for std::make_unique<Widget>(1).
func (c *checker) callee(call *ts.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	text := c.tree.Text(fn)
	if i := strings.IndexByte(text, '<'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}
