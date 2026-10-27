package dart

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// visitIdioms finds the Dart idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "conditional_assignable_selector", "if_null_expression":
		c.idiom("dart.idiom.null-aware", n)
	case "assignment_expression":
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "??=" {
			c.idiom("dart.idiom.null-aware", n)
		}
	case "function_body", "function_expression_body":
		switch {
		case parsing.HasToken(n, "async"):
			c.idiom("dart.idiom.async-await", n)
		case parsing.HasToken(n, "async*", "sync*"):
			c.idiom("dart.idiom.generator", n)
		}
	case "constant_constructor_signature":
		c.idiom("dart.idiom.const-constructor", n)
	case "optional_formal_parameters":
		if parsing.HasToken(n, "required") {
			c.idiom("dart.idiom.required-named-parameters", n)
		}
	case "extension_declaration":
		c.idiom("dart.idiom.extension-methods", n)
	case "class_definition":
		if hasChild(n, "sealed") {
			c.idiom("dart.idiom.sealed-class", n)
		}
	case "switch_expression", "pattern_variable_declaration":
		c.idiom("dart.idiom.pattern-matching", n)
	case "if_statement":
		if parsing.HasToken(n, "case") {
			c.idiom("dart.idiom.pattern-matching", n)
		}
	case "record_literal", "record_type":
		c.idiom("dart.idiom.records", n)
	case "if_element", "for_element":
		c.idiom("dart.idiom.collection-control-flow", n)
	}
	return true
}
