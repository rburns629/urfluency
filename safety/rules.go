package safety

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// lineRule matches single lines of the files it applies to.
type lineRule struct {
	id, category, title, why string
	severity                 Severity
	confidence               Confidence
	re                       *regexp.Regexp
	applies                  func(p string) bool // nil: every text file
}

func ext(p string) string { return strings.ToLower(path.Ext(p)) }

func isDoc(p string) bool {
	switch ext(p) {
	case ".md", ".rst", ".txt", ".adoc", ".html":
		return true
	}
	return false
}

func isCode(p string) bool { return !isDoc(p) }

// isScript is where a pipe to a shell runs as written.
func isScript(p string) bool {
	base := path.Base(p)
	switch {
	case base == "Makefile" || base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || base == "package.json":
		return true
	case strings.HasPrefix(p, ".github/workflows/"):
		return true
	}
	switch ext(p) {
	case ".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd", ".mk":
		return true
	}
	return false
}

// isPHP and isRuby are source files of each language, where their own
// web-shell and evaluation patterns run as written.
func isPHP(p string) bool {
	switch ext(p) {
	case ".php", ".phtml", ".inc":
		return true
	}
	return false
}

func isRuby(p string) bool {
	switch ext(p) {
	case ".rb", ".rake", ".gemspec", ".ru":
		return true
	}
	base := path.Base(p)
	return base == "Gemfile" || base == "Rakefile"
}

