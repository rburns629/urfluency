package cli

import (
	"fmt"
	"io"
	"slices"
)

const usage = `urfluency rates the code you wrote and reports strengths and areas to improve.
Your code stays on your machine. Dependency versions are checked against osv.dev
for known vulnerabilities, unless you scan --offline.

Rate your code
  scan [flags] [path]       Scan the repository at path, or every project below it
                            (default ".")
  dismiss RULE FILE:LINE    Stop counting a finding that is not a real problem

Share your results (optional)
  login                     Link this CLI to your urfluency.com account
  upload [flags] [path]     Scan again, show what would be sent, and upload the
                            ratings after you confirm
  status                    Show the login, when it expires, and your plan
  logout                    Delete the upload token on this machine

Before running someone else's code
  check URL|PATH            Look for signs of malicious behavior; runs nothing

Repository badges
  attest                    In GitHub Actions: rate the repository for its badges

Setup and troubleshooting
  init [path]               Confirm and save which commit addresses are yours
  vulns update|status|remove
                            Download vulnerability databases for --offline scans
  doctor                    Check Git and optional features; installs nothing
  version                   Print the version

Run "urfluency help COMMAND" or "urfluency COMMAND --help" for its flags.
`

// scanFlagsHelp describes the flags scan and upload share.
const scanFlagsHelp = `  --since WHEN        Only count lines written since WHEN: a duration like 90d,
                      a date like 2026-01-31, or all (default 365d)
  --exclude GLOB      Skip folders whose name or relative path matches GLOB,
                      such as 'Archive*'; repeat for several
  --no-git            Ignore Git and read files as they are now, counting every
                      line as yours, including code others committed. For
                      projects you don't track in Git or keep private. Results
                      are marked declared, not verified; history measures are
                      left out, and --since and --author cannot be used with it
  --author EMAIL      Also count commits from this address as yours; repeat for
                      several. Your Git name, email, and account name are found
                      automatically, so this is for an address they miss
  --include-ext EXT   Also read files with these extensions and check them for
                      secrets, such as .tf,.yml; repeat or separate with commas.
                      Without it, only files in supported languages are read
                      (Go, Python, JavaScript, TypeScript, Rust, C, C++, C#,
                      Java, Kotlin, PHP, Ruby, Swift, Dart, Shell, PowerShell)
  --sessions          Also grade your prompts from local Claude Code, Codex, and
                      GitHub Copilot Chat sessions for these projects, and report
                      how much code agents wrote. Prompts are read in memory only
  --offline           Check dependencies against the downloaded databases and
                      send nothing (see urfluency vulns update)
  --online            Check dependencies live, overriding a saved offline default
`

