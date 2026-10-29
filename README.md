# urFluency

Free tooling that shows engineers how their code and agent prompting are developing,
and checks whether public repositories are safe to run.

- **Personal metrics:** a local CLI analyzes your own commits and agent sessions and
  uploads aggregate metrics, never code. The web app at urfluency.com shows
  strengths, weaknesses, and what to practice across clean code, architecture, type
  safety, language proficiency, and prompting.
- **Repository safety scans:** submit a public Git URL to get a report of malicious
  or privacy-invasive indicators.
- **Badges and API:** add badges to repositories and embed your published metrics in
  a portfolio site.

Status: phase 1, a local CLI with analyzers for Go, Python, JavaScript, and
TypeScript. See [DESIGN.md](DESIGN.md).

The CLI command and Go module use lowercase: `urfluency`,
`github.com/rburns629/urfluency`.

## Install

Once releases are published:

```sh
brew install rburns629/tap/urfluency    # macOS and Linux; installs Git if missing
urfluency doctor                        # checks dependencies; installs nothing
```

Release archives for macOS (Apple Silicon and Intel, macOS 13+) and Linux (x86-64
and ARM, static binaries) are also attached to each GitHub Release with a
`checksums.txt`. To build from source you need Go 1.26.10 or later, a C compiler
(for tree-sitter), and Git: `go build -o urfluency ./cmd/urfluency`.

`urfluency doctor` checks Git by capability, lists the agent sessions `--sessions`
would read, and looks for a local model runtime. It never installs anything; it
prints the official install command for whatever is missing.

## Try the CLI

```sh
./urfluency scan /path/to/your/repo              # finds your commits automatically
./urfluency scan --since 90d --locations .
./urfluency scan --json .
./urfluency scan --sessions ~/Projects           # every repo below, plus your agent prompts
./urfluency scan --exclude 'Archive*' ~/Projects # skip folders by name or relative path
```

The scan analyzes only lines that `git blame` attributes to your emails at the
latest commit, never runs the repository's code, and writes nothing to disk.
Progress (the repository and stage it is on) goes to stderr, so `--json` output stays
clean. Files over 2 MB are skipped before anything reads them, since they are
vendored or generated, and a file that takes more than 60 seconds to attribute is
skipped and counted in the report. Third-party code is left out even when it was
committed: files `.gitattributes` marks `linguist-vendored` or `linguist-generated`,
and JavaScript or TypeScript libraries recognized by a `/*!` or `@license` banner,
a `.bundle.` or `.min.` name, or a `plugins`, `lib`, or `vendor` folder under
`public`, `static`, or `assets`.
`--locations` adds `file:line` for each finding; locations are never uploaded.

You don't list your email addresses. urFluency starts from your Git name and
email and your account's full name, then follows history: an address you
committed under with one of your names is yours, and so is a name you used with
one of your addresses, so old work, school, and machine-default addresses are
found. Bots and GitHub's shared web address never link. Addresses that only look
like yours are listed as possible and not counted; add any that are yours with
`--author`. Addresses are shown in your terminal and never written to the report.

To settle this once, run `urfluency init ~/Projects`. It asks about each possible
address and saves your answers to `settings.json` in your user config folder
(`~/Library/Application Support/urfluency` on macOS, `~/.config/urfluency` on
Linux), readable only by you and never uploaded. Later scans start from the saved
addresses, still find new ones, and stop suggesting the ones you declined.

Projects that lost their Git history (or never had commits) are scanned too. A
folder counts as a project when it has a manifest such as `go.mod` or
`package.json`, or source files and no projects inside it. With no history to
attribute lines, every line counts as yours, so these results appear in their own
**Projects without Git** section marked as declared authorship, never mixed with
attributed results, and without the measures that need history (AI share,
durability, tests with changes).

To scan everything that way, pass `--no-git`: repositories are read as plain
folders, as their files are now, and every line counts as yours, including code
someone else committed. It suits projects you don't track in Git or keep private.
The results, and an upload made with it, are marked declared rather than verified;
`--since` and `--author` do not apply.

