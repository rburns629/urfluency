package vulns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

const (
	osvAPI         = "https://api.osv.dev"
	defaultTimeout = 10 * time.Second
	batchSize      = 1000 // the most queries OSV accepts in one querybatch request
	detailWorkers  = 20
	// maxPages bounds follow-up requests for queries with more matches than
	// one response holds; such a package is already known to be vulnerable.
	maxPages         = 10
	maxResponseBytes = 32 << 20
)

// liveChecker queries the OSV API.
type liveChecker struct {
	client  *http.Client
	base    string // overridden in tests
	timeout time.Duration
}

// Live returns a Checker that queries api.osv.dev. Each check sends only
// ecosystem, name, and version, queries each distinct triple once, fetches
// details only for matched advisories, and gives up after 10 seconds. A nil
// client uses one with a 10 second timeout and the default transport, which
// honors HTTPS_PROXY.
func Live(client *http.Client) Checker {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &liveChecker{client: client, base: osvAPI, timeout: defaultTimeout}
}

// query is exactly what leaves the machine for one package.
type query struct {
	ecosystem, name, version string
}

type batchQuery struct {
	Package   batchPackage `json:"package"`
	Version   string       `json:"version"`
	PageToken string       `json:"page_token,omitempty"`
}

type batchPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
}

type batchResponse struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
		NextPageToken string `json:"next_page_token"`
	} `json:"results"`
}

func (l *liveChecker) Check(ctx context.Context, pkgs []Package) ([]Finding, Coverage) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	t := newTally("live")

	keys := make([]int, len(pkgs)) // index into queries, or -1
	index := map[query]int{}
	var queries []query
	for i, p := range pkgs {
		keys[i] = -1
		if reason := checkable(p); reason != "" {
			t.skip(p, reason)
			continue
		}
		q := query{p.Ecosystem, p.Name, queryVersion(p.Ecosystem, p.Version)}
		if _, ok := index[q]; !ok {
			index[q] = len(queries)
			queries = append(queries, q)
		}
		keys[i] = index[q]
	}

	ids := make([][]string, len(queries))
	failed := make([]error, len(queries))
	for start := 0; start < len(queries); start += batchSize {
		end := min(start+batchSize, len(queries))
		got, err := l.queryBatch(ctx, queries[start:end])
		for j := start; j < end; j++ {
			if err != nil {
				failed[j] = err
			} else {
				ids[j] = got[j-start]
			}
		}
	}

	var matched []string
	seen := map[string]bool{}
	for _, list := range ids {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				matched = append(matched, id)
			}
		}
	}
	details := l.fetchDetails(ctx, matched)

	var findings []Finding
	for i, p := range pkgs {
		k := keys[i]
		if k < 0 {
			continue
		}
		if failed[k] != nil {
			t.skip(p, failureReason(failed[k]))
			continue
		}
		t.check(p)
		var advs []Advisory
		for _, id := range ids[k] {
			rec, ok := details[id]
			switch {
			case !ok:
				// Matched, but its details did not arrive in time: still report it.
				advs = append(advs, Advisory{ID: id, URL: advisoryURL(id)})
			case rec.Withdrawn == "":
				advs = append(advs, liveAdvisory(rec, p))
			}
		}
		if len(advs) > 0 {
			findings = append(findings, Finding{Package: p, Advisories: advs})
		}
	}
	sortFindings(findings)
	return findings, t.coverage()
}

func failureReason(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "OSV did not answer in time"
	case errors.Is(err, context.Canceled):
		return "check canceled"
	}
	return "OSV query failed: " + err.Error()
}

func liveAdvisory(rec osvRecord, p Package) Advisory {
	sev, score := rec.severity()
	a := Advisory{ID: rec.ID, Aliases: rec.Aliases, Summary: rec.summary(), Severity: sev, Score: score, URL: advisoryURL(rec.ID)}
	var ranges []indexRange
	for _, e := range rec.entriesFor(p.Ecosystem, p.Name) {
		ranges = append(ranges, e.Ranges...)
	}
	a.Fixed = fixedVersions(p.Ecosystem, ranges)
	return a
}

// queryBatch returns the advisory IDs matching each query, following page
// tokens for queries with more matches than one response holds.
func (l *liveChecker) queryBatch(ctx context.Context, qs []query) ([][]string, error) {
	ids := make([][]string, len(qs))
	tokens := make([]string, len(qs))
	pending := make([]int, len(qs))
	for i := range pending {
		pending[i] = i
	}
	for page := 0; len(pending) > 0 && page < maxPages; page++ {
		req := struct {
			Queries []batchQuery `json:"queries"`
		}{}
		for _, i := range pending {
			q := qs[i]
			req.Queries = append(req.Queries, batchQuery{Package: batchPackage{q.ecosystem, q.name}, Version: q.version, PageToken: tokens[i]})
		}
		var resp batchResponse
		if err := l.do(ctx, http.MethodPost, "/v1/querybatch", req, &resp); err != nil {
			return nil, err
		}
		if len(resp.Results) != len(pending) {
			return nil, fmt.Errorf("OSV returned %d results for %d queries", len(resp.Results), len(pending))
		}
		var next []int
		for k, r := range resp.Results {
			i := pending[k]
			for _, v := range r.Vulns {
				if v.ID != "" && !slices.Contains(ids[i], v.ID) {
					ids[i] = append(ids[i], v.ID)
				}
			}
			if r.NextPageToken != "" {
				tokens[i] = r.NextPageToken
				next = append(next, i)
			}
		}
		pending = next
	}
	return ids, nil
}

// fetchDetails fetches full records, at most detailWorkers at a time. Records
// that fail to arrive are left out; callers report their IDs alone.
func (l *liveChecker) fetchDetails(ctx context.Context, ids []string) map[string]osvRecord {
	out := map[string]osvRecord{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, detailWorkers)
	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return out
		}
		wg.Go(func() {
			defer func() { <-sem }()
			var rec osvRecord
			if err := l.do(ctx, http.MethodGet, "/v1/vulns/"+url.PathEscape(id), nil, &rec); err != nil || rec.ID != id {
				return
			}
			mu.Lock()
			out[id] = rec
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// do sends one request to the OSV API and decodes its JSON response.
func (l *liveChecker) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode OSV request: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, r)
	if err != nil {
		return fmt.Errorf("build OSV request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("OSV request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OSV answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("decode OSV response: %w", err)
	}
	return nil
}
