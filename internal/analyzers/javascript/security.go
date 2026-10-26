package javascript

import (
	"regexp"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/internal/analysis"
	"github.com/rburns629/urfluency/internal/parsing"
)

// Frameworks implements analysis.FrameworkDetector for both analyzers.
func (JavaScript) Frameworks(f analysis.File) []string { return detect(f, false) }
func (TypeScript) Frameworks(f analysis.File) []string { return detect(f, true) }

func detect(f analysis.File, typescript bool) []string {
	tree, err := parsing.Parse(grammarFor(f.Path, typescript), f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var out []string
	for fw := range frameworksOf(tree, f.Path) {
		if fw != "child_process" {
			out = append(out, fw)
		}
	}
	return out
}

// frameworksOf returns framework packs (and child_process) a file imports. JSX
// files count as React even without an import, since the modern JSX transform
// needs none.
func frameworksOf(t *parsing.Tree, path string) map[string]bool {
	found := map[string]bool{}
	add := func(module string) {
		module = strings.Trim(module, "'\"`")
		switch {
		case module == "react" || module == "react-dom" || strings.HasPrefix(module, "react-dom/") ||
			module == "next" || strings.HasPrefix(module, "next/") || module == "preact":
			found["react"] = true
		case strings.HasPrefix(module, "@angular/"):
			found["angular"] = true
		case module == "fastify":
			found["fastify"] = true
		case module == "express":
			found["express"] = true
		case module == "child_process" || module == "node:child_process":
			found["child_process"] = true
		}
	}
	parsing.Walk(t.Root(), func(n *ts.Node) bool {
		switch n.Kind() {
		case "import_statement":
			if src := n.ChildByFieldName("source"); src != nil {
				add(t.Text(src))
			}
		case "call_expression":
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn != nil && args != nil && t.Text(fn) == "require" && args.NamedChildCount() == 1 && args.NamedChild(0).Kind() == "string" {
				add(t.Text(args.NamedChild(0)))
			}
		}
		return true
	})
	if strings.HasSuffix(path, ".jsx") || strings.HasSuffix(path, ".tsx") {
		found["react"] = true
	}
	return found
}

var (
	sqlMethods     = map[string]bool{"query": true, "execute": true, "raw": true, "$queryRawUnsafe": true, "$executeRawUnsafe": true, "unsafe": true}
	routeShortcuts = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	bodyRoutes     = map[string]bool{"post": true, "put": true, "patch": true}
	hookName       = regexp.MustCompile(`^use[A-Z0-9]`)
)

// firstArg returns a call's first argument when the call has an argument list
// (tagged templates such as sql`...` do not), or nil.
func firstArg(call *ts.Node) (*ts.Node, *ts.Node) {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.Kind() != "arguments" || args.NamedChildCount() == 0 {
		return nil, args
	}
	return args.NamedChild(0), args
}

// isBuiltString reports a template with substitutions or + concatenation.
func isBuiltString(n *ts.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "template_string":
		for _, c := range parsing.NamedChildren(n) {
			if c.Kind() == "template_substitution" {
				return true
			}
		}
	case "binary_expression":
		return parsing.HasToken(n, "+")
	}
	return false
}

func isPlainString(n *ts.Node) bool {
	return n != nil && (n.Kind() == "string" || (n.Kind() == "template_string" && !isBuiltString(n)))
}

// memberProperty returns the property name of a member call such as a.b(), or "".
func (c *checker) memberProperty(call *ts.Node) (string, *ts.Node) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" {
		return "", nil
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil {
		return "", nil
	}
	return c.tree.Text(prop), fn.ChildByFieldName("object")
}

// effectHooks run after rendering; without a dependency array they run every time.
var effectHooks = map[string]bool{"useEffect": true, "useLayoutEffect": true, "React.useEffect": true, "React.useLayoutEffect": true}