var lineRules = []lineRule{
	// PHP and Ruby: code or commands taken straight from a request, and
	// evaluated payloads. These are the shapes web shells take.
	{"safety.php-request-exec", "remote-code", "Runs code or commands taken from a web request",
		"Passing request input straight to eval, assert, or a shell function lets anyone who can reach the page run code on the server: the shape of a PHP web shell.",
		High, Likely, regexp.MustCompile(`(?i)\b(eval|assert|create_function|system|exec|shell_exec|passthru|popen|proc_open|pcntl_exec)\s*\(\s*@?\s*\$_(GET|POST|REQUEST|COOKIE|SERVER|FILES)\b`), isPHP},
	{"safety.php-eval-decoded", "obfuscation", "Evaluates a decoded or decompressed PHP payload",
		"eval or assert of base64_decode, gzinflate, or str_rot13 output hides the code that runs from anyone reading the file.",
		High, Likely, regexp.MustCompile(`(?i)\b(eval|assert)\s*\(\s*@?\s*(base64_decode|gzinflate|gzuncompress|gzdecode|str_rot13|hex2bin|convert_uudecode)\s*\(`), isPHP},
	{"safety.php-preg-eval", "remote-code", "Regular expression replacement evaluated as PHP (/e)",
		"preg_replace with the /e modifier evaluates the replacement as PHP code; it was removed from PHP 7 because it ran injected code.",
		Medium, Likely, regexp.MustCompile(`\bpreg_replace\s*\(\s*['"]/[^'"]*/[a-zA-Z]*e[a-zA-Z]*['"]`), isPHP},
	{"safety.ruby-eval-fetched", "remote-code", "Evaluates downloaded or decoded Ruby code",
		"eval of a download or a decoded string runs code that is not in the repository and cannot be reviewed.",
		High, Likely, regexp.MustCompile(`\b(eval|instance_eval|class_eval|module_eval)\b\s*\(?\s*(Base64\.(strict_|urlsafe_)?decode64|URI\.open|URI\.parse\([^)]*\)\.read|Net::HTTP\.get|Zlib::Inflate\.inflate|open\(\s*['"]https?:)`), isRuby},
	{"safety.ruby-params-exec", "remote-code", "Runs a command or code from request parameters",
		"Passing params to system, exec, eval, or a backtick lets anyone who can send a request run commands on the server.",
		High, Likely, regexp.MustCompile(`\b(system|exec|spawn|eval|instance_eval|IO\.popen|Open3\.\w+)\b\s*\(?\s*params\[`), isRuby},
	// Remote code execution.
	{"safety.pipe-to-shell", "remote-code", "Downloads a script and runs it",
		"Piping a download straight into a shell runs whatever the server returns, with no chance to read it first.",
		Medium, Possible, regexp.MustCompile(`(?i)\b(curl|wget)\b[^|\n]*\|\s*(sudo\s+)?(ba|z|da)?sh\b`), isScript},
	{"safety.pipe-to-shell-docs", "remote-code", "Install instructions pipe a download into a shell",
		"Common in install docs, but the script should be pinned to a version and checked before running.",
		Low, Weak, regexp.MustCompile(`(?i)\b(curl|wget)\b[^|\n]*\|\s*(sudo\s+)?(ba|z|da)?sh\b`), isDoc},
	{"safety.powershell-download-exec", "remote-code", "PowerShell downloads and runs code",
		"Invoke-Expression on downloaded content runs remote code directly.",
		High, Likely, regexp.MustCompile(`(?i)\b(iex|invoke-expression)\b\s*\(?\s*\(?\s*(iwr|irm|invoke-webrequest|invoke-restmethod|new-object\s+net\.webclient)`), isCode},
	{"safety.exec-decoded", "obfuscation", "Runs decoded or decompressed code",
		"Executing code that is decoded at run time hides what it does from readers and reviewers.",
		High, Likely, regexp.MustCompile(`\b(exec|eval)\s*\(\s*(compile\s*\(\s*)?(base64\.b64decode|zlib\.decompress|marshal\.loads|codecs\.decode|bytes\.fromhex|atob|Buffer\.from)\b`), isCode},
	{"safety.exec-fetched", "remote-code", "Runs code fetched from the network",
		"Evaluating a network response runs whatever the server sends.",
		High, Likely, regexp.MustCompile(`\b(exec|eval)\s*\(\s*(requests\.get|urllib\.request\.urlopen|urlopen|await\s+fetch|fetch)\s*\(`), isCode},

	// Data collection.
	{"safety.chat-webhook", "data-collection", "Sends data to a Discord or Telegram endpoint",
		"Chat webhooks are a common channel for quietly sending stolen data.",
		High, Possible, regexp.MustCompile(`(?i)(discord(app)?\.com/api/webhooks/|api\.telegram\.org/bot)`), isCode},
	{"safety.env-dump", "data-collection", "Serializes every environment variable",
		"Environment variables often hold tokens and keys; collecting all of them is rarely needed.",
		Medium, Possible, regexp.MustCompile(`JSON\.stringify\(\s*process\.env\s*\)|dict\(\s*os\.environ\s*\)|os\.environ\.copy\(\)|json\.dumps\(\s*(dict\()?os\.environ`), isCode},

	// Credential access.
	{"safety.ssh-keys", "credentials", "Reads SSH private keys",
		"Code that opens ~/.ssh keys can impersonate the user on every server they can reach.",
		Medium, Possible, regexp.MustCompile(`\.ssh/(id_(rsa|ed25519|ecdsa|dsa)|identity)\b`), isCode},
	{"safety.cloud-credentials", "credentials", "Reads cloud or container credentials",
		"Cloud, Kubernetes, and Docker credential files grant access to infrastructure.",
		Medium, Possible, regexp.MustCompile(`\.aws/credentials|\.config/gcloud|application_default_credentials\.json|\.kube/config|\.docker/config\.json`), isCode},
	{"safety.tool-credentials", "credentials", "Reads package or HTTP credential files",
		"Package managers and HTTP clients read .netrc, .npmrc, and .pypirc legitimately; elsewhere it is worth a look.",
		Low, Weak, regexp.MustCompile(`\.npmrc|\.pypirc|\.netrc`), isCode},
	{"safety.browser-data", "credentials", "Reads browser profiles",
		"Browser Login Data, Cookies, and Local State files hold saved passwords and sessions.",
		High, Possible, regexp.MustCompile(`(?i)(google[/\\]chrome|chromium|bravesoftware|microsoft[/\\]edge|mozilla[/\\]firefox)[^"'\n]{0,80}(login data|cookies|local state|logins\.json|key4\.db)`), isCode},
	{"safety.keychain", "credentials", "Reads the macOS keychain",
		"Querying the keychain from code can extract saved passwords.",
		Medium, Possible, regexp.MustCompile(`\bsecurity\s+(find-generic-password|find-internet-password|dump-keychain)`), isCode},
	{"safety.crypto-wallets", "credentials", "Looks for cryptocurrency wallets",
		"Wallet files and wallet browser extensions are a frequent target of stealers.",
		High, Possible, regexp.MustCompile(`(?i)(exodus[/\\]exodus\.wallet|electrum[/\\]wallets|nkbihfbeogaeaoehlefnkodbefgpgknn|metamask[/\\]|wallet\.dat\b)`), isCode},

	// Persistence and abuse.
	{"safety.reverse-shell", "persistence", "Opens a reverse shell",
		"Connecting a shell to a remote host gives that host control of the machine.",
		High, Likely, regexp.MustCompile(`/dev/tcp/[\w.-]+/\d+|\bnc(at)?\s+(-\w+\s+)*-[ec]\s+/?(bin/)?(ba)?sh|pty\.spawn\(\s*["']/bin/(ba)?sh`), isCode},
	{"safety.crypto-miner", "persistence", "References a cryptocurrency miner",
		"Mining pools and miners use the machine's resources for someone else.",
		High, Possible, regexp.MustCompile(`(?i)stratum\+(tcp|ssl)://|\bxmrig\b|\bcoinhive\b|\bcryptonight\b`), isCode},
	{"safety.cron", "persistence", "Installs a scheduled task",
		"Writing to crontab or cron directories keeps code running after the program exits.",
		Medium, Possible, regexp.MustCompile(`\bcrontab\s+-|/etc/cron\.|/var/spool/cron`), isCode},
	{"safety.launch-agent", "persistence", "Installs a macOS launch agent",
		"Launch agents and daemons start code at every login or boot.",
		Medium, Possible, regexp.MustCompile(`Library/Launch(Agents|Daemons)`), isCode},
	{"safety.windows-run-key", "persistence", "Adds a Windows startup entry",
		"Run keys start code at every login.",
		Medium, Possible, regexp.MustCompile(`(?i)currentversion\\+run(once)?\b`), isCode},
	{"safety.shell-profile", "persistence", "Appends to a shell startup file",
		"Changing .bashrc or .zshrc runs code in every new terminal.",
		Medium, Possible, regexp.MustCompile(`>>\s*["']?(~|\$HOME|\$\{HOME\})/\.(bashrc|zshrc|bash_profile|profile)`), isScript},
	{"safety.shell-profile-text", "persistence", "Mentions appending to a shell startup file",
		"Usually setup instructions printed for the user, such as shell completions; worth confirming nothing writes it automatically.",
		Low, Weak, regexp.MustCompile(`>>\s*["']?(~|\$HOME|\$\{HOME\})/\.(bashrc|zshrc|bash_profile|profile)`), func(p string) bool { return isCode(p) && !isScript(p) }},

	// Supply chain.
	{"safety.docker-add-url", "supply-chain", "Dockerfile adds a file from a URL",
		"ADD with a URL fetches whatever is there at build time, unpinned and unverified.",
		Low, Possible, regexp.MustCompile(`(?i)^\s*ADD\s+https?://`), func(p string) bool {
			b := path.Base(p)
			return b == "Dockerfile" || strings.HasPrefix(b, "Dockerfile.")
		}},
}

