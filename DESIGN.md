# urFluency design

Status: draft, 2026-10-26. Product name **urFluency**; lowercase `urfluency` for the
CLI, Go module (`github.com/rburns629/urfluency`), and URLs. Domain: urfluency.com.

## Purpose

urFluency is a free tool that helps engineers see how their code and prompting
practices are developing, and helps anyone check whether a public repository is
safe to run.

1. **Personal metrics.** A local CLI analyzes the user's own commits and agent
   sessions and uploads aggregate metrics. The web app shows strengths and
   weaknesses with explanations and specific points of focus.
2. **Repository badges.** Repository owners connect a repo to get badges for its
   README.
3. **Repository safety scans.** Anyone submits a public Git URL and receives a
   report of malicious or privacy-invasive indicators, aimed at the growing number of
   AI-generated ("vibecoded") projects being shared.
4. **Public API.** Engineers embed their metrics in portfolio sites; repositories
   embed badges.

## Principles

- **Only scores are stored.** Nothing about an analyzed repository is kept: no
  source code, repository names, URLs, file paths, commit hashes, findings with
  locations, or secrets. The CLI uploads aggregate scores and rule counts combined
  across all scanned repositories. Ad-hoc safety scans keep nothing after the report
  is delivered. The one exception is a repository its owner connects for badges,
  which stores `owner/repo` and its scores, because a badge must be looked up by
  name.
- **Trends, not rankings.** Metrics describe one person's trajectory. There are no
  leaderboards and no cross-person comparisons.
- **Honest provenance.** Every number shows its trust tier, period, sample size, and
  method version.
- **Explain, don't grade.** Each weakness links to what was detected, why it
  matters, and what to practice. Each strength says what evidence supports it.
- **Opt-in visibility.** Everything is private by default; users publish per metric
  and per language.

## Architecture

```
┌────────── user machine ──────────┐        ┌──────────── urfluency.com ────────────┐
│ urfluency CLI                    │        │ API (Go)                              │
│  ├ login (GitHub device flow)    │ HTTPS  │  ├ auth: GitHub OAuth, API tokens     │
│  ├ analyzers (local)             │──────► │  ├ metrics ingest + scoring           │
│  │  ├ code: per-language rules   │ metrics│  ├ public profile + badge endpoints   │
│  │  └ sessions: agent logs       │  only  │  └ repo connections (GitHub App)      │
│  └ upload (aggregates)           │        │ Web app: login, dashboard, settings   │
└──────────────────────────────────┘        │ Scan workers (isolated)               │
                                            │  └ safety scanner for public Git URLs │
┌────────── repository CI ─────────┐ OIDC   │ Postgres · object storage · job queue │
│ urfluency Action (later)         │──────► │                                       │
└──────────────────────────────────┘        └───────────────────────────────────────┘
```

Suggested stack: Go for the CLI, API, analyzers, and workers; Postgres; S3-compatible
object storage for scan reports; a Postgres-backed job queue to start; a
server-rendered web app (Go templates plus htmx) unless richer interactivity is
needed. All are decisions to confirm, not requirements.

## Metrics

Six categories. Each is computed per language where it applies, over a rolling
window (for example 90 days), and reports a band, a trend, evidence counts, and
confidence.

