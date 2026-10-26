package secrets

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/internal/analysis"
)

// Fake credentials are assembled at runtime so this file contains nothing
// token-shaped for secret scanners or readers to mistake for a real secret.
func fake(parts ...string) string { return strings.Join(parts, "") }

func scan(t *testing.T, path, content string) []string {
	t.Helper()
	got, err := Scanner{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(content)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestKnownFormats(t *testing.T) {
	lines := []string{
		fake("-----BEGIN ", "RSA PRIVATE KEY-----"),
		fake("AWS_KEY=", "AK", "IA", strings.Repeat("Q", 16)),
		fake("token: ", "gh", "p_", strings.Repeat("a", 36)),
		fake("SLACK=", "xo", "xb-", "1234567890-abcdefghij"),
		fake("stripe = '", "sk", "_live_", strings.Repeat("b", 24), "'"),
		fake("key=", "AI", "za", strings.Repeat("c", 35)),
		fake("ANTHROPIC=", "sk-", "ant-", strings.Repeat("d", 30)),
		"nothing to see here",
	}
	want := []string{"sec.api-token:3", "sec.api-token:4", "sec.api-token:5", "sec.api-token:6", "sec.api-token:7",
		"sec.cloud-credential:2", "sec.private-key:1"}
	// Known formats apply even in test files: a real token is a leak anywhere.
	for _, p := range []string{"config/app.yaml", "src/app.test.ts"} {
		if got := scan(t, p, strings.Join(lines, "\n")); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", p, got, want)
		}
	}
}

func TestGenericSecrets(t *testing.T) {
	lines := []string{
		fake(`DATABASE_URL = "postgres://admin:`, "Zq8vLm2pW", `@db.internal/app"`),
		fake(`password = "`, "Xk9mP2vL7qR4", `"`),
		`password = "changeme123"`,
		`api_key = "${API_KEY}"`,
		`url = "postgres://user:pass@localhost/db"`,
		`secret = "aaaaaaaaaaaa"`,
		`password = os.environ["DB_PASSWORD"]`,
		`DATABASE_URL=postgres://app:{DB_PASSWORD}@db.internal/app`,
	}
	content := strings.Join(lines, "\n")
	want := []string{"sec.credentials-in-url:1", "sec.hardcoded-password:2"}
	if got := scan(t, "app/settings.py", content); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := scan(t, "tests/test_db.py", content); len(got) != 0 {
		t.Errorf("generic checks should skip test files, got %v", got)
	}
}

func TestApplies(t *testing.T) {
	for p, want := range map[string]bool{
		"config/app.ini": true, "config/prod.yaml": true, "src/app.ts": true,
		"package-lock.json": false, "go.sum": false, "logo.png": false, "vendor.min.js": false,
	} {
		if got := (Scanner{}).Applies(p); got != want {
			t.Errorf("Applies(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestFindingsCarryNoValue(t *testing.T) {
	// analysis.Finding has only Rule, Path, and Line; this guards against adding a
	// field that could carry a matched secret.
	fields := reflect.TypeOf(analysis.Finding{}).NumField()
	if fields != 3 {
		t.Fatalf("analysis.Finding has %d fields; secrets must never be stored on findings", fields)
	}
}

func TestCommentsAndVariableReferencesAreNotSecrets(t *testing.T) {
	lines := []string{
		fake(`# e.g. postgres://admin:`, "Zq8vLm2pW", `@db.internal/app`),
		fake(`  // url: "mysql://root:`, "Zq8vLm2pW", `@db/app"`),
		`PGPASSWORD="$(DB_PASSWORD)"`,
		`password: "$(POSTGRES_PASSWORD)"`,
		`secret = "$$CLIENT_SECRET_VALUE"`,
		`DATABASE_URL=postgres://app:$(DB_PASSWORD)@db.internal/app`,
		"# token: " + "gh" + "p_" + strings.Repeat("z", 36),
	}
	want := []string{"sec.api-token:7"}
	if got := scan(t, "deploy/values.yaml", strings.Join(lines, "\n")); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want only the real token format in a comment: %v", got, want)
	}
}
