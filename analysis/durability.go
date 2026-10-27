package analysis

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
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
	tools map[string][]string, touched map[string]bool, excluded func(p string) bool, since, now time.Time,
	progress func(stage string, done, total int)) (Durability, error) {
	var d Durability
	isCode := func(p string) bool { return !inSkippedDir(p) && !IsProtected(p) && ForPath(p) != nil && !excluded(p) }
	// A fix to a file the authors never touched cannot change their lines.
	ownCode := func(p string) bool { return touched[p] && isCode(p) }
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
	var relevant []gitdata.FixCommit
	for _, f := range fixes {
		if !f.Time.Before(oldest) && !f.Time.After(newest.Add(DurabilityWindow)) {
			relevant = append(relevant, f)
		}
	}
	// Fixes are traced in parallel, bounded like file reading; each result is
	// added under the lock, since only the totals matter.
	var mu sync.Mutex
	var wg sync.WaitGroup
	var done atomic.Int64
	limit := make(chan struct{}, runtime.NumCPU())
	progress("tracing fixes", 0, len(relevant))
	for _, f := range relevant {
		wg.Add(1)
		go func(f gitdata.FixCommit) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			defer func() { progress("tracing fixes", int(done.Add(1)), len(relevant)) }()
			if ctx.Err() != nil {
				return
			}
			fixed, err := repo.FixedLines(ctx, f.Hash, ownCode)
			if err != nil {
				return // a root commit has no parent to blame
			}
			mu.Lock()
			defer mu.Unlock()
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
		}(f)
	}
	wg.Wait()
	return d, ctx.Err()
}
