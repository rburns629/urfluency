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

## Proficiency

Each language gets a score from 0 to 100 and a level (Beginner, Developing,
Proficient, Advanced, Expert). Both come from your own lines: the share of the
language's idioms you use well, such as error wrapping in Go, dataclasses and
generators in Python, or type guards in TypeScript, and how cleanly you write it
in each category (security, type safety, clean code, language proficiency). A weak
category caps the score, so strengths cannot hide it. The report lists the idioms
you demonstrate under **Shows**. The model (version `0.1-uncalibrated`) is
documented in DESIGN.md and will be calibrated against outcomes before scores are
published anywhere.

## What it checks

| Area | Coverage |
| --- | --- |
| Languages | Go, Python, JavaScript, TypeScript |
| Frameworks | React, Angular, Express, Fastify, FastAPI, Django, Flask (detected from each file's imports) |
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

## License

MIT. See [LICENSE](LICENSE).
