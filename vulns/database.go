package vulns

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	osvBucket = "https://osv-vulnerabilities.storage.googleapis.com"
	// indexFormat changes whenever the index layout does; an index in another
	// format is reported as needing an update.
	indexFormat = 1
	// maxZipBytes and maxRecordBytes bound what a download may hold: the
	// largest ecosystem (npm, with its malicious-package reports) is well
	// under both.
	maxZipBytes    = 4 << 30
	maxRecordBytes = 16 << 20
)

// RemoteInfo describes an ecosystem's published database.
type RemoteInfo struct {
	Published time.Time
	Bytes     int64 // download size; -1 if the server did not say
}

// Status describes a downloaded database.
type Status struct {
	Ecosystem             string
	Published, Downloaded time.Time
	Bytes                 int64 // size of the local index
}

// indexHeader is decoded alone by LocalStatus, without the index body.
type indexHeader struct {
	Format                int
	Ecosystem             string
	Published, Downloaded time.Time
}

// indexBody holds only what offline matching needs.
type indexBody struct {
	Advisories []indexAdvisory
	Packages   map[string][]indexEntry // normalized package name -> affected entries
}

type indexAdvisory struct {
	ID       string
	Aliases  []string
	Summary  string
	Severity string
	Score    float64
}

type indexEntry struct {
	Advisory int // index into Advisories
	Entry    affectedEntry
}

func indexPath(dir, ecosystem string) string { return filepath.Join(dir, ecosystem+".gob") }

func checkEcosystems(ecosystems []string) error {
	for _, e := range ecosystems {
		if !isSupported(e) {
			return fmt.Errorf("unsupported ecosystem %q", e)
		}
	}
	return nil
}

// Remote returns the size and publish date of each ecosystem's database, from
// HEAD requests, so a caller can ask before downloading. A nil client uses
// one with a 10 second timeout.
func Remote(ctx context.Context, client *http.Client, ecosystems []string) (map[string]RemoteInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return remote(ctx, client, osvBucket, ecosystems)
}

func remote(ctx context.Context, client *http.Client, base string, ecosystems []string) (map[string]RemoteInfo, error) {
	if err := checkEcosystems(ecosystems); err != nil {
		return nil, err
	}
	out := map[string]RemoteInfo{}
	for _, e := range ecosystems {
		info, err := head(ctx, client, base, e)
		if err != nil {
			return out, err
		}
		out[e] = info
	}
	return out, nil
}

func zipURL(base, ecosystem string) string { return base + "/" + ecosystem + "/all.zip" }

func head(ctx context.Context, client *http.Client, base, ecosystem string) (RemoteInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, zipURL(base, ecosystem), nil)
	if err != nil {
		return RemoteInfo{}, fmt.Errorf("check %s database: %w", ecosystem, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return RemoteInfo{}, fmt.Errorf("check %s database: %w", ecosystem, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RemoteInfo{}, fmt.Errorf("check %s database: server answered %s", ecosystem, resp.Status)
	}
	return RemoteInfo{Published: lastModified(resp), Bytes: resp.ContentLength}, nil
}

func lastModified(resp *http.Response) time.Time {
	t, err := http.ParseTime(resp.Header.Get("Last-Modified"))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// Update downloads the databases of the given ecosystems into dir, skipping
// any whose published copy is not newer than the local one, and returns the
// ecosystems it replaced. Each download is indexed and then swapped in with a
// rename, so an interrupted update leaves the previous copy usable. progress,
// if not nil, receives bytes downloaded and the total (-1 if unknown). An
// error for one ecosystem does not stop the others; errors are joined. A nil
// client uses one without a timeout, bounded by ctx.
func Update(ctx context.Context, client *http.Client, dir string, ecosystems []string, progress func(ecosystem string, done, total int64)) ([]string, error) {
	if client == nil {
		client = &http.Client{}
	}
	return update(ctx, client, osvBucket, dir, ecosystems, progress)
}

func update(ctx context.Context, client *http.Client, base, dir string, ecosystems []string, progress func(string, int64, int64)) ([]string, error) {
	if err := checkEcosystems(ecosystems); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	var updated []string
	var errs []error
	done := map[string]bool{}
	for _, e := range ecosystems {
		if done[e] {
			continue
		}
		done[e] = true
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("update %s: %w", e, err))
			continue
		}
		info, err := head(ctx, client, base, e)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if local, err := readHeader(indexPath(dir, e)); err == nil && local.Format == indexFormat &&
			!info.Published.IsZero() && !info.Published.After(local.Published) {
			continue
		}
		if err := download(ctx, client, base, dir, e, info, progress); err != nil {
			errs = append(errs, err)
			continue
		}
		updated = append(updated, e)
	}
	return updated, errors.Join(errs...)
}

