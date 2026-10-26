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

Status: phase 1, a local CLI with a Go analyzer. See [DESIGN.md](DESIGN.md).

The CLI command and Go module use lowercase: `urfluency`,
`github.com/rburns629/urfluency`.

## Try the CLI

Requires Go 1.24 or later and Git.

```sh
go build -o urfluency ./cmd/urfluency
./urfluency scan /path/to/your/repo              # uses git user.email
./urfluency scan --author you@example.com --since 90d --locations .
./urfluency scan --json .
```

The scan analyzes only lines that `git blame` attributes to your emails, never runs
the repository's code, and writes nothing to disk. `--locations` adds `file:line`
for each finding; locations are never uploaded.

