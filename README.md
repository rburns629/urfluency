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

## Try the CLI

Requires Go 1.26.10 or later, a C compiler (for tree-sitter), and Git. Run
`urfluency doctor` to check dependencies and optional features; it never installs
anything and prints the install command for whatever is missing.

```sh
go build -o urfluency ./cmd/urfluency
./urfluency scan /path/to/your/repo              # uses git user.email
./urfluency scan --author you@example.com --since 90d --locations .
./urfluency scan --json .
./urfluency scan --sessions ~/Projects           # every repo below, plus your agent prompts
```

The scan analyzes only lines that `git blame` attributes to your emails at the
latest commit, never runs the repository's code, and writes nothing to disk.
`--locations` adds `file:line` for each finding; locations are never uploaded.

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
