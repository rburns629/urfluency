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

Requires Go 1.24.6 or later, a C compiler (for tree-sitter), and Git.

```sh
go build -o urfluency ./cmd/urfluency
./urfluency scan /path/to/your/repo              # uses git user.email
./urfluency scan --author you@example.com --since 90d --locations .
./urfluency scan --json .
```

The scan analyzes only lines that `git blame` attributes to your emails at the
latest commit, never runs the repository's code, and writes nothing to disk.
`--locations` adds `file:line` for each finding; locations are never uploaded.

## What it checks

| Area | Coverage |
| --- | --- |
| Languages | Go, Python, JavaScript, TypeScript |
| Frameworks | React, Angular, Express, Fastify, FastAPI, Django, Flask (detected from each file's imports) |
| Security | Credentials committed in source and config files (private keys, cloud keys, API tokens, passwords in URLs or code), SQL and shell injection, `eval`, unsafe deserialization, disabled TLS verification, and framework misconfiguration |
| Categories | Clean code, architecture, type safety, language proficiency, security, prompting |

Files that hold secrets by design are never read: `.env` files, key and certificate
files, credential configs such as `.npmrc` or service-account JSON, Terraform state
and variables, and directories such as `secrets/` or `.ssh/`. Secret findings show
the rule and line only. Credential values are never printed, stored, or uploaded,
and nothing about a scanned repository is kept: only scores.

