package gitdata

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Blobs reads files at a revision through one long-running git cat-file
// process, instead of starting git once per file as Show does: on a large
// repository that is tens of thousands of processes. It is safe for
// concurrent use; reads take turns. Close it when done.
type Blobs struct {
	repo Repo
	ctx  context.Context
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
}

// OpenBlobs starts the cat-file process, with the same safe configuration as
// every other git command here.
func (r Repo) OpenBlobs(ctx context.Context) (*Blobs, error) {
	full := append(append(r.base(), safeConfig...), "cat-file", "--batch")
	cmd := exec.CommandContext(ctx, "git", full...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	return &Blobs{repo: r, ctx: ctx, cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10)}, nil
}

// maxBlobBytes bounds one file read into memory. Callers skip files over 2 MB
// before reading them; this guards against anything that slips past.
const maxBlobBytes = 64 << 20

// errMissing is a path cat-file does not have at the revision.
var errMissing = errors.New("not in the revision")

// Show returns a file's content at rev, as Repo.Show does.
func (b *Blobs) Show(rev, path string) ([]byte, error) {
	// cat-file reads one request per line; a path with a line break cannot
	// be asked for that way.
	if strings.ContainsAny(path, "\n\r") {
		return b.repo.Show(b.ctx, rev, path)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := io.WriteString(b.in, rev+":"+path+"\n"); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	// "<oid> <type> <size>", or "<request> missing" (or "ambiguous").
	header, err := b.out.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	// A missing object echoes the request, path and all, so check the end
	// before splitting a path with spaces into fields.
	if line := strings.TrimRight(header, "\n"); strings.HasSuffix(line, " missing") || strings.HasSuffix(line, " ambiguous") {
		return nil, fmt.Errorf("%s: %w", path, errMissing)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 {
		return nil, fmt.Errorf("git cat-file: bad header %q", strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, fmt.Errorf("git cat-file: bad header %q", strings.TrimSpace(header))
	}
	if size > maxBlobBytes {
		// Skip it, keeping the stream in step for the next request.
		if _, err := io.CopyN(io.Discard, b.out, size+1); err != nil {
			return nil, fmt.Errorf("git cat-file: %w", err)
		}
		return nil, fmt.Errorf("%s: over %d bytes", path, maxBlobBytes)
	}
	content := make([]byte, size+1) // and the newline after it
	if _, err := io.ReadFull(b.out, content); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	if fields[1] != "blob" {
		return nil, fmt.Errorf("%s: a %s, not a file", path, fields[1])
	}
	return content[:size], nil
}

// Close ends the cat-file process.
func (b *Blobs) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.in.Close()
	return b.cmd.Wait()
}
