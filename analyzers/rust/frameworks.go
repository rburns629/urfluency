package rust

import (
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// frameworkCrates maps crate names, as written in paths, to framework names.
var frameworkCrates = map[string]string{
	"tokio": "tokio", "actix_web": "actix", "axum": "axum", "rocket": "rocket", "warp": "warp",
	"serde": "serde", "diesel": "diesel", "sqlx": "sqlx", "bevy": "bevy", "tauri": "tauri",
}

// Frameworks implements analysis.FrameworkDetector from use declarations and
// attribute paths such as #[tokio::main].
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.Rust, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	found := map[string]bool{}
	parsing.Walk(tree.Root(), func(n *ts.Node) bool {
		var p string
		switch n.Kind() {
		case "use_declaration":
			if arg := n.ChildByFieldName("argument"); arg != nil {
				p = tree.Text(arg)
			}
		case "attribute_item":
			p = attributeText(tree, n)
		default:
			return true
		}
		if fw, ok := frameworkCrates[rootCrate(p)]; ok {
			found[fw] = true
		}
		return false
	})
	out := make([]string, 0, len(found))
	for fw := range found {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}

// rootCrate returns the first path segment, such as tokio in ::tokio::sync::Mutex.
func rootCrate(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "::")
	if i := strings.IndexFunc(p, func(r rune) bool { return !isIdentRune(r) }); i >= 0 {
		p = p[:i]
	}
	return p
}