| Category | What it measures | Main signals | Confidence |
| --- | --- | --- | --- |
| **Clean code** | Readable, maintainable changes | Duplicated or near-duplicate code introduced; function length and complexity; dead code; discarded errors; naming drift from the repo's own conventions | Medium-high (static) |
| **Architecture** | Sound structure in the user's changes | Dependency-direction and layering violations introduced; global or shared mutable state; abstractions added without variation; source-specific branching repeated across functions | Medium (static rules plus optional model review) |
| **Type safety** | Reliable data contracts | Unchecked casts and assertions; untyped maps and `any` at boundaries; ignored decode errors; missing boundary validation | High for typed languages |
| **Language proficiency** | Correct use of each language's semantics and idioms | Language-semantic pitfalls per 1,000 changed lines (for Go: `switch` fall-through assumptions, reused decode targets, `WaitGroup` miscounts, assertions on nil interfaces); lint findings on changed lines; idiom adoption, adjusted for when features became available | High for detected pitfalls |
| **Security** | Safe handling of credentials and untrusted input | Leaked credentials (keys, tokens, private keys, hard-coded passwords) on the user's lines; injection-prone code (SQL built from strings, shell commands from input, `eval`, unsafe deserialization); disabled TLS verification; framework misconfiguration such as Django `DEBUG = True` or `csrf_exempt` | High for credentials and explicit patterns; values are never shown, stored, or uploaded |
| **Prompting standards** | Clear communication with coding agents | From opted-in local agent sessions: tokens per task, reworked files, corrections, first-attempt acceptance, reported per task type | Medium; validated in [clean-agent-code's prompting evaluation](https://github.com/rburns629/clean-agent-code/blob/main/evaluations/results/2026-10-19-prompting.md) |

### Strengths, weaknesses, and focus points

Every detector belongs to a versioned **rule catalog** entry:

```yaml
id: go.decode-target-reuse
category: language-proficiency
language: go
title: Reusing one variable for every json.Unmarshal
why: Fields missing from later documents keep earlier values, and appended records share slices and maps.
focus: Declare the decode target inside the loop, or decode with DataTo into a fresh value.
learn: https://urfluency.com/rules/go.decode-target-reuse
```

- **Weaknesses** are the rules with the highest incidence per volume of change,
  shown with the rule's explanation, occurrence count, trend, and focus advice.
  No code is shown; occurrences are counts only.
- **Strengths** are categories with low incidence on substantial volume, plus
  positive idiom signals (for example consistent error wrapping), with the evidence
  behind them.
- **Points of focus** are the top three rules to practice, chosen by frequency and
  impact.

### Scores

Each category shows a band (**Strong**, **Solid**, **Developing**, **Focus area**)
and a 0–100 value derived from incidence rates, with sample size and confidence
beside it. Categories with too little evidence show **Not enough data**. There is no
overall average: a strong category cannot hide a weak one, following the
reasoning in clean-agent-code's AUDIT.md.

### Measurement limits

- Authorship comes from commits matching the user's verified GitHub emails; the
  attested tiers count only signed commits.
- AI-assisted code cannot be distinguished; metrics describe committed code however
  it was written.
- Prompting signals depend on task type; a bug with an evident symptom needs little
  specification, so signals are grouped by task type.
- Architecture and clean code involve judgment; model-assisted review is optional,
  runs with the user's own or employer-approved model provider, and is labeled.

## CLI

```
urfluency login                 # GitHub device flow; token in the OS keychain
urfluency scan [path...]        # analyze repos; writes a local report
urfluency report                # show the local report and what would be uploaded
urfluency upload                # send aggregates; prints the exact payload first
urfluency sessions --enable     # opt in to agent-session analysis (Claude Code, Codex)
urfluency logout
```

- `scan` filters to the user's commits, runs language analyzers only on changed
  lines, and never executes project code beyond the language's static tools.
- `upload` always shows the payload and asks for confirmation unless `--yes` is
  passed.
- Session adapters read local transcripts (`~/.claude/projects/`, `~/.codex/sessions/`)
  only after opt-in, compute signals locally, and discard text.

### Analyzer architecture

Go comes first, but the CLI must grow to most languages and their frameworks
without a rewrite. Analysis is organized as plug-in analyzers behind one
interface, sharing Git access, ownership, and the rule catalog.

1. **Detection.** Each analyzer declares the languages and frameworks it handles
   and how to detect them: manifests (`go.mod`, `package.json`, `pyproject.toml`,
   `Cargo.toml`, `pom.xml`, `*.csproj`), file extensions, and dependencies that
   identify frameworks (for example React, Django, Spring).
2. **Ownership.** `internal/gitdata` lists the user's lines with `git blame` at the
   analyzed revision, matched to the user's emails. Analyzers receive files plus the
   owned line ranges and report findings; the engine keeps only findings on owned
   lines.
3. **Analyzer kinds.**
   - **Native analyzers** parse in process. Go uses `go/ast` with `go/types`. Other
     languages use the shared `internal/parsing` layer over the official tree-sitter
     Go bindings, through cgo (decided 2026-10-26; a WebAssembly runtime would have
     kept a pure-Go binary but needed a bridge nobody maintains). Release binaries
     are built per platform.
   - **Tool adapters** (opt-in) run linters a project already configures, such as
     ESLint or Ruff, and map their findings to catalog rules. Some linter
     configurations execute project code, so adapters run only on the user's own
     repositories and only when enabled.
   - **Framework packs** add rules on top of a language analyzer, such as React hook
     misuse, raw SQL in Django views, or field injection in Spring. A pack applies to a
     file when the file imports the framework, so detection needs no manifest
     parsing. Initial packs: React, Angular, Express, Fastify, FastAPI, Django, and
     Flask.
4. **Rules.** Every finding references a catalog rule with a language and optional
   framework, so strengths, weaknesses, and focus points work the same way for
   every language.
5. **Aggregation.** Metrics are reported per language and per framework, with
   changed-line volume, so rates stay comparable over time.

Adding a language means adding an analyzer package and its rules; the CLI, upload
format, and service do not change.

### Upload format

```json
{
  "method": "1.0",
  "tool": "0.1.0",
  "period": {"from": "2026-07-01", "to": "2026-09-30"},
  "tier": "self-reported",
  "languages": [{
    "language": "typescript",
    "frameworks": ["react"],
    "owned_lines": 11035,
    "rules": {"ts.explicit-any": 18, "ts.non-null-assertion": 10, "sec.api-token": 1}
  }],
  "prompting": {"tasks": 42, "by_task_type": {"feature": {"tokens_median": 81000, "corrections_per_task": 0.4, "rework_files_per_task": 0.6, "first_attempt_acceptance": 0.62}}}
}
```

Counts and volume are combined across every repository scanned in the period. The
payload has no repository names, paths, commit hashes, locations, or secret values,
and the CLI prints it for confirmation before sending. The server computes bands and
scores from rule counts and volume, so scoring can improve without a new CLI release,
and every result records the method version.

## Trust tiers

| Tier | Source | Shown as |
| --- | --- | --- |
| **Self-reported** | Local CLI | Default label on all CLI metrics |
| **CI-attested** | urFluency GitHub Action authenticated with GitHub OIDC; signed commits only | "CI-attested" |
| **Verified** | Server analysis of a public repo | "Verified" |

Server-side plausibility checks flag, but do not reject, implausible jumps, volumes
inconsistent with public activity, and unknown tool versions.

## Repository safety scanner

Anyone can submit a public Git URL. The scanner reports indicators of malicious or
privacy-invasive behavior; it cannot prove a repository is safe, and every report
says so.

### What it checks

| Area | Examples |
| --- | --- |
| Install-time execution | npm `preinstall`/`postinstall`, `setup.py` and build hooks, Makefile or script steps that download and run code |
| Remote code execution | `eval`/`exec` of fetched content, `curl \| sh`, dynamic imports from URLs |
| Data collection and reporting | Undisclosed telemetry or analytics, network calls to hard-coded endpoints, collection of hostnames, usernames, environment variables, or files |
| Credential access | Reading `~/.ssh`, cloud credential files, browser profiles, keychains, `.env` files |
| Obfuscation | Encoded or packed payloads, minified code in source directories, unexplained binaries |
| Persistence and abuse | Cron or startup entries, crypto-mining libraries or pools, reverse shells |
| Supply chain | Typosquatted or newly published dependencies, known vulnerabilities (OSV), unpinned remote scripts |
| Secrets | Committed keys or tokens (reported as present, never reproduced) |

Each finding includes the file and line, the behavior, the evidence, a severity, and
a confidence. Reports summarize as **No indicators found**, **Review recommended**,
or **High-risk indicators**, never "safe".

### Isolation

Submitted repositories are untrusted. The scanner:

- Runs in disposable, unprivileged containers or microVMs separate from the API,
  with no credentials and network access limited to fetching the repository.
- Clones with hooks disabled, without submodules or LFS, and with size, file-count,
  and time limits.
- Runs Git commands only inside the sandbox, because a repository's Git
  configuration can execute commands.
- Performs static analysis only at first. Dynamic analysis (running install steps
  in an instrumented sandbox to observe network and file access) is a later phase.
- Rate-limits submissions per user and IP.
- Keeps nothing after delivery: the clone, the report, and the submitted URL are
  deleted once the report is returned. Users who want to keep a report download it.
  A repository connected for a safety badge stores only `owner/repo` and its verdict
  and counts.

### Model assistance

A model may summarize findings and review suspicious files flagged by static rules,
with the repository content treated as untrusted input: instructions in the code or
README never change the scanner's behavior.

## Repository connections and badges

Owners connect repositories through a urFluency GitHub App:

- **Public repositories** are analyzed on the server at each push to the default
  branch (Verified tier).
- **Private repositories** use the urFluency GitHub Action, which analyzes in CI and
  uploads metrics with an OIDC token, so code never leaves the CI runner
  (CI-attested tier).

Badges are SVG, with a shields.io-compatible JSON variant:

```
https://urfluency.com/badge/repo/{owner}/{repo}/{category}.svg
https://urfluency.com/badge/repo/{owner}/{repo}/safety.svg
https://urfluency.com/badge/user/{handle}/{language}.svg
```

## API

Base URL `https://api.urfluency.com/v1`. Public endpoints return only what the user or
repository owner has published.

| Method and path | Auth | Purpose |
| --- | --- | --- |
| `POST /auth/device` | none | Start CLI device flow |
| `POST /metrics` | user token | Upload CLI metrics |
| `GET /users/{handle}` | none | Published profile: categories, languages, tiers, trends |
| `GET /users/{handle}/languages/{language}` | none | Published per-language metrics |
| `GET /me` | user token | Full private metrics with strengths, weaknesses, focus points |
| `DELETE /me` | user token | Delete account and all data |
| `GET /repos/{owner}/{repo}` | none | Published repository metrics |
| `POST /scans` | user token | Submit a public Git URL for a safety scan |
| `GET /scans/{id}` | scan owner, or public if shared | Scan status and report |
| `GET /rules/{id}` | none | Rule catalog entry |

- Tokens: GitHub OAuth for the web app; device flow for the CLI; personal API tokens
  with read-only scopes for portfolio use.
- Public endpoints allow CORS and are cached; all endpoints are rate limited.
- Responses include `method`, `tier`, `period`, and `sample` so consumers can show
  provenance.

## Data model (initial)

| Table | Key fields |
| --- | --- |
| `users` | id, github_id, handle, verified_emails, created_at |
| `tokens` | id, user_id, kind (cli, api), scopes, hash, last_used_at |
| `metric_uploads` | id, user_id, tier, method, tool, period, payload, received_at |
| `category_results` | user_id, language, category, band, score, sample, confidence, method, period |
| `visibility` | user_id, scope (category or language), public |
| `repos` | id, owner, name, installation_id, tier, category scores, safety verdict (connected repositories only) |
| `scan_jobs` | id, status, submitted_by, created_at; the URL and report exist only in the job's memory and temporary storage until delivery, then are deleted |
| `rules` | id, version, category, language, title, why, focus |

## Privacy and security

- No source code or repository details are stored. Scan workers hold a temporary
  clone and report only until delivery; connected repositories store `owner/repo`
  and scores only.
- Metrics are private by default. Users can export and delete everything.
- Aggregate metrics can still reveal employer information (activity, technology);
  the CLI shows a notice to check employment agreements before publishing.
- Session analysis is opt-in, local, and discards prompt text.
- Secrets are reported by type and location in the local CLI and in a delivered scan
  report only; their values are never printed, stored, or uploaded.
- Threat model to write before launch: forged uploads, token theft, scanner escape,
  prompt injection through scanned code, badge spoofing, and abuse of public
  endpoints.

## Phases

1. **Local CLI:** Go analyzers (pitfalls, type safety, clean code), local report; no
   accounts.
2. **Service and web app:** GitHub login, upload, dashboard with strengths,
   weaknesses, and focus points; public profiles and user badges; API tokens.
3. **Safety scanner:** static scanning of public URLs in isolated workers; safety
   badges.
4. **Prompting:** session adapters for Claude Code and Codex; task-type grouping.
5. **Repository connections:** GitHub App for public repos; Action with OIDC for
   private repos.
6. **More languages and dynamic scanning.**

## Open decisions

- Web stack: server-rendered Go with htmx, or a separate frontend.
- Hosting and the isolation technology for scan workers (containers with gVisor,
  or microVMs).
- Exact score formula per category and the minimum sample for a band.
- Whether model-assisted review is offered in the hosted scanner, and its cost limits.
- Languages after Go, Python, JavaScript, and TypeScript.
- License (for example MIT for the CLI and analyzers, with the service code to be
  decided).
