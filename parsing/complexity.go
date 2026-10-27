package parsing

import ts "github.com/tree-sitter/go-tree-sitter"

// CognitiveThreshold is the complexity above which a function is reported,
// SonarSource's default.
const CognitiveThreshold = 15

// ComplexitySyntax names a grammar's nodes for cognitive complexity, following
// SonarSource's specification: +1 for each break in linear flow, +1 more per
// level of nesting for structures that nest, and +1 per sequence of like logical
// operators. Nested functions deepen the nesting and count toward the enclosing
// function, as in the specification.
type ComplexitySyntax struct {
	Functions  map[string]bool // nodes that start a function
	Structures map[string]bool // +1 plus nesting, and nest their children: if, loops, switch, catch, ternary
	Hybrids    map[string]bool // +1 without a nesting penalty: else, elif
	ElseClause string          // the node an "else if" sits in, so the inner if is not counted again
	// ElseToken is the unnamed else keyword in grammars without an else node:
	// each one in a structure adds 1, and an if right after it is an else if.
	ElseToken  string
	Logical    string          // binary logical operator node
	LogicalOps map[string]bool // operators that count: &&, ||, ?? or and, or
	// LogicalKinds are nodes that are each one logical operator, for grammars
	// with a node per operator, mapped to that operator.
	LogicalKinds map[string]string
	Jumps        map[string]bool // break or continue nodes; +1 only when labeled
	Label        string          // child kind that marks a labeled jump
	Call         string          // call node; a call to the enclosing function adds 1 (recursion)
	Self         []string        // receivers of method calls: this, self
}

// FunctionComplexity is one top-level function and its cognitive complexity.
type FunctionComplexity struct {
	Line       int
	Complexity int
}

// Cognitive returns the complexity of each outermost function in a tree.
func Cognitive(t *Tree, syn ComplexitySyntax) []FunctionComplexity {
	var out []FunctionComplexity
	Walk(t.Root(), func(n *ts.Node) bool {
		if !syn.Functions[n.Kind()] {
			return true
		}
		c := &counter{syn: syn, tree: t}
		if name := n.ChildByFieldName("name"); name != nil {
			c.name = t.Text(name)
		}
		for _, child := range NamedChildren(n) {
			c.visit(child, 0)
		}
		out = append(out, FunctionComplexity{Line: Line(n), Complexity: c.total})
		return false // nested functions count toward this one
	})
	return out
}

type counter struct {
	syn   ComplexitySyntax
	tree  *Tree
	name  string // the function's name, for recursion
	total int
}

func (c *counter) visit(n *ts.Node, nesting int) {
	if n == nil || n.IsError() {
		return
	}
	kind := n.Kind()
	childNesting := nesting
	switch {
	case c.syn.Functions[kind]:
		childNesting = nesting + 1
	case c.syn.Structures[kind]:
		// The if of an "else if" was already counted by its else.
		if c.isElseIf(n) {
			childNesting = nesting
		} else {
			c.total += 1 + nesting
			childNesting = nesting + 1
		}
		if c.syn.ElseToken != "" {
			for i := uint(0); i < n.ChildCount(); i++ {
				if child := n.Child(i); !child.IsNamed() && child.Kind() == c.syn.ElseToken {
					c.total++
				}
			}
		}
	case c.syn.Hybrids[kind]:
		c.total++
	case kind == c.syn.Logical || c.syn.LogicalKinds[kind] != "":
		if op := c.operator(n); c.syn.LogicalOps[op] {
			if parent := n.Parent(); parent == nil || c.operator(parent) != op {
				c.total++
			}
		}
	case kind == c.syn.Call && c.name != "":
		if fn := n.ChildByFieldName("function"); fn != nil && c.callsSelf(c.tree.Text(fn)) {
			c.total++
		}
	case c.syn.Jumps[kind]:
		for _, child := range NamedChildren(n) {
			if child.Kind() == c.syn.Label {
				c.total++
			}
		}
	}
	for _, child := range NamedChildren(n) {
		c.visit(child, childNesting)
	}
}

func (c *counter) isElseIf(n *ts.Node) bool {
	if parent := n.Parent(); c.syn.ElseClause != "" && parent != nil && parent.Kind() == c.syn.ElseClause {
		return true
	}
	prev := n.PrevSibling()
	return c.syn.ElseToken != "" && prev != nil && !prev.IsNamed() && prev.Kind() == c.syn.ElseToken
}

// operator returns the logical operator a node applies, or "".
func (c *counter) operator(n *ts.Node) string {
	if op, ok := c.syn.LogicalKinds[n.Kind()]; ok {
		return op
	}
	if n.Kind() == c.syn.Logical {
		if op := n.ChildByFieldName("operator"); op != nil {
			return op.Kind()
		}
	}
	return ""
}

func (c *counter) callsSelf(callee string) bool {
	if callee == c.name {
		return true
	}
	for _, s := range c.syn.Self {
		if callee == s+"."+c.name {
			return true
		}
	}
	return false
}

// Complexity syntax for the tree-sitter grammars.
var (
	PythonComplexity = ComplexitySyntax{
		Functions:  set("function_definition", "lambda"),
		Structures: set("if_statement", "for_statement", "while_statement", "except_clause", "conditional_expression", "match_statement"),
		Hybrids:    set("elif_clause", "else_clause"),
		Logical:    "boolean_operator",
		LogicalOps: set("and", "or"),
		Call:       "call",
		Self:       []string{"self", "cls"},
	}
	JavaScriptComplexity = ComplexitySyntax{
		Functions: set("function_declaration", "function_expression", "function", "arrow_function",
			"method_definition", "generator_function_declaration", "generator_function"),
		Structures: set("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement",
			"switch_statement", "catch_clause", "ternary_expression"),
		Hybrids:    set("else_clause"),
		ElseClause: "else_clause",
		Logical:    "binary_expression",
		LogicalOps: set("&&", "||", "??"),
		Jumps:      set("break_statement", "continue_statement"),
		Label:      "statement_identifier",
		Call:       "call_expression",
		Self:       []string{"this"},
	}
)

func set(kinds ...string) map[string]bool {
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}
