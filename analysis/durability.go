package analysis

import (
	"context"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

// DurabilityWindow bounds both how old a commit must be to count and how soon
// after it a fix must land. Without it, recent commits (most AI-assisted ones)
// would look more durable only for having had less time to fail.
const DurabilityWindow = 90 * 24 * time.Hour

// Durability counts the user's added code lines, from commits old enough to judge,
// and how many of them a fix commit changed within DurabilityWindow, split by
// whether the commit was marked AI-assisted.
type Durability struct {
	AssistedAdded, AssistedFixed int
	OtherAdded, OtherFixed       int
}

// durability applies the SZZ method: each fix commit's changed lines are traced
// back to the commits that introduced them.
func durability(ctx context.Context, repo gitdata.Repo, added map[string]map[string]int, commits map[string]gitdata.Commit,
	tools map[string][]string, since, now time.Time) (Durability, error) {
	var d Durability
	isCode := func(p string) bool { return !inSkippedDir(p) && !IsProtected(p) && ForPath(p) != nil }
	eligible := map[string]bool{}
	var oldest, newest time.Time
	for hash, c := range commits {
		if c.Time.Before(since) || now.Sub(c.Time) < DurabilityWindow {
			continue
		}
		eligible[hash] = true
		if oldest.IsZero() || c.Time.Before(oldest) {
			oldest = c.Time
		}
		if c.Time.After(newest) {
			newest = c.Time
		}
		for p, n := range added[hash] {
			if !isCode(p) {
				continue
			}
			if len(tools[hash]) > 0 {
				d.AssistedAdded += n
			} else {
				d.OtherAdded += n
			}
		}
	}
	if len(eligible) == 0 {
		return d, nil
	}
	fixes, err := repo.FixCommits(ctx)
	if err != nil {
		return d, err
	}
	for _, f := range fixes {
		if f.Time.Before(oldest) || f.Time.After(newest.Add(DurabilityWindow)) {
			continue
		}
		fixed, err := repo.FixedLines(ctx, f.Hash, isCode)
		if err != nil {
			if ctx.Err() != nil {
				return d, ctx.Err()
			}
			continue // a root commit has no parent to blame
		}
		for intro, n := range fixed {
			c := commits[intro]
			if !eligible[intro] || f.Time.Before(c.Time) || f.Time.Sub(c.Time) > DurabilityWindow {
				continue
			}
			if len(tools[intro]) > 0 {
				d.AssistedFixed += n
			} else {
				d.OtherFixed += n
			}
		}
	}
	return d, nil
}