// printedText recognizes lines that print a message: echo, printf, Make's
// $(warning …), $(info …), and $(error …), and Write-Host.
var printedText = regexp.MustCompile(`(?i)^\s*(@?echo\b|printf\b|\$\((warning|info|error)\s|write-host\b)`)

// patternStart finds a regular expression built from a string literal:
// Go, Python, JavaScript, Ruby, Java, C#, and Rust, up to the literal's
// opening quote. PHP's preg functions are left out: php-preg-eval matches
// their patterns on purpose.
var patternStart = regexp.MustCompile(`(\bregexp\.(Must)?Compile(POSIX)?|\bre\.(compile|search|match|fullmatch|findall|finditer)|\bRegExp|\bRegexp\.new|\bPattern\.compile|\bnew\s+Regex|\bRegex::new)\s*\(\s*[rRbu@]*(["'\x60])`)

// inPattern reports whether every match of re on line falls inside the string
// literal of a regular expression: a scanner's own rules, which match text
// rather than run it.
func inPattern(re *regexp.Regexp, line string) bool {
	var literals [][2]int
	for _, m := range patternStart.FindAllStringSubmatchIndex(line, -1) {
		open, quote := m[len(m)-1], line[m[len(m)-2]]
		end := open
		for end < len(line) && line[end] != quote {
			if line[end] == '\\' && quote != '`' {
				end++
			}
			end++
		}
		literals = append(literals, [2]int{open, end})
	}
	matches := re.FindAllStringIndex(line, -1)
	if len(literals) == 0 || len(matches) == 0 {
		return false
	}
	for _, m := range matches {
		inside := false
		for _, l := range literals {
			inside = inside || (m[0] >= l[0] && m[1] <= l[1])
		}
		if !inside {
			return false
		}
	}
	return true
}

