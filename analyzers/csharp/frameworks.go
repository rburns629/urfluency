package csharp

import (
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// Frameworks implements analysis.FrameworkDetector from a file's using directives.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.CSharp, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var out []string
	for fw := range frameworksOf(tree) {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}

// namespaceFrameworks maps namespaces to frameworks, most specific first, so
// System.Windows.Forms is WinForms and not WPF, and Blazor's components are not
// counted as server-side ASP.NET Core.
var namespaceFrameworks = []struct{ namespace, framework string }{
	{"Microsoft.AspNetCore.Components", "blazor"},
	{"Microsoft.AspNetCore", "aspnetcore"},
	{"Microsoft.EntityFrameworkCore", "efcore"},
	{"Microsoft.Maui", "maui"},
	{"System.Windows.Forms", "winforms"},
	{"System.Windows", "wpf"},
	{"UnityEngine", "unity"},
	{"Xunit", "xunit"},
	{"NUnit", "nunit"},
	{"Microsoft.VisualStudio.TestTools", "mstest"},
}

// frameworksOf returns the frameworks named by a file's using directives,
// including global, aliased, and namespace-scoped ones.
func frameworksOf(t *parsing.Tree) map[string]bool {
	found := map[string]bool{}
	parsing.Walk(t.Root(), func(n *ts.Node) bool {
		switch n.Kind() {
		case "compilation_unit", "namespace_declaration", "file_scoped_namespace_declaration", "declaration_list":
			return true
		case "using_directive":
			alias := n.ChildByFieldName("name")
			for _, child := range parsing.NamedChildren(n) {
				if alias != nil && child.Id() == alias.Id() || (child.Kind() != "identifier" && child.Kind() != "qualified_name") {
					continue
				}
				if fw := framework(t.Text(child)); fw != "" {
					found[fw] = true
				}
			}
		}
		return false
	})
	return found
}

func framework(namespace string) string {
	for _, nf := range namespaceFrameworks {
		if namespace == nf.namespace || strings.HasPrefix(namespace, nf.namespace+".") {
			return nf.framework
		}
	}
	return ""
}
