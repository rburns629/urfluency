// Package rules is the versioned catalog that every finding references.
package rules

import "sort"

// Version identifies the catalog; it changes whenever rules are added or redefined.
const Version = "0.3"

// Category groups rules into the metrics users see.
type Category string

const (
	CleanCode           Category = "clean-code"
	Architecture        Category = "architecture"
	TypeSafety          Category = "type-safety"
	LanguageProficiency Category = "language-proficiency"
	Security            Category = "security"
	Prompting           Category = "prompting"
)

// Rule describes one detectable practice and how to improve on it.
type Rule struct {
	ID        string
	Category  Category
	Languages []string
	Framework string
	Title     string
	Why       string
	Focus     string
}

var (
	catalog    = map[string]Rule{}
	duplicates []string
)

// Register adds rules to the catalog. A duplicate ID keeps the first definition
// and is recorded for Duplicates, which tests check.
func Register(rs ...Rule) {
	for _, r := range rs {
		if _, exists := catalog[r.ID]; exists {
			duplicates = append(duplicates, r.ID)
			continue
		}
		catalog[r.ID] = r
	}
}

// Duplicates returns IDs that were registered more than once.
func Duplicates() []string { return append([]string(nil), duplicates...) }

// Lookup returns the rule with the given ID.
func Lookup(id string) (Rule, bool) {
	r, ok := catalog[id]
	return r, ok
}

// ForLanguage returns the rules that apply to a language, sorted by ID.
func ForLanguage(language string) []Rule {
	var out []Rule
	for _, r := range catalog {
		for _, l := range r.Languages {
			if l == language {
				out = append(out, r)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
