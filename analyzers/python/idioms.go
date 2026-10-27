package python

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// visitIdioms finds the Python idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "function_definition":
		if n.ChildByFieldName("return_type") != nil {
			c.idiom("py.idiom.type-hints", n)
		}
		if parsing.HasToken(n, "async") {
			c.idiom("py.idiom.async", n)
		}
	case "decorator":
		text := c.tree.Text(n)
		switch {
		case strings.Contains(text, "dataclass"):
			c.idiom("py.idiom.structured-data", n)
		case strings.Contains(text, "pytest.mark.parametrize"), strings.Contains(text, "pytest.fixture"):
			c.idiom("py.idiom.pytest", n)
		}
	case "class_definition":
		if bases := n.ChildByFieldName("superclasses"); bases != nil {
			text := c.tree.Text(bases)
			switch {
			case strings.Contains(text, "BaseModel"), strings.Contains(text, "TypedDict"), strings.Contains(text, "NamedTuple"):
				c.idiom("py.idiom.structured-data", n)
			case strings.Contains(text, "Exception"), strings.Contains(text, "Error"):
				c.idiom("py.idiom.custom-exception", n)
			case strings.Contains(text, "Protocol"):
				c.idiom("py.idiom.protocol", n)
			}
		}
	case "with_statement":
		c.idiom("py.idiom.context-manager", n)
	case "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
		c.idiom("py.idiom.comprehension", n)
	case "yield":
		c.idiom("py.idiom.generator", n)
	case "string":
		if start := n.Child(0); start != nil && start.Kind() == "string_start" && strings.ContainsAny(c.tree.Text(start), "fF") {
			c.idiom("py.idiom.f-string", n)
		}
	case "import_from_statement":
		if m := n.ChildByFieldName("module_name"); m != nil && c.tree.Text(m) == "pathlib" {
			c.idiom("py.idiom.pathlib", n)
		}
	}
	return true
}
