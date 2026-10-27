package cpp

import (
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// Frameworks returns the libraries a file includes, sorted.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.CPP, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var out []string
	for fw := range includedFrameworks(tree) {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}

// includePrefixes map include paths to frameworks.
var includePrefixes = []struct{ prefix, framework string }{
	{"boost/", "boost"},
	{"gtest/", "gtest"},
	{"gmock/", "gtest"},
	{"catch2/", "catch2"},
	{"catch.hpp", "catch2"},
	{"CoreMinimal.h", "unreal"},
	{"opencv2/", "opencv"},
	{"Eigen/", "eigen"},
	{"grpc/", "grpc"},
	{"grpcpp/", "grpc"},
	{"grpc++/", "grpc"},
	{"absl/", "abseil"},
}

// includedFrameworks returns the frameworks named by #include directives,
// including those inside #if blocks.
func includedFrameworks(t *parsing.Tree) map[string]bool {
	found := map[string]bool{}
	parsing.Walk(t.Root(), func(n *ts.Node) bool {
		if n.Kind() != "preproc_include" {
			return true
		}
		p := n.ChildByFieldName("path")
		if p == nil {
			return false
		}
		inc := strings.Trim(t.Text(p), "<>\"")
		if isQtInclude(inc) {
			found["qt"] = true
		}
		for _, ip := range includePrefixes {
			if strings.HasPrefix(inc, ip.prefix) {
				found[ip.framework] = true
			}
		}
		return false
	})
	return found
}

// isQtInclude recognizes module paths such as QtCore/QString and Qt class
// headers such as QString or QWidget, which have no extension.
func isQtInclude(inc string) bool {
	if len(inc) < 3 || inc[0] != 'Q' {
		return false
	}
	if strings.HasPrefix(inc, "Qt") && strings.Contains(inc, "/") {
		return inc[2] >= 'A' && inc[2] <= 'Z'
	}
	return inc[1] >= 'A' && inc[1] <= 'Z' && !strings.ContainsAny(inc, "./")
}
