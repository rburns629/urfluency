// Package parsing wraps tree-sitter for analyzers of languages other than Go.
// It uses cgo; see PROJECT_CONTEXT.md for the approved exception.
package parsing

import (
	"errors"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
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
)

// Tree is a parsed file. Call Close when done; nodes are invalid afterwards.
type Tree struct {
	tree   *ts.Tree
	Source []byte
}

// Parse parses src with the given grammar. Files with syntax errors still parse;
// error nodes are skipped by Walk.
func Parse(g Grammar, src []byte) (*Tree, error) {
	p := ts.NewParser()
	defer p.Close()
	if err := p.SetLanguage(g.lang); err != nil {
		return nil, err
	}
	t := p.Parse(src, nil)
	if t == nil {
		return nil, errors.New("parse failed")
	}
	return &Tree{tree: t, Source: src}, nil
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
