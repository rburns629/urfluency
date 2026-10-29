// Package parsing wraps tree-sitter for analyzers of languages other than Go.
// It uses cgo; see PROJECT_CONTEXT.md for the approved exception.
package parsing

import (
	"errors"
	"strings"
	"time"

	dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	powershell "github.com/airbus-cert/tree-sitter-powershell/bindings/go"
	kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
	bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	swift "github.com/rburns629/urfluency/third_party/tree-sitter-swift"
)

// Grammar is a tree-sitter language.
type Grammar struct {
	lang *ts.Language
}

// Supported grammars. Languages are immutable and safe to share across goroutines;
// parsers are not, so Parse creates one per call.
var (
	Python     = Grammar{ts.NewLanguage(python.Language())}
	JavaScript = Grammar{ts.NewLanguage(javascript.Language())}
	TypeScript = Grammar{ts.NewLanguage(typescript.LanguageTypescript())}
	TSX        = Grammar{ts.NewLanguage(typescript.LanguageTSX())}
	Rust       = Grammar{ts.NewLanguage(rust.Language())}
	C          = Grammar{ts.NewLanguage(c.Language())}
	CPP        = Grammar{ts.NewLanguage(cpp.Language())}
	CSharp     = Grammar{ts.NewLanguage(csharp.Language())}
	Java       = Grammar{ts.NewLanguage(java.Language())}
	Kotlin     = Grammar{ts.NewLanguage(kotlin.Language())}
	Swift      = Grammar{ts.NewLanguage(swift.Language())}
	Dart       = Grammar{ts.NewLanguage(dart.Language())}
	Bash       = Grammar{ts.NewLanguage(bash.Language())}
	PowerShell = Grammar{ts.NewLanguage(powershell.Language())}
)

// Tree is a parsed file. Call Close when done; nodes are invalid afterwards.
type Tree struct {
	tree   *ts.Tree
	Source []byte
}

// Parsing reads untrusted files, so each parse is bounded: a file that takes
// longer than parseBudget, or nests deeper than maxDepth, is refused. Real
// code parses in milliseconds and nests a few dozen levels; a crafted file
// could otherwise stall a scan or exhaust the stack of the recursive walks
// that analyzers run over the tree. ParseBudget is a variable only so fuzz
// tests can lower it; nothing else should change it.
var ParseBudget = 5 * time.Second

const maxDepth = 2000

// ErrTooComplex marks a file refused for taking too long or nesting too deeply.
var ErrTooComplex = errors.New("file is too complex to analyze")

// Parse parses src with the given grammar. Files with syntax errors still parse;
// error nodes are skipped by Walk.
func Parse(g Grammar, src []byte) (*Tree, error) {
	p := ts.NewParser()
	defer p.Close()
	if err := p.SetLanguage(g.lang); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(ParseBudget)
	t := p.ParseWithOptions(func(i int, _ ts.Point) []byte {
		if i < len(src) {
			return src[i:]
		}
		return []byte{}
	}, nil, &ts.ParseOptions{ProgressCallback: func(ts.ParseState) bool { return time.Now().After(deadline) }})
	if t == nil {
		return nil, ErrTooComplex // cancelled at the deadline
	}
	if tooDeep(t) {
		t.Close()
		return nil, ErrTooComplex
	}
	return &Tree{tree: t, Source: src}, nil
}

// tooDeep reports whether any node is nested more than maxDepth levels, walking
// with a cursor so the check itself does not recurse.
func tooDeep(t *ts.Tree) bool {
	c := t.Walk()
	defer c.Close()
	depth := 0
	for {
		if c.GotoFirstChild() {
			if depth++; depth > maxDepth {
				return true
			}
			continue
		}
		for !c.GotoNextSibling() {
			if !c.GotoParent() {
				return false
			}
			depth--
		}
	}
}

// Root returns the root node.
func (t *Tree) Root() *ts.Node { return t.tree.RootNode() }

// Close releases the tree.
func (t *Tree) Close() { t.tree.Close() }

// Text returns the source text of a node.
func (t *Tree) Text(n *ts.Node) string { return n.Utf8Text(t.Source) }

// Walk visits named nodes depth-first, skipping error nodes. When fn returns false,
// the node's children are not visited.
func Walk(n *ts.Node, fn func(*ts.Node) bool) {
	if n == nil || n.IsError() || !fn(n) {
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		Walk(n.NamedChild(i), fn)
	}
}

// Line returns the 1-based line where a node starts.
func Line(n *ts.Node) int { return int(n.StartPosition().Row) + 1 }

// HasToken reports whether a node has a direct child, named or anonymous, of one
// of the given kinds; anonymous children are operators and keywords such as "is".
func HasToken(n *ts.Node, kinds ...string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		k := n.Child(i).Kind()
		for _, want := range kinds {
			if k == want {
				return true
			}
		}
	}
	return false
}

// NamedChildren returns a node's named children.
func NamedChildren(n *ts.Node) []*ts.Node {
	out := make([]*ts.Node, 0, n.NamedChildCount())
	for i := uint(0); i < n.NamedChildCount(); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

// InDir reports whether a slash-separated path has one of the directory names.
func InDir(path string, dirs ...string) bool {
	parts := strings.Split(path, "/")
	for _, p := range parts[:len(parts)-1] {
		for _, d := range dirs {
			if p == d {
				return true
			}
		}
	}
	return false
}
