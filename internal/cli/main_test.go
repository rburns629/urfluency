package cli

import (
	"context"
	"os"
	"testing"

	"github.com/rburns629/urfluency/vulns"
)

// TestMain keeps tests away from the developer's saved settings and cache,
// and from the network: dependency checks get a checker that sends nothing.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "urfluency-test-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("URFLUENCY_CONFIG_DIR", dir)
	os.Setenv("URFLUENCY_CACHE_DIR", dir)
	newLiveChecker = func() vulns.Checker { return offlineChecker{} }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// offlineChecker reports every package unchecked, as when osv.dev cannot be
// reached.
type offlineChecker struct{}

func (offlineChecker) Check(_ context.Context, pkgs []vulns.Package) ([]vulns.Finding, vulns.Coverage) {
	cov := vulns.Coverage{Mode: "live", Checked: map[string]int{}, Unchecked: map[string]string{}}
	for _, e := range vulns.Ecosystems(pkgs) {
		cov.Unchecked[e] = "osv.dev could not be reached"
	}
	return nil, cov
}
