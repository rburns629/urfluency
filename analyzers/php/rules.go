package php

import (
	"regexp"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/parsing"
)

var (
	// sqlMethods run a SQL string on PDO, mysqli, SQLite3, CodeIgniter, and
	// Doctrine DBAL connections, or add raw SQL to a Laravel query builder.
	sqlMethods = map[string]bool{
		"query": true, "exec": true, "multi_query": true, "real_query": true, "prepare": true,
		"executequery": true, "executestatement": true,
		"whereraw": true, "selectraw": true, "orderbyraw": true, "havingraw": true, "groupbyraw": true,
	}
	// sqlFacadeMethods run raw SQL through Laravel's DB facade.
	sqlFacadeMethods = map[string]bool{
		"select": true, "insert": true, "update": true, "delete": true, "statement": true, "unprepared": true, "raw": true,
	}
	// wpdbMethods run raw SQL on WordPress's $wpdb.
	wpdbMethods = map[string]bool{"get_results": true, "get_var": true, "get_row": true, "get_col": true}
	// sqlFunctions take the SQL as their last argument after an optional link.
	sqlFunctions = map[string]bool{
		"mysqli_query": true, "mysqli_real_query": true, "mysqli_multi_query": true, "mysqli_prepare": true,
		"mysql_query": true, "pg_query": true, "pg_prepare": true, "sqlsrv_query": true,
	}
	// sqlSafeCalls return values that cannot break out of SQL: numbers, or
	// strings escaped or already bound by a driver.
	sqlSafeCalls = map[string]bool{
		"intval": true, "floatval": true, "absint": true, "count": true, "esc_sql": true, "prepare": true,
		"quote": true, "real_escape_string": true, "mysqli_real_escape_string": true, "pg_escape_string": true,
		"pg_escape_literal": true, "escape": true, "escape_str": true,
	}
	// shellFunctions run a command string through the shell.
	shellFunctions = map[string]bool{
		"exec": true, "shell_exec": true, "system": true, "passthru": true, "proc_open": true, "popen": true,
	}
	// shellEscapes quote a value for the shell.
	shellEscapes = map[string]bool{"escapeshellarg": true, "escapeshellcmd": true}
	// numericCasts produce numbers, which cannot inject anything.
	numericCasts = map[string]bool{"int": true, "integer": true, "float": true, "double": true, "bool": true, "boolean": true}
	// wordpressGlobals are the globals WordPress exposes as its API; declaring
	// them with global is how plugins and themes are expected to reach them.
	wordpressGlobals = map[string]bool{
		"wpdb": true, "post": true, "wp_query": true, "wp": true, "current_user": true, "pagenow": true, "wp_rewrite": true,
		"wp_roles": true, "wp_version": true, "wp_filesystem": true, "wp_scripts": true, "wp_styles": true, "typenow": true,
		"product": true, "woocommerce": true, "wp_locale": true, "wp_admin_bar": true, "wp_customize": true,
	}
	// guzzleRequestMethods take a Guzzle options array.
	guzzleRequestMethods = map[string]bool{
		"request": true, "requestasync": true, "send": true, "sendasync": true, "get": true, "post": true, "put": true,
		"patch": true, "delete": true, "head": true, "options": true,
	}
)

func (c *checker) visit(n *ts.Node) bool {
	switch n.Kind() {
	case "function_call_expression", "member_call_expression", "nullsafe_member_call_expression", "scoped_call_expression":
		c.checkCall(n)
	case "shell_command_expression":
		for _, part := range parsing.NamedChildren(n) {
			if part.Kind() != "string_content" && part.Kind() != "escape_sequence" {
				c.report("php.command-built", n)
				break
			}
		}
	case "include_expression", "include_once_expression", "require_expression", "require_once_expression":
		if p := n.NamedChild(0); p != nil && c.tainted(p) {
			c.report("php.include-variable-path", n)
		}
	case "array_element_initializer":
		c.checkTLSOption(n)
	case "echo_statement", "print_intrinsic":
		c.checkEcho(parsing.NamedChildren(n), n)
	case "expression_statement":
		if c.shortEcho(n) {
			c.checkEcho(parsing.NamedChildren(n), n)
		}
	case "binary_expression":
		c.checkComparison(n)
	case "error_suppression_expression":
		if !c.isTest {
			c.report("php.error-suppression", n)
		}
	case "global_declaration":
		c.checkGlobal(n)
	case "catch_clause":
		c.checkCatch(n)
	case "function_definition", "method_declaration":
		if !c.isTest {
			c.checkTypes(n)
		}
	}
	return true
}

