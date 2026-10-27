package dart

import (
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// frameworkPackages map pub package names to framework packs.
var frameworkPackages = map[string]string{
	"flutter":          "flutter",
	"flutter_riverpod": "riverpod",
	"hooks_riverpod":   "riverpod",
	"riverpod":         "riverpod",
	"flutter_bloc":     "bloc",
	"bloc":             "bloc",
	"provider":         "provider",
	"get":              "get",
	"dio":              "dio",
	"cloud_firestore":  "firebase",
	"cloud_functions":  "firebase",
}

// Frameworks implements analysis.FrameworkDetector.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.Dart, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	out := []string{}
	for fw := range importedFrameworks(tree) {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}

// importedFrameworks returns the framework packs a library imports or exports.
func importedFrameworks(t *parsing.Tree) map[string]bool {
	found := map[string]bool{}
	for _, directive := range parsing.NamedChildren(t.Root()) {
		if directive.Kind() != "import_or_export" {
			continue
		}
		parsing.Walk(directive, func(n *ts.Node) bool {
			if n.Kind() != "uri" {
				return true
			}
			if fw := frameworkFor(strings.Trim(t.Text(n), `'"`)); fw != "" {
				found[fw] = true
			}
			return false
		})
	}
	return found
}

// frameworkFor maps a package URI such as package:flutter/material.dart to its
// framework pack, or "".
func frameworkFor(uri string) string {
	rest, ok := strings.CutPrefix(uri, "package:")
	if !ok {
		return ""
	}
	pkg, _, _ := strings.Cut(rest, "/")
	if strings.HasPrefix(pkg, "firebase_") {
		return "firebase"
	}
	return frameworkPackages[pkg]
}