// download fetches one ecosystem's zip to a temporary file, indexes it, and
// renames the index into place. The temporary files are always removed.
func download(ctx context.Context, client *http.Client, base, dir, ecosystem string, info RemoteInfo, progress func(string, int64, int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL(base, ecosystem), nil)
	if err != nil {
		return fmt.Errorf("download %s database: %w", ecosystem, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s database: %w", ecosystem, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s database: server answered %s", ecosystem, resp.Status)
	}
	published := lastModified(resp)
	if published.IsZero() {
		published = info.Published
	}

	tmp, err := os.CreateTemp(dir, "."+ecosystem+"-*.zip.tmp") // created 0600
	if err != nil {
		return fmt.Errorf("download %s database: %w", ecosystem, err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	w := &progressWriter{w: tmp, ecosystem: ecosystem, total: resp.ContentLength, report: progress}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxZipBytes+1))
	if err != nil {
		return fmt.Errorf("download %s database: %w", ecosystem, err)
	}
	if n > maxZipBytes {
		return fmt.Errorf("download %s database: larger than %d bytes", ecosystem, int64(maxZipBytes))
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fmt.Errorf("download %s database: got %d of %d bytes", ecosystem, n, resp.ContentLength)
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return fmt.Errorf("open %s database: %w", ecosystem, err)
	}
	body, err := buildIndex(ctx, zr, ecosystem)
	if err != nil {
		return fmt.Errorf("index %s database: %w", ecosystem, err)
	}
	h := indexHeader{Format: indexFormat, Ecosystem: ecosystem, Published: published, Downloaded: time.Now().UTC()}
	if err := writeIndex(dir, h, body); err != nil {
		return fmt.Errorf("save %s database: %w", ecosystem, err)
	}
	return nil
}

type progressWriter struct {
	w         io.Writer
	ecosystem string
	done      int64
	total     int64
	report    func(string, int64, int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.report != nil {
		p.report(p.ecosystem, p.done, p.total)
	}
	return n, err
}

// buildIndex reduces every record in an OSV zip to the fields matching needs.
// Withdrawn records are left out, as are records too large or malformed to
// decode; the rest of the database stays usable.
func buildIndex(ctx context.Context, zr *zip.Reader, ecosystem string) (indexBody, error) {
	body := indexBody{Packages: map[string][]indexEntry{}}
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return indexBody{}, err
		}
		if !strings.HasSuffix(f.Name, ".json") || f.FileInfo().IsDir() {
			continue
		}
		rec, ok := readRecord(f)
		if !ok || rec.ID == "" || rec.Withdrawn != "" {
			continue
		}
		adv := len(body.Advisories)
		seen := map[string]bool{}
		for _, a := range rec.Affected {
			name := normalizeName(ecosystem, a.Package.Name)
			if a.Package.Ecosystem != ecosystem || seen[name] {
				continue
			}
			seen[name] = true
			for _, e := range rec.entriesFor(ecosystem, a.Package.Name) {
				body.Packages[name] = append(body.Packages[name], indexEntry{Advisory: adv, Entry: e})
			}
		}
		if len(seen) == 0 {
			continue
		}
		sev, score := rec.severity()
		body.Advisories = append(body.Advisories, indexAdvisory{ID: rec.ID, Aliases: rec.Aliases, Summary: rec.summary(), Severity: sev, Score: score})
	}
	return body, nil
}