Every scan includes an **AI engineering** section: does your agent-assisted work
hold up as well as your own? Using agents is never counted against you, and the
share of AI-assisted code is shown but never scored. The section compares findings
on AI-assisted lines (from commit trailers such as `Co-Authored-By: Claude`, and
from sessions) with your own hand-written baseline per language, names the rules
where agent code falls short, and shows how often lines needed a fix within 90 days
of being written.

`--sessions` adds what happened in your local Claude Code, Codex, and GitHub
Copilot Chat (VS Code) sessions for the scanned repositories: whether agent changes
were tested or checked, whether tasks needed corrections, and how often you
rejected or changed agent edits. Prompt text is reduced to signals as it is read
and is never stored, printed, or uploaded.

Prompting rules ship already validated: a rule is graded only if it reflects what
actually happened in sessions. Maintainers check candidate rules with
`urfluency validate`, which compares outcomes for tasks with and without each prompt
signal and prints counts only (see DESIGN.md).

## Check a repository before you run it

```
urfluency check https://github.com/owner/repo
urfluency check ./some-folder
```

`check` looks for signs of malicious or privacy-invasive behavior: install-time
scripts, downloaded code that is run, data sent to fixed addresses, reads of SSH
keys, cloud credentials, or browser profiles, obfuscated payloads, persistence
such as cron jobs or reverse shells, risky dependencies, and committed secrets.
It reads files only and runs nothing. A URL is cloned into a temporary folder
with Git hooks, submodules, symlinks, and LFS off and without your Git
configuration or credentials, then deleted. The report gives a verdict
(No indicators found, Review recommended, or High-risk indicators) and a score
out of 100; it can find indicators but never proves a repository is safe.
Findings in tests, CI, and container builds count less, since they do not run on
install. `--json` prints the report as data.

## Sharing your results (optional)

Scans never send anything. To publish a profile and badges on urfluency.com:

```
urfluency login                      # approve the code at urfluency.com/connect
urfluency upload --since all ~/Projects
```

`upload` runs its own scan; it does not reuse an earlier `urfluency scan`, so
pass it the same path and flags. It says what it will send and asks first;
`--dry-run` prints the exact payload instead. It sends the report's summary and
ratings only: never code, prompt text, repository names, paths, commit hashes,
or email addresses, and it refuses a payload that looks like it contains them.
Uploads start private. `urfluency logout` deletes the token on this machine.

## What is read

