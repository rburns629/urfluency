package rust

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
	// iterSources start an iterator chain.
	iterSources = set("iter", "into_iter", "iter_mut", "chars", "bytes", "lines", "keys", "values", "values_mut", "windows", "chunks", "drain")
	// iterTransforms are the adapters and folds that make a chain worth noting.
	iterTransforms = set("map", "filter", "filter_map", "flat_map", "fold", "zip", "enumerate", "take_while", "skip_while", "find_map", "scan", "try_fold")
	// iterMethods are every method that continues a chain.
	iterMethods = set("map", "filter", "filter_map", "flat_map", "fold", "zip", "enumerate", "take_while", "skip_while", "find_map", "scan", "try_fold",
		"collect", "sum", "product", "count", "any", "all", "find", "position", "max", "min", "max_by_key", "min_by_key", "for_each",
		"last", "nth", "rev", "take", "skip", "chain", "cloned", "copied", "peekable", "flatten", "step_by", "inspect")
	// combinators transform Option and Result values without matching.
	combinators = set("map_or", "map_or_else", "and_then", "ok_or", "ok_or_else", "unwrap_or_else", "unwrap_or_default", "map_err")
	// genericItems may declare type parameters and where clauses.
	genericItems = set("function_item", "function_signature_item", "struct_item", "enum_item", "impl_item", "trait_item")
)

// visitIdioms finds the Rust idioms that show fluency: each is a deliberate
// choice the language does not force.
func (c *checker) visitIdioms(n *ts.Node) bool {
	kind := n.Kind()
	switch kind {
	case "try_expression":
		c.idiom("rust.idiom.question-mark", n)
	case "function_item":
		if isResult(c.tree, n.ChildByFieldName("return_type")) {
			c.idiom("rust.idiom.result-return", n)
		}
	case "call_expression":
		name, _, _ := method(c.tree, n)
		if combinators[name] {
			c.idiom("rust.idiom.combinators", n)
		}
		if c.isIteratorChain(n) {
			c.idiom("rust.idiom.iterator-chain", n)
		}
	case "match_expression":
		if c.matchesVariants(n) {
			c.idiom("rust.idiom.enum-match", n)
		}
	case "trait_item":
		c.idiom("rust.idiom.traits", n)
	case "impl_item":
		if trait := n.ChildByFieldName("trait"); trait != nil {
			c.idiom("rust.idiom.traits", n)
			if t := c.tree.Text(trait); t == "Error" || strings.HasSuffix(t, "::Error") {
				c.idiom("rust.idiom.error-type", n)
			}
		}
	case "mod_item":
		if c.hasTestAttribute(n) {
			c.idiom("rust.idiom.test-module", n)
		}
	case "attribute_item":
		if c.derivesError(n) {
			c.idiom("rust.idiom.error-type", n)
		}
	case "type_parameters":
		for _, p := range parsing.NamedChildren(n) {
			if p.Kind() == "lifetime_parameter" {
				c.idiom("rust.idiom.lifetimes", n)
				break
			}
		}
	}
	if genericItems[kind] && hasTraitBounds(n) {
		c.idiom("rust.idiom.trait-bounds", n)
	}
	return true
}

// isResult matches Result<T, E>, io::Result<T>, and anyhow::Result<T>.
func isResult(t *parsing.Tree, typ *ts.Node) bool {
	if typ == nil || typ.Kind() != "generic_type" {
		return false
	}
	name := typ.ChildByFieldName("type")
	return name != nil && (t.Text(name) == "Result" || strings.HasSuffix(t.Text(name), "::Result"))
}

// isIteratorChain reports the outermost call of a chain that starts at an
// iterator source, such as iter(), and transforms it with map, filter, or fold.
func (c *checker) isIteratorChain(call *ts.Node) bool {
	name, receiver, _ := method(c.tree, call)
	if !iterMethods[name] {
		return false
	}
	// Report only the outermost call: skip when the chain continues above.
	if p := call.Parent(); p != nil && p.Kind() == "field_expression" {
		outer := p.Parent()
		if outer != nil && outer.Kind() == "generic_function" {
			outer = outer.Parent()
		}
		if outer != nil && outer.Kind() == "call_expression" {
			if next, _, _ := method(c.tree, outer); iterMethods[next] {
				return false
			}
		}
	}
	transformed := iterTransforms[name]
	for receiver != nil && receiver.Kind() == "call_expression" {
		inner, next, _ := method(c.tree, receiver)
		switch {
		case iterSources[inner]:
			return transformed
		case iterMethods[inner]:
			transformed = transformed || iterTransforms[inner]
			receiver = next
		default:
			return false
		}
	}
	return false
}

// matchesVariants reports whether a match has an arm matching an enum variant:
// Some(x), None, Ok(x), Err(e), Kind::A, or Kind::B { .. }.
func (c *checker) matchesVariants(m *ts.Node) bool {
	body := m.ChildByFieldName("body")
	if body == nil {
		return false
	}
	for _, arm := range parsing.NamedChildren(body) {
		pattern := arm.ChildByFieldName("pattern")
		if arm.Kind() == "match_arm" && pattern != nil && pattern.NamedChildCount() > 0 && c.isVariant(pattern.NamedChild(0)) {
			return true
		}
	}
	return false
}

func (c *checker) isVariant(p *ts.Node) bool {
	switch p.Kind() {
	case "scoped_identifier", "tuple_struct_pattern", "struct_pattern":
		return true
	case "identifier":
		return c.tree.Text(p) == "None"
	case "or_pattern":
		for _, alt := range parsing.NamedChildren(p) {
			if c.isVariant(alt) {
				return true
			}
		}
	}
	return false
}

// derivesError matches #[derive(Error)] and #[derive(thiserror::Error)].
func (c *checker) derivesError(item *ts.Node) bool {
	attr := attributeText(c.tree, item)
	if !strings.HasPrefix(attr, "derive(") {
		return false
	}
	for _, word := range strings.FieldsFunc(attr, func(r rune) bool { return !isIdentRune(r) }) {
		if word == "Error" {
			return true
		}
	}
	return false
}

// hasTraitBounds reports whether an item constrains a generic by behavior: a
// bounded type parameter, a where clause, or an impl Trait parameter.
func hasTraitBounds(item *ts.Node) bool {
	if tp := item.ChildByFieldName("type_parameters"); tp != nil {
		for _, p := range parsing.NamedChildren(tp) {
			if p.Kind() == "type_parameter" && p.ChildByFieldName("bounds") != nil {
				return true
			}
		}
	}
	for _, child := range parsing.NamedChildren(item) {
		if child.Kind() == "where_clause" {
			return true
		}
	}
	if params := item.ChildByFieldName("parameters"); params != nil {
		for _, p := range parsing.NamedChildren(params) {
			if typ := p.ChildByFieldName("type"); typ != nil && typ.Kind() == "abstract_type" {
				return true
			}
		}
	}
	return false
}