// isCommentLine recognizes whole-line comments in common languages.
func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	for _, p := range []string{"#", "//", "/*", "*", "--", ";", "rem ", "REM "} {
		if strings.HasPrefix(t, p) && !strings.HasPrefix(t, "#!") {
			return true
		}
	}
	return false
}

var (
	// Go caps repeat counts at 1000, so 2,000+ characters is two runs.
	longBase64 = regexp.MustCompile(`["'` + "`" + `][A-Za-z0-9+/]{1000}[A-Za-z0-9+/]{1000,}={0,2}["'` + "`" + `]`)
	hexEscapes = regexp.MustCompile(`(\\x[0-9a-fA-F]{2}){80,}`)
)

// checkFile applies the line rules and the file-level rules to one file.
func checkFile(f analysis.File) []Finding {
	if isBinary(f.Content) {
		if isExecutable(f.Content) {
			return []Finding{{Rule: "safety.executable", Category: "obfuscation", Title: "Committed executable",
				Severity: Medium, Confidence: Possible, Path: f.Path,
				Why: "A compiled program in the repository cannot be read or checked against its source."}}
		}
		return nil
	}
	var out []Finding
	lines := strings.Split(string(f.Content), "\n")
	for i, line := range lines {
		if len(line) > 20000 {
			line = line[:20000]
		}
		comment := isCode(f.Path) && isCommentLine(line)
		for _, r := range lineRules {
			if r.applies != nil && !r.applies(f.Path) {
				continue
			}
			if comment || !r.re.MatchString(line) {
				continue
			}
			fd := Finding{Rule: r.id, Category: r.category, Title: r.title, Severity: r.severity, Confidence: r.confidence,
				Path: f.Path, Line: i + 1, Evidence: excerpt(line), Why: r.why}
			// Text printed for a person to read runs nothing by itself.
			if fd.Severity != Low && printedText.MatchString(line) {
				fd.Severity, fd.Confidence = Low, Weak
				fd.Why = "Printed as a message rather than run here; " + strings.ToLower(fd.Why[:1]) + fd.Why[1:]
			}
			// A pattern that looks for the behavior, such as a scanner's rule.
			if fd.Severity != Low && inPattern(r.re, line) {
				fd.Severity, fd.Confidence = Low, Weak
				fd.Why = "Inside a regular expression, which matches text rather than running it; " + strings.ToLower(fd.Why[:1]) + fd.Why[1:]
			}
			out = append(out, fd)
		}
		if isCode(f.Path) && !generated(f.Path) {
			if loc := longBase64.FindStringIndex(line); loc != nil {
				out = append(out, Finding{Rule: "safety.encoded-blob", Category: "obfuscation", Title: "Large encoded blob in source",
					Severity: Medium, Confidence: Possible, Path: f.Path, Line: i + 1, Evidence: excerpt(line[:min(len(line), loc[0]+60)]),
					Why: "Kilobytes of base64 in source code often hide a payload that is decoded at run time."})
			}
			if hexEscapes.MatchString(line) {
				out = append(out, Finding{Rule: "safety.hex-escapes", Category: "obfuscation", Title: "Long run of hex-escaped bytes",
					Severity: Medium, Confidence: Possible, Path: f.Path, Line: i + 1, Evidence: excerpt(line),
					Why: "Long hex-escaped strings are a common way to hide code from readers."})
			}
		}
	}
	return append(out, checkCoOccurrence(f, lines)...)
}

// generated reports files where long lines and blobs are expected.
func generated(p string) bool {
	low := strings.ToLower(p)
	for _, s := range []string{".min.js", ".map", ".lock", "-lock.json", ".svg", "/dist/", "/build/", "/vendor/", "/testdata/", "/fixtures/"} {
		if strings.Contains(low, s) || strings.HasPrefix(low, strings.TrimPrefix(s, "/")) {
			return true
		}
	}
	return false
}