// checkCallSecurity applies call-based security and framework rules.
func (c *checker) checkCallSecurity(call *ts.Node) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return
	}
	callee := c.tree.Text(fn)
	prop, object := c.memberProperty(call)
	first, args := firstArg(call)

	switch {
	case callee == "eval":
		c.report("js.eval", call)
	case (callee == "setTimeout" || callee == "setInterval") && first != nil && (first.Kind() == "string" || first.Kind() == "template_string"):
		c.report("js.eval", call)
	case c.frameworks["child_process"] && (callee == "exec" || callee == "execSync" || prop == "exec" || prop == "execSync") && isBuiltString(first):
		c.report("js.child-process-injection", call)
	case sqlMethods[prop] && isBuiltString(first):
		c.report("js.sql-string-built", call)
	case prop == "insertAdjacentHTML" && args != nil && args.NamedChildCount() > 1 && !isPlainString(args.NamedChild(1)):
		c.report("js.html-injection", call)
	case callee == "document.write" && first != nil && !isPlainString(first):
		c.report("js.html-injection", call)
	}

	if c.frameworks["react"] && c.isConditionalHook(call, fn, prop, object) {
		c.report("react.hook-in-condition", call)
	}
	if c.frameworks["react"] && effectHooks[callee] && args != nil && args.NamedChildCount() == 1 {
		c.report("react.effect-without-deps", call)
	}
	if c.frameworks["react"] && prop == "map" {
		c.checkListKeys(first)
	}
	if c.frameworks["angular"] && strings.HasPrefix(prop, "bypassSecurityTrust") {
		c.report("angular.bypass-security-trust", call)
	}
	if c.frameworks["angular"] && prop == "subscribe" && insideSubscribe(c, call) {
		c.report("angular.nested-subscribe", call)
	}
	if c.frameworks["fastify"] && routeShortcuts[prop] && args != nil {
		c.checkFastifyRoute(call, prop, args)
	}
	if c.frameworks["express"] && (prop == "send" || prop == "write") && first != nil {
		text := c.tree.Text(first)
		if strings.HasPrefix(text, "req.query") || strings.HasPrefix(text, "req.params") || strings.HasPrefix(text, "req.body") {
			c.report("express.reflected-input", call)
		}
	}
}

// checkNodeSecurity applies rules on assignments, constructors, objects, and JSX.
func (c *checker) checkNodeSecurity(n *ts.Node) {
	switch n.Kind() {
	case "new_expression":
		if ctor := n.ChildByFieldName("constructor"); ctor != nil && c.tree.Text(ctor) == "Function" {
			c.report("js.eval", n)
		}
	case "assignment_expression":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if left == nil || right == nil {
			return
		}
		if left.Kind() == "member_expression" {
			prop := c.tree.Text(left.ChildByFieldName("property"))
			if (prop == "innerHTML" || prop == "outerHTML") && !isPlainString(right) {
				c.report("js.html-injection", n)
			}
		}
		if c.tree.Text(left) == "process.env.NODE_TLS_REJECT_UNAUTHORIZED" && strings.Trim(c.tree.Text(right), "'\"`") == "0" {
			c.report("js.tls-verify-disabled", n)
		}
	case "pair":
		key, value := n.ChildByFieldName("key"), n.ChildByFieldName("value")
		if key != nil && value != nil && strings.Trim(c.tree.Text(key), "'\"") == "rejectUnauthorized" && value.Kind() == "false" {
			c.report("js.tls-verify-disabled", n)
		}
	case "jsx_attribute":
		if c.frameworks["react"] && n.NamedChildCount() > 0 && c.tree.Text(n.NamedChild(0)) == "dangerouslySetInnerHTML" {
			c.report("react.dangerously-set-inner-html", n)
		}
	}
}

