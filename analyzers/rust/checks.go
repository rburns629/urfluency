package rust

import (
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/parsing"
)

// lockMethods return a poisoned-lock Result whose unwrap is the accepted idiom.
var lockMethods = set("lock", "read", "write")

// checkUnwrap flags .unwrap() and .expect("...") where panicking is not
// acceptable. expect must take a string literal, so parser methods named expect
// are not mistaken for Option::expect.
func (c *checker) checkUnwrap(call *ts.Node) {
	name, receiver, args := method(c.tree, call)
	if args == nil {
		return
	}
	argv := parsing.NamedChildren(args)
	switch {
	case name == "unwrap" && len(argv) == 0:
	case name == "expect" && len(argv) == 1 && argv[0].Kind() == "string_literal":
	default:
		return
	}
	if receiver != nil && receiver.Kind() == "call_expression" {
		if inner, _, _ := method(c.tree, receiver); lockMethods[inner] {
			return
		}
	}
	if !c.mayPanic(call, true) {
		c.report("rust.unwrap-in-library", call)
	}
}

// checkPanic flags panic!, todo!, and unimplemented! where panicking is not
// acceptable. Only panic! is excused by a # Panics doc section; placeholders
// are unfinished code.
func (c *checker) checkPanic(n *ts.Node) {
	m := n.ChildByFieldName("macro")
	if m == nil {
		return
	}
	if m.Kind() == "scoped_identifier" {
		m = m.ChildByFieldName("name")
	}
	if m == nil {
		return
	}
	switch c.tree.Text(m) {
	case "panic":
		if !c.mayPanic(n, true) {
			c.report("rust.panic-in-library", n)
		}
	case "todo", "unimplemented":
		if !c.mayPanic(n, false) {
			c.report("rust.panic-in-library", n)
		}
	}
}

// hasSafetyComment reports whether a SAFETY: comment is on the unsafe block's
// line, its statement's line, or the comment lines directly above either.
func (c *checker) hasSafetyComment(n *ts.Node) bool {
	lines := strings.Split(string(c.tree.Source), "\n")
	starts := []int{int(n.StartPosition().Row)}
	if stmt := statement(n); stmt != nil {
		starts = append(starts, int(stmt.StartPosition().Row))
	}
	for _, row := range starts {
		if hasSafety(lines[row]) {
			return true
		}
		for r := row - 1; r >= 0; r-- {
			line := strings.TrimSpace(lines[r])
			if !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "/*") && !strings.HasPrefix(line, "*") && !strings.HasPrefix(line, "#[") {
				break
			}
			if hasSafety(line) {
				return true
			}
		}
	}
	return false
}

func hasSafety(line string) bool { return strings.Contains(strings.ToUpper(line), "SAFETY:") }

// statement returns the statement or item that contains n directly in a block.
func statement(n *ts.Node) *ts.Node {
	for p := n; p != nil; p = p.Parent() {
		if parent := p.Parent(); parent != nil {
			switch parent.Kind() {
			case "block", "declaration_list", "source_file":
				return p
			}
		}
	}
	return nil
}

