package clang

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
	boundedStringFunctions = map[string]bool{"snprintf": true, "vsnprintf": true, "strlcpy": true, "strlcat": true}
	allocators             = map[string]bool{"malloc": true, "calloc": true, "realloc": true, "reallocarray": true}
	staticAsserts          = map[string]bool{"static_assert": true, "_Static_assert": true}
	fixedWidthTypes        = map[string]bool{
		"int8_t": true, "int16_t": true, "int32_t": true, "int64_t": true,
		"uint8_t": true, "uint16_t": true, "uint32_t": true, "uint64_t": true,
		"intptr_t": true, "uintptr_t": true,
	}
	// cleanupLabels are the conventional names of a function's single exit.
	cleanupLabels = []string{"cleanup", "out", "err", "error", "fail", "done", "exit", "free", "unlock", "bail"}
)

// visitIdioms finds the C idioms that show fluency: each is a deliberate choice
// the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "call_expression":
		name := c.callee(n)
		switch {
		case boundedStringFunctions[name]:
			c.idiom("c.idiom.bounded-string", n)
		case staticAsserts[name]:
			c.idiom("c.idiom.static-assert", n)
		case allocators[name] && c.sizesObject(n):
			c.idiom("c.idiom.sizeof-object", n)
		}
	case "function_definition":
		for _, child := range parsing.NamedChildren(n) {
			if child.Kind() == "storage_class_specifier" && c.tree.Text(child) == "static" {
				c.idiom("c.idiom.static-function", n)
			}
		}
		if hasConstPointerParam(n) {
			c.idiom("c.idiom.const-pointer-param", n)
		}
	case "initializer_list":
		for _, child := range parsing.NamedChildren(n) {
			if child.Kind() == "initializer_pair" {
				c.idiom("c.idiom.designated-initializer", n)
				break
			}
		}
	case "enum_specifier":
		if n.ChildByFieldName("body") != nil {
			c.idiom("c.idiom.enum-constants", n)
		}
	case "declaration", "field_declaration", "parameter_declaration":
		if typ := n.ChildByFieldName("type"); typ != nil && fixedWidthTypes[c.tree.Text(typ)] {
			c.idiom("c.idiom.fixed-width-types", n)
		}
	case "labeled_statement":
		if c.isCleanupLabel(n) {
			c.idiom("c.idiom.goto-cleanup", n)
		}
	}
	return true
}

// sizesObject reports whether an allocation sizes by the object, as in
// malloc(sizeof *p) or sizeof p[0], which stays correct if p's type changes.
func (c *checker) sizesObject(call *ts.Node) bool {
	found := false
	for _, arg := range arguments(call) {
		parsing.Walk(arg, func(n *ts.Node) bool {
			if n.Kind() == "sizeof_expression" {
				v := unwrap(n.ChildByFieldName("value"))
				if v != nil && (v.Kind() == "pointer_expression" && parsing.HasToken(v, "*") || v.Kind() == "subscript_expression") {
					found = true
				}
				return false
			}
			return true
		})
	}
	return found
}

// hasConstPointerParam reports whether a function takes a pointer to const,
// promising not to modify what the caller passes.
func hasConstPointerParam(fn *ts.Node) bool {
	decl := fn.ChildByFieldName("declarator")
	for decl != nil && decl.Kind() != "function_declarator" {
		decl = decl.ChildByFieldName("declarator")
	}
	if decl == nil {
		return false
	}
	params := decl.ChildByFieldName("parameters")
	if params == nil {
		return false
	}
	for _, p := range parsing.NamedChildren(params) {
		if p.Kind() != "parameter_declaration" {
			continue
		}
		d := p.ChildByFieldName("declarator")
		if d == nil || d.Kind() != "pointer_declarator" {
			continue
		}
		// A const beside the type qualifies the pointee; char *const p is
		// qualified inside the pointer declarator and does not count.
		for _, child := range parsing.NamedChildren(p) {
			if child.Kind() == "type_qualifier" && parsing.HasToken(child, "const") {
				return true
			}
		}
	}
	return false
}

// isCleanupLabel reports whether a label with a conventional cleanup name is the
// target of a forward goto in the same function: single-exit error handling.
func (c *checker) isCleanupLabel(label *ts.Node) bool {
	id := label.ChildByFieldName("label")
	if id == nil {
		return false
	}
	name := c.tree.Text(id)
	conventional := false
	for _, l := range cleanupLabels {
		if strings.HasPrefix(strings.ToLower(name), l) {
			conventional = true
			break
		}
	}
	fn := label.Parent()
	for fn != nil && fn.Kind() != "function_definition" {
		fn = fn.Parent()
	}
	if !conventional || fn == nil {
		return false
	}
	forward := false
	parsing.Walk(fn, func(n *ts.Node) bool {
		if n.Kind() == "goto_statement" && n.StartByte() < label.StartByte() {
			if target := n.ChildByFieldName("label"); target != nil && c.tree.Text(target) == name {
				forward = true
			}
		}
		return !forward
	})
	return forward
}

// hasIncludeGuard reports whether a header is wrapped in #ifndef X / #define X
// or starts with #pragma once.
func (c *checker) hasIncludeGuard() bool {
	for _, n := range parsing.NamedChildren(c.tree.Root()) {
		switch n.Kind() {
		case "comment":
			continue
		case "preproc_call":
			d, arg := n.ChildByFieldName("directive"), n.ChildByFieldName("argument")
			return d != nil && arg != nil && c.tree.Text(d) == "#pragma" && strings.TrimSpace(c.tree.Text(arg)) == "once"
		case "preproc_ifdef":
			if !parsing.HasToken(n, "#ifndef") {
				return false
			}
			name := n.ChildByFieldName("name")
			if name == nil {
				return false
			}
			for _, child := range parsing.NamedChildren(n) {
				if child.Kind() == "comment" || child.Equals(*name) {
					continue
				}
				def := child.ChildByFieldName("name")
				return child.Kind() == "preproc_def" && def != nil && c.tree.Text(def) == c.tree.Text(name)
			}
			return false
		default:
			return false
		}
	}
	return false
}