// isConditionalHook reports a hook call nested under a condition or loop before
// reaching its enclosing function.
func (c *checker) isConditionalHook(call, fn *ts.Node, prop string, object *ts.Node) bool {
	name := c.tree.Text(fn)
	if prop != "" {
		if object == nil || c.tree.Text(object) != "React" {
			return false
		}
		name = prop
	}
	if !hookName.MatchString(name) {
		return false
	}
	for p := call.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "function_declaration", "function_expression", "arrow_function", "method_definition", "function":
			return false
		case "if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement",
			"ternary_expression", "switch_case":
			return true
		case "binary_expression":
			if parsing.HasToken(p, "&&", "||", "??") {
				return true
			}
		}
	}
	return false
}

// checkListKeys inspects items.map(cb) where cb returns JSX, flagging a missing
// key or a key taken from the callback's index parameter.
func (c *checker) checkListKeys(callback *ts.Node) {
	if callback == nil || (callback.Kind() != "arrow_function" && callback.Kind() != "function_expression") {
		return
	}
	root := returnedJSX(callback)
	if root == nil {
		return
	}
	var attrs []*ts.Node
	switch root.Kind() {
	case "jsx_element":
		if open := root.ChildByFieldName("open_tag"); open != nil {
			attrs = parsing.NamedChildren(open)
		}
	case "jsx_self_closing_element":
		attrs = parsing.NamedChildren(root)
	}
	index := ""
	if params := callback.ChildByFieldName("parameters"); params != nil && params.NamedChildCount() > 1 {
		index = c.tree.Text(params.NamedChild(1))
	}
	for _, a := range attrs {
		if a.Kind() != "jsx_attribute" || a.NamedChildCount() == 0 || c.tree.Text(a.NamedChild(0)) != "key" {
			continue
		}
		if index != "" && a.NamedChildCount() > 1 && strings.Trim(c.tree.Text(a.NamedChild(1)), "{} ") == index {
			c.report("react.index-as-key", a)
		}
		return
	}
	c.report("react.missing-key", root)
}

func returnedJSX(fn *ts.Node) *ts.Node {
	body := fn.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	if body.Kind() == "statement_block" {
		for _, stmt := range parsing.NamedChildren(body) {
			if stmt.Kind() == "return_statement" && stmt.NamedChildCount() > 0 {
				body = stmt.NamedChild(0)
				break
			}
		}
	}
	for body != nil && body.Kind() == "parenthesized_expression" && body.NamedChildCount() > 0 {
		body = body.NamedChild(0)
	}
	if body != nil && (body.Kind() == "jsx_element" || body.Kind() == "jsx_self_closing_element") {
		return body
	}
	return nil
}

func insideSubscribe(c *checker, call *ts.Node) bool {
	for p := call.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "call_expression" {
			if prop, _ := c.memberProperty(p); prop == "subscribe" {
				return true
			}
		}
	}
	return false
}

// checkFastifyRoute flags body routes without an options object and async
// handlers that call reply.send() without returning it.
func (c *checker) checkFastifyRoute(call *ts.Node, method string, args *ts.Node) {
	list := parsing.NamedChildren(args)
	if len(list) == 0 || (list[0].Kind() != "string" && list[0].Kind() != "template_string") {
		return
	}
	handler := list[len(list)-1]
	if handler.Kind() != "arrow_function" && handler.Kind() != "function_expression" {
		return
	}
	if bodyRoutes[method] && len(list) == 2 {
		c.report("fastify.route-without-schema", call)
	}
	if !parsing.HasToken(handler, "async") {
		return
	}
	parsing.Walk(handler.ChildByFieldName("body"), func(n *ts.Node) bool {
		switch n.Kind() {
		case "arrow_function", "function_expression", "function_declaration":
			return false
		case "expression_statement":
			if n.NamedChildCount() > 0 && n.NamedChild(0).Kind() == "call_expression" {
				if prop, obj := c.memberProperty(n.NamedChild(0)); prop == "send" && obj != nil && c.tree.Text(obj) == "reply" {
					c.report("fastify.async-reply-send", n)
				}
			}
		}
		return true
	})
}
