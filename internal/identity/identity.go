// Package identity works out which commit authors are the user, so a scan needs no
// list of email addresses. It starts from what the user's machine says (Git's
// user.name and user.email, the account's full name) and links through commit
// history: an address used with one of the user's names is theirs, and a name
// used with one of their addresses is theirs. Addresses that only look like the
// user's are reported as possible, never counted, so the user can claim them.
//
// Everything stays on the machine; addresses are shown to the user and never
// written to the report.
package identity

import (
	"sort"
	"strings"
	"unicode"
)

// Author is a name and email pair with its commit and repository counts.
type Author struct {
	Name    string
	Email   string
	Commits int
	Repos   int
}

// Identity is the outcome of detection.
type Identity struct {
	// Emails are the addresses counted as the user's.
	Emails []string
	// Names are the author names linked to the user.
	Names []string
	// Commits is how many commits the user's addresses authored.
	Commits int
	// Possible are addresses that resemble the user's but could not be linked,
	// most commits first. They are not counted.
	Possible []Possible
}

// Possible is an address that resembles the user's.
type Possible struct {
	Email   string
	Commits int
	Repos   int
}

// MaxPossible caps how many possible addresses are reported.
const MaxPossible = 10

// Detect links seed names and emails to the authors found in history. Seeds are
// trusted as given; names and emails reached through history link further only
// when they identify one person (not a bot, a shared address, or a one-word name).
func Detect(seedNames, seedEmails []string, authors []Author) Identity {
	names, emails := map[string]bool{}, map[string]bool{}
	for _, n := range seedNames {
		if k := nameKey(n); k != "" {
			names[k] = true
		}
	}
	for _, e := range seedEmails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emails[e] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, a := range authors {
			if shared(a) {
				continue
			}
			k := nameKey(a.Name)
			if names[k] && !emails[a.Email] {
				emails[a.Email], changed = true, true
			}
			if emails[a.Email] && k != "" && !names[k] && personal(a.Name) {
				names[k], changed = true, true
			}
		}
	}

	var id Identity
	byEmail := map[string]*Possible{}
	tokens := patterns(names, emails)
	for _, a := range authors {
		if emails[a.Email] {
			id.Commits += a.Commits
			continue
		}
		if shared(a) || !resembles(a, tokens) {
			continue
		}
		p := byEmail[a.Email]
		if p == nil {
			p = &Possible{Email: a.Email}
			byEmail[a.Email] = p
		}
		p.Commits += a.Commits
		p.Repos = max(p.Repos, a.Repos)
	}
	for e := range emails {
		id.Emails = append(id.Emails, e)
	}
	for n := range names {
		id.Names = append(id.Names, n)
	}
	for _, p := range byEmail {
		id.Possible = append(id.Possible, *p)
	}
	sort.Strings(id.Emails)
	sort.Strings(id.Names)
	sort.Slice(id.Possible, func(i, j int) bool {
		if id.Possible[i].Commits != id.Possible[j].Commits {
			return id.Possible[i].Commits > id.Possible[j].Commits
		}
		return id.Possible[i].Email < id.Possible[j].Email
	})
	if len(id.Possible) > MaxPossible {
		id.Possible = id.Possible[:MaxPossible]
	}
	return id
}

// nameKey normalizes a name to its first and last words, lowercased, so
// "Robert L. Burns" and "robert burns" match.
func nameKey(name string) string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return words[0] + " " + words[len(words)-1]
}

// personal reports whether a name found through history is specific enough to
// link further: at least two words.
func personal(name string) bool {
	return strings.Contains(nameKey(name), " ")
}

// shared reports whether an author is a bot or a shared address that many
// people commit through, which must never link identities.
func shared(a Author) bool {
	e, n := a.Email, strings.ToLower(a.Name)
	return e == "" || e == "noreply@github.com" || strings.Contains(e, "[bot]") || strings.Contains(n, "[bot]") ||
		strings.HasSuffix(n, " bot") || n == "github" || n == "root" || strings.HasPrefix(e, "root@")
}

// patterns builds the words an address of the user's would likely contain: the
// last name, first initial plus last name, first and last name together, and
// the letters of the user's known addresses before the @.
func patterns(names, emails map[string]bool) []string {
	set := map[string]bool{}
	for n := range names {
		words := strings.Fields(n)
		last := words[len(words)-1]
		set[last] = true
		if len(words) > 1 {
			set[words[0][:1]+last] = true
			set[words[0]+last] = true
		}
	}
	for e := range emails {
		local, _, _ := strings.Cut(e, "@")
		if _, after, ok := strings.Cut(local, "+"); ok {
			local = after // GitHub noreply: 123+handle
		}
		set[letters(local)] = true
	}
	var out []string
	for p := range set {
		if len(p) >= 4 {
			out = append(out, p)
		}
	}
	return out
}

// resembles reports whether an author's address or name contains one of the
// user's patterns.
func resembles(a Author, tokens []string) bool {
	local, _, _ := strings.Cut(a.Email, "@")
	hay := letters(local) + " " + letters(strings.ToLower(a.Name))
	for _, t := range tokens {
		if strings.Contains(hay, t) {
			return true
		}
	}
	return false
}

func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
