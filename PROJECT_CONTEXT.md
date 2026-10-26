# Project context: urFluency

Completed from [templates/PROJECT_CONTEXT.md](templates/PROJECT_CONTEXT.md).
Agent instructions adopted from clean-agent-code at commit `13aedcb`.

| Area | Project facts, safe references, owners, or commands |
| --- | --- |
| Project purpose | Free tooling for engineers: a local CLI that analyzes the user's own commits and agent sessions and uploads aggregate metrics; a web app and API (later) for profiles and badges; a safety scanner for public Git URLs (later). See [DESIGN.md](DESIGN.md) |
| Languages | Go, minimum 1.24.6 (module directive `go 1.24.6`, the first release with fixes for GO-2025-3956 and GO-2025-3750); developed with Go 1.27. Analyzed languages: Go, Python, JavaScript, TypeScript |
| Frameworks | Standard library plus the official tree-sitter Go bindings and grammars (`github.com/tree-sitter/go-tree-sitter` and `tree-sitter-{python,javascript,typescript}`), MIT licensed. Add dependencies only with a stated need |
| Runtime environment | CLI on macOS, Linux, and Windows; one binary per platform built with cgo (`CGO_ENABLED=1`) because tree-sitter is C. Release builds run per platform in CI |
| Deployment targets | None yet. Service hosting is an open decision in DESIGN.md |
| Architecture | `cmd/urfluency` → `internal/cli` → `internal/analysis` (engine, registry, interfaces) → `internal/analyzers/<language>`. Analyzers depend only on `internal/analysis`, `internal/rules`, and `internal/parsing` (the shared tree-sitter layer), never on the CLI or each other. `internal/gitdata` owns all Git access |
| Module ownership | Robert Burns owns all modules |
| Domain terminology | **Rule**: a versioned catalog entry (`go.decode-target-reuse`). **Finding**: one occurrence of a rule. **Owned line**: a line attributed to the user by `git blame` at the analyzed revision. **Category**: clean-code, architecture, type-safety, language-proficiency, prompting |
| Existing coding conventions | Errors wrapped with `%w` and returned, never discarded; no `panic` outside `main` initialization; typed structs over `map[string]any`; table-driven tests |
| Reference implementations | `internal/analyzers/golang` for go/ast analyzers; `internal/analyzers/python` for tree-sitter analyzers; `internal/gitdata` for subprocess handling |
| Error handling | Library packages return wrapped errors; the CLI prints one user-facing message and exits non-zero |
| Authentication | Not yet implemented. Planned: GitHub device flow, tokens in the OS keychain |
| Authorization | Not applicable to the local CLI yet |
| Data sensitivity | Analyzed source code never leaves the machine. Reports contain rule IDs, counts, and volumes only; no code, file paths of private repositories, or prompt text |
| Protected paths and value files | `.env`, `.env.*`, `*.env` (for example `api.env`), keychain entries, and real agent transcripts under `~/.claude/projects/` and `~/.codex/sessions/`. Tests use synthetic fixtures under `testdata/` only. Fixtures containing fake secrets are marked with the string `URFLUENCY-CANARY` and are non-sensitive |
| Trust boundaries | Repositories being analyzed are untrusted input: parse them, never execute their code, hooks, or build scripts. Git runs with hooks disabled and without external diff or text conversion |
| Type checking | `go vet ./...` |
| Formatting | `gofmt -l .` must print nothing |
| Linting | `staticcheck` 2025.1.1 does not support Go 1.27, so run it in the container: `docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.24.6-bookworm sh -c 'go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...'` |
| Testing | `gofmt -l .` and `go test -race ./...` with host Go (1.27.3); requires a C compiler (Xcode Command Line Tools on macOS) |
| Security tooling | `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` with host Go; secret scanning to be added |
| CLI operations | GitHub repository `rburns629/urfluency` (private), `gh` account `rburns629`. Pushes, releases, and visibility changes need approval. No cloud resources yet |
| Auto-mode filesystem recovery | Clean tracked files use their commit as the recovery copy. Other backups go to `~/.urfluency-backups/<timestamp>/`, outside the repository. Robert Burns approves cleanup; nothing is deleted automatically |
| Performance requirements | Not yet defined |
| Operational requirements | Not yet defined |
| Compatibility expectations | The local report and upload formats carry a version field; scoring methods are versioned |
| Known limitations | Go, Python, JavaScript, and TypeScript only; blame-based ownership counts lines the user last touched, not lines they originally wrote; AI-assisted code counts as the committer's |
| Approved architectural exceptions | cgo for tree-sitter parsing, approved by Robert Burns on 2026-10-26 in place of the original `CGO_ENABLED=0` single static binary. Review if a maintained pure-Go tree-sitter runtime appears |