func (c *checker) checkCall(call *ts.Node) {
	receiver, name := c.callee(call)
	args := arguments(call)
	if name == "withoutverifying" { // Laravel's Http::withoutVerifying()
		c.report("php.tls-verify-disabled", call)
	}
	switch call.Kind() {
	case "function_call_expression":
		c.checkFunction(call, name, args)
	case "scoped_call_expression":
		if sqlFacadeMethods[name] && receiver != nil && c.tree.Text(receiver) == "DB" && len(args) > 0 && c.built(args[0]) {
			c.report("php.sql-string-built", call)
		}
	default:
		if (sqlMethods[name] || wpdbMethods[name] && c.variableName(receiver) == "wpdb") && len(args) > 0 && c.built(args[0]) {
			c.report("php.sql-string-built", call)
		}
	}
}

func (c *checker) checkFunction(call *ts.Node, name string, args []*ts.Node) {
	switch {
	case name == "eval" || name == "create_function":
		c.report("php.eval", call)
	case name == "unserialize":
		if len(args) > 0 && c.usesRequestInput(args[0]) && !c.restrictsClasses(args) {
			c.report("php.unsafe-deserialization", call)
		}
	case name == "extract":
		if len(args) > 0 && c.usesRequestInput(args[0]) {
			c.report("php.extract-request-input", call)
		}
	case shellFunctions[name]:
		// proc_open with an array runs the program without a shell.
		if len(args) > 0 && args[0].Kind() != "array_creation_expression" && c.tainted(args[0]) {
			c.report("php.command-built", call)
		}
	case sqlFunctions[name]:
		if len(args) > 0 && c.built(args[len(args)-1]) {
			c.report("php.sql-string-built", call)
		}
	case name == "md5" || name == "sha1":
		if len(args) > 0 && c.mentionsPassword(args[0]) {
			c.report("php.password-fast-hash", call)
		}
	case name == "hash":
		if len(args) > 1 && c.mentionsPassword(args[1]) {
			c.report("php.password-fast-hash", call)
		}
	case name == "curl_setopt":
		if len(args) == 3 && c.disablesTLS(args[1], args[2]) {
			c.report("php.tls-verify-disabled", call)
		}
	}
}

// restrictsClasses reports whether unserialize is given allowed_classes, which
// stops it from instantiating objects named in the data.
func (c *checker) restrictsClasses(args []*ts.Node) bool {
	return len(args) > 1 && strings.Contains(c.tree.Text(args[1]), "allowed_classes")
}

// checkTLSOption flags curl options in an array (curl_setopt_array) and
// stream context SSL options that turn off certificate checks, and Guzzle's
// verify option set to false in a client or request.
func (c *checker) checkTLSOption(el *ts.Node) {
	parts := parsing.NamedChildren(el)
	if len(parts) != 2 {
		return
	}
	key, value := parts[0], parts[1]
	name := c.stringValue(key)
	if c.disablesTLS(key, value) ||
		(name == "verify_peer" || name == "verify_peer_name") && disabled(c.tree.Text(value)) ||
		name == "verify" && strings.EqualFold(c.tree.Text(value), "false") && c.guzzleOptions(el) {
		c.report("php.tls-verify-disabled", el)
	}
}

