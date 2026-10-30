package vulns

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func buildZip(t *testing.T, records map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range records {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeBucket serves per-ecosystem all.zip files like the OSV storage bucket.
type fakeBucket struct {
	mu       sync.Mutex
	zips     map[string][]byte
	modified time.Time
	truncate bool // send half the body, then drop the connection
	gets     atomic.Int32
}

func (b *fakeBucket) set(eco string, data []byte, modified time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.zips[eco], b.modified = data, modified
}

func (b *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	eco, _ := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/all.zip")
	data, ok := b.zips[eco]
	modified, truncate := b.modified, b.truncate
	b.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	b.gets.Add(1)
	if truncate {
		w.Write(data[:len(data)/2])
		panic(http.ErrAbortHandler) // drops the connection mid-body
	}
	w.Write(data)
}

var pypiRecords = map[string]string{
	"PYSEC-2021-66.json": `{
  "id": "PYSEC-2021-66", "modified": "2021-03-01T00:00:00Z",
  "aliases": ["CVE-2020-28493", "GHSA-g3rq-g295-4j3m"],
  "details": "This affects the package jinja2 from 0.0.0 and before 2.11.3.\nMore text.",
  "affected": [{"package": {"ecosystem": "PyPI", "name": "jinja2"},
    "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "2.11.3"}]},
               {"type": "GIT", "repo": "https://github.com/pallets/jinja", "events": [{"introduced": "0"}, {"fixed": "abcdef"}]}],
    "versions": []}]
}`,
	"GHSA-enum.json": `{
  "id": "GHSA-enum", "summary": "Enumerated versions only",
  "severity": [{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}],
  "affected": [{"package": {"ecosystem": "PyPI", "name": "Zope.Interface"}, "versions": ["5.0", "5.1"]}]
}`,
	"GHSA-badrange.json": `{
  "id": "GHSA-badrange", "summary": "Unparseable event",
  "affected": [{"package": {"ecosystem": "PyPI", "name": "weird"},
    "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "not a version"}]}]}]
}`,
	"PYSEC-withdrawn.json": `{"id": "PYSEC-withdrawn", "withdrawn": "2022-01-01T00:00:00Z",
  "affected": [{"package": {"ecosystem": "PyPI", "name": "jinja2"}, "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}]}]}]}`,
	"malformed.json": `{"id": `,
	"README.txt":     "not a record",
}

func newBucket(t *testing.T) (*fakeBucket, *httptest.Server) {
	b := &fakeBucket{zips: map[string][]byte{}}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return b, srv
}

func TestOfflineUpdateAndMatch(t *testing.T) {
	b, srv := newBucket(t)
	published := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	b.set("PyPI", buildZip(t, pypiRecords), published)
	dir := filepath.Join(t.TempDir(), "urfluency", "vulns")
	ctx := context.Background()

	var lastDone, lastTotal int64
	updated, err := update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI"}, func(eco string, done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil || len(updated) != 1 || updated[0] != "PyPI" {
		t.Fatalf("update = %v, %v", updated, err)
	}
	if lastDone != lastTotal || lastTotal <= 0 {
		t.Errorf("progress ended at %d of %d", lastDone, lastTotal)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %v, %v", info.Mode(), err)
		}
		if info, err := os.Stat(indexPath(dir, "PyPI")); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("index mode = %v, %v", info.Mode(), err)
		}
	}

	pkgs := []Package{
		{"PyPI", "Jinja2", "2.11.2", "poetry.lock"},
		{"PyPI", "jinja2", "2.11.3", "requirements.txt"},
		{"PyPI", "zope_interface", "5.1.0", "requirements.txt"},
		{"PyPI", "weird", "1.0", "requirements.txt"},
		{"PyPI", "broken", "1.0-foo", "requirements.txt"},
		{"npm", "lodash", "4.17.20", "package-lock.json"},
	}
	findings, cov := Offline(dir).Check(ctx, pkgs)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	j, z := findings[0], findings[1]
	if j.Package.Name != "Jinja2" || len(j.Advisories) != 1 {
		t.Fatalf("jinja2 finding = %+v (the withdrawn advisory must not match)", j)
	}
	if a := j.Advisories[0]; a.ID != "PYSEC-2021-66" || a.Summary != "This affects the package jinja2 from 0.0.0 and before 2.11.3." ||
		len(a.Fixed) != 1 || a.Fixed[0] != "2.11.3" || len(a.Aliases) != 2 || a.Severity != "" {
		t.Errorf("jinja2 advisory = %+v", a)
	}
	if a := z.Advisories[0]; z.Package.Name != "zope_interface" || a.ID != "GHSA-enum" || a.Severity != "critical" || a.Score != 9.8 {
		t.Errorf("zope finding = %+v", z)
	}
	if cov.Mode != "offline" || cov.Checked["PyPI"] != 3 || !cov.DatabaseDates["PyPI"].Equal(published) {
		t.Errorf("coverage = %+v", cov)
	}
	if u := cov.Unchecked["PyPI"]; !strings.Contains(u, "weird@1.0") || !strings.Contains(u, "broken@1.0-foo") || !strings.Contains(u, unparseable) {
		t.Errorf("PyPI unchecked = %q", u)
	}
	if u := cov.Unchecked["npm"]; !strings.Contains(u, "no offline database") {
		t.Errorf("npm unchecked = %q", u)
	}

	status, err := LocalStatus(dir)
	if err != nil || len(status) != 1 || status[0].Ecosystem != "PyPI" || !status[0].Published.Equal(published) ||
		status[0].Downloaded.IsZero() || status[0].Bytes <= 0 {
		t.Errorf("status = %+v, %v", status, err)
	}
	info, err := remote(ctx, srv.Client(), srv.URL, []string{"PyPI"})
	if err != nil || !info["PyPI"].Published.Equal(published) || info["PyPI"].Bytes <= 0 {
		t.Errorf("remote = %+v, %v", info, err)
	}
}

