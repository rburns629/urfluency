package java

import (
	"regexp"
	"slices"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// Frameworks implements analysis.FrameworkDetector from a file's imports.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.Java, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	found := map[string]bool{}
	for _, imp := range imports(tree) {
		for _, fw := range frameworkPackages {
			if imp == fw.prefix || strings.HasPrefix(imp, fw.prefix+".") {
				found[fw.name] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for fw := range found {
		out = append(out, fw)
	}
	slices.Sort(out)
	return out
}

// frameworkPackages map package prefixes to framework names. A Spring Boot
// import also matches org.springframework, so it reports both.
var frameworkPackages = []struct{ prefix, name string }{
	{"org.springframework", "spring"},
	{"org.springframework.boot", "spring-boot"},
	{"jakarta", "jakarta-ee"},
	{"javax.servlet", "jakarta-ee"},
	{"javax.ejb", "jakarta-ee"},
	{"android", "android"},
	{"androidx", "android"},
	{"io.quarkus", "quarkus"},
	{"io.micronaut", "micronaut"},
	{"org.hibernate", "hibernate"},
	{"org.junit", "junit"},
	{"lombok", "lombok"},
}

// sqlMethods run or prepare SQL or JPQL given as their first argument.
var sqlMethods = set("execute", "executeQuery", "executeUpdate", "executeLargeUpdate", "addBatch",
	"prepareStatement", "prepareCall", "createQuery", "createNativeQuery", "createSQLQuery")

var sqlKeyword = regexp.MustCompile(`(?i)\b(select|insert|update|delete|from|where|values|order\s+by)\b`)

// constantName matches UPPER_SNAKE_CASE names, which are compile-time constants
// by convention rather than user input.
var constantName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// sqlConcatenation reports whether n concatenates a SQL literal with a
// non-constant value: "SELECT * FROM t WHERE id = " + id.
func (c *checker) sqlConcatenation(n *ts.Node) bool {
	if !c.dynamicConcatenation(n) {
		return false
	}
	for _, leaf := range concatOperands(n) {
		if leaf.Kind() == "string_literal" && sqlKeyword.MatchString(c.text(leaf)) {
			return true
		}
	}
	return false
}

// dynamicConcatenation reports whether n is a + expression that joins at least
// one string literal with at least one value that is not a constant.
func (c *checker) dynamicConcatenation(n *ts.Node) bool {
	if n.Kind() != "binary_expression" || operator(n) != "+" {
		return false
	}
	literal, dynamic := false, false
	for _, leaf := range concatOperands(n) {
		switch {
		case leaf.Kind() == "string_literal":
			literal = true
		case !c.constant(leaf):
			dynamic = true
		}
	}
	return literal && dynamic
}

// concatOperands flattens a chain of + expressions into its operands.
func concatOperands(n *ts.Node) []*ts.Node {
	if n.Kind() == "parenthesized_expression" && n.NamedChildCount() == 1 {
		return concatOperands(n.NamedChild(0))
	}
	if n.Kind() != "binary_expression" || operator(n) != "+" {
		return []*ts.Node{n}
	}
	var out []*ts.Node
	for _, side := range []string{"left", "right"} {
		if s := n.ChildByFieldName(side); s != nil {
			out = append(out, concatOperands(s)...)
		}
	}
	return out
}

// constant reports whether an operand is a literal or a named constant.
func (c *checker) constant(n *ts.Node) bool {
	switch n.Kind() {
	case "string_literal", "character_literal", "decimal_integer_literal", "hex_integer_literal",
		"decimal_floating_point_literal", "true", "false":
		return true
	case "identifier":
		return constantName.MatchString(c.text(n))
	case "field_access":
		return constantName.MatchString(c.text(n.ChildByFieldName("field")))
	}
	return false
}

// checkProcessBuilder flags a ProcessBuilder that runs a shell (sh -c, cmd /c)
// on a concatenated command. Concatenated arguments without a shell are not
// reinterpreted, so they are not reported.
func (c *checker) checkProcessBuilder(n *ts.Node) {
	args := argumentNodes(n)
	shell, built := false, false
	for _, a := range args {
		switch {
		case a.Kind() == "string_literal" && (c.text(a) == `"-c"` || strings.EqualFold(c.text(a), `"/c"`)):
			shell = true
		case c.dynamicConcatenation(a):
			built = true
		}
	}
	if shell && built {
		c.report("java.command-built", n)
	}
}

// filtered reports whether the method creating a deserialization stream also
// installs an ObjectInputFilter, the JDK's allowlist mechanism.
func (c *checker) filtered(n *ts.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "method_declaration", "constructor_declaration", "lambda_expression":
			return strings.Contains(c.text(p), "setObjectInputFilter")
		}
	}
	return false
}

// checkTrustAll flags trust managers whose checkServerTrusted accepts every
// certificate, and hostname verifiers whose verify always returns true.
func (c *checker) checkTrustAll(n *ts.Node) {
	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	switch c.text(n.ChildByFieldName("name")) {
	case "checkServerTrusted":
		for _, stmt := range parsing.NamedChildren(body) {
			if stmt.Kind() != "line_comment" && stmt.Kind() != "block_comment" {
				return
			}
		}
		c.report("java.trust-all-certificates", n)
	case "verify":
		params := n.ChildByFieldName("parameters")
		if params == nil || params.NamedChildCount() != 2 || simpleName(c.text(params.NamedChild(1).ChildByFieldName("type"))) != "SSLSession" {
			return
		}
		if c.returnsTrue(body) {
			c.report("java.trust-all-certificates", n)
		}
	}
}

// returnsTrue reports whether a block is only "return true;".
func (c *checker) returnsTrue(block *ts.Node) bool {
	stmts := parsing.NamedChildren(block)
	return len(stmts) == 1 && stmts[0].Kind() == "return_statement" && stmts[0].NamedChildCount() == 1 &&
		stmts[0].NamedChild(0).Kind() == "true"
}

// verifierSetters install a HostnameVerifier passed as their argument.
var verifierSetters = set("setHostnameVerifier", "setDefaultHostnameVerifier", "hostnameVerifier")

// permissiveVerifierLambda reports whether a two-parameter lambda that always
// returns true is used as a HostnameVerifier: declared with that type or passed
// to a verifier setter.
func (c *checker) permissiveVerifierLambda(n *ts.Node) bool {
	params := n.ChildByFieldName("parameters")
	body := n.ChildByFieldName("body")
	if params == nil || body == nil || params.NamedChildCount() != 2 {
		return false
	}
	if body.Kind() != "true" && (body.Kind() != "block" || !c.returnsTrue(body)) {
		return false
	}
	parent := n.Parent()
	switch kindOf(parent) {
	case "variable_declarator":
		decl := parent.Parent()
		return decl != nil && simpleName(c.text(decl.ChildByFieldName("type"))) == "HostnameVerifier"
	case "argument_list":
		call := parent.Parent()
		return call != nil && call.Kind() == "method_invocation" && verifierSetters[c.text(call.ChildByFieldName("name"))]
	}
	return false
}