var (
	collects  = regexp.MustCompile(`os\.hostname\(|os\.userInfo\(|os\.networkInterfaces\(|getpass\.getuser\(|socket\.gethostname\(|platform\.node\(|os\.getlogin\(|whoami`)
	sends     = regexp.MustCompile(`\b(fetch|axios\.(post|put)|requests\.(post|put)|http\.request|https\.request|urllib\.request\.urlopen|XMLHttpRequest)\b`)
	hardURL   = regexp.MustCompile(`https?://[a-zA-Z0-9.-]+\.[a-z]{2,}`)
	fetches   = regexp.MustCompile(`\b(fetch|requests\.get|urlopen|http\.get|https\.get|axios\.get)\s*\(`)
	evaluates = regexp.MustCompile(`\beval\s*\(|new\s+Function\s*\(|\bexec\s*\(`)
)

// checkCoOccurrence flags combinations that are only suspicious together: a
// file that collects machine identity and sends to a hard-coded address, or
// one that both fetches and evaluates code.
func checkCoOccurrence(f analysis.File, lines []string) []Finding {
	if !isCode(f.Path) || generated(f.Path) {
		return nil
	}
	text := string(f.Content)
	firstLine := func(re *regexp.Regexp) (int, string) {
		for i, l := range lines {
			if re.MatchString(l) {
				return i + 1, l
			}
		}
		return 0, ""
	}
	var out []Finding
	if collects.MatchString(text) && sends.MatchString(text) && hardURL.MatchString(text) {
		n, l := firstLine(sends)
		out = append(out, Finding{Rule: "safety.identity-beacon", Category: "data-collection", Title: "Collects machine details and sends them to a fixed address",
			Severity: Medium, Confidence: Possible, Path: f.Path, Line: n, Evidence: excerpt(l),
			Why: "Reading the hostname or username and posting to a hard-coded server is how undisclosed telemetry and stealers report back."})
	}
	if fetches.MatchString(text) && evaluates.MatchString(text) {
		n, l := firstLine(evaluates)
		out = append(out, Finding{Rule: "safety.fetch-and-eval", Category: "remote-code", Title: "Fetches from the network and evaluates code in the same file",
			Severity: Medium, Confidence: Weak, Path: f.Path, Line: n, Evidence: excerpt(l),
			Why: "Worth reading: evaluating text from a network response runs remote code."})
	}
	return out
}

// checkRepository applies rules that read manifests rather than lines.
func checkRepository(files []analysis.File) []Finding {
	var out []Finding
	for _, f := range files {
		switch path.Base(f.Path) {
		case "package.json":
			out = append(out, checkPackageJSON(f)...)
		case "setup.py":
			out = append(out, checkSetupPy(f)...)
		case "requirements.txt":
			out = append(out, checkRequirements(f)...)
		case "composer.json":
			out = append(out, checkComposerJSON(f)...)
		case "Gemfile":
			out = append(out, checkGemfile(f)...)
		}
		if strings.HasSuffix(f.Path, ".gemspec") {
			out = append(out, checkGemspec(f)...)
		}
	}
	return out
}

var risky = regexp.MustCompile(`(?i)\b(curl|wget|node\s+-e|python\s+-c|https?://|eval|base64)\b`)

func checkPackageJSON(f analysis.File) []Finding {
	var pkg struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(f.Content, &pkg) != nil {
		return nil
	}
	var out []Finding
	for _, hook := range []string{"preinstall", "install", "postinstall"} {
		script, ok := pkg.Scripts[hook]
		if !ok {
			continue
		}
		sev, conf := Medium, Possible
		if risky.MatchString(script) {
			sev, conf = High, Likely
		}
		out = append(out, Finding{Rule: "safety.npm-install-script", Category: "install", Title: "Runs a script when the package is installed",
			Severity: sev, Confidence: conf, Path: f.Path, Line: lineOf(f.Content, `"`+hook+`"`), Evidence: excerpt(hook + ": " + script),
			Why: "npm runs " + hook + " scripts automatically during install, before anyone has used the package."})
	}
	for name, spec := range merge(pkg.Dependencies, pkg.DevDependencies) {
		if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "git+") || strings.HasPrefix(spec, "git://") {
			out = append(out, Finding{Rule: "safety.url-dependency", Category: "supply-chain", Title: "Dependency installed from a URL",
				Severity: Low, Confidence: Possible, Path: f.Path, Line: lineOf(f.Content, `"`+name+`"`), Evidence: excerpt(name + ": " + spec),
				Why: "URL dependencies skip the registry's checks and can change without a version bump."})
		}
		if near := typosquat(name, popularNPM); near != "" {
			out = append(out, typosquatFinding(f, name, near))
		}
	}
	return out
}

