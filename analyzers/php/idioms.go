package php

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// visitIdioms finds the modern PHP features that show fluency: each is a
// deliberate choice that older PHP code and the language's defaults do not make.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "declare_statement":
		if c.declaresStrictTypes(n) {
			c.idiom("php.idiom.strict-types", n)
		}
	case "property_declaration":
		if n.ChildByFieldName("type") != nil {
			c.idiom("php.idiom.typed-property", n)
		}
		if hasChild(n, "readonly_modifier") {
			c.idiom("php.idiom.readonly", n)
		}
	case "class_declaration":
		if hasChild(n, "readonly_modifier") {
			c.idiom("php.idiom.readonly", n)
		}
	case "method_declaration":
		c.constructorIdioms(n)
	case "enum_declaration":
		c.idiom("php.idiom.enum", n)
	case "match_expression":
		c.idiom("php.idiom.match", n)
	case "nullsafe_member_access_expression", "nullsafe_member_call_expression":
		c.idiom("php.idiom.nullsafe-operator", n)
	case "arguments":
		c.argumentIdioms(n)
	case "member_call_expression":
		if _, name := c.callee(n); name == "prepare" && c.placeholders(arguments(n)) {
			c.idiom("php.idiom.prepared-statement", n)
		}
	}
	return true
}

// constructorIdioms records promoted constructor parameters, and readonly
// ones, once per constructor.
func (c *checker) constructorIdioms(fn *ts.Node) {
	name := fn.ChildByFieldName("name")
	params := fn.ChildByFieldName("parameters")
	if name == nil || params == nil || !strings.EqualFold(c.tree.Text(name), "__construct") {
		return
	}
	promoted, readonly := false, false
	for _, p := range parsing.NamedChildren(params) {
		if p.Kind() == "property_promotion_parameter" {
			promoted = true
			readonly = readonly || p.ChildByFieldName("readonly") != nil
		}
	}
	if promoted {
		c.idiom("php.idiom.constructor-promotion", fn)
	}
	if readonly {
		c.idiom("php.idiom.readonly", fn)
	}
}

// argumentIdioms records named arguments and first-class callables
// (strlen(...)), once per call.
func (c *checker) argumentIdioms(args *ts.Node) {
	for _, arg := range parsing.NamedChildren(args) {
		switch {
		case arg.Kind() == "variadic_placeholder":
			c.idiom("php.idiom.first-class-callable", args)
			return
		case arg.Kind() == "argument" && arg.ChildByFieldName("name") != nil:
			c.idiom("php.idiom.named-arguments", args)
			return
		}
	}
}

// placeholders reports whether a prepare call's SQL is a literal with bound
// parameters (?, :name, or $wpdb's %s and %d), so values travel separately
// from the query.
func (c *checker) placeholders(args []*ts.Node) bool {
	if len(args) == 0 {
		return false
	}
	sql := args[0]
	if k := sql.Kind(); k != "string" && k != "nowdoc" && k != "encapsed_string" && k != "heredoc" || c.built(sql) {
		return false
	}
	text := c.tree.Text(sql)
	if strings.Contains(text, "?") || strings.Contains(text, "%s") || strings.Contains(text, "%d") {
		return true
	}
	for i := strings.Index(text, ":"); i >= 0 && i+1 < len(text); {
		if r := text[i+1]; r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
		next := strings.Index(text[i+1:], ":")
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return false
}

func hasChild(n *ts.Node, kind string) bool {
	for _, child := range parsing.NamedChildren(n) {
		if child.Kind() == kind {
			return true
		}
	}
	return false
}
