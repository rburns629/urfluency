package swift

import (
	"sort"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

// frameworkModules maps imported modules to framework names.
var frameworkModules = map[string]string{
	"SwiftUI": "swiftui", "UIKit": "uikit", "AppKit": "appkit", "Combine": "combine", "Vapor": "vapor",
	"SwiftData": "swiftdata", "CoreData": "coredata", "XCTest": "xctest", "Testing": "swift-testing",
}

// Frameworks returns the frameworks a file imports, sorted and unique.
func (Analyzer) Frameworks(f analysis.File) []string {
	tree, err := parsing.Parse(parsing.Swift, f.Content)
	if err != nil {
		return nil
	}
	defer tree.Close()
	found := map[string]bool{}
	for _, decl := range parsing.NamedChildren(tree.Root()) {
		if decl.Kind() != "import_declaration" {
			continue
		}
		for _, id := range parsing.NamedChildren(decl) {
			// import UIKit.UIView names the module first.
			if id.Kind() != "identifier" || id.NamedChildCount() == 0 {
				continue
			}
			if fw, ok := frameworkModules[tree.Text(id.NamedChild(0))]; ok {
				found[fw] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for fw := range found {
		out = append(out, fw)
	}
	sort.Strings(out)
	return out
}
