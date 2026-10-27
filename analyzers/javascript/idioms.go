package javascript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// utilityTypes are TypeScript's built-in type transformers.
var utilityTypes = map[string]bool{
	"Partial": true, "Required": true, "Readonly": true, "Pick": true, "Omit": true, "Record": true,
	"ReturnType": true, "Parameters": true, "Awaited": true, "NonNullable": true, "Exclude": true, "Extract": true,
}

// visitIdioms finds the JavaScript and TypeScript idioms that show fluency: each
// is a deliberate choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "optional_chain":
		c.idiom("js.idiom.optional-chaining", n)
	case "binary_expression":
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "??" {
			c.idiom("js.idiom.nullish-coalescing", n)
		}
	case "await_expression":
		c.idiom("js.idiom.async-await", n)
	case "object_pattern", "array_pattern":
		c.idiom("js.idiom.destructuring", n)
	case "call_expression":
		if fn := n.ChildByFieldName("function"); fn != nil && fn.Kind() == "member_expression" {
			switch c.tree.Text(fn) {
			case "Promise.all", "Promise.allSettled", "Promise.race", "Promise.any":
				c.idiom("js.idiom.concurrent-promises", n)
			}
		}
	}
	if !c.typescript {
		return true
	}
	switch n.Kind() {
	case "type_parameters":
		c.idiom("ts.idiom.generics", n)
	case "union_type":
		if n.Parent() == nil || n.Parent().Kind() != "union_type" {
			c.idiom("ts.idiom.union-types", n)
		}
	case "type_predicate":
		c.idiom("ts.idiom.type-guard", n)
	case "interface_declaration", "type_alias_declaration":
		c.idiom("ts.idiom.declared-types", n)
	case "predefined_type":
		if c.tree.Text(n) == "unknown" {
			c.idiom("ts.idiom.unknown", n)
		}
	case "generic_type":
		if name := n.ChildByFieldName("name"); name != nil && utilityTypes[c.tree.Text(name)] {
			c.idiom("ts.idiom.utility-types", n)
		}
	}
	return true
}
