package python

import (
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/parsing"
)

// frameworkModules maps top-level import names to framework packs.
var frameworkModules = map[string]string{"django": "django", "fastapi": "fastapi", "flask": "flask"}

// Frameworks implements analysis.FrameworkDetector.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.Python, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var out []string
	for fw := range importedFrameworks(tree) {
		out = append(out, fw)
	}
	return out
}

// importedFrameworks returns the framework packs a module imports at top level.
func importedFrameworks(t *parsing.Tree) map[string]bool {
	found := map[string]bool{}
	for _, stmt := range parsing.NamedChildren(t.Root()) {
		var names []*ts.Node
		switch stmt.Kind() {
		case "import_from_statement":
			names = append(names, stmt.ChildByFieldName("module_name"))
		case "import_statement":
			names = parsing.NamedChildren(stmt)
		}
		for _, n := range names {
			// Relative imports (from .models import X) are local modules.
			if n == nil || strings.HasPrefix(t.Text(n), ".") {
				continue
			}
			top := strings.SplitN(t.Text(n), ".", 2)[0]
			top = strings.Fields(top + " ")[0]
			if fw, ok := frameworkModules[top]; ok {
				found[fw] = true
			}
		}
	}
	return found
}

var (
	sqlMethods    = map[string]bool{"execute": true, "executemany": true, "executescript": true, "raw": true, "extra": true}
	unsafeLoads   = map[string]bool{"pickle.loads": true, "pickle.load": true, "cPickle.loads": true, "marshal.loads": true, "marshal.load": true, "dill.loads": true, "yaml.unsafe_load": true}
	shellCalls    = map[string]bool{"os.system": true, "os.popen": true, "commands.getoutput": true}
	subprocesses  = map[string]bool{"subprocess.run": true, "subprocess.call": true, "subprocess.Popen": true, "subprocess.check_output": true, "subprocess.check_call": true}
	textFields    = []string{"CharField", "TextField", "SlugField", "EmailField", "URLField"}
	blockingCalls = map[string]bool{"time.sleep": true, "requests.get": true, "requests.post": true, "requests.put": true, "requests.patch": true, "requests.delete": true, "requests.request": true}
	routeMethods  = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	bodyMethods   = map[string]bool{"post": true, "put": true, "patch": true}
)

// calleeName returns the dotted name of a call's function, such as "cursor.execute".
func (c *checker) calleeName(call *ts.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	return c.tree.Text(fn)
}

func lastPart(name string) string { return name[strings.LastIndex(name, ".")+1:] }

// positionalArgs returns a call's positional arguments.
func positionalArgs(call *ts.Node) []*ts.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []*ts.Node
	for _, a := range parsing.NamedChildren(args) {
		switch a.Kind() {
		case "keyword_argument", "list_splat", "dictionary_splat", "comment":
		default:
			out = append(out, a)
		}
	}
	return out
}

// keyword returns the source text of a keyword argument's value, or "".
func (c *checker) keyword(call *ts.Node, name string) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for _, a := range parsing.NamedChildren(args) {
		if a.Kind() == "keyword_argument" && c.tree.Text(a.ChildByFieldName("name")) == name {
			return c.tree.Text(a.ChildByFieldName("value"))
		}
	}
	return ""
}

// isBuiltString reports whether an expression builds a string from values: an
// f-string with interpolation, % formatting, + concatenation, or .format().
func (c *checker) isBuiltString(n *ts.Node) bool {
	switch n.Kind() {
	case "string":
		for _, child := range parsing.NamedChildren(n) {
			if child.Kind() == "interpolation" {
				return true
			}
		}
	case "binary_operator":
		if parsing.HasToken(n, "%", "+") {
			left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
			return isStringNode(left) || isStringNode(right) || c.isBuiltString(left)
		}
	case "call":
		fn := n.ChildByFieldName("function")
		return fn != nil && fn.Kind() == "attribute" && c.tree.Text(fn.ChildByFieldName("attribute")) == "format" &&
			isStringNode(fn.ChildByFieldName("object"))
	}
	return false
}

func isStringNode(n *ts.Node) bool {
	return n != nil && (n.Kind() == "string" || n.Kind() == "concatenated_string")
}

// isDynamic reports whether an argument is anything but a plain string literal.
func (c *checker) isDynamic(n *ts.Node) bool {
	return !isStringNode(n) || c.isBuiltString(n)
}

