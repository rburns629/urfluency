package gitdata

import (
	"bufio"
	"bytes"
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AddedLines returns the lines each non-merge commit by one of emails added, per
// path. Renames are followed, so moving a file does not count as adding it.
func (r Repo) AddedLines(ctx context.Context, emails []string) (map[string]map[string]int, error) {
	args := []string{"log", "--no-merges", "-M", "--numstat", "--format=%x1e%H"}
	args = append(args, authorArgs(emails)...)
	out, err := r.git(ctx, append(args, "HEAD")...)
	if err != nil {
		return nil, err
	}
	added := map[string]map[string]int{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		lines := strings.Split(strings.TrimSpace(rec), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		perPath := map[string]int{}
		for _, l := range lines[1:] {
			fields := strings.SplitN(l, "\t", 3)
			if len(fields) != 3 {
				continue
			}
			n, err := strconv.Atoi(fields[0])
			if err != nil { // "-" for binary files
				continue
			}
			perPath[renamedTo(fields[2])] += n
		}
		added[lines[0]] = perPath
	}
	return added, nil
}

var (
	braceRename = regexp.MustCompile(`\{[^{}]* => ([^{}]*)\}`)
	slashes     = regexp.MustCompile(`/{2,}`)
)

// renamedTo returns the new path of a numstat rename such as "a/{b => c}/d" or
// "old => new".
func renamedTo(p string) string {
	if strings.Contains(p, "{") {
		return slashes.ReplaceAllString(braceRename.ReplaceAllString(p, "$1"), "/")
	}
	if _, after, ok := strings.Cut(p, " => "); ok {
		return after
	}
	return p
}

// TouchedPaths returns the current paths of files that a non-merge commit by one
// of emails ever changed, following later renames by anyone, so a file the user
// wrote that someone else renamed still counts.
func (r Repo) TouchedPaths(ctx context.Context, emails []string) (map[string]bool, error) {
	args := []string{"log", "--no-merges", "--name-only", "--format="}
	args = append(args, authorArgs(emails)...)
	out, err := r.git(ctx, append(args, "HEAD")...)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	for _, p := range strings.Split(string(out), "\n") {
		if p != "" {
			touched[p] = true
		}
	}
	if len(touched) == 0 {
		return touched, nil
	}
	// Renames by any author, newest first: "R100\told\tnew".
	out, err = r.git(ctx, "log", "--no-merges", "-M", "--name-status", "--diff-filter=R", "--format=", "HEAD")
	if err != nil {
		return nil, err
	}
	renamed := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Split(l, "\t"); len(f) == 3 && strings.HasPrefix(f[0], "R") {
			renamed[f[1]] = f[2]
		}
	}
	for p := range touched {
		for range 100 { // bounded, in case of rename cycles
			next, ok := renamed[p]
			if !ok || touched[next] {
				break
			}
			touched[next] = true
			p = next
		}
	}
	return touched, nil
}

// FixCommit is a non-merge commit whose subject marks a bug fix.
type FixCommit struct {
	Hash string
	Time time.Time
}

// fixSubject matches commit subjects that mark a fix, including Conventional
// Commits ("fix: ..."), "fixes #12", "bugfix", "hotfix", and reverts.
var fixSubject = regexp.MustCompile(`(?i)\b(fix(es|ed|ing)?|bug ?fix|hotfix|bug|revert)\b`)

// FixCommits returns the fix commits reachable from HEAD, by any author.
func (r Repo) FixCommits(ctx context.Context) ([]FixCommit, error) {
	out, err := r.git(ctx, "log", "--no-merges", "--format=%H%x1f%at%x1f%s", "HEAD")
	if err != nil {
		return nil, err
	}
	var fixes []FixCommit
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(line, "\x1f", 3)
		if len(fields) != 3 || !fixSubject.MatchString(fields[2]) {
			continue
		}
		secs, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		fixes = append(fixes, FixCommit{Hash: fields[0], Time: time.Unix(secs, 0)})
	}
	return fixes, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? `)

// FixedLines maps each commit that last changed a line the fix removed or
// rewrote to the number of such lines, for paths keep accepts. This is the SZZ
// method: the fix's parent is blamed for the lines the fix touched.
func (r Repo) FixedLines(ctx context.Context, fix string, keep func(path string) bool) (map[string]int, error) {
	out, err := r.git(ctx, "diff", "-U0", "-M", "--no-color", "--no-ext-diff", "--no-textconv", fix+"^", fix)
	if err != nil {
		return nil, err
	}
	ranges := map[string][]string{} // old path → "-L start,+count" arguments
	path := ""
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			path = ""
			if p, ok := strings.CutPrefix(line, "--- a/"); ok && keep(p) {
				path = p
			}
		case strings.HasPrefix(line, "@@ ") && path != "":
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			if count > 0 {
				ranges[path] = append(ranges[path], "-L", m[1]+",+"+strconv.Itoa(count))
			}
		}
	}
	fixed := map[string]int{}
	for p, args := range ranges {
		blame := append([]string{"blame", "--porcelain", "-w", "--no-textconv"}, args...)
		out, err := r.git(ctx, append(blame, fix+"^", "--", p)...)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 3 && (len(fields[0]) == 40 || len(fields[0]) == 64) && isHex(fields[0]) {
				fixed[fields[0]]++
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return fixed, nil
}
