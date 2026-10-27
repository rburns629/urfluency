package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

// progress shows what a scan is doing on stderr, so the report on stdout stays
// clean. In a terminal it redraws one line in place; otherwise, such as in CI or
// when stderr is redirected, it prints one line per repository and stage change.
type progress struct {
	mu       sync.Mutex
	w        io.Writer
	tty      bool
	prefix   string // "[3/15] montera"
	stage    string
	lastDraw time.Time
	started  time.Time
}

// redrawEvery throttles terminal redraws.
const redrawEvery = 100 * time.Millisecond

func newProgress(w io.Writer) *progress {
	return &progress{w: w, tty: isTerminal(w), started: time.Now()}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// phase shows a step before repositories are scanned, such as discovery.
func (p *progress) phase(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prefix, p.stage = text, ""
	p.emit(text)
}

// repo starts scanning repository i of n.
func (p *progress) repo(i, n int, label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prefix, p.stage = fmt.Sprintf("[%d/%d] %s", i, n, label), ""
	p.emit(p.prefix)
}

// update shows the current stage of the current repository. It is safe for
// concurrent use, as the engine requires.
func (p *progress) update(stage string, done, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tty {
		// One line per stage change keeps logs short and readable.
		if stage != p.stage {
			p.stage = stage
			fmt.Fprintf(p.w, "  %s\n", stage)
		}
		return
	}
	p.stage = stage
	final := total > 0 && done >= total
	if !final && time.Since(p.lastDraw) < redrawEvery {
		return
	}
	line := p.prefix + " · " + stage
	if total > 0 {
		line += " " + commas(done) + "/" + commas(total)
	}
	p.draw(line)
}

// note prints a message that should stay visible, such as a skipped repository.
func (p *progress) note(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty {
		fmt.Fprint(p.w, "\r\033[K")
	}
	fmt.Fprintln(p.w, msg)
}

// clear removes the progress line, so an error message starts on a clean line.
func (p *progress) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

// finish clears the progress line and reports the total time.
func (p *progress) finish(repos int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty {
		fmt.Fprint(p.w, "\r\033[K")
	}
	fmt.Fprintf(p.w, "Scanned %d %s in %s\n", repos, plural(repos, "project", "projects"), time.Since(p.started).Round(time.Second))
}

func (p *progress) emit(line string) {
	if p.tty {
		p.draw(line)
		return
	}
	fmt.Fprintln(p.w, line)
}

func (p *progress) draw(line string) {
	p.lastDraw = time.Now()
	fmt.Fprint(p.w, "\r\033[K"+line)
}

// commas formats n with thousands separators.
func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
