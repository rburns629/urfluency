package rules

import "testing"

func TestCatalogIsComplete(t *testing.T) {
	if d := Duplicates(); len(d) > 0 {
		t.Fatalf("duplicate rule IDs: %v", d)
	}
	valid := map[Category]bool{CleanCode: true, Architecture: true, TypeSafety: true, LanguageProficiency: true, Security: true, AIEngineering: true}
	for id, r := range catalog {
		if r.ID != id || r.Title == "" || r.Why == "" || r.Focus == "" || len(r.Languages) == 0 {
			t.Errorf("rule %s is missing fields: %+v", id, r)
		}
		if !valid[r.Category] {
			t.Errorf("rule %s has unknown category %q", id, r.Category)
		}
	}
}

func TestStandards(t *testing.T) {
	tests := map[string]string{
		"clean.cognitive-complexity":    "standards/ARCHITECTURE.md#simplest-sufficient-solution",
		"ts.explicit-any":               "standards/TYPE_SAFETY.md",
		"sec.api-token":                 "standards/SECURITY.md",
		"prompt.unverified-change":      "standards/QUALITY_GATES.md",
		"go.waitgroup-add-in-goroutine": "",
	}
	for id, want := range tests {
		r, ok := Lookup(id)
		if !ok {
			t.Fatalf("rule %s missing", id)
		}
		got := r.Standard()
		if want == "" && got != "" || want != "" && got != standardsBase+want {
			t.Errorf("%s standard = %q, want %q", id, got, want)
		}
	}
}
