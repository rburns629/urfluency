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
