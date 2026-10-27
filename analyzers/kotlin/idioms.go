package kotlin

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

var (
	scopeFunctions      = map[string]bool{"let": true, "apply": true, "also": true, "run": true}
	structuredScopes    = map[string]bool{"coroutineScope": true, "supervisorScope": true, "withContext": true}
	collectionFunctions = map[string]bool{
		"map": true, "mapNotNull": true, "flatMap": true, "filter": true, "filterNot": true, "filterIsInstance": true,
		"fold": true, "reduce": true, "associate": true, "associateBy": true, "groupBy": true, "partition": true,
		"sumOf": true, "any": true, "all": true, "none": true, "count": true, "firstOrNull": true,
	}
	// typeKinds are the nodes a receiver type can be.
	typeKinds = map[string]bool{"user_type": true, "nullable_type": true, "parenthesized_type": true, "function_type": true}
	// expressionContexts are parents in which a when is used as a value.
	expressionContexts = map[string]bool{
		"function_body": true, "property_declaration": true, "return_expression": true, "assignment": true, "value_argument": true,
	}
)

// visitIdioms finds the Kotlin idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "class_declaration":
		switch {
		case c.hasModifier(n, "data"):
			c.idiom("kotlin.idiom.data-class", n)
		case c.hasModifier(n, "sealed"):
			c.idiom("kotlin.idiom.sealed-hierarchy", n)
		}
	case "function_declaration":
		if c.hasModifier(n, "suspend") {
			c.idiom("kotlin.idiom.suspend-function", n)
		}
		if isExtension(n) {
			c.idiom("kotlin.idiom.extension-function", n)
		}
		if c.hasModifier(n, "inline") && hasReified(n) {
			c.idiom("kotlin.idiom.reified-generics", n)
		}
	case "when_expression":
		if c.exhaustiveWhen(n) {
			c.idiom("kotlin.idiom.exhaustive-when", n)
		}
	case "navigation_expression":
		if parsing.HasToken(n, "?.") {
			c.idiom("kotlin.idiom.null-safety", n)
		}
	case "binary_expression":
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "?:" {
			c.idiom("kotlin.idiom.null-safety", n)
		}
	case "call_expression":
		receiver, name := c.callee(n)
		switch {
		case structuredScopes[name] && receiver == nil:
			c.idiom("kotlin.idiom.structured-concurrency", n)
		case scopeFunctions[name] && receiver != nil && hasLambda(n):
			c.idiom("kotlin.idiom.scope-function", n)
		case collectionFunctions[name] && receiver != nil && hasLambda(n):
			c.idiom("kotlin.idiom.collection-operations", n)
		}
	}
	return true
}

// hasModifier reports whether a declaration has a modifier keyword such as
// data, sealed, suspend, or inline.
func (c *checker) hasModifier(decl *ts.Node, keyword string) bool {
	for _, child := range parsing.NamedChildren(decl) {
		if child.Kind() != "modifiers" {
			continue
		}
		for _, m := range parsing.NamedChildren(child) {
			if c.tree.Text(m) == keyword {
				return true
			}
		}
	}
	return false
}

// isExtension reports whether a function declares a receiver type, which the
// grammar places before the name: fun String.shout().
func isExtension(fn *ts.Node) bool {
	name := fn.ChildByFieldName("name")
	if name == nil {
		return false
	}
	for _, child := range parsing.NamedChildren(fn) {
		if child.StartByte() >= name.StartByte() {
			return false
		}
		if typeKinds[child.Kind()] {
			return true
		}
	}
	return false
}

func hasReified(fn *ts.Node) bool {
	found := false
	parsing.Walk(fn, func(n *ts.Node) bool {
		switch n.Kind() {
		case "reification_modifier":
			found = true
		case "function_body":
			return false
		}
		return !found
	})
	return found
}

// exhaustiveWhen reports a when over a subject, used as a value, with no else
// branch: the compiler then checks that every case of a sealed type or enum is
// handled, and adding a case breaks the build until it is.
func (c *checker) exhaustiveWhen(n *ts.Node) bool {
	parent := n.Parent()
	if parent == nil || !expressionContexts[parent.Kind()] {
		return false
	}
	hasSubject := false
	for _, child := range parsing.NamedChildren(n) {
		switch child.Kind() {
		case "when_subject":
			hasSubject = true
		case "when_entry":
			if parsing.HasToken(child, "else") {
				return false
			}
		}
	}
	return hasSubject
}