// shells are programs that interpret their argument as a command line.
var shells = set("sh", "bash", "zsh", "dash", "ksh", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh")

// checkCommandShell flags .arg(format!(...)) or .args([.., format!(..)]) in a
// call chain that starts at Command::new with a shell.
func (c *checker) checkCommandShell(call *ts.Node) {
	name, receiver, args := method(c.tree, call)
	if (name != "arg" && name != "args") || args == nil || !c.containsFormat(args) {
		return
	}
	for receiver != nil && receiver.Kind() == "call_expression" {
		fn := receiver.ChildByFieldName("function")
		if fn != nil && fn.Kind() == "scoped_identifier" && strings.HasSuffix(c.tree.Text(fn), "Command::new") {
			if c.isShell(receiver.ChildByFieldName("arguments")) {
				c.report("rust.command-shell", call)
			}
			return
		}
		_, receiver, _ = method(c.tree, receiver)
	}
}

func (c *checker) containsFormat(n *ts.Node) bool {
	found := false
	parsing.Walk(n, func(m *ts.Node) bool {
		if m.Kind() == "macro_invocation" {
			if name := m.ChildByFieldName("macro"); name != nil && c.tree.Text(name) == "format" {
				found = true
			}
		}
		return !found
	})
	return found
}

func (c *checker) isShell(args *ts.Node) bool {
	if args == nil || args.NamedChildCount() == 0 {
		return false
	}
	first := args.NamedChild(0)
	if first.Kind() != "string_literal" {
		return false
	}
	program := strings.Trim(c.tree.Text(first), `"`)
	program = path.Base(strings.ReplaceAll(program, `\\`, "/"))
	return shells[strings.ToLower(program)]
}

// widths are the bit widths of integer types; usize and isize count as 64-bit.
var widths = map[string]int{
	"i8": 8, "u8": 8, "i16": 16, "u16": 16, "i32": 32, "u32": 32,
	"i64": 64, "u64": 64, "i128": 128, "u128": 128, "isize": 64, "usize": 64,
}

// usizeMethods return usize.
var usizeMethods = set("len", "count", "capacity")

// checkCast flags as casts to a narrower integer, or from a float to an integer,
// when the source type is known from syntax: a typed parameter or let, a
// len()/count()/capacity() call, a float literal, or another cast.
func (c *checker) checkCast(n *ts.Node) {
	target := n.ChildByFieldName("type")
	value := n.ChildByFieldName("value")
	if target == nil || value == nil || target.Kind() != "primitive_type" {
		return
	}
	to, ok := widths[c.tree.Text(target)]
	if !ok {
		return
	}
	from := c.sourceType(value, n)
	if from == "f32" || from == "f64" || widths[from] > to {
		c.report("rust.lossy-cast", n)
	}
}

func (c *checker) sourceType(value, cast *ts.Node) string {
	switch value.Kind() {
	case "float_literal":
		return "f64"
	case "parenthesized_expression":
		if value.NamedChildCount() == 1 {
			return c.sourceType(value.NamedChild(0), cast)
		}
	case "type_cast_expression":
		if t := value.ChildByFieldName("type"); t != nil && t.Kind() == "primitive_type" {
			return c.tree.Text(t)
		}
	case "call_expression":
		if name, _, args := method(c.tree, value); usizeMethods[name] && args != nil && args.NamedChildCount() == 0 {
			return "usize"
		}
	case "identifier":
		return c.declaredType(c.tree.Text(value), cast)
	}
	return ""
}

// declaredType returns the primitive type of the last parameter or let binding
// named name before the cast in the enclosing function, or "".
func (c *checker) declaredType(name string, cast *ts.Node) string {
	fn := enclosingFunction(cast)
	if fn == nil {
		return ""
	}
	found := ""
	parsing.Walk(fn, func(n *ts.Node) bool {
		if n.StartByte() >= cast.StartByte() {
			return false
		}
		if n.Kind() == "closure_expression" {
			return false // closure parameters and locals are out of scope
		}
		if n.Kind() == "parameter" || n.Kind() == "let_declaration" {
			pattern, typ := n.ChildByFieldName("pattern"), n.ChildByFieldName("type")
			if pattern != nil && typ != nil && pattern.Kind() == "identifier" && c.tree.Text(pattern) == name && typ.Kind() == "primitive_type" {
				found = c.tree.Text(typ)
			} else if pattern != nil && c.tree.Text(pattern) == name {
				found = "" // shadowed without a known type
			}
		}
		return true
	})
	return found
}

// ownedRefs map owning types to the borrowed type a parameter should take.
var ownedRefs = set("Vec", "String", "PathBuf")

// checkPtrArgs flags parameters of type &Vec<T>, &String, or &PathBuf. Methods
// of trait implementations are skipped because the trait fixes the signature.
func (c *checker) checkPtrArgs(fn *ts.Node) {
	if impl := fn.Parent(); impl != nil && impl.Parent() != nil && impl.Parent().Kind() == "impl_item" && impl.Parent().ChildByFieldName("trait") != nil {
		return
	}
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return
	}
	for _, p := range parsing.NamedChildren(params) {
		typ := p.ChildByFieldName("type")
		if p.Kind() != "parameter" || typ == nil || typ.Kind() != "reference_type" || parsing.HasToken(typ, "mutable_specifier") {
			continue
		}
		inner := typ.ChildByFieldName("type")
		if inner != nil && inner.Kind() == "generic_type" {
			inner = inner.ChildByFieldName("type")
		}
		if inner != nil && inner.Kind() == "type_identifier" && ownedRefs[c.tree.Text(inner)] {
			c.report("rust.ptr-arg", p)
		}
	}
}

// checkIndexLoop flags for i in 0..v.len() whose body indexes v only as v[i].
func (c *checker) checkIndexLoop(loop *ts.Node) {
	pattern, rng, body := loop.ChildByFieldName("pattern"), loop.ChildByFieldName("value"), loop.ChildByFieldName("body")
	if pattern == nil || rng == nil || body == nil || pattern.Kind() != "identifier" || rng.Kind() != "range_expression" ||
		!parsing.HasToken(rng, "..") || rng.NamedChildCount() != 2 {
		return
	}
	start, end := rng.NamedChild(0), rng.NamedChild(1)
	if c.tree.Text(start) != "0" || end.Kind() != "call_expression" {
		return
	}
	name, receiver, args := method(c.tree, end)
	if name != "len" || receiver == nil || args == nil || args.NamedChildCount() != 0 {
		return
	}
	collection, index := c.tree.Text(receiver), c.tree.Text(pattern)
	byIndex, other := false, false
	parsing.Walk(body, func(n *ts.Node) bool {
		if n.Kind() == "index_expression" && n.NamedChildCount() == 2 && c.tree.Text(n.NamedChild(0)) == collection {
			if c.tree.Text(n.NamedChild(1)) == index {
				byIndex = true
			} else {
				other = true // v[i + 1] and similar need the index
			}
		}
		return true
	})
	if byIndex && !other {
		c.report("rust.index-loop", loop)
	}
}
