package analysis

import (
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

// Written counts the code lines the user's commits added since the scan's
// start, split by whether the commit was marked AI-assisted. Unlike owned
// lines, which are what survives at HEAD, it counts what was written when, so
// code rewritten since still counts in the month it was first written.
type Written struct {
	Months    map[string]WrittenLines // by UTC month, as YYYY-MM
	Languages map[string]WrittenLines
	Tools     map[string]int // AI-assisted lines by each tool a commit names
}

// WrittenLines splits added code lines by whether their commit was marked
// AI-assisted.
type WrittenLines struct {
	Assisted, Other int
}

func (w WrittenLines) add(o WrittenLines) WrittenLines {
	return WrittenLines{w.Assisted + o.Assisted, w.Other + o.Other}
}

func newWritten() Written {
	return Written{Months: map[string]WrittenLines{}, Languages: map[string]WrittenLines{}, Tools: map[string]int{}}
}

// written tallies added code lines per month, language, and tool. excluded
// drops what notUserCode found is not the user's own code.
func written(added map[string]map[string]int, commits map[string]gitdata.Commit, tools map[string][]string, excluded func(string) bool, since time.Time) Written {
	w := newWritten()
	for hash, perPath := range added {
		c, ok := commits[hash]
		if !ok || c.Time.Before(since) {
			continue
		}
		month := c.Time.UTC().Format("2006-01")
		for p, n := range perPath {
			a := ForPath(p)
			if n == 0 || a == nil || inSkippedDir(p) || IsProtected(p) || excluded(p) {
				continue
			}
			lines := WrittenLines{Other: n}
			if t := tools[hash]; len(t) > 0 {
				lines = WrittenLines{Assisted: n}
				for _, name := range t {
					w.Tools[name] += n
				}
			}
			w.Months[month] = w.Months[month].add(lines)
			w.Languages[a.Language()] = w.Languages[a.Language()].add(lines)
		}
	}
	return w
}

// merge adds another result's written lines.
func (w *Written) merge(o Written) {
	for k, v := range o.Months {
		w.Months[k] = w.Months[k].add(v)
	}
	for k, v := range o.Languages {
		w.Languages[k] = w.Languages[k].add(v)
	}
	for k, v := range o.Tools {
		w.Tools[k] += v
	}
}
