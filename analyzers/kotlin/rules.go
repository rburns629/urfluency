package kotlin

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/parsing"
)

// sqlMethods execute a SQL or JPQL string on Android SQLite, JDBC, and JPA.
var sqlMethods = map[string]bool{
	"rawQuery": true, "execSQL": true, "createQuery": true, "createNativeQuery": true,
	"prepareStatement": true, "executeQuery": true, "executeUpdate": true,
}

func (c *checker) visit(n *ts.Node) bool {
	switch n.Kind() {
	case "unary_expression":
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "!!" && !c.isTest {
			c.report("kotlin.not-null-assertion", n)
		}
	case "catch_block":
		c.checkCatch(n)
	case "call_expression":
		c.checkCall(n)
	}
	return true
}

func (c *checker) checkCall(call *ts.Node) {
	receiver, name := c.callee(call)
	switch {
	case (name == "launch" || name == "async") && receiver != nil:
		if r := c.tree.Text(receiver); r == "GlobalScope" || strings.HasSuffix(r, ".GlobalScope") {
			c.report("kotlin.global-scope", call)
		}
	case name == "runBlocking":
		if !c.isTest && !c.inEntryPoint(call) {
			c.report("kotlin.run-blocking-in-production", call)
		}
	case sqlMethods[name] && receiver != nil:
		if args := arguments(call); len(args) > 0 && c.built(args[0]) {
			c.report("kotlin.sql-string-built", call)
		}
	case name == "exec" && receiver != nil && strings.ReplaceAll(c.tree.Text(receiver), " ", "") == "Runtime.getRuntime()":
		if args := arguments(call); len(args) > 0 && c.built(args[0]) {
			c.report("kotlin.command-built", call)
		}
	case name == "ProcessBuilder" && receiver == nil:
		c.checkProcessBuilder(call)
	}
}

// inEntryPoint reports whether a call is in main or at the top level of a
// script, where blocking the thread is the point.
func (c *checker) inEntryPoint(call *ts.Node) bool {
	fn := c.enclosingFunction(call)
	return fn == "main" || (fn == "" && c.isScript)
}

// checkProcessBuilder flags a shell (sh -c "...") whose command string is built
// from values. Separate arguments without a shell are not interpreted, so they
// are not reported.
func (c *checker) checkProcessBuilder(call *ts.Node) {
	shell := false
	for _, arg := range arguments(call) {
		if shell && c.built(arg) {
			c.report("kotlin.command-built", call)
			return
		}
		if c.tree.Text(arg) == `"-c"` {
			shell = true
		}
	}
}

// checkCatch flags a catch block with no statements and no comment explaining
// why. Tests and exceptions named _, ignored, or expected are intentional.
func (c *checker) checkCatch(n *ts.Node) {
	if c.isTest {
		return
	}
	var block *ts.Node
	for _, child := range parsing.NamedChildren(n) {
		switch child.Kind() {
		case "identifier":
			name := c.tree.Text(child)
			if name == "_" || strings.HasPrefix(name, "ignored") || strings.HasPrefix(name, "expected") {
				return
			}
		case "block":
			block = child
		}
	}
	if block != nil && block.NamedChildCount() == 0 {
		c.report("kotlin.empty-catch", n)
	}
}

// built reports whether an expression is a string assembled from values: a
// template with a non-constant ("$id", "${user.id}") or a concatenation of a
// string literal with one. UPPER_CASE names are constants by convention and
// do not count.
func (c *checker) built(n *ts.Node) bool {
	switch n.Kind() {
	case "parenthesized_expression":
		return n.NamedChildCount() == 1 && c.built(n.NamedChild(0))
	case "string_literal", "multiline_string_literal":
		return c.templated(n)
	case "binary_expression":
		op := n.ChildByFieldName("operator")
		if op == nil || op.Kind() != "+" {
			return false
		}
		var hasString, hasValue bool
		c.concatOperands(n, &hasString, &hasValue)
		return hasString && hasValue
	}
	return false
}

// concatOperands classifies the operands of a + chain.
func (c *checker) concatOperands(n *ts.Node, hasString, hasValue *bool) {
	if n.Kind() == "binary_expression" {
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "+" {
			for _, side := range []string{"left", "right"} {
				if operand := n.ChildByFieldName(side); operand != nil {
					c.concatOperands(operand, hasString, hasValue)
				}
			}
			return
		}
	}
	switch n.Kind() {
	case "string_literal", "multiline_string_literal":
		*hasString = true
		if c.templated(n) {
			*hasValue = true
		}
	case "number_literal", "character_literal":
	case "identifier":
		if !isConstant(c.tree.Text(n)) {
			*hasValue = true
		}
	default:
		*hasValue = true
	}
}

// templated reports whether a string literal interpolates a non-constant. The
// grammar parses "${x}" (and $x in raw strings) as interpolation, but $x in a
// plain string as a "$" string_content followed by the name.
func (c *checker) templated(lit *ts.Node) bool {
	parts := parsing.NamedChildren(lit)
	for i, part := range parts {
		switch {
		case part.Kind() == "interpolation":
			expr := part.NamedChild(0)
			if expr == nil || expr.Kind() != "identifier" || !isConstant(c.tree.Text(expr)) {
				return true
			}
		case part.Kind() == "string_content" && c.tree.Text(part) == "$" && i+1 < len(parts) && parts[i+1].Kind() == "string_content":
			if name := leadingName(c.tree.Text(parts[i+1])); name != "" && !isConstant(name) {
				return true
			}
		}
	}
	return false
}

// leadingName returns the identifier at the start of s, or "".
func leadingName(s string) string {
	end := 0
	for i, r := range s {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			end = i + 1
			continue
		}
		break
	}
	return s[:end]
}

// isConstant reports whether a name is UPPER_CASE, the convention for const val.
func isConstant(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for _, r := range name {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