var commandHelp = map[string]string{
	"scan": `Usage: urfluency scan [flags] [path]

Scans the repository at path, or every project below it (default "."), and
prints a report. Repositories are rated on the lines Git says you wrote; project
folders without Git are rated on every line and reported separately. Your code
is never sent. Locked dependency versions (package name, version, and ecosystem
only) are checked against osv.dev for known vulnerabilities, which are shown to
you and never uploaded; --offline checks the downloaded databases instead.
Nothing is saved: to share the results, run upload with the same path and flags.

Flags:
` + scanFlagsHelp + `  --locations         Show file:line for each finding, as dismiss takes them
                      (never uploaded)
  --json              Print the report as JSON

Examples:
  urfluency scan ~/Projects
  urfluency scan --since all --exclude 'Archive*' ~/Projects
  urfluency scan --no-git ~/Private
`,
	"upload": `Usage: urfluency upload [flags] [path]

Scans path again with the same flags as scan, shows exactly what would be sent,
and uploads the ratings after you confirm. It does not reuse an earlier scan.
Run login first.

Uploads contain ratings, counts, and the summary only: never code, prompt text,
repository names, paths, commit hashes, or email addresses.

Flags:
` + scanFlagsHelp + `  --dry-run           Print the exact payload and send nothing
  --yes               Send without asking (required when not in a terminal)
`,
	"dismiss": `Usage:
  urfluency dismiss RULE FILE:LINE [--reason TEXT]   Stop counting a finding
  urfluency dismiss --undo RULE FILE:LINE            Count it again
  urfluency dismiss --list                           List dismissed findings

For a finding that is not a real problem, such as a placeholder flagged as a
secret. RULE is the finding's id and FILE:LINE one of its locations, both
shown by scan --json --locations. Later scans and
uploads leave it out while that line stays the same; reports say how many
findings you dismissed, never which. Only the rule and a hash of the line are
saved, on this machine.
`,
	"check": `Usage: urfluency check [flags] URL|PATH

Looks for signs of malicious behavior in a repository before you run it, such as
install scripts that download and execute code, and for known vulnerabilities in
its locked dependencies and pinned runtimes. A URL is cloned into a temporary
folder with hooks, submodules, and LFS off, then removed. Nothing is run. Only
dependency names and versions are sent, to osv.dev, unless --offline.

Flags:
  --json              Print the result as JSON
  --offline           Check dependencies against the downloaded databases and
                      send nothing
  --online            Check dependencies live, overriding a saved offline default
`,
	"vulns": `Usage:
  urfluency vulns update [PATH...]        Download databases for the ecosystems
                                          the projects at PATH use
  urfluency vulns update --all            Every ecosystem (about 285 MB)
  urfluency vulns update --ecosystem npm,Packagist
  urfluency vulns status                  What is downloaded, and how old
  urfluency vulns remove                  Delete the downloaded databases

Scans check dependencies live against osv.dev by default. These databases let
scan --offline and check --offline do it with nothing sent. update shows each
download's size and asks first (--yes skips the question), and fetches only
databases with newer advisories. They are kept in your user cache folder.
Run update now and then: reports say when databases are over a week old.
`,
	"attest": `Usage: urfluency attest [--since WHEN] [--dry-run]

In GitHub Actions, rates every author's code in the checked-out repository and
sends the ratings for its badges with the job's OIDC token, marked CI-attested.
The code stays on the runner. The job needs "permissions: id-token: write".

Set URFLUENCY_LINK_CODE to a link code from urfluency.com to connect the
repository on its first run without installing the GitHub App.

Flags:
  --since WHEN        Only count lines written since WHEN (default 365d)
  --dry-run           Print what would be sent and send nothing
`,
	"init": `Usage: urfluency init [flags] [path]

Optional. Finds the addresses you commit with in the repositories below path,
asks about ones that only look like yours, and saves your answers so later
scans start from them. Scans find your addresses without it.

Flags:
  --author EMAIL      Also save this address as yours; repeat for several
  --yes               Don't ask; save only the addresses found for certain
  --include-ext EXT   Save extra file extensions for every scan to read, such
                      as .tf,.yml
  --offline           Make offline the default: dependency checks use the
                      downloaded databases and nothing is sent; --online on a
                      scan overrides it
  --online            Go back to checking dependencies live by default
`,
	"login": `Usage: urfluency login

Links this CLI to your urfluency.com account in your browser, so you can upload.
The upload token is saved on this machine; logout deletes it.
`,
	"status": `Usage: urfluency status

Shows whether this CLI is logged in, when the login expires, and your account's
plan. Every plan gets the same ratings, findings, and steps from the CLI; Pro
adds features on the site.
`,
	"logout": `Usage: urfluency logout

Deletes the upload token on this machine. Your account and uploads are kept.
`,
	"doctor": `Usage: urfluency doctor

Checks that Git works and which optional features, such as --sessions, have
data to read. Nothing is installed or changed.
`,
	"version": `Usage: urfluency version
`,
	// validate is for maintainers and left out of the command list.
	"validate": `Usage: urfluency validate [path]

For maintainers: shows whether prompt signals predict what happened next in
local agent sessions, the evidence a prompting rule needs before it is graded.
`,
}

// wantsHelp reports whether args ask for help before any "--".
func wantsHelp(args []string) bool {
	end := slices.Index(args, "--")
	if end < 0 {
		end = len(args)
	}
	return slices.ContainsFunc(args[:end], func(a string) bool { return a == "-h" || a == "-help" || a == "--help" })
}

// help prints the help for a command, or the command list.
func help(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprint(stdout, usage)
		return err
	}
	text, ok := commandHelp[args[0]]
	if !ok {
		return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
	}
	_, err := fmt.Fprint(stdout, text)
	return err
}
