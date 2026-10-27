package csharp

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// linqMethods are Enumerable and Queryable operators that take a lambda.
var linqMethods = map[string]bool{
	"Where": true, "Select": true, "SelectMany": true, "OrderBy": true, "OrderByDescending": true,
	"ThenBy": true, "ThenByDescending": true, "GroupBy": true, "Join": true, "GroupJoin": true,
	"Any": true, "All": true, "Count": true, "First": true, "FirstOrDefault": true, "Single": true,
	"SingleOrDefault": true, "Last": true, "LastOrDefault": true, "Sum": true, "Min": true, "Max": true,
	"Average": true, "Aggregate": true, "ToDictionary": true, "ToLookup": true, "TakeWhile": true,
	"SkipWhile": true, "DistinctBy": true, "MinBy": true, "MaxBy": true, "Zip": true,
}

// visitIdioms finds the C# idioms that show fluency: each is a deliberate choice
// the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "method_declaration", "local_function_statement":
		if c.hasModifier(n, "async") && c.text(returnType(n)) != "void" {
			c.idiom("csharp.idiom.async-await", n)
		}
	case "query_expression":
		c.idiom("csharp.idiom.linq", n)
	case "invocation_expression":
		if c.isLinq(n) && !c.isLinq(receiver(n)) {
			c.idiom("csharp.idiom.linq", n) // once per chain, at its first operator
		}
	case "using_statement":
		c.idiom("csharp.idiom.using-disposal", n)
	case "local_declaration_statement":
		if parsing.HasToken(n, "using") {
			c.idiom("csharp.idiom.using-disposal", n)
		}
	case "record_declaration":
		c.idiom("csharp.idiom.records", n)
	case "is_pattern_expression", "switch_expression":
		c.idiom("csharp.idiom.pattern-matching", n)
	case "preproc_nullable":
		if parsing.HasToken(n, "enable") {
			c.idiom("csharp.idiom.nullable-annotations", n)
		}
	case "nullable_type":
		// string? and object? are nullable references; T? on a named type may be
		// a Nullable<T> struct, which predates nullable reference types.
		if inner := n.ChildByFieldName("type"); inner != nil && (c.text(inner) == "string" || c.text(inner) == "object") {
			c.idiom("csharp.idiom.nullable-annotations", n)
		}
	case "arrow_expression_clause":
		c.idiom("csharp.idiom.expression-bodied", n)
	case "property_declaration", "field_declaration":
		if c.hasModifier(n, "required") || c.hasInitAccessor(n) {
			c.idiom("csharp.idiom.init-only", n)
		}
	case "parameter":
		if t := c.lastName(n.ChildByFieldName("type")); t == "CancellationToken" {
			c.idiom("csharp.idiom.cancellation-token", n)
		}
	}
	return true
}

// isLinq reports whether a node calls a LINQ operator with a lambda argument.
func (c *checker) isLinq(n *ts.Node) bool {
	if n == nil || n.Kind() != "invocation_expression" || !linqMethods[c.memberName(n)] {
		return false
	}
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for _, a := range parsing.NamedChildren(args) {
		for _, e := range parsing.NamedChildren(a) {
			if e.Kind() == "lambda_expression" {
				return true
			}
		}
	}
	return false
}

// receiver returns the object a method is called on, such as xs in xs.Where(...).
func receiver(call *ts.Node) *ts.Node {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_access_expression" {
		return nil
	}
	return fn.ChildByFieldName("expression")
}

func (c *checker) hasInitAccessor(n *ts.Node) bool {
	accessors := n.ChildByFieldName("accessors")
	if accessors == nil {
		return false
	}
	for _, a := range parsing.NamedChildren(accessors) {
		if name := a.ChildByFieldName("name"); name != nil && c.text(name) == "init" {
			return true
		}
	}
	return false
}
