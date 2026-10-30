package vulns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOSV serves recorded-shape OSV API responses. vulns maps
// "ecosystem/name/version" to advisory IDs; records holds the details.
type fakeOSV struct {
	t           *testing.T
	vulns       map[string][]string
	records     map[string]string
	failBatch   int // 1-based querybatch call that answers 500; 0 for none
	delay       time.Duration
	batches     atomic.Int32
	queries     atomic.Int32
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	mu          sync.Mutex
	fetched     []string
}

// strictQuery is everything a query may contain; anything else in a request
// would be data leaving the machine that the design does not allow.
type strictQuery struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Version   string `json:"version"`
	PageToken string `json:"page_token"`
}

func (f *fakeOSV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-r.Context().Done():
			return
		}
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/querybatch":
		n := f.batches.Add(1)
		var req struct {
			Queries []strictQuery `json:"queries"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			f.t.Errorf("request carries more than ecosystem, name, and version: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(req.Queries) > batchSize {
			f.t.Errorf("batch of %d queries", len(req.Queries))
		}
		f.queries.Add(int32(len(req.Queries)))
		if int(n) == f.failBatch {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		buf.WriteString(`{"results":[`)
		for i, q := range req.Queries {
			if i > 0 {
				buf.WriteByte(',')
			}
			ids := f.vulns[q.Package.Ecosystem+"/"+q.Package.Name+"/"+q.Version]
			if len(ids) == 0 {
				buf.WriteString(`{}`)
				continue
			}
			// The first page holds one ID; the rest follow with a page token.
			id, rest := ids[0], ids[1:]
			if q.PageToken != "" {
				id, rest = ids[1], nil
			}
			fmt.Fprintf(&buf, `{"vulns":[{"id":%q,"modified":"2024-01-02T03:04:05Z"}]`, id)
			if len(rest) > 0 {
				buf.WriteString(`,"next_page_token":"page2"`)
			}
			buf.WriteByte('}')
		}
		buf.WriteString(`]}`)
		w.Header().Set("Content-Type", "application/json")
		w.Write(buf.Bytes())
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
		cur := f.inFlight.Add(1)
		defer f.inFlight.Add(-1)
		for {
			m := f.maxInFlight.Load()
			if cur <= m || f.maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond) // let requests overlap
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		f.mu.Lock()
		f.fetched = append(f.fetched, id)
		f.mu.Unlock()
		rec, ok := f.records[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(rec))
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func newLive(t *testing.T, f *fakeOSV, timeout time.Duration) *liveChecker {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &liveChecker{client: srv.Client(), base: srv.URL, timeout: timeout}
}

const lodashRecord = `{
  "id": "GHSA-35jh-r3h4-6jhm",
  "modified": "2024-01-02T03:04:05Z",
  "aliases": ["CVE-2021-23337"],
  "summary": "Command Injection in lodash",
  "severity": [{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:L/PR:H/UI:N/S:U/C:H/I:H/A:H"}],
  "affected": [{
    "package": {"ecosystem": "npm", "name": "lodash", "purl": "pkg:npm/lodash"},
    "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "4.17.21"}]}],
    "database_specific": {"source": "https://github.com/github/advisory-database"}
  }],
  "database_specific": {"severity": "HIGH", "cwe_ids": ["CWE-77"], "github_reviewed": true}
}`

func TestLiveFindsAndDeduplicates(t *testing.T) {
	f := &fakeOSV{t: t,
		vulns: map[string][]string{"npm/lodash/4.17.20": {"GHSA-35jh-r3h4-6jhm"}, "Go/golang.org/x/net/0.17.0": {"GO-2023-2102"}},
		records: map[string]string{"GHSA-35jh-r3h4-6jhm": lodashRecord, "GO-2023-2102": `{"id":"GO-2023-2102","summary":"HTTP/2 rapid reset",
			"affected":[{"package":{"ecosystem":"Go","name":"golang.org/x/net"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"0.17.0"}]}]}]}`},
	}
	l := newLive(t, f, 5*time.Second)
	pkgs := []Package{
		{"npm", "lodash", "4.17.20", "web/package-lock.json"},
		{"npm", "lodash", "4.17.20", "api/package-lock.json"},
		{"npm", "left-pad", "1.3.0", "web/package-lock.json"},
		{"Go", "golang.org/x/net", "v0.17.0", "go.mod"},
		{"PyPI", "django", "latest", "requirements.txt"},
		{"Maven", "org.example:lib", "1.0", "pom.xml"},
	}
	findings, cov := l.Check(context.Background(), pkgs)

	if got := f.queries.Load(); got != 3 {
		t.Errorf("queried %d packages, want 3 distinct", got)
	}
	f.mu.Lock()
	fetched := f.fetched
	f.mu.Unlock()
	if len(fetched) != 2 {
		t.Errorf("fetched details %q, want the two matched advisories only", fetched)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %+v, want lodash twice and x/net", findings)
	}
	a := findings[1].Advisories[0] // Go sorts before npm
	if findings[1].Package.Lockfile != "api/package-lock.json" || a.ID != "GHSA-35jh-r3h4-6jhm" ||
		a.Severity != "high" || a.Score != 7.2 || a.Summary != "Command Injection in lodash" ||
		len(a.Aliases) != 1 || a.Aliases[0] != "CVE-2021-23337" || len(a.Fixed) != 1 || a.Fixed[0] != "4.17.21" ||
		a.URL != "https://osv.dev/vulnerability/GHSA-35jh-r3h4-6jhm" {
		t.Errorf("advisory = %+v", a)
	}
	if g := findings[0].Advisories[0]; g.ID != "GO-2023-2102" || g.Fixed[0] != "0.17.0" || g.Severity != "" {
		t.Errorf("Go advisory = %+v", g)
	}
	if cov.Mode != "live" || cov.Checked["npm"] != 2 || cov.Checked["Go"] != 1 {
		t.Errorf("checked = %v", cov.Checked)
	}
	if !strings.Contains(cov.Unchecked["PyPI"], unparseable) || !strings.Contains(cov.Unchecked["PyPI"], "django@latest") {
		t.Errorf("PyPI unchecked = %q", cov.Unchecked["PyPI"])
	}
	if !strings.Contains(cov.Unchecked["Maven"], "not supported") {
		t.Errorf("Maven unchecked = %q", cov.Unchecked["Maven"])
	}
}

func manyPackages(n int) []Package {
	pkgs := make([]Package, n)
	for i := range pkgs {
		pkgs[i] = Package{"npm", fmt.Sprintf("pkg-%d", i), "1.0.0", "package-lock.json"}
	}
	return pkgs
}

func TestLiveBatchesAndPartialFailure(t *testing.T) {
	f := &fakeOSV{t: t, failBatch: 2,
		vulns:   map[string][]string{"npm/pkg-5/1.0.0": {"GHSA-35jh-r3h4-6jhm"}, "npm/pkg-1500/1.0.0": {"GHSA-35jh-r3h4-6jhm"}},
		records: map[string]string{"GHSA-35jh-r3h4-6jhm": lodashRecord},
	}
	l := newLive(t, f, 5*time.Second)
	findings, cov := l.Check(context.Background(), manyPackages(2500))
	if got := f.batches.Load(); got != 3 {
		t.Errorf("%d batches, want 3", got)
	}
	if cov.Checked["npm"] != 1500 {
		t.Errorf("checked %d, want 1500 (batches 1 and 3)", cov.Checked["npm"])
	}
	if u := cov.Unchecked["npm"]; !strings.Contains(u, "OSV query failed") || !strings.Contains(u, "(1000)") {
		t.Errorf("unchecked = %q", u)
	}
	if len(findings) != 1 || findings[0].Package.Name != "pkg-5" {
		t.Errorf("findings = %+v; pkg-1500 was in the failed batch", findings)
	}
}

func TestLiveTimeoutMarksUnchecked(t *testing.T) {
	f := &fakeOSV{t: t, delay: time.Second}
	l := newLive(t, f, 50*time.Millisecond)
	start := time.Now()
	findings, cov := l.Check(context.Background(), manyPackages(3))
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("check took %v; the timeout is 50ms", time.Since(start))
	}
	if len(findings) != 0 || cov.Checked["npm"] != 0 {
		t.Errorf("findings %v, checked %v after a timeout", findings, cov.Checked)
	}
	if u := cov.Unchecked["npm"]; !strings.Contains(u, "did not answer in time") || !strings.Contains(u, "(3)") {
		t.Errorf("unchecked = %q", u)
	}
}

func TestLiveDetailsAndPaging(t *testing.T) {
	ids := map[string][]string{}
	records := map[string]string{}
	for i := range 60 {
		id := fmt.Sprintf("GHSA-%04d", i)
		ids[fmt.Sprintf("npm/pkg-%d/1.0.0", i)] = []string{id}
		records[id] = fmt.Sprintf(`{"id":%q,"summary":"issue %d"}`, id, i)
	}
	// pkg-0 has a second advisory on a second page, whose details fail.
	ids["npm/pkg-0/1.0.0"] = []string{"GHSA-0000", "GHSA-gone"}
	f := &fakeOSV{t: t, vulns: ids, records: records}
	l := newLive(t, f, 5*time.Second)
	findings, cov := l.Check(context.Background(), manyPackages(60))
	if m := f.maxInFlight.Load(); m > detailWorkers {
		t.Errorf("%d detail requests in flight, limit %d", m, detailWorkers)
	}
	if len(findings) != 60 || cov.Checked["npm"] != 60 {
		t.Fatalf("%d findings, %d checked", len(findings), cov.Checked["npm"])
	}
	var first Finding
	for _, fd := range findings {
		if fd.Package.Name == "pkg-0" {
			first = fd
		}
	}
	if len(first.Advisories) != 2 || first.Advisories[1].ID != "GHSA-gone" || first.Advisories[1].URL == "" {
		t.Errorf("pkg-0 advisories = %+v; an advisory without details is still reported", first.Advisories)
	}
}

func TestLiveDefaultClient(t *testing.T) {
	l := Live(nil).(*liveChecker)
	if l.client.Timeout != defaultTimeout || l.timeout != defaultTimeout || l.base != osvAPI {
		t.Errorf("Live(nil) = %+v", l)
	}
}