// checkComposerJSON reports Composer scripts that run on install. Composer
// runs a project's own scripts on composer install or update; a repository
// someone clones and installs runs them on that machine.
func checkComposerJSON(f analysis.File) []Finding {
	var pkg struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if json.Unmarshal(f.Content, &pkg) != nil {
		return nil
	}
	var out []Finding
	// post-root-package-install and post-create-project-cmd run only for
	// composer create-project, not when a cloned repository is installed.
	for _, hook := range []string{"pre-install-cmd", "post-install-cmd", "pre-update-cmd", "post-update-cmd"} {
		raw, ok := pkg.Scripts[hook]
		if !ok {
			continue
		}
		// A script is a string or a list of strings.
		var cmds []string
		if json.Unmarshal(raw, &cmds) != nil {
			var one string
			if json.Unmarshal(raw, &one) != nil {
				continue
			}
			cmds = []string{one}
		}
		script := strings.Join(cmds, " && ")
		sev, conf := Medium, Possible
		switch {
		case risky.MatchString(script) || composerRisky.MatchString(script):
			sev, conf = High, Likely
		case composerOwn(cmds):
			// Only the project's own Composer scripts or framework commands,
			// such as Laravel's @php artisan: worth knowing, rarely a threat.
			sev, conf = Low, Weak
		}
		out = append(out, Finding{Rule: "safety.composer-install-script", Category: "install", Title: "Runs a script when the project is installed",
			Severity: sev, Confidence: conf, Path: f.Path, Line: lineOf(f.Content, `"`+hook+`"`), Evidence: excerpt(hook + ": " + script),
			Why: "Composer runs " + hook + " scripts automatically during composer install or update, before anyone has used the project."})
	}
	return out
}

// composerOwn reports whether every command only references another Composer
// script, Composer itself, or the project's own artisan or bin/console.
func composerOwn(cmds []string) bool {
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		if !(strings.HasPrefix(c, "@composer ") || strings.HasPrefix(c, "@php artisan ") || strings.HasPrefix(c, "@php bin/console ") ||
			(strings.HasPrefix(c, "@") && !strings.ContainsAny(c, " \t"))) {
			return false
		}
	}
	return len(cmds) > 0
}

// composerRisky are commands that run code inline from a Composer script.
var composerRisky = regexp.MustCompile(`(?i)\bphp\s+-r\b|\bbash\s+-c\b|\bsh\s+-c\b`)

var (
	gemSource     = regexp.MustCompile(`(?m)^\s*source\s+['"]http://`)
	gemFromGit    = regexp.MustCompile(`(?m)^\s*gem\s+['"]([^'"]+)['"][^\n]*\b(git|github):\s*['"]`)
	gemExtensions = regexp.MustCompile(`\.extensions\s*(=|<<)`)
)

// checkGemfile reports gem sources that skip the registry's integrity: plain
// HTTP sources and gems taken from a Git repository.
func checkGemfile(f analysis.File) []Finding {
	var out []Finding
	if loc := gemSource.FindIndex(f.Content); loc != nil {
		out = append(out, Finding{Rule: "safety.gem-insecure-source", Category: "supply-chain", Title: "Gems installed from a plain HTTP source",
			Severity: Medium, Confidence: Likely, Path: f.Path, Line: lineAt(f.Content, loc[0]), Evidence: excerpt(string(f.Content[loc[0]:loc[1]])),
			Why: "Over plain HTTP, anyone on the network path can swap the gems that get installed."})
	}
	for _, m := range gemFromGit.FindAllSubmatchIndex(f.Content, -1) {
		out = append(out, Finding{Rule: "safety.url-dependency", Category: "supply-chain", Title: "Dependency installed from a URL",
			Severity: Low, Confidence: Possible, Path: f.Path, Line: lineAt(f.Content, m[0]), Evidence: excerpt(string(f.Content[m[2]:m[3]])),
			Why: "Git dependencies skip the registry's checks and can change without a version bump."})
	}
	return out
}

