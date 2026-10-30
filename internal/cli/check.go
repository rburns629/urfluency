package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/safety"
)

// check scans a repository for signs of malicious behavior before it is run.
// A URL is cloned defensively into a temporary folder that is removed after
// the scan; a local path is read in place. Nothing is executed or uploaded.
func check(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	offlineFlag := fs.Bool("offline", false, "")
	onlineFlag := fs.Bool("online", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	saved, err := loadSettings()
	if err != nil {
		return fmt.Errorf("read settings (run urfluency init to rewrite them): %w", err)
	}
	offline, err := offlineMode(*offlineFlag, *onlineFlag, saved)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: check takes one repository URL or path", errUsage)
	}
	target := fs.Arg(0)
	dir := target
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		repoURL, err := safety.ValidateURL(target, nil)
		if err != nil {
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		tmp, err := os.MkdirTemp("", "urfluency-check-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = filepath.Join(tmp, "repo")
		prog := newProgress(stderr)
		prog.phase("Cloning " + repoURL + " (hooks, submodules, and LFS off)")
		err = safety.Clone(ctx, repoURL, dir, safety.DefaultCloneLimits)
		prog.clear()
		if err != nil {
			return err
		}
		target = repoURL
	}
	deps := make(chan *report.Vulnerabilities, 1)
	go func() { deps <- checkDependencies(ctx, dir, []string{dir}, offline) }()
	r, err := safety.Scan(ctx, dir, safety.DefaultLimits)
	if err != nil {
		return err
	}
	v := <-deps
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Target string `json:"target"`
			safety.Report
			Vulnerabilities *report.Vulnerabilities `json:"vulnerabilities"`
		}{target, r, v})
	}
	writeSafety(stdout, target, r)
	return report.WriteVulnerabilities(stdout, v)
}

func writeSafety(w io.Writer, target string, r safety.Report) {
	fmt.Fprintf(w, "urfluency check · %s · rules %s\n\n", target, r.Version)
	fmt.Fprintf(w, "%s · score %d/100 · %d %s checked", r.Verdict, r.Score, r.Files, plural(r.Files, "file", "files"))
	if r.Truncated {
		fmt.Fprint(w, " (file limit reached; the rest were not read)")
	}
	fmt.Fprint(w, "\n\n")
	for _, c := range r.Counts {
		status := "none found"
		if c.Findings > 0 {
			status = fmt.Sprintf("%d %s", c.Findings, map[bool]string{true: "indicator", false: "indicators"}[c.Findings == 1])
		}
		fmt.Fprintf(w, "  %-24s %s\n", c.Title, status)
	}
	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "\nFINDINGS")
	}
	for _, f := range r.Findings {
		where := f.Path
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d", f.Path, f.Line)
		}
		fmt.Fprintf(w, "  [%s, %s confidence] %s\n    at %s\n", strings.ToUpper(string(f.Severity)), f.Confidence, f.Title, where)
		if f.Evidence != "" {
			fmt.Fprintf(w, "    > %s\n", f.Evidence)
		}
		fmt.Fprintf(w, "    %s\n", f.Why)
	}
	fmt.Fprintf(w, "\n%s Secrets are reported by type and location, never shown.\n", r.Caveat)
}