// checkCallSecurity applies call-based security and framework rules.
func (c *checker) checkCallSecurity(call *ts.Node) {
	name := c.calleeName(call)
	args := positionalArgs(call)
	first := func() *ts.Node {
		if len(args) > 0 {
			return args[0]
		}
		return nil
	}()

	switch {
	case sqlMethods[lastPart(name)] && strings.Contains(name, ".") && first != nil && c.isBuiltString(first):
		c.report("py.sql-string-built", call)
	case (name == "eval" || name == "exec") && first != nil && c.isDynamic(first):
		c.report("py.eval-exec", call)
	case unsafeLoads[name]:
		c.report("py.unsafe-deserialization", call)
	case name == "yaml.load" && !strings.Contains(c.keyword(call, "Loader"), "SafeLoader") &&
		!(len(args) > 1 && strings.Contains(c.tree.Text(args[1]), "SafeLoader")):
		c.report("py.unsafe-deserialization", call)
	case shellCalls[name] && first != nil && c.isDynamic(first):
		c.report("py.shell-injection", call)
	case subprocesses[name] && c.keyword(call, "shell") == "True" && first != nil && c.isDynamic(first):
		c.report("py.shell-injection", call)
	}
	if c.keyword(call, "verify") == "False" {
		c.report("py.tls-verify-disabled", call)
	}
	if c.frameworks["django"] && c.keyword(call, "null") == "True" {
		for _, field := range textFields {
			if lastPart(name) == field {
				c.report("django.null-on-text-field", call)
			}
		}
	}
	if c.frameworks["flask"] && lastPart(name) == "run" && c.keyword(call, "debug") == "True" {
		c.report("flask.debug-mode", call)
	}
	// Chained calls such as Model.objects.filter(...).delete() are reported once,
	// at the query.
	if c.frameworks["django"] && !c.isTest && strings.Contains(name, ".objects.") && !strings.Contains(name, "(") && insideLoopBody(call) {
		c.report("django.query-in-loop", call)
	}
	if c.frameworks["fastapi"] && blockingCalls[name] && insideAsyncFunction(call) {
		c.report("fastapi.blocking-call-in-async", call)
	}
}

func insideAsyncFunction(n *ts.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_definition" {
			return parsing.HasToken(p, "async")
		}
	}
	return false
}

// insideLoopBody reports whether n runs once per iteration of a loop in the same
// function: in a for or while body, or a comprehension's element or condition. A
// loop's iterable is evaluated once, so a query there is not per iteration.
func insideLoopBody(n *ts.Node) bool {
	child := n
	for p := n.Parent(); p != nil; child, p = p, p.Parent() {
		switch p.Kind() {
		case "function_definition", "lambda", "class_definition":
			return false
		case "while_statement":
			return true
		case "for_statement":
			if !sameNode(p.ChildByFieldName("right"), child) {
				return true
			}
		case "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
			if child.Kind() != "for_in_clause" {
				return true
			}
		}
	}
	return false
}

func sameNode(a, b *ts.Node) bool {
	return a != nil && b != nil && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}

// checkDecorated applies decorator-based framework rules.
func (c *checker) checkDecorated(n *ts.Node) {
	def := n.ChildByFieldName("definition")
	for _, d := range parsing.NamedChildren(n) {
		if d.Kind() != "decorator" {
			continue
		}
		text := c.tree.Text(d)
		if strings.Contains(text, "csrf_exempt") {
			c.report("django.csrf-exempt", d)
		}
		if !c.frameworks["fastapi"] || def == nil || def.Kind() != "function_definition" || d.NamedChildCount() == 0 {
			continue
		}
		call := d.NamedChild(0)
		if call.Kind() != "call" {
			continue
		}
		method := lastPart(c.calleeName(call))
		if !routeMethods[method] || !bodyMethods[method] {
			continue
		}
		params := def.ChildByFieldName("parameters")
		if params == nil {
			continue
		}
		for _, p := range parsing.NamedChildren(params) {
			typ := p.ChildByFieldName("type")
			if typ == nil {
				continue
			}
			t := c.tree.Text(typ)
			if t == "dict" || t == "Any" || strings.HasPrefix(t, "Dict") || strings.HasPrefix(t, "dict[") {
				c.report("fastapi.untyped-body", p)
			}
		}
	}
}

// checkDebugSetting flags DEBUG = True at module level in Django settings files.
func (c *checker) checkDebugSetting(assign *ts.Node) {
	base := path.Base(c.path)
	isSettings := strings.HasPrefix(base, "settings") || parsing.InDir(c.path, "settings")
	if !isSettings {
		return
	}
	left, right := assign.ChildByFieldName("left"), assign.ChildByFieldName("right")
	if left == nil || right == nil || c.tree.Text(left) != "DEBUG" || right.Kind() != "true" {
		return
	}
	if stmt := assign.Parent(); stmt != nil && stmt.Parent() != nil && stmt.Parent().Kind() == "module" {
		c.report("django.debug-true", assign)
	}
}
