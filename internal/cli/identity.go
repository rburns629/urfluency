package cli

import (
	"context"
	"fmt"
	"os/user"
	"runtime"
	"strings"
	"sync"

	"github.com/rburns629/urfluency/gitdata"
	"github.com/rburns629/urfluency/internal/identity"
)

// detectIdentity works out the user's commit addresses across the repositories.
// Seeds are --author values, user.name and user.email as each repository resolves
// them, and the account's full name. Repositories whose history cannot be read
// are skipped.
func detectIdentity(ctx context.Context, repos, extra []string) identity.Identity {
	type perRepo struct {
		name, email string
		authors     []gitdata.Author
	}
	results := make([]perRepo, len(repos))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, dir := range repos {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			r := gitdata.Repo{Dir: dir}
			email, _ := r.ConfiguredEmail(ctx)
			authors, _ := r.Authors(ctx)
			results[i] = perRepo{r.ConfiguredName(ctx), email, authors}
		})
	}
	wg.Wait()

	names, emails := []string{}, append([]string{}, extra...)
	if u, err := user.Current(); err == nil {
		names = append(names, u.Name)
	}
	type key struct{ name, email string }
	merged := map[key]*identity.Author{}
	for _, r := range results {
		names, emails = append(names, r.name), append(emails, r.email)
		for _, a := range r.authors {
			k := key{a.Name, a.Email}
			m := merged[k]
			if m == nil {
				m = &identity.Author{Name: a.Name, Email: a.Email}
				merged[k] = m
			}
			m.Commits += a.Commits
			m.Repos++
		}
	}
	var authors []identity.Author
	for _, a := range merged {
		authors = append(authors, *a)
	}
	return identity.Detect(names, emails, authors)
}

// describeIdentity tells the user, on stderr only, which addresses were counted
// as theirs and which look like theirs but were not.
func describeIdentity(id identity.Identity, repos int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Counted as yours: %d %s with %s %s in %d %s\n",
		len(id.Emails), plural(len(id.Emails), "address", "addresses"), commas(id.Commits), plural(id.Commits, "commit", "commits"),
		repos, plural(repos, "repository", "repositories"))
	fmt.Fprintf(&b, "  %s\n", strings.Join(id.Emails, ", "))
	if len(id.Possible) > 0 {
		b.WriteString("Possibly yours, not counted (add any that are yours with --author EMAIL):\n")
		for _, p := range id.Possible {
			fmt.Fprintf(&b, "  %s (%s %s in %d %s)\n", p.Email, commas(p.Commits), plural(p.Commits, "commit", "commits"),
				p.Repos, plural(p.Repos, "repository", "repositories"))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