func readRecord(f *zip.File) (osvRecord, bool) {
	rc, err := f.Open()
	if err != nil {
		return osvRecord{}, false
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxRecordBytes+1))
	if err != nil || len(data) > maxRecordBytes {
		return osvRecord{}, false
	}
	var rec osvRecord
	if json.Unmarshal(data, &rec) != nil {
		return osvRecord{}, false
	}
	return rec, true
}

// writeIndex writes the index to a temporary file and renames it over the
// previous one, so readers see either the old index or the new one.
func writeIndex(dir string, h indexHeader, body indexBody) error {
	tmp, err := os.CreateTemp(dir, "."+h.Ecosystem+"-*.gob.tmp") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly once renamed
	bw := bufio.NewWriter(tmp)
	enc := gob.NewEncoder(bw)
	if err := enc.Encode(h); err != nil {
		tmp.Close()
		return err
	}
	if err := enc.Encode(body); err != nil {
		tmp.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), indexPath(dir, h.Ecosystem))
}

func readHeader(path string) (indexHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return indexHeader{}, err
	}
	defer f.Close()
	var h indexHeader
	if err := gob.NewDecoder(bufio.NewReader(f)).Decode(&h); err != nil {
		return indexHeader{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return h, nil
}

// errOutdated means an index was written in another format.
var errOutdated = errors.New("offline database is from another version of urFluency; update it")

func readIndex(path string) (indexHeader, indexBody, error) {
	f, err := os.Open(path)
	if err != nil {
		return indexHeader{}, indexBody{}, err
	}
	defer f.Close()
	dec := gob.NewDecoder(bufio.NewReader(f))
	var h indexHeader
	if err := dec.Decode(&h); err != nil {
		return indexHeader{}, indexBody{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if h.Format != indexFormat {
		return h, indexBody{}, errOutdated
	}
	var body indexBody
	if err := dec.Decode(&body); err != nil {
		return indexHeader{}, indexBody{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return h, body, nil
}

// LocalStatus lists the databases downloaded into dir, sorted by ecosystem.
// A missing dir has none.
func LocalStatus(dir string) ([]Status, error) {
	var out []Status
	for _, e := range supported {
		p := indexPath(dir, e)
		info, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("database status: %w", err)
		}
		h, err := readHeader(p)
		if err != nil {
			return nil, fmt.Errorf("database status: %w", err)
		}
		out = append(out, Status{Ecosystem: e, Published: h.Published, Downloaded: h.Downloaded, Bytes: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ecosystem < out[j].Ecosystem })
	return out, nil
}

// Remove deletes the databases and leftover temporary files Update wrote in
// dir, then dir itself if nothing else is in it. Other files are kept, so a
// mistaken dir loses nothing it did not get from Update.
func Remove(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove databases: %w", err)
	}
	for _, ent := range entries {
		if !ent.Type().IsRegular() || !ownFile(ent.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, ent.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove databases: %w", err)
		}
	}
	if rest, err := os.ReadDir(dir); err == nil && len(rest) == 0 {
		if err := os.Remove(dir); err != nil {
			return fmt.Errorf("remove database directory: %w", err)
		}
	}
	return nil
}

// ownFile reports whether a file name is one Update writes: an index or a
// temporary download or index file.
func ownFile(name string) bool {
	for _, e := range supported {
		if name == e+".gob" {
			return true
		}
		if strings.HasPrefix(name, "."+e+"-") && (strings.HasSuffix(name, ".zip.tmp") || strings.HasSuffix(name, ".gob.tmp")) {
			return true
		}
	}
	return false
}
