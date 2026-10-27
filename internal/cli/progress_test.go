package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressWithoutTerminal(t *testing.T) {
	var b bytes.Buffer
	p := newProgress(&b) // a buffer is not a terminal
	p.phase("Finding repositories under ~/Projects")
	p.repo(1, 2, "api")
	p.update("reading files", 0, 300)
	p.update("reading files", 150, 300)
	p.update("tracing fixes", 1, 9)
	p.note("urfluency: skipping web: no commits")
	p.finish(1)
	want := "Finding repositories under ~/Projects\n[1/2] api\n  reading files\n  tracing fixes\nurfluency: skipping web: no commits\nScanned 1 repository in 0s\n"
	if b.String() != want {
		t.Errorf("output:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestProgressInTerminal(t *testing.T) {
	var b bytes.Buffer
	p := newProgress(&b)
	p.tty = true
	p.repo(3, 15, "montera")
	p.update("reading files", 1, 1234) // throttled: drawn too soon after repo
	p.update("reading files", 1234, 1234)
	p.finish(15)
	out := b.String()
	if !strings.Contains(out, "\r\033[K[3/15] montera") || !strings.Contains(out, "[3/15] montera · reading files 1,234/1,234") {
		t.Errorf("output: %q", out)
	}
	if strings.Contains(out, "reading files 1/1,234") {
		t.Error("a redraw within the throttle window was not skipped")
	}
	if !strings.HasSuffix(out, "\r\033[KScanned 15 repositories in 0s\n") {
		t.Errorf("finish should clear the line: %q", out)
	}
}

func TestCommas(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := commas(n); got != want {
			t.Errorf("commas(%d) = %q, want %q", n, got, want)
		}
	}
}
