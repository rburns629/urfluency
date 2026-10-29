# Project context: urFluency

Completed from [templates/PROJECT_CONTEXT.md](templates/PROJECT_CONTEXT.md).
Agent instructions adopted from clean-agent-code at commit `13aedcb`.

| Area | Project facts, safe references, owners, or commands |
| --- | --- |
| Project purpose | Free tooling for engineers: a local CLI that analyzes the user's own commits and agent sessions and uploads aggregate metrics; a web app and API (later) for profiles and badges; a safety scanner for public Git URLs (later). See [DESIGN.md](DESIGN.md) |
| Languages | Go, minimum 1.26.10 (module directive `go 1.26.10`). Keep the minimum on a supported release (the two newest Go minor versions) at its latest patch: Go 1.24.6 reached GO-2026-4602 through `filepath.WalkDir`; developed with Go 1.27. Analyzed languages: Go, Python, JavaScript, TypeScript, Rust, C, C++, C#, Java, Kotlin, Swift, Dart |
| Frameworks | Standard library plus tree-sitter: the official Go bindings (`github.com/tree-sitter/go-tree-sitter`) and grammars (`github.com/tree-sitter/tree-sitter-{python,javascript,typescript,rust,c,cpp,c-sharp,java}`), Kotlin from `github.com/tree-sitter-grammars/tree-sitter-kotlin`, Dart from `github.com/UserNobody14/tree-sitter-dart` pinned to a reviewed commit (no tagged releases), Bash from `github.com/tree-sitter/tree-sitter-bash` and PowerShell from `github.com/airbus-cert/tree-sitter-powershell` (tagged releases with Go bindings), and Swift vendored under `third_party/tree-sitter-swift` (its Go module lacks the generated parser; provenance and update steps in its README). All MIT licensed. Add dependencies only with a stated need |
| Runtime environment | CLI on macOS, Linux, and Windows; one binary per platform built with cgo (`CGO_ENABLED=1`) because tree-sitter is C. Release builds run per platform in CI |
| Deployment targets | None yet. Service hosting is an open decision in DESIGN.md |
| Architecture | `cmd/urfluency` → `internal/cli` → `analysis` (engine, registry, interfaces) → `analyzers/<language>`. Analyzers depend only on `analysis`, `rules`, and `parsing` (the shared tree-sitter layer), never on the CLI or each other. `gitdata` owns all Git access. Top-level packages (`rules`, `analysis`, `analyzers/*`, `parsing`, `gitdata`, `gates`, `upload`) are public because `urfluency-app` imports them; keep their exported API small and stable. Sessions, reports, and the CLI stay in `internal/` |
| Module ownership | Robert Burns owns all modules |
| Domain terminology | **Rule**: a versioned catalog entry (`go.decode-target-reuse`). **Finding**: one occurrence of a rule. **Owned line**: a line attributed to the user by `git blame` at the analyzed revision. **Category**: clean-code, architecture, type-safety, language-proficiency, prompting |
| Existing coding conventions | Errors wrapped with `%w` and returned, never discarded; no `panic` outside `main` initialization; typed structs over `map[string]any`; table-driven tests |
| Reference implementations | `analyzers/golang` for go/ast analyzers; `analyzers/python` for tree-sitter analyzers; `gitdata` for subprocess handling |
| Error handling | Library packages return wrapped errors; the CLI prints one user-facing message and exits non-zero |
| Authentication | Not yet implemented. Planned: GitHub device flow, tokens in the OS keychain |
| Authorization | Not applicable to the local CLI yet |
| Data sensitivity | Only scores leave the machine: uploads contain rule counts and volumes aggregated across repositories, with no repository names, URLs, paths, commit hashes, code, or prompt text. Secret findings show type and location locally and never their value, in any output. The engine never reads protected file types (`analysis/protected.go`); keep that list aligned with AGENTS.md |
| Protected paths and value files | `.env`, `.env.*`, `*.env` (for example `api.env`), keychain entries, and real agent transcripts under `~/.claude/projects/` and `~/.codex/sessions/`. Tests use synthetic fixtures under `testdata/` only. Fixtures containing fake secrets are marked with the string `URFLUENCY-CANARY` and are non-sensitive |
| Trust boundaries | Repositories being analyzed are untrusted input: parse them, never execute their code, hooks, or build scripts. Git runs with hooks disabled and without external diff or text conversion |
| Type checking | `go vet ./...` |
| Formatting | `gofmt -l .` must print nothing |
| Linting | `go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...` |
| Testing | `gofmt -l .` and `go test -race ./...` with host Go (1.27.3); requires a C compiler (Xcode Command Line Tools on macOS) |
| Security tooling | `GOTOOLCHAIN=go$(go mod edit -json | jq -r .Go) go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`, which checks the minimum Go version as CI does; with a newer host Go it would miss vulnerabilities in the minimum version. Secret scanning to be added |
| Releases | Push a `vMAJOR.MINOR.PATCH` tag. `.github/workflows/release.yml` builds natively on `macos-15`, `macos-15-intel`, `ubuntu-24.04`, and `ubuntu-24.04-arm` with `scripts/build-release.sh` (Linux builds are static, in a digest-pinned Go Alpine image), publishes archives and `checksums.txt`, and, when the `HOMEBREW_TAP_TOKEN` secret exists, renders `packaging/homebrew/urfluency.rb.tmpl` into `rburns629/homebrew-tap`. Tags, releases, and tap pushes need approval |
| Continuous integration | `.github/workflows/ci.yml` runs format, vet, race tests, staticcheck, and govulncheck on every push and pull request with the Go version from `go.mod`. Actions are pinned to commit SHAs |
| CLI operations | GitHub repository `rburns629/urfluency` (private), `gh` account `rburns629`. Pushes, releases, and visibility changes need approval. No cloud resources yet |
| Auto-mode filesystem recovery | Clean tracked files use their commit as the recovery copy. Other backups go to `~/.urfluency-backups/<timestamp>/`, outside the repository. Robert Burns approves cleanup; nothing is deleted automatically |
| Performance requirements | Not yet defined |
| Operational requirements | Not yet defined |
| Compatibility expectations | The local report and upload formats carry a version field; scoring methods are versioned |
| Known limitations | Go, Python, JavaScript, TypeScript, Rust, C, C++, C#, Java, Kotlin, Swift, Dart, Shell, PowerShell only (shell scripts by `.sh` or `.bash` extension; scripts without one are not read) (`.h` headers count as C, and C++-only headers are skipped); blame-based ownership counts lines the user last touched, not lines they originally wrote; AI-assisted code counts as the committer's |
| Approved architectural exceptions | cgo for tree-sitter parsing, approved by Robert Burns on 2026-10-26 in place of the original `CGO_ENABLED=0` single static binary. Review if a maintained pure-Go tree-sitter runtime appears. Vendoring the generated Swift parser (`third_party/tree-sitter-swift`) and depending on the Dart grammar by commit, approved by Robert Burns on 2026-10-27 |
