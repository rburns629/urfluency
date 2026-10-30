package ruby

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/parsing"
)

var (
	// evalMethods run a string as Ruby code.
	evalMethods = set("eval", "instance_eval", "class_eval", "module_eval")
	// dispatchMethods call a method or look up a class by a name given at runtime.
	dispatchMethods = set("send", "public_send", "__send__", "const_get")
	// shellMethods run one string argument through /bin/sh.
	shellMethods = set("system", "exec", "spawn")
	open3Methods = set("popen3", "popen2", "popen2e", "capture2", "capture2e", "capture3",
		"pipeline", "pipeline_r", "pipeline_w", "pipeline_rw", "pipeline_start")
	// sqlMethods take a SQL fragment in ActiveRecord or its connection.
	sqlMethods = set("where", "not", "rewhere", "having", "order", "reorder", "group", "joins", "from", "select",
		"pluck", "find_by", "find_by_sql", "count_by_sql", "exists?", "update_all", "delete_all", "lock",
		"execute", "exec_query", "exec_insert", "exec_update", "exec_delete",
		"select_all", "select_one", "select_value", "select_values", "select_rows")
	// connectionMethods are sqlMethods called on a connection. Migrations call
	// execute without one, almost always with table names from code.
	connectionMethods = set("execute", "exec_query", "exec_insert", "exec_update", "exec_delete",
		"select_all", "select_one", "select_value", "select_values", "select_rows")
	// The safe sets are methods whose result can be interpolated: they escape
	// or convert the value, or name schema objects from code.
	sqlSafe = set("table_name", "quoted_table_name", "primary_key", "quote", "quote_column_name", "quote_table_name",
		"sanitize_sql", "sanitize_sql_array", "sanitize_sql_for_conditions", "sanitize_sql_like", "to_i", "to_f")
	htmlSafe = set("h", "html_escape", "escape_html", "escapeHTML", "escape_once", "sanitize", "strip_tags",
		"json_escape", "t", "translate", "link_to", "button_to", "mail_to", "content_tag", "image_tag", "render", "to_i")
	shellSafe = set("shellescape", "shelljoin", "escape", "to_i")
	// bindingDebuggers and bareDebuggers open a debugger console: binding.pry,
	// binding.irb, byebug.
	bindingDebuggers = set("pry", "irb", "break", "remote_pry", "pry_remote")
	bareDebuggers    = set("byebug", "debugger", "remote_byebug")
	// statementParents hold a list of statements, where a bare name is a call.
	statementParents = set("program", "body_statement", "block_body", "then", "else", "do", "begin", "ensure",
		"parenthesized_statements")
	// coreClasses are Ruby's built-in classes and modules.
	coreClasses = set("String", "Array", "Hash", "Integer", "Float", "Numeric", "Symbol", "Object", "BasicObject",
		"NilClass", "TrueClass", "FalseClass", "Kernel", "Comparable", "Enumerable", "Module", "Class", "Proc",
		"Range", "Regexp", "Time", "IO", "File", "Struct", "Exception")
	monkeyPatchMethods = set("class_eval", "class_exec", "module_eval", "include", "prepend")
	// builtinGlobals are Ruby's own global variables, which programs may assign.
	builtinGlobals = set("$stdout", "$stderr", "$stdin", "$VERBOSE", "$DEBUG", "$PROGRAM_NAME", "$0", "$LOAD_PATH",
		"$:", "$LOADED_FEATURES", `$"`, "$,", "$/", `$\`, "$;", "$<", "$>", "$FILENAME", "$SAFE", "$KCODE")
)

func (c *checker) visit(n *ts.Node) bool {
	switch n.Kind() {
	case "call":
		c.checkCall(n)
	case "identifier":
		if bareDebuggers[c.tree.Text(n)] && isStatement(n) {
			c.report("ruby.debugging-call", n)
		}
	case "subshell":
		if c.interpolated(n, shellSafe) {
			c.report("ruby.command-built", n)
		}
	case "scope_resolution":
		if name := n.ChildByFieldName("name"); name != nil && c.tree.Text(name) == "VERIFY_NONE" {
			if scope := n.ChildByFieldName("scope"); scope != nil && strings.TrimPrefix(c.tree.Text(scope), "::") == "OpenSSL::SSL" {
				c.report("ruby.ssl-verify-none", n)
			}
		}
	case "rescue":
		c.checkRescue(n)
	case "rescue_modifier":
		if !c.isTest {
			c.report("ruby.rescue-modifier", n)
		}
	case "assignment", "operator_assignment":
		if left := n.ChildByFieldName("left"); left != nil && left.Kind() == "global_variable" && !builtinGlobals[c.tree.Text(left)] && !c.isTest {
			c.report("ruby.global-variable", n)
		}
	case "class", "module":
		c.checkReopen(n)
	}
	return true
}

func (c *checker) checkCall(call *ts.Node) {
	receiver, name := c.callee(call)
	args := arguments(call)
	if monkeyPatchMethods[name] && receiver != nil && coreClasses[strings.TrimPrefix(c.tree.Text(receiver), "::")] {
		c.report("ruby.core-class-reopened", call)
	}
	switch {
	case evalMethods[name]:
		if len(args) > 0 && (c.built(args[0], nil) || c.fromParams(args[0])) {
			c.report("ruby.eval-built", call)
		}
	case dispatchMethods[name]:
		if len(args) > 0 && c.fromParams(args[0]) {
			c.report("ruby.dynamic-dispatch-from-params", call)
		}
	case name == "constantize" || name == "safe_constantize":
		if receiver != nil && c.fromParams(receiver) {
			c.report("ruby.dynamic-dispatch-from-params", call)
		}
	case (name == "load" || name == "unsafe_load") && c.receiverIs(receiver, "YAML", "Psych"):
		c.report("ruby.unsafe-deserialization", call)
	case (name == "load" || name == "restore") && c.receiverIs(receiver, "Marshal"):
		// Marshal.load(Marshal.dump(x)) is the deep-copy idiom on trusted data.
		if len(args) == 0 || !c.isCall(args[0], "Marshal", "dump") {
			c.report("ruby.unsafe-deserialization", call)
		}
	case shellMethods[name] && (receiver == nil || c.receiverIs(receiver, "Kernel", "Process")),
		name == "popen" && c.receiverIs(receiver, "IO"),
		open3Methods[name] && c.receiverIs(receiver, "Open3"):
		c.checkCommand(call, args)
	case sqlMethods[name]:
		if connectionMethods[name] && receiver == nil {
			return
		}
		if len(args) > 0 && c.built(args[0], sqlSafe) {
			c.report("ruby.sql-string-built", call)
		}
	case name == "html_safe":
		if receiver != nil && (c.built(receiver, htmlSafe) || c.fromParams(receiver)) {
			c.report("ruby.html-safe-built", call)
		}
	case name == "raw" && receiver == nil:
		if len(args) == 1 && (c.built(args[0], htmlSafe) || c.fromParams(args[0])) {
			c.report("ruby.html-safe-built", call)
		}
	case name == "open" && (receiver == nil || c.receiverIs(receiver, "Kernel")):
		if len(args) > 0 && !c.literalPath(args[0]) {
			c.report("ruby.kernel-open", call)
		}
	case bindingDebuggers[name] && receiver != nil && receiver.Kind() == "identifier" && c.tree.Text(receiver) == "binding",
		bareDebuggers[name] && receiver == nil:
		c.report("ruby.debugging-call", call)
	case (name == "p" || name == "pp") && receiver == nil:
		c.checkPrint(call, args)
	}
}

// isStatement reports whether a bare name stands as a statement, where Ruby
// calls it as a method: on its own line or as the body of a modifier if.
func isStatement(n *ts.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "if_modifier", "unless_modifier":
		return isField(parent, "body", n)
	}
	return statementParents[parent.Kind()]
}

// checkCommand flags a command given as one string built from values, which
// Ruby passes to the shell. Separate arguments (system("ls", dir)) run the
// program directly and are not reported. An environment hash and keyword
// options do not count as arguments.
func (c *checker) checkCommand(call *ts.Node, args []*ts.Node) {
	var command []*ts.Node
	for _, arg := range args {
		if arg.Kind() != "hash" && arg.Kind() != "pair" {
			command = append(command, arg)
		}
	}
	if len(command) == 1 && c.built(command[0], shellSafe) {
		c.report("ruby.command-built", call)
	}
}

// checkPrint flags p and pp, which print inspect output for debugging, in
// application and library code. A block marks an HTML builder's p element
// (Phlex, Markaby), not a print.
func (c *checker) checkPrint(call *ts.Node, args []*ts.Node) {
	if c.isTest || len(args) == 0 || call.ChildByFieldName("block") != nil || !parsing.InDir(c.path, "app", "lib") {
		return
	}
	c.report("ruby.debugging-call", call)
}

// checkRescue flags rescue Exception that does not re-raise, and a rescue with
// no statements and no comment explaining why. Tests and exceptions named with
// a leading underscore are intentional.
func (c *checker) checkRescue(n *ts.Node) {
	if c.isTest {
		return
	}
	body := n.ChildByFieldName("body")
	if exceptions := n.ChildByFieldName("exceptions"); exceptions != nil {
		for _, e := range parsing.NamedChildren(exceptions) {
			if strings.TrimPrefix(c.tree.Text(e), "::") == "Exception" && !c.reraises(body) {
				c.report("ruby.rescue-exception", n)
				break
			}
		}
	}
	if body != nil && body.NamedChildCount() > 0 {
		return
	}
	for _, child := range parsing.NamedChildren(n) {
		switch child.Kind() {
		case "comment":
			return
		case "exception_variable":
			if name := child.NamedChild(0); name != nil && strings.HasPrefix(c.tree.Text(name), "_") {
				return
			}
		}
	}
	c.report("ruby.empty-rescue", n)
}

// reraises reports whether a rescue body calls raise or fail.
func (c *checker) reraises(body *ts.Node) bool {
	if body == nil {
		return false
	}
	found := false
	parsing.Walk(body, func(n *ts.Node) bool {
		switch n.Kind() {
		case "identifier":
			found = found || c.tree.Text(n) == "raise" || c.tree.Text(n) == "fail"
		case "call":
			if receiver, name := c.callee(n); receiver == nil && (name == "raise" || name == "fail") {
				found = true
			}
		}
		return !found
	})
	return found
}

// checkReopen flags a top-level class or module that reopens one of Ruby's
// own. Inside a namespace (module App; class String) it defines a new constant.
func (c *checker) checkReopen(n *ts.Node) {
	name := n.ChildByFieldName("name")
	if name == nil || n.ChildByFieldName("superclass") != nil || !coreClasses[strings.TrimPrefix(c.tree.Text(name), "::")] {
		return
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "class", "module", "singleton_class":
			return
		}
	}
	c.report("ruby.core-class-reopened", n)
}

// isCall reports whether n is receiver.method(...).
func (c *checker) isCall(n *ts.Node, receiver, method string) bool {
	if n.Kind() != "call" {
		return false
	}
	r, name := c.callee(n)
	return name == method && c.receiverIs(r, receiver)
}

// fromParams reports whether an expression reads request parameters (params,
// request.params), the input a client controls.
func (c *checker) fromParams(n *ts.Node) bool {
	found := false
	parsing.Walk(n, func(n *ts.Node) bool {
		found = found || (n.Kind() == "identifier" && c.tree.Text(n) == "params")
		return !found
	})
	return found
}

// literalPath reports whether an argument to open cannot start with "|": a
// constant, a path builder such as File.join, or a string that begins with
// literal text other than "|".
func (c *checker) literalPath(n *ts.Node) bool {
	switch n.Kind() {
	case "constant", "scope_resolution":
		return true
	case "string":
		if !c.interpolated(n, nil) {
			return true
		}
		first := n.NamedChild(0)
		return first != nil && first.Kind() == "string_content" && !strings.HasPrefix(c.tree.Text(first), "|")
	case "call":
		_, name := c.callee(n)
		return name == "join" || name == "expand_path"
	}
	return false
}

// built reports whether an expression is a string assembled from values: an
// interpolated string or heredoc, or a + concatenation of a string literal
// with a value. Constants, literals, and calls in safe (escaping and
// converting methods) do not count as values.
func (c *checker) built(n *ts.Node, safe map[string]bool) bool {
	switch n.Kind() {
	case "parenthesized_statements":
		return n.NamedChildCount() == 1 && c.built(n.NamedChild(0), safe)
	case "string":
		return c.interpolated(n, safe)
	case "heredoc_beginning":
		body := c.heredocs[n.StartByte()]
		return body != nil && c.interpolated(body, safe)
	case "chained_string":
		for _, part := range parsing.NamedChildren(n) {
			if c.interpolated(part, safe) {
				return true
			}
		}
	case "binary":
		if op := n.ChildByFieldName("operator"); op == nil || op.Kind() != "+" {
			return false
		}
		var hasString, hasValue bool
		c.concatOperands(n, safe, &hasString, &hasValue)
		return hasString && hasValue
	}
	return false
}

// concatOperands classifies the operands of a + chain.
func (c *checker) concatOperands(n *ts.Node, safe map[string]bool, hasString, hasValue *bool) {
	if n.Kind() == "binary" {
		if op := n.ChildByFieldName("operator"); op != nil && op.Kind() == "+" {
			for _, side := range []string{"left", "right"} {
				if operand := n.ChildByFieldName(side); operand != nil {
					c.concatOperands(operand, safe, hasString, hasValue)
				}
			}
			return
		}
	}
	switch n.Kind() {
	case "string", "heredoc_beginning", "chained_string":
		*hasString = true
		if c.built(n, safe) {
			*hasValue = true
		}
	default:
		if !c.safeValue(n, safe) {
			*hasValue = true
		}
	}
}

// interpolated reports whether a string, subshell, or heredoc body
// interpolates a value (#{...}) that is not safe.
func (c *checker) interpolated(lit *ts.Node, safe map[string]bool) bool {
	for _, part := range parsing.NamedChildren(lit) {
		if part.Kind() != "interpolation" {
			continue
		}
		for _, expr := range parsing.NamedChildren(part) {
			if !c.safeValue(expr, safe) {
				return true
			}
		}
	}
	return false
}

// safeValue reports whether an expression is a constant, a literal, or a call
// to a method in safe.
func (c *checker) safeValue(n *ts.Node, safe map[string]bool) bool {
	switch n.Kind() {
	case "constant", "integer", "float", "simple_symbol", "true", "false", "nil", "comment":
		return true
	case "scope_resolution":
		name := n.ChildByFieldName("name")
		return name != nil && name.Kind() == "constant"
	case "string":
		return !c.interpolated(n, safe)
	case "parenthesized_statements":
		return n.NamedChildCount() == 1 && c.safeValue(n.NamedChild(0), safe)
	case "identifier":
		return safe[c.tree.Text(n)]
	case "call":
		_, name := c.callee(n)
		return safe[name]
	}
	return false
}
