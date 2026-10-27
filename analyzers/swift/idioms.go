package swift

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// standardProtocols are standard library and SwiftUI protocols whose extensions
// are protocol-oriented code, such as extension Collection where Element: Equatable.
var standardProtocols = map[string]bool{
	"Sequence": true, "Collection": true, "BidirectionalCollection": true, "RandomAccessCollection": true,
	"MutableCollection": true, "RangeReplaceableCollection": true, "StringProtocol": true, "Numeric": true,
	"BinaryInteger": true, "FloatingPoint": true, "Equatable": true, "Hashable": true, "Comparable": true,
	"Identifiable": true, "Error": true, "View": true, "ViewModifier": true,
}

var codableProtocols = map[string]bool{"Codable": true, "Decodable": true, "Encodable": true}

// propertyWrappers are the common SwiftUI, Combine, and SwiftData wrappers.
var propertyWrappers = map[string]bool{
	"State": true, "StateObject": true, "ObservedObject": true, "EnvironmentObject": true, "Environment": true,
	"Binding": true, "Bindable": true, "Published": true, "AppStorage": true, "SceneStorage": true,
	"FocusState": true, "Query": true, "propertyWrapper": true,
}

// visitIdioms finds the Swift idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "guard_statement":
		c.idiom("swift.idiom.guard-early-exit", n)
	case "class_declaration":
		c.declarationIdioms(n)
	case "function_declaration", "init_declaration":
		if parsing.HasToken(n, "async") {
			c.idiom("swift.idiom.async-await", n)
		}
		if constrained(n) {
			c.idiom("swift.idiom.constrained-generics", n)
		}
	case "call_expression":
		// The grammar parses defer { ... } as a call of defer with a trailing closure.
		if c.calleeName(n) == "defer" {
			c.idiom("swift.idiom.defer", n)
		}
	case "attribute":
		name := n.NamedChild(0)
		if name == nil {
			break
		}
		switch text := c.tree.Text(name); {
		case text == "MainActor":
			c.idiom("swift.idiom.actor-isolation", n)
		case propertyWrappers[text]:
			c.idiom("swift.idiom.property-wrapper", n)
		}
	}
	return true
}

// declarationIdioms handles struct, class, enum, actor, and extension
// declarations, which the grammar all calls class_declaration.
func (c *checker) declarationIdioms(n *ts.Node) {
	kind := ""
	if k := n.ChildByFieldName("declaration_kind"); k != nil {
		kind = k.Kind()
	}
	switch kind {
	case "struct":
		c.idiom("swift.idiom.value-type", n)
	case "actor":
		c.idiom("swift.idiom.actor-isolation", n)
	case "enum":
		if c.hasAssociatedValues(n) {
			c.idiom("swift.idiom.associated-values", n)
		}
	case "extension":
		if name := n.ChildByFieldName("name"); name != nil {
			if text := c.tree.Text(name); c.protocols[text] || standardProtocols[text] {
				c.idiom("swift.idiom.protocol-extension", n)
			}
		}
	}
	if kind != "extension" && constrained(n) {
		c.idiom("swift.idiom.constrained-generics", n)
	}
	for _, child := range parsing.NamedChildren(n) {
		if child.Kind() != "inheritance_specifier" {
			continue
		}
		if t := child.ChildByFieldName("inherits_from"); t != nil && codableProtocols[c.tree.Text(t)] {
			c.idiom("swift.idiom.codable", n)
			break
		}
	}
}

func (c *checker) hasAssociatedValues(enum *ts.Node) bool {
	body := enum.ChildByFieldName("body")
	if body == nil {
		return false
	}
	for _, entry := range parsing.NamedChildren(body) {
		if entry.Kind() == "enum_entry" && entry.ChildByFieldName("data_contents") != nil {
			return true
		}
	}
	return false
}

// constrained reports whether a declaration has a generic parameter with a
// constraint, <T: Comparable>, or a where clause.
func constrained(decl *ts.Node) bool {
	for _, child := range parsing.NamedChildren(decl) {
		switch child.Kind() {
		case "type_constraints":
			return true
		case "type_parameters":
			for _, p := range parsing.NamedChildren(child) {
				if p.Kind() == "type_parameter" && p.NamedChildCount() > 1 {
					return true
				}
			}
		}
	}
	return false
}
