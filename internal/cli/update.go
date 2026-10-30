package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The update notice tells people running an old version that a newer one is
// out. It asks GitHub for the latest release at most once a day, sends nothing
// about the user, and never runs offline, in CI, or for development builds.
var (
	latestReleaseURL = "https://api.github.com/repos/rburns629/urfluency/releases/latest"
	updateEvery      = 24 * time.Hour
	updateTimeout    = 2 * time.Second
)

// updateCommands are the commands that show the notice; the rest are quick or
// print machine-readable output.
var updateCommands = map[string]bool{"scan": true, "check": true, "upload": true, "doctor": true}

// updateCache is what the last check found, so the next day's runs need no
// request.
type updateCache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// startUpdateCheck starts the check for a command and returns a function that
// gives the notice, or "", waiting at most a moment for the answer: the
// command never waits on the network.
func startUpdateCheck(ctx context.Context, args []string) func() string {
	if !updateCommands[args[0]] || !wantUpdateCheck(args[1:]) {
		return func() string { return "" }
	}
	done := make(chan string, 1)
	go func() { done <- updateNotice(ctx, Version) }()
	return func() string {
		select {
		case n := <-done:
			return n
		case <-time.After(300 * time.Millisecond):
			return ""
		}
	}
}

// wantUpdateCheck reports whether this run may check: not offline, not in CI,
// not turned off, and not a development build.
func wantUpdateCheck(args []string) bool {
	if _, ok := parseVersion(Version); !ok || os.Getenv("URFLUENCY_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return false
	}
	if slices.Contains(args, "--offline") {
		return false
	}
	if slices.Contains(args, "--online") {
		return true
	}
	s, err := loadSettings()
	return err == nil && !s.Offline
}

// updateNotice returns a one-line notice when a newer release than current
// exists, or "".
func updateNotice(ctx context.Context, current string) string {
	cachePath := ""
	if base, err := os.UserCacheDir(); err == nil {
		cachePath = filepath.Join(base, "urfluency", "update-check.json")
	}
	var c updateCache
	if cachePath != "" {
		if data, err := os.ReadFile(cachePath); err == nil {
			_ = json.Unmarshal(data, &c) // a damaged cache is checked again
		}
	}
	if time.Since(c.Checked) >= updateEvery {
		latest, err := latestRelease(ctx)
		if err != nil {
			return ""
		}
		c = updateCache{Checked: time.Now().UTC(), Latest: latest}
		if cachePath != "" && os.MkdirAll(filepath.Dir(cachePath), 0o700) == nil {
			if data, err := json.Marshal(c); err == nil {
				_ = os.WriteFile(cachePath, data, 0o600) // only a cache
			}
		}
	}
	if !newer(c.Latest, current) {
		return ""
	}
	return fmt.Sprintf("urfluency %s is available (you have %s): brew upgrade urfluency, or see https://github.com/rburns629/urfluency/releases",
		strings.TrimPrefix(c.Latest, "v"), strings.TrimPrefix(current, "v"))
}

// latestRelease asks GitHub for the latest release's tag.
func latestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("latest release: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

// newer reports whether release version a is newer than b.
func newer(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	return slices.Compare(va[:], vb[:]) > 0
}

// parseVersion reads a release version such as v0.3.0 or 0.3.0.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
