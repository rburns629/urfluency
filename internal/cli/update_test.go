package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.3.1", "0.3.0", true}, {"v0.10.0", "0.9.9", true}, {"v0.3.0", "0.3.0", false},
		{"v0.2.15", "0.3.0", false}, {"v1.0.0-rc1", "0.3.0", false}, {"v0.3.1", "dev", false},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestUpdateNoticeChecksOnceADay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"tag_name":"v0.4.0"}`))
	}))
	defer srv.Close()
	defer func(u string) { latestReleaseURL = u }(latestReleaseURL)
	latestReleaseURL = srv.URL

	n := updateNotice(context.Background(), "0.3.0")
	if !strings.HasPrefix(n, "urfluency 0.4.0 is available (you have 0.3.0)") {
		t.Errorf("notice = %q", n)
	}
	if n := updateNotice(context.Background(), "0.4.0"); n != "" || requests != 1 {
		t.Errorf("second run: notice %q after %d requests, want none and one request", n, requests)
	}
}

func TestNoUpdateCheckOfflineOrInCI(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	Version = "0.3.0"
	t.Setenv("URFLUENCY_CONFIG_DIR", t.TempDir())
	t.Setenv("CI", "")
	t.Setenv("URFLUENCY_NO_UPDATE_CHECK", "")
	if wantUpdateCheck([]string{"--offline", "."}) {
		t.Error("checked with --offline")
	}
	if !wantUpdateCheck([]string{"."}) {
		t.Error("no check by default")
	}
	if _, err := (settings{Offline: true}).save(); err != nil {
		t.Fatal(err)
	}
	if wantUpdateCheck([]string{"."}) {
		t.Error("checked with offline saved as the default")
	}
	if !wantUpdateCheck([]string{"--online", "."}) {
		t.Error("--online did not override the saved setting")
	}
	t.Setenv("CI", "true")
	if wantUpdateCheck([]string{"--online"}) {
		t.Error("checked in CI")
	}
	Version = "dev"
	t.Setenv("CI", "")
	if wantUpdateCheck(nil) {
		t.Error("checked for a development build")
	}
}