// disablesTLS reports whether a curl option and value turn off peer or host
// verification.
func (c *checker) disablesTLS(option, value *ts.Node) bool {
	name := strings.TrimPrefix(c.tree.Text(option), `\`)
	return (name == "CURLOPT_SSL_VERIFYPEER" || name == "CURLOPT_SSL_VERIFYHOST") && disabled(c.tree.Text(value))
}

func disabled(value string) bool {
	return strings.EqualFold(value, "false") || value == "0"
}

// guzzleOptions reports whether an array element belongs to an options array
// passed straight to new Client(...) or a request method such as ->get().
func (c *checker) guzzleOptions(el *ts.Node) bool {
	arr := el.Parent()
	if arr == nil || arr.Kind() != "array_creation_expression" || arr.Parent() == nil || arr.Parent().Kind() != "argument" {
		return false
	}
	call := arr.Parent().Parent()
	if call != nil {
		call = call.Parent()
	}
	if call == nil {
		return false
	}
	switch call.Kind() {
	case "object_creation_expression":
		for _, child := range parsing.NamedChildren(call) {
			if child.Kind() == "name" || child.Kind() == "qualified_name" {
				return strings.HasSuffix(c.tree.Text(child), "Client")
			}
		}
	case "member_call_expression":
		_, name := c.callee(call)
		return guzzleRequestMethods[name]
	}
	return false
}

// stringValue returns the contents of a string literal without escapes or
// interpolation, or "" for anything else.
func (c *checker) stringValue(n *ts.Node) string {
	if n.Kind() != "string" && n.Kind() != "encapsed_string" || n.NamedChildCount() != 1 {
		return ""
	}
	if part := n.NamedChild(0); part.Kind() == "string_content" {
		return c.tree.Text(part)
	}
	return ""
}

// shortEcho reports whether an expression statement is the body of a <?= tag.
func (c *checker) shortEcho(stmt *ts.Node) bool {
	prev := stmt.PrevNamedSibling()
	if prev != nil && prev.Kind() == "text_interpolation" && prev.NamedChildCount() > 0 {
		prev = prev.NamedChild(prev.NamedChildCount() - 1)
	}
	return prev != nil && prev.Kind() == "php_tag" && c.tree.Text(prev) == "<?="
}

// checkEcho flags output of request input that no escaping function has seen.
func (c *checker) checkEcho(exprs []*ts.Node, at *ts.Node) {
	for _, e := range exprs {
		if c.reflects(e) {
			c.report("php.reflected-input", at)
			return
		}
	}
}

// passThrough are string functions that return their input still unescaped.
var passThrough = map[string]bool{
	"trim": true, "ltrim": true, "rtrim": true, "strtolower": true, "strtoupper": true, "ucfirst": true, "ucwords": true,
	"stripslashes": true, "urldecode": true, "rawurldecode": true, "base64_decode": true, "sprintf": true,
	"implode": true, "nl2br": true, "str_replace": true, "substr": true,
}

// reflectedServerKeys are $_SERVER entries the client controls.
var reflectedServerKeys = map[string]bool{
	"PHP_SELF": true, "REQUEST_URI": true, "QUERY_STRING": true, "PATH_INFO": true, "HTTP_REFERER": true,
	"HTTP_USER_AGENT": true,
}

// reflects reports whether an echoed expression is request input that reaches
// the output unescaped. Comparisons, other calls, and casts produce something
// else, so they stop the search: only concatenation, ??, the branches of a
// ternary, interpolation, and pass-through string functions carry input on.
func (c *checker) reflects(n *ts.Node) bool {
	switch n.Kind() {
	case "variable_name":
		return requestInput[c.variableName(n)]
	case "subscript_expression":
		base := n.NamedChild(0)
		if c.variableName(base) == "_SERVER" && n.NamedChildCount() > 1 {
			return reflectedServerKeys[c.stringValue(n.NamedChild(1))]
		}
		return base != nil && c.reflects(base)
	case "parenthesized_expression":
		return n.NamedChildCount() == 1 && c.reflects(n.NamedChild(0))
	case "sequence_expression": // echo $a, $b
		for _, part := range parsing.NamedChildren(n) {
			if c.reflects(part) {
				return true
			}
		}
	case "binary_expression":
		op := n.ChildByFieldName("operator")
		if op == nil || op.Kind() != "." && op.Kind() != "??" {
			return false
		}
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		return left != nil && c.reflects(left) || right != nil && c.reflects(right)
	case "conditional_expression":
		branch := n.ChildByFieldName("body")
		if branch == nil { // $a ?: $b returns the condition itself
			branch = n.ChildByFieldName("condition")
		}
		alt := n.ChildByFieldName("alternative")
		return branch != nil && c.reflects(branch) || alt != nil && c.reflects(alt)
	case "encapsed_string", "heredoc_body", "heredoc":
		for _, part := range parsing.NamedChildren(n) {
			if c.reflects(part) {
				return true
			}
		}
	case "function_call_expression":
		if _, name := c.callee(n); passThrough[name] {
			for _, arg := range arguments(n) {
				if c.reflects(arg) {
					return true
				}
			}
		}
	}
	return false
}

// tainted reports whether an expression contains a variable that no shell
// escape or numeric cast has seen, so a value can change what the command or
// path means. Constants such as __DIR__ are names, not variables.
func (c *checker) tainted(n *ts.Node) bool {
	found := false
	parsing.Walk(n, func(m *ts.Node) bool {
		switch m.Kind() {
		case "variable_name", "dynamic_variable_name":
			found = true
		case "function_call_expression":
			if _, name := c.callee(m); shellEscapes[name] {
				return false
			}
		case "cast_expression":
			if t := m.ChildByFieldName("type"); t != nil && numericCasts[strings.ToLower(c.tree.Text(t))] {
				return false
			}
		}
		return !found
	})
	return found
}

// built reports whether an expression is SQL assembled from values: a string
// that interpolates one ("... $id", "{$user->id}", heredoc) or a concatenation
// of a string with one. A plain variable is not reported: where it was built
// is out of sight.
func (c *checker) built(n *ts.Node) bool {
	switch n.Kind() {
	case "parenthesized_expression":
		return n.NamedChildCount() == 1 && c.built(n.NamedChild(0))
	case "encapsed_string", "heredoc":
		return c.interpolated(n)
	case "binary_expression":
		var hasString, hasValue bool
		if op := n.ChildByFieldName("operator"); op == nil || op.Kind() != "." {
			return false
		}
		c.concatOperands(n, &hasString, &hasValue)
		return hasString && hasValue
	}
	return false
}

// concatOperands classifies the operands of a . chain.
func (c *checker) concatOperands(n *ts.Node, hasString, hasValue *bool) {
	if n.Kind() == "binary_expression" {
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "." {
			for _, side := range []string{"left", "right"} {
				if operand := n.ChildByFieldName(side); operand != nil {
					c.concatOperands(operand, hasString, hasValue)
				}
			}
			return
		}
	}
	switch n.Kind() {
	case "string", "nowdoc":
		*hasString = true
	case "encapsed_string", "heredoc":
		*hasString = true
		if c.interpolated(n) {
			*hasValue = true
		}
	case "parenthesized_expression":
		if n.NamedChildCount() == 1 {
			c.concatOperands(n.NamedChild(0), hasString, hasValue)
		}
	default:
		if c.sqlValue(n) {
			*hasValue = true
		}
	}
}

// interpolated reports whether a double-quoted string or heredoc embeds a
// value that could carry SQL.
func (c *checker) interpolated(n *ts.Node) bool {
	parts := parsing.NamedChildren(n)
	if n.Kind() == "heredoc" {
		parts = nil
		if body := n.ChildByFieldName("value"); body != nil {
			parts = parsing.NamedChildren(body)
		}
	}
	for _, part := range parts {
		if part.Kind() != "string_content" && part.Kind() != "escape_sequence" && c.sqlValue(part) {
			return true
		}
	}
	return false
}

// sqlValue reports whether an operand of a query string can carry
// arbitrary SQL. Literals, constants, numeric casts, escaping calls, and
// names that hold identifiers by convention do not: $wpdb->prefix and other
// $wpdb properties are table names, and variables or properties named for a
// table, prefix, or placeholders ("IN ($placeholders)") build the query's
// structure, not its values.
func (c *checker) sqlValue(n *ts.Node) bool {
	switch n.Kind() {
	case "string", "nowdoc", "integer", "float", "boolean", "null", "name", "qualified_name",
		"class_constant_access_expression":
		return false
	case "encapsed_string", "heredoc":
		return c.interpolated(n)
	case "cast_expression":
		t := n.ChildByFieldName("type")
		return t == nil || !numericCasts[strings.ToLower(c.tree.Text(t))]
	case "function_call_expression", "member_call_expression", "scoped_call_expression":
		_, name := c.callee(n)
		return !sqlSafeCalls[name]
	case "variable_name":
		return !structuralName(c.variableName(n))
	case "member_access_expression", "nullsafe_member_access_expression", "scoped_property_access_expression":
		if c.variableName(n.ChildByFieldName("object")) == "wpdb" {
			return false
		}
		if name := n.ChildByFieldName("name"); name != nil && structuralName(c.tree.Text(name)) {
			return false
		}
	}
	return true
}

func structuralName(name string) bool {
	name = strings.ToLower(strings.TrimPrefix(name, "$"))
	return strings.Contains(name, "table") || strings.Contains(name, "prefix") || strings.Contains(name, "placeholder")
}

// mentionsPassword reports whether an expression reads a variable, property,
// or array key named for a password: $password, $user->passwd, $_POST['pwd'].
func (c *checker) mentionsPassword(n *ts.Node) bool {
	found := false
	parsing.Walk(n, func(m *ts.Node) bool {
		var name string
		switch m.Kind() {
		case "variable_name":
			name = c.variableName(m)
		case "member_access_expression", "nullsafe_member_access_expression":
			if field := m.ChildByFieldName("name"); field != nil {
				name = c.tree.Text(field)
			}
		case "subscript_expression":
			if m.NamedChildCount() > 1 {
				name = c.stringValue(m.NamedChild(1))
			}
		}
		name = strings.ToLower(name)
		if strings.Contains(name, "password") || strings.Contains(name, "passwd") || strings.Contains(name, "pwd") || name == "pass" {
			found = true
		}
		return !found
	})
	return found
}

// looseOperands are the literals that == and != convert surprisingly: null
// equals 0, "", and []; false equals "0"; "" equals null.
func (c *checker) looseOperand(n *ts.Node) bool {
	switch n.Kind() {
	case "boolean", "null":
		return true
	case "string", "encapsed_string":
		return n.NamedChildCount() == 0
	}
	return false
}

// checkComparison flags == and != against true, false, null, or "", where
// type juggling makes the result depend on the other operand's type.
func (c *checker) checkComparison(n *ts.Node) {
	op := n.ChildByFieldName("operator")
	if op == nil || op.Kind() != "==" && op.Kind() != "!=" && op.Kind() != "<>" {
		return
	}
	left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
	if left != nil && c.looseOperand(left) || right != nil && c.looseOperand(right) {
		c.report("php.loose-comparison", n)
	}
}

// checkGlobal flags global statements, except for WordPress's API globals.
func (c *checker) checkGlobal(n *ts.Node) {
	for _, v := range parsing.NamedChildren(n) {
		if v.Kind() == "variable_name" && !wordpressGlobals[c.variableName(v)] {
			c.report("php.global-variable", n)
			return
		}
	}
}

// checkCatch flags a catch block with no statements and no comment explaining
// why. Tests and exceptions named $ignored or $expected are intentional.
func (c *checker) checkCatch(n *ts.Node) {
	if c.isTest {
		return
	}
	name := c.variableName(n.ChildByFieldName("name"))
	if strings.HasPrefix(name, "ignored") || strings.HasPrefix(name, "expected") {
		return
	}
	if body := n.ChildByFieldName("body"); body != nil && body.NamedChildCount() == 0 {
		c.report("php.empty-catch", n)
	}
}

// untypedReturns are methods that cannot declare a return type.
var untypedReturns = map[string]bool{"__construct": true, "__destruct": true}

// checkTypes flags a named function or method with a parameter or return value
// that has no type declaration. Closures and arrow functions are left out,
// as their types usually follow from where they are passed. A type written in
// the function's docblock (@param Type $x, @return Type) counts, as static
// analyzers such as PHPStan and Psalm check those.
func (c *checker) checkTypes(fn *ts.Node) {
	name := fn.ChildByFieldName("name")
	if name == nil {
		return
	}
	doc := c.docblock(fn)
	documented := map[string]bool{}
	for _, m := range docParams.FindAllStringSubmatch(doc, -1) {
		documented[m[1]] = true
	}
	untyped := fn.ChildByFieldName("return_type") == nil && !untypedReturns[strings.ToLower(c.tree.Text(name))] && !docReturn.MatchString(doc)
	if params := fn.ChildByFieldName("parameters"); params != nil {
		for _, p := range parsing.NamedChildren(params) {
			switch p.Kind() {
			case "simple_parameter", "variadic_parameter", "property_promotion_parameter":
				pn := p.ChildByFieldName("name")
				if p.ChildByFieldName("type") == nil && (pn == nil || !documented[c.tree.Text(pn)]) {
					untyped = true
				}
			}
		}
	}
	if untyped {
		c.report("php.missing-type-declaration", fn)
	}
}

// docReturn matches a docblock's @return tag with a type.
var docReturn = regexp.MustCompile(`@return\s+\S`)

// docblock returns the /** */ comment right before a declaration, skipping
// attributes, or "" when there is none.
func (c *checker) docblock(fn *ts.Node) string {
	prev := fn.PrevNamedSibling()
	for prev != nil && prev.Kind() == "attribute_list" {
		prev = prev.PrevNamedSibling()
	}
	if prev == nil || prev.Kind() != "comment" {
		return ""
	}
	if text := c.tree.Text(prev); strings.HasPrefix(text, "/**") {
		return text
	}
	return ""
}

// docParams matches a docblock's @param tags that give a type, with any
// PHPDoc type including generics and unions, capturing the parameter name.
var docParams = regexp.MustCompile(`@param\s+[^\s$][^$\n]*?\s+(?:\.\.\.)?&?(\$\w+)`)

// declaresStrictTypes reports whether a declare statement is strict_types=1.
func (c *checker) declaresStrictTypes(n *ts.Node) bool {
	for _, d := range parsing.NamedChildren(n) {
		if d.Kind() == "declare_directive" && parsing.HasToken(d, "strict_types") {
			if v := d.NamedChild(0); v != nil && c.tree.Text(v) == "1" {
				return true
			}
		}
	}
	return false
}
