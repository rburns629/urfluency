package cli

import (
	"os"
	"testing"
)

// TestMain keeps tests away from the developer's saved settings.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "urfluency-test-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("URFLUENCY_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
