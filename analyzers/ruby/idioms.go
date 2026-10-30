package ruby

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

var (
	enumerableMethods = set("map", "flat_map", "collect", "select", "filter", "reject", "filter_map", "each_with_object",
		"inject", "reduce", "sum", "group_by", "partition", "sort_by", "min_by", "max_by", "find", "detect",
		"any?", "all?", "none?", "count", "each_slice", "each_cons", "chunk_while", "slice_when", "take_while", "drop_while")
	mixins = set("Comparable", "Enumerable")
)

// visitIdioms finds the Ruby idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "program":
		if c.frozenStringLiterals(n) {
			c.idiom("ruby.idiom.frozen-string-literal", n)
		}
	case "method", "singleton_method":
		if hasKeywordParameter(n) {
			c.idiom("ruby.idiom.keyword-arguments", n)
		}
		if parsing.HasToken(n, "=") {
			c.idiom("ruby.idiom.endless-method", n)
		}
		if yields(n) {
			c.idiom("ruby.idiom.yield-block", n)
		}
	case "case_match", "test_pattern", "match_pattern":
		c.idiom("ruby.idiom.pattern-matching", n)
	case "if_modifier", "unless_modifier":
		if c.isGuard(n) {
			c.idiom("ruby.idiom.guard-clause", n)
		}
	case "call":
		receiver, name := c.callee(n)
		if parsing.HasToken(n, "&.") {
			c.idiom("ruby.idiom.safe-navigation", n)
		}
		switch {
		case (name == "new" && c.receiverIs(receiver, "Struct")) || (name == "define" && c.receiverIs(receiver, "Data")):
			c.idiom("ruby.idiom.value-object", n)
		case enumerableMethods[name] && receiver != nil && hasBlock(n):
			c.idiom("ruby.idiom.enumerable-methods", n)
		case (name == "include" || name == "extend") && receiver == nil:
			for _, arg := range arguments(n) {
				if mixins[c.tree.Text(arg)] {
					c.idiom("ruby.idiom.comparable-enumerable", n)
				}
			}
		}
	}
	return true
}

// frozenStringLiterals reports whether the magic comments before the first
// statement include frozen_string_literal: true.
func (c *checker) frozenStringLiterals(program *ts.Node) bool {
	for _, child := range parsing.NamedChildren(program) {
		if child.Kind() != "comment" {
			return false
		}
		text := strings.ToLower(strings.TrimLeft(c.tree.Text(child), "# "))
		text = strings.NewReplacer(" ", "", "\t", "", "-", "_").Replace(text)
		if strings.Contains(text, "frozen_string_literal:true") {
			return true
		}
	}
	return false
}

func hasKeywordParameter(method *ts.Node) bool {
	params := method.ChildByFieldName("parameters")
	if params == nil {
		return false
	}
	for _, p := range parsing.NamedChildren(params) {
		if p.Kind() == "keyword_parameter" {
			return true
		}
	}
	return false
}

// yields reports whether a method calls the block it was given with yield,
// not counting methods defined inside it.
func yields(method *ts.Node) bool {
	found := false
	parsing.Walk(method, func(n *ts.Node) bool {
		switch {
		case n.Kind() == "yield":
			found = true
		case (n.Kind() == "method" || n.Kind() == "singleton_method") && n.Id() != method.Id():
			return false
		}
		return !found
	})
	return found
}

// isGuard reports whether a modifier if or unless leaves early: return, next,
// break, or raise.
func (c *checker) isGuard(n *ts.Node) bool {
	body := n.ChildByFieldName("body")
	if body == nil {
		return false
	}
	switch body.Kind() {
	case "return", "next", "break":
		return true
	case "identifier":
		return c.tree.Text(body) == "raise"
	case "call":
		receiver, name := c.callee(body)
		return receiver == nil && name == "raise"
	}
	return false
}