// checkGemspec reports gems that build native extensions: installing the gem
// runs its extconf.rb on the installing machine.
func checkGemspec(f analysis.File) []Finding {
	loc := gemExtensions.FindIndex(f.Content)
	if loc == nil {
		return nil
	}
	return []Finding{{Rule: "safety.gem-native-extension", Category: "install", Title: "Runs a build script when the gem is installed",
		Severity: Low, Confidence: Possible, Path: f.Path, Line: lineAt(f.Content, loc[0]),
		Why: "A gem with native extensions runs its extconf.rb during gem install; read it before installing."}}
}

var (
	setupCmdclass = regexp.MustCompile(`cmdclass\s*=`)
	setupActions  = regexp.MustCompile(`\b(urllib|requests|subprocess|os\.system|socket|base64)\b`)
)

func checkSetupPy(f analysis.File) []Finding {
	text := string(f.Content)
	var out []Finding
	if setupCmdclass.MatchString(text) {
		sev, conf := Medium, Possible
		if setupActions.MatchString(text) {
			sev, conf = High, Possible
		}
		out = append(out, Finding{Rule: "safety.setup-py-hook", Category: "install", Title: "setup.py overrides install commands",
			Severity: sev, Confidence: conf, Path: f.Path, Line: lineOf(f.Content, "cmdclass"),
			Why: "Custom install commands in setup.py run arbitrary code when the package is installed."})
	} else if setupActions.MatchString(text) {
		n := lineOf(f.Content, setupActions.FindString(text))
		out = append(out, Finding{Rule: "safety.setup-py-actions", Category: "install", Title: "setup.py runs commands or network calls",
			Severity: Medium, Confidence: Possible, Path: f.Path, Line: n,
			Why: "setup.py runs during install; network calls or subprocesses there execute on every installing machine."})
	}
	return out
}

var reqName = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

func checkRequirements(f analysis.File) []Finding {
	var out []Finding
	for _, line := range strings.Split(string(f.Content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := reqName.FindStringSubmatch(line); m != nil {
			if near := typosquat(strings.ToLower(m[1]), popularPyPI); near != "" {
				out = append(out, typosquatFinding(f, m[1], near))
			}
		}
	}
	return out
}

func typosquatFinding(f analysis.File, name, near string) Finding {
	return Finding{Rule: "safety.typosquat", Category: "supply-chain", Title: "Dependency name is one letter from a popular package",
		Severity: Low, Confidence: Weak, Path: f.Path, Line: lineOf(f.Content, name), Evidence: name + " (did you mean " + near + "?)",
		Why: "Typosquatted packages copy a popular name with one letter changed; confirm this is the package you meant."}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func lineOf(content []byte, needle string) int {
	for i, l := range strings.Split(string(content), "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 0
}

// lineAt is the 1-based line of a byte offset in content.
func lineAt(content []byte, offset int) int {
	return bytes.Count(content[:offset], []byte("\n")) + 1
}

// popular packages often typosquatted; a dependency one edit away from one of
// these, and not itself on the list, is flagged.
var (
	popularNPM = []string{"react", "express", "lodash", "axios", "chalk", "commander", "request", "moment", "debug", "webpack", "typescript",
		"eslint", "dotenv", "colors", "electron", "jquery", "underscore", "mongoose", "socket.io", "nodemailer", "bcrypt", "crypto-js",
		"cross-env", "babel-cli", "node-fetch", "discord.js", "puppeteer", "prettier", "nodemon", "vue", "next"}
	popularPyPI = []string{"requests", "numpy", "pandas", "urllib3", "setuptools", "django", "flask", "boto3", "pyyaml", "cryptography",
		"beautifulsoup4", "selenium", "matplotlib", "colorama", "pillow", "scipy", "tensorflow", "pytorch", "openai", "httpx", "aiohttp", "discord.py"}
)

func typosquat(name string, popular []string) string {
	if len(name) < 5 {
		return ""
	}
	for _, p := range popular {
		if name == p {
			return ""
		}
	}
	for _, p := range popular {
		if len(p) >= 5 && editDistance(name, p) == 1 {
			return p
		}
	}
	return ""
}

// editDistance is the optimal string alignment distance: insertions,
// deletions, substitutions, and swaps of adjacent letters each cost one, since
// swapped letters ("lodahs") are a favourite of typosquats.
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}