Only files in the supported languages (Go, Python, JavaScript, TypeScript, Rust, C, C++, C#, Java, Kotlin, Swift, Dart, Shell, PowerShell) are
read and checked, plus the CI, build, and agent instruction files that the
practices measures need. Other extensions are ignored unless you include them,
for example `--include-ext .tf,.yml` to also check Terraform and YAML for secrets;
`urfluency init --include-ext ...` saves them for every scan. Secrets in files you
do not include are not caught.

## Tools

The report lists the tools you work with, judged from the paths of files you
committed to, never their contents: infrastructure (Terraform, Docker, Helm,
Kustomize, Serverless), CI (GitHub Actions, GitLab CI, Jenkins, Cloud Build, Azure
Pipelines), build (Make, Gradle, Maven), packages (npm, Go modules, pip), and
quality tooling (pre-commit, ESLint, Prettier, golangci-lint), each with commits,
files, repositories, and when you last used it. Git itself is measured from your
commit messages: subject length, descriptive subjects, bodies that explain the
change, and Conventional Commits. Messages are measured, never stored.

## Summary

Every report ends with a summary that answers one question: is this person
proficient? It rates all your work at once, with and without Git (`combined` in
JSON), while the sections before it keep Git repositories and folders without
Git apart so you can trace what was verified and what was declared. It leads with strengths, then shows how you design systems, how you
brief agents, what holds the ratings down, and how far to trust the result.

Every area gets a rating from 0 to 100 and a level: **Beginner** (below 40),
**Intermediate** (40+), **Advanced** (70+), or **Expert** (85+).

| Area | Rated on |
| --- | --- |
| Languages | How well you write it (see Proficiency); one rating per language, combining work with and without Git by lines |
| Frameworks | Experience: repositories and files of yours that use it |
| Design | Experience in each kind of system you build, from the libraries your files import: cloud services, integrations with other services, web APIs, messaging, databases, analytics, auth, observability, infrastructure as code, AI applications, machine learning, front ends, mobile, and command-line tools |
| Tools | Experience: repositories and files of yours for each tool |
| Ways of working | Commit messages, changes committed with tests, and the quality gates your CI runs |
| Prompting | With `--sessions`: how often agent tasks were verified and right the first time |

Design areas that appear together name the kind of work you do, such as
"cloud integration" (cloud services with calls to other services) or "platform and
infrastructure". With `--sessions`, the summary also describes how you brief
agents: how often you explain the goal, set limits, state expected behavior, point
at specific code, or ask for checks. Only counts are kept; prompt text is never
stored. These traits are descriptive and are not scored.

Experience ratings measure how much you used something, not how well; only
language ratings judge quality.

## Proficiency

Each language gets a score from 0 to 100 and a level. Both come from your own
lines: the share of the language's idioms you use well, such as error wrapping in
Go, dataclasses and generators in Python, or type guards in TypeScript, and how
cleanly you write it in each category (security, type safety, clean code, language
proficiency). A weak category caps the score, so strengths cannot hide it. The
report lists the idioms you demonstrate under **Shows**. The model (version
`0.2-uncalibrated`) is documented in DESIGN.md and will be calibrated against
outcomes before scores are published anywhere.

## What it checks

| Area | Coverage |
| --- | --- |
| Languages | Go, Python, JavaScript, TypeScript, Rust, C, C++, C#, Java, Kotlin, Swift, Dart, Shell (`.sh`, `.bash`), PowerShell (`.ps1`, `.psm1`) |
| Frameworks | Detected from each file's imports: React, React Native (and Expo), Angular, Express, Fastify, FastAPI, Django, Flask; Go web frameworks (Fiber, Gin, Echo, Chi, Gorilla, gRPC, fasthttp); and the main frameworks of each newer language, such as .NET (ASP.NET Core, Entity Framework, Blazor, MAUI), Spring, Android and Jetpack Compose, Ktor, SwiftUI, Vapor, Flutter, Tokio, and Qt |
| Security | Credentials committed in source and config files (private keys, cloud keys, API tokens, passwords in URLs or code), SQL and shell injection, `eval`, unsafe deserialization, disabled TLS verification, and framework misconfiguration |
| AI engineering | Quality of AI-assisted code against your own baseline per language, durability, and with `--sessions` verification, corrections, and review activity |
| Practices | Test lines per code line, code changes committed with tests (AI-assisted and other), and what the CI you wrote runs; shown, not yet scored |
| Complexity | Functions above cognitive complexity 15 (SonarSource's measure), in every language |
| Reliability | Go: fresh contexts where a request context exists, `defer` in loops, HTTP clients without timeouts, a goroutine per channel message without a bound; React effects without dependencies; Django queries inside loops |
| Categories | Clean code, architecture, type safety, language proficiency, security, prompting |

Files that hold secrets by design are never read: `.env` files, key and certificate
files, credential configs such as `.npmrc` or service-account JSON, Terraform state
and variables, and directories such as `secrets/` or `.ssh/`. Secret findings show
the rule and line only. Credential values are never printed, stored, or uploaded,
and nothing about a scanned repository is kept: only scores.

A finding that is not a real problem, such as a placeholder flagged as a secret,
can be left out of later scans and uploads:

```
urfluency scan --locations ~/Projects            # shows each finding's rule and file:line
urfluency dismiss sec.hardcoded-password src/model.ts:336 --reason "SDK placeholder"
urfluency dismiss --list                         # or --undo RULE FILE:LINE
```

A secret removed from your files no longer counts, even if the change is not
committed; it can still be in the repository's history, so rotate any that
were live. Only the rule and a hash of the line are saved, on this machine. A dismissal
follows the line if the file moves and lapses if the line changes. Reports and
uploads say how many findings you dismissed, never which.

## License

MIT. See [LICENSE](LICENSE).