func TestOfflineUpdateSkipsAndReplaces(t *testing.T) {
	b, srv := newBucket(t)
	ctx := context.Background()
	dir := t.TempDir()
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	b.set("PyPI", buildZip(t, pypiRecords), old)
	if _, err := update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI"}, nil); err != nil {
		t.Fatal(err)
	}

	// Not newer: no download.
	updated, err := update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI", "PyPI"}, nil)
	if err != nil || len(updated) != 0 || b.gets.Load() != 1 {
		t.Errorf("unchanged database: updated %v, %v, %d downloads", updated, err, b.gets.Load())
	}

	// Newer: replaced, so jinja2 no longer matches.
	newer := old.Add(24 * time.Hour)
	b.set("PyPI", buildZip(t, map[string]string{"GHSA-enum.json": pypiRecords["GHSA-enum.json"]}), newer)
	updated, err = update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI"}, nil)
	if err != nil || len(updated) != 1 {
		t.Fatalf("newer database: updated %v, %v", updated, err)
	}
	findings, cov := Offline(dir).Check(ctx, []Package{{"PyPI", "jinja2", "2.11.2", "requirements.txt"}})
	if len(findings) != 0 || !cov.DatabaseDates["PyPI"].Equal(newer) || cov.Checked["PyPI"] != 1 {
		t.Errorf("after replace: %+v, %+v", findings, cov)
	}
	assertNoTempFiles(t, dir)
}

func TestOfflineInterruptedDownloadKeepsOldCopy(t *testing.T) {
	b, srv := newBucket(t)
	ctx := context.Background()
	dir := t.TempDir()
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	b.set("PyPI", buildZip(t, pypiRecords), old)
	if _, err := update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI"}, nil); err != nil {
		t.Fatal(err)
	}
	b.set("PyPI", buildZip(t, map[string]string{"GHSA-enum.json": pypiRecords["GHSA-enum.json"]}), old.Add(time.Hour))
	b.mu.Lock()
	b.truncate = true
	b.mu.Unlock()

	updated, err := update(ctx, srv.Client(), srv.URL, dir, []string{"PyPI"}, nil)
	if err == nil || len(updated) != 0 {
		t.Fatalf("interrupted download: updated %v, err %v", updated, err)
	}
	findings, cov := Offline(dir).Check(ctx, []Package{{"PyPI", "jinja2", "2.11.2", "requirements.txt"}})
	if len(findings) != 1 || !cov.DatabaseDates["PyPI"].Equal(old) {
		t.Errorf("old copy not used: %+v, %+v", findings, cov)
	}
	assertNoTempFiles(t, dir)
}

func TestUpdateRejectsUnknownEcosystem(t *testing.T) {
	_, srv := newBucket(t)
	for _, eco := range []string{"Maven", "../PyPI", "pypi"} {
		if _, err := update(context.Background(), srv.Client(), srv.URL, t.TempDir(), []string{eco}, nil); err == nil {
			t.Errorf("%q accepted", eco)
		}
	}
}

func TestUpdateReportsServerErrors(t *testing.T) {
	_, srv := newBucket(t) // serves nothing: every request is 404
	updated, err := update(context.Background(), srv.Client(), srv.URL, t.TempDir(), []string{"npm", "Go"}, nil)
	if err == nil || len(updated) != 0 || !strings.Contains(err.Error(), "npm") || !strings.Contains(err.Error(), "Go") {
		t.Errorf("update = %v, %v; want errors for both ecosystems", updated, err)
	}
}

func TestRemove(t *testing.T) {
	b, srv := newBucket(t)
	b.set("PyPI", buildZip(t, pypiRecords), time.Now())
	parent := t.TempDir()
	dir := filepath.Join(parent, "vulns")
	if _, err := update(context.Background(), srv.Client(), srv.URL, dir, []string{"PyPI"}, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".npm-123.zip.tmp"), "partial")
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir still exists: %v", err)
	}
	if err := Remove(dir); err != nil {
		t.Errorf("removing again: %v", err)
	}

	// Files Update did not write are kept, and so is the directory.
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "notes.txt"), "keep")
	writeFile(t, filepath.Join(other, "PyPI.gob"), "x")
	if err := Remove(other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "notes.txt")); err != nil {
		t.Errorf("unrelated file removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "PyPI.gob")); !os.IsNotExist(err) {
		t.Errorf("index kept: %v", err)
	}
}

func TestOfflineUnreadableIndex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, indexPath(dir, "npm"), "not gob")
	_, cov := Offline(dir).Check(context.Background(), []Package{{"npm", "lodash", "4.17.20", "package-lock.json"}})
	if !strings.Contains(cov.Unchecked["npm"], "unreadable") || cov.Checked["npm"] != 0 {
		t.Errorf("coverage = %+v", cov)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestEcosystems(t *testing.T) {
	got := Ecosystems([]Package{{Ecosystem: "npm"}, {Ecosystem: "Go"}, {Ecosystem: "npm"}, {Ecosystem: "PyPI"}})
	if strings.Join(got, ",") != "Go,PyPI,npm" {
		t.Errorf("Ecosystems = %v", got)
	}
}
