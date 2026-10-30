package vulns

import (
	"context"
	"errors"
	"io/fs"
)

// offlineChecker matches packages against databases Update downloaded.
type offlineChecker struct {
	dir string
}

// Offline returns a Checker that matches packages against the databases
// Update downloaded into dir. Nothing leaves the machine. Ecosystems without a
// database, and versions or advisory ranges the ecosystem's comparator cannot
// parse, are reported unchecked.
func Offline(dir string) Checker { return offlineChecker{dir: dir} }

type loadedIndex struct {
	header indexHeader
	body   indexBody
	reason string // why the index cannot be used; "" if it can
}

func (o offlineChecker) load(ecosystem string) loadedIndex {
	h, body, err := readIndex(indexPath(o.dir, ecosystem))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return loadedIndex{reason: "no offline database downloaded"}
	case errors.Is(err, errOutdated):
		return loadedIndex{reason: errOutdated.Error()}
	case err != nil:
		return loadedIndex{reason: "offline database unreadable: " + err.Error()}
	}
	return loadedIndex{header: h, body: body}
}

func (o offlineChecker) Check(ctx context.Context, pkgs []Package) ([]Finding, Coverage) {
	t := newTally("offline")
	dbs := map[string]loadedIndex{}
	var findings []Finding
	for _, p := range pkgs {
		if ctx.Err() != nil {
			t.skip(p, "check canceled")
			continue
		}
		if reason := checkable(p); reason != "" {
			t.skip(p, reason)
			continue
		}
		db, ok := dbs[p.Ecosystem]
		if !ok {
			db = o.load(p.Ecosystem)
			dbs[p.Ecosystem] = db
			if db.reason == "" {
				t.dates[p.Ecosystem] = db.header.Published
			}
		}
		if db.reason != "" {
			t.skip(p, db.reason)
			continue
		}
		advs, err := db.body.match(p)
		if len(advs) > 0 {
			findings = append(findings, Finding{Package: p, Advisories: advs})
		}
		if err != nil {
			// Some advisory could not be evaluated: what matched is reported,
			// but the package is not counted as checked.
			t.skip(p, err.Error())
			continue
		}
		t.check(p)
	}
	sortFindings(findings)
	return findings, t.coverage()
}

// match returns the advisories affecting p, and an error if any affected
// entry could not be evaluated.
func (b indexBody) match(p Package) ([]Advisory, error) {
	raw := queryVersion(p.Ecosystem, p.Version)
	var advs []Advisory
	var ranges = map[int][]indexRange{}
	var order []int
	var firstErr error
	for _, e := range b.Packages[normalizeName(p.Ecosystem, p.Name)] {
		hit, err := affects(p.Ecosystem, raw, e.Entry.Versions, e.Entry.Ranges)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if !hit || e.Advisory < 0 || e.Advisory >= len(b.Advisories) {
			continue
		}
		if _, seen := ranges[e.Advisory]; !seen {
			order = append(order, e.Advisory)
		}
		ranges[e.Advisory] = append(ranges[e.Advisory], e.Entry.Ranges...)
	}
	for _, i := range order {
		a := b.Advisories[i]
		advs = append(advs, Advisory{ID: a.ID, Aliases: a.Aliases, Summary: a.Summary, Severity: a.Severity,
			Score: a.Score, Fixed: fixedVersions(p.Ecosystem, ranges[i]), URL: advisoryURL(a.ID)})
	}
	return advs, firstErr
}
