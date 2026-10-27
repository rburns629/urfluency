package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// streamSources start a stream pipeline; streamSteps are intermediate
// operations; streamEnds are terminal operations.
var (
	streamSources = set("stream", "parallelStream")
	streamSteps   = set("map", "filter", "flatMap", "mapToInt", "mapToLong", "mapToDouble", "mapToObj", "sorted",
		"distinct", "limit", "skip", "peek", "takeWhile", "dropWhile", "boxed")
	streamEnds = set("collect", "toList", "reduce", "anyMatch", "allMatch", "noneMatch", "findFirst", "findAny",
		"count", "sum", "min", "max", "toArray", "forEach", "average")
)

// visitIdioms finds the modern Java idioms that show fluency: each is a
// deliberate choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "try_with_resources_statement":
		c.idiom("java.idiom.try-with-resources", n)
	case "record_declaration":
		c.idiom("java.idiom.records", n)
	case "lambda_expression", "method_reference":
		c.idiom("java.idiom.lambdas", n)
	case "local_variable_declaration", "resource":
		if t := n.ChildByFieldName("type"); t != nil && t.Kind() == "type_identifier" && c.text(t) == "var" {
			c.idiom("java.idiom.var", n)
		}
	case "switch_expression":
		if body := n.ChildByFieldName("body"); body != nil && hasChildKind(body, "switch_rule") {
			c.idiom("java.idiom.switch-expression", n)
		}
	case "instanceof_expression":
		if n.ChildByFieldName("name") != nil || n.ChildByFieldName("pattern") != nil {
			c.idiom("java.idiom.pattern-matching", n)
		}
	case "type_parameter":
		if hasChildKind(n, "type_bound") {
			c.idiom("java.idiom.bounded-generics", n)
		}
	case "wildcard":
		if n.NamedChildCount() > 0 {
			c.idiom("java.idiom.bounded-generics", n)
		}
	case "modifiers":
		if parsing.HasToken(n, "sealed") {
			c.idiom("java.idiom.sealed-types", n)
		}
	case "method_invocation":
		name := c.text(n.ChildByFieldName("name"))
		switch {
		case c.text(n.ChildByFieldName("object")) == "Optional" && (name == "of" || name == "ofNullable" || name == "empty"):
			c.idiom("java.idiom.optional", n)
		case streamEnds[name] && c.streamPipeline(n):
			c.idiom("java.idiom.streams", n)
		}
	}
	return true
}

// streamPipeline reports whether a terminal call ends a chain that starts with
// stream() or parallelStream() and has at least one intermediate step.
func (c *checker) streamPipeline(end *ts.Node) bool {
	step := false
	for n := end.ChildByFieldName("object"); n != nil && n.Kind() == "method_invocation"; n = n.ChildByFieldName("object") {
		name := c.text(n.ChildByFieldName("name"))
		switch {
		case streamSteps[name]:
			step = true
		case streamSources[name]:
			return step
		}
	}
	return false
}
