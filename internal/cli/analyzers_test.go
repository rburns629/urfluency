package cli

import (
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

// TestAnalyzersClaimDistinctExtensions checks that, with every analyzer the CLI
// registers, each extension routes to exactly one language.
func TestAnalyzersClaimDistinctExtensions(t *testing.T) {
	owner := map[string]string{}
	for _, a := range analysis.Analyzers() {
		for _, ext := range a.Extensions() {
			if prev, ok := owner[ext]; ok {
				t.Errorf("%s is claimed by %s and %s", ext, prev, a.Language())
			}
			owner[ext] = a.Language()
			if got := analysis.ForPath("x" + ext); got == nil || got.Language() != a.Language() {
				t.Errorf("ForPath(x%s) does not route to %s", ext, a.Language())
			}
		}
	}
	want := []string{"c", "cpp", "csharp", "dart", "go", "java", "javascript", "kotlin", "php", "powershell", "python", "ruby", "rust", "shell", "swift", "typescript"}
	got := map[string]bool{}
	for _, a := range analysis.Analyzers() {
		got[a.Language()] = true
	}
	for _, l := range want {
		if !got[l] {
			t.Errorf("%s analyzer is not registered", l)
		}
	}
}
