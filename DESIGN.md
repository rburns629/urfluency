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
- **Code and prompts never leave the machine.** Analysis runs locally. Optional
  model-assisted reviews call only a model runtime on the same machine (such as
  Ollama or LM Studio); hosted model providers are never used, so urFluency is safe
  to run on a work laptop.
- **AI fluency is a skill, not a penalty.** Coding agents are part of how most
  engineers now work. urFluency never counts agent use against anyone; it measures
  whether agent-assisted work meets the engineer's own bar, so it can show that the
  work is not slop.
- **Trends, not rankings.** Metrics describe one person's trajectory. There are no
  leaderboards and no cross-person comparisons.
- **Honest provenance.** Every number shows its trust tier, period, sample size, and
  method version.
- **Never read secret stores.** Files that hold secrets by design (`.env` files,
  keys and certificates, credential configs, Terraform state and variables, and
  directories such as `secrets/` or `.ssh/`) are excluded before any Git command
  reads them, in the CLI and the scanner alike.
- **Explain, don't grade.** Each weakness links to what was detected, why it
  matters, and what to practice. Each strength says what evidence supports it.
- **Opt-in visibility.** Everything is private by default; users publish per metric
  and per language.

## Architecture

```
┌────────── user machine ──────────┐        ┌──────────── urfluency.com ────────────┐
│ urfluency CLI                    │        │ API (Go)                              │
│  ├ login (device flow)           │ HTTPS  │  ├ auth: Firebase, upload tokens      │
│  ├ analyzers (local)             │──────► │  ├ metrics ingest + scoring           │
│  │  ├ code: per-language rules   │ metrics│  ├ public profile + badge endpoints   │
│  │  └ sessions: agent logs       │  only  │  └ repo connections (GitHub App)      │
│  └ upload (aggregates)           │        │ Web app: login, dashboard, settings   │
└──────────────────────────────────┘        │ Scan workers (isolated)               │
                                            │  └ safety scanner for public Git URLs │
┌────────── repository CI ─────────┐ OIDC   │ Firestore · Cloud Run · API Gateway   │
│ urfluency Action (later)         │──────► │                                       │
└──────────────────────────────────┘        └───────────────────────────────────────┘
```

Suggested stack: Go for the CLI, API, analyzers, and workers; Postgres; S3-compatible
object storage for scan reports; a Postgres-backed job queue to start; a
server-rendered web app (Go templates plus htmx) unless richer interactivity is
needed. All are decisions to confirm, not requirements.

## Repositories and distribution

Decided 2026-10-27: split by what runs on the user's machine versus what is
operated, not by CLI versus website.

| Repository | Visibility | Contents |
| --- | --- | --- |
| `rburns629/urfluency` | Public, open source | The CLI; all analyzers and the rule catalog; the upload format (the CLI-to-API contract); the scoring formula that turns rule counts into bands; and the safety scanner's detection engine |
| `rburns629/urfluency-app` | Private | The website and dashboard, API and accounts, badge serving, the hosted scan service (job queue, isolated workers, rate limits, abuse controls), and infrastructure and deployment configuration |
| `rburns629/homebrew-tap` | Public | The Homebrew formula |

- **Why.** Everything that runs on an engineer's laptop or decides their score can be
  audited, which is urFluency's strongest trust argument for work machines. Scanner
  rules are public, as in Semgrep and OSV; how scanning is operated stays private.
  Keeping the scoring formula public follows the honest-provenance principle.
- **Shared code.** `urfluency-app` imports the public module at a pinned version, so
  the hosted scanner and the CLI run identical detection. The shared packages are public
  top-level packages (`rules`, `analysis`, `analyzers/*`, `parsing`, `gitdata`, and
  `gates`), moved out of `internal/` on 2026-10-27, and `upload`, the upload
  contract (2026-10-27); the scoring
  formula, and scanner engine will be created as public packages when built.
  Sessions, reports, and the CLI stay internal.
- **Local safety checks.** Because the scanner engine is public, the CLI can check a
  repository locally: `urfluency check <git-url>` clones into a temporary directory and
  scans without executing anything.
- **Homebrew.** `brew install rburns629/tap/urfluency` installs a prebuilt binary.
  Tagging a version runs a release workflow that builds natively on macOS (Apple
  Silicon and Intel) and Linux (x86-64 and ARM), because tree-sitter uses cgo;
  publishes tarballs with SHA-256 checksums to a GitHub Release; and updates the
  formula, which declares `depends_on "git"`. A separate tap repository keeps the
  install to one command and the tap tiny, and keeps release bot commits out of the
  main history; once urFluency meets homebrew-core's bar (stable releases and enough
  adoption), the formula moves there and installs as `brew install urfluency`. Direct tarball downloads cover other
  setups; winget or Scoop can follow for Windows. The workflow, build script, and
  formula template are built (2026-10-27): macOS binaries target macOS 13 and later,
  Linux binaries are statically linked so they run on any distribution, and the
  formula renderer fails rather than publish a missing checksum. urFluency itself never installs
  dependencies: `urfluency doctor` checks them by capability and prints the official
  install command for anything missing.
- **License:** MIT, copyright urFluency contributors (decided 2026-10-27); all
  dependencies are MIT, ISC, or BSD licensed.
- **History audit** (2026-10-27): all 290 file versions and 70 commits were checked
  for credentials, personal addresses, local paths, and private project names; none
  were found.
- **Before the repository goes public:** create a fine-grained token with write
  access to the tap repository only, stored as an Actions secret.

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
| **AI engineering** | Turning coding agents into code that holds up | Quality of AI-assisted lines against the engineer's own hand-written baseline; agent changes verified by tests or checks; review engagement (agent edits rejected or reworked before commit); durability (later fix commits on AI-assisted versus other lines); tasks that needed a correction. See [AI engineering](#ai-engineering-proving-it-is-not-slop) | Medium; validated per signal like prompting rules |

### AI engineering: proving it is not slop

"Slop" is code shipped because an agent produced it, not because anyone made sure
it works and fits. The AI engineering category answers one question with evidence:
does this engineer's agent-assisted work hold up as well as their own? It replaces
the earlier "prompting standards" category, whose prompt-text signals did not
predict outcomes (see [Prompting validation](#prompting-validation)).

| Signal | Strength when | Weakness when |
| --- | --- | --- |
| Quality parity | Findings per 1,000 AI-assisted lines are at or below the engineer's hand-written rate | AI-assisted lines carry clearly more findings, named by rule ("agent TypeScript adds 4x your rate of `as unknown as`") |
| Verified changes | Agent edits are followed by tests, linters, builds, or type checks in the task | Agent edits ship with nothing run |
| Review engagement | The engineer rejects or reworks agent edits before committing | Every agent edit is accepted unchanged across large commits |
| Durability | AI-assisted lines need no more later fix commits than hand-written ones | AI-assisted lines are fixed or reverted soon after |
| Corrections | Tasks rarely need "no, that's not it" | Many tasks need corrections |

The AI-assisted share itself is never scored: a high share with strong parity,
verification, and durability is the best result in the category. Reports lead with
what the engineer does well with agents, then what to tighten. Each signal passes
the same validation as other prompting rules before it is shown.

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

Each language with at least 300 owned lines gets a **proficiency score** (0–100)
and a **level**, built from two kinds of evidence read from the syntax of the
user's own lines:

- **Idiom range** (positive): the share of the language's idiom catalog the user
  demonstrates, such as error wrapping with `%w` and table-driven tests in Go,
  dataclasses and generators in Python, or type guards and utility types in
  TypeScript. An idiom counts with at least 3 uses across at least 2 files, so one
  copied snippet does not. Idioms are rules in the `idiom` category, reported by
  the analyzers as positive findings and kept only on owned lines, like problems.
- **Category scores** (problems): for security, type safety, clean code, and
  language proficiency, `100 / (1 + rate / k)` where `rate` is findings per 1,000
  owned lines and `k` is the rate that scores 50: 0.5 for security, 10 for the
  others. Each category shows a band: **Strong** (85+), **Solid** (70+),
  **Developing** (50+), **Focus area**.

The score is `0.35 × range + 0.65 × mean(category scores)`, capped at the weakest
category plus 30, so a strong category cannot hide a weak one (the reasoning of
clean-agent-code's AUDIT.md). Levels, shared by every rating in the report:
**Expert** 85+, **Advanced** 70+, **Intermediate** 40+, **Beginner** below.
Confidence comes from volume: high from 10,000 owned lines, medium from 2,000, low
below.

The model is versioned (`proficiency.model`, now `0.2-uncalibrated`; 0.2 moved to
the four levels above). Its weights
and `k` values are reasoned starting points. Before scores are uploaded or shown
publicly they are calibrated against outcomes: per language, scores should
predict durability (how often the user's lines later need fixes), and once uploads
exist, the level bands are set from the distribution across users. Each change to
the model changes its version, and trends compare only results with the same
version.

### Summary and experience ratings

The final result is `combined`: all work with and without Git, merged at the
analysis level and scored as one, so a language is rated on every line of it
rather than averaged from its parts. It records how much is traced to the user's
commits (`verified_share`). The Git sections (`languages`, `tools`, `design`) and
`unversioned` are its traceable parts: they keep locations and show what was
verified and what was declared. An upload is built from `combined`.

The summary (`summary` in JSON) is built from `combined` and the rest of the
report, so it adds no data. It orders evidence the way a reviewer would read it:
strengths first, then design trends and communication, then what holds the
ratings down, a ratings table, and caveats. The text report prints the headline
first and the full summary last, where a terminal leaves it; design tables in the
parts show counts only, so a part never reads as a rating.

- **Languages** keep their proficiency score from `combined`, with the evidence
  showing lines with and without Git. The overall rating is the mean of language
  scores weighted by lines.
- **Frameworks, tools, and design areas** get an *experience* rating:
  `60 × min(1, log2(1 + repos) / log2(1 + R)) + 40 × min(1, log2(1 + files) / log2(1 + F))`,
  with R = 10 and F = 200 for frameworks and tools. Design areas pool many
  libraries and differ in how their code spreads: broad areas (cloud, APIs,
  data, front ends, mobile) use R = 25 and F = 500; narrow ones whose code sits
  in a few modules per service (messaging, auth, observability, command-line
  tools, AI) use R = 15 and F = 100; the rest use R = 15 and F = 200. The logarithm makes the first projects
  count most, and repositories weigh more than files because one large project
  can reflect a team's choices. Confidence: high from five repositories, medium
  from two. Package managers are listed but not rated: a manifest shows a
  language, not a skill.
- **Ways of working**: commit messages
  (`0.4 × descriptive + 0.2 × short subject + 0.4 × min(1, explained in body / 0.3)`,
  from 20 commits), testing (share of larger code changes with tests, where half
  rates 100, from 10 changes), and quality gates (the mean share of mandatory
  gates the CI the user wrote runs).
- **Prompting** (with `--sessions`): the mean share of tasks that met each graded
  rule with at least five applicable tasks.

**Design areas** come from imports in files with owned lines. Each analyzer's
language has an import pattern (Go uses its parser), and a table maps import
prefixes to an area and a library name, such as `google.cloud`, `@google-cloud/`
or `cloud.google.com/go` to cloud/gcp, or `requests`, `axios`, and `okhttp3` to
integrations/http-client. A bare name matches the import or its subpackages, never
a domain, so `github` (PyGithub) does not match `github.com/...`. Relative imports
are the user's own code and are ignored. Some work shows without a client
import, so a few content patterns also count: Pub/Sub-triggered functions
(`functions.pubsub.topic`, `onMessagePublished`; scheduled functions are cron, not
messaging), push-subscription handlers that read `message.data`, and CloudEvent
handlers (`@functions_framework.cloud_event`, `functions.CloudEvent`). Infrastructure also counts the user's
infrastructure tool files. Only area and library names and counts are kept.
Areas that appear together in at least three repositories and a tenth of those
scanned name the kind of work: cloud integration (cloud and integrations),
backend services (APIs and data), event-driven systems (messaging and cloud),
platform and infrastructure, data engineering, AI applications, machine learning,
mobile apps, web front ends, and developer tooling.

**Communication** (with `--sessions`) counts task prompts that explain why
(rationale), set limits (constraints), state expected behavior (criteria), name
specific code (context), or ask for checks (verification), plus how often the
user rejects or changes agent edits. Prompt text is classified in memory and only
counts are kept. These are descriptive traits: prompt-text signals did not predict
outcomes in validation (see Prompting validation), so they are not scored.

Experience is not quality. A framework rated Expert by experience says the user
built a lot with it; only language ratings judge how well. All ratings share the
model version and are uncalibrated.

### Tools and Git

Tool usage comes from paths only: a file belongs to a tool by name or extension
(`*.tf` to Terraform, `Dockerfile` to Docker, `.github/workflows/*.yml` to GitHub
Actions, and so on), and usage is the user's commits, files, lines added, and
latest commit to those files. Contents are never read for this, so files that hold
values, such as `.tfvars`, stay closed. Without Git, only files present are
counted. Git habits are shares of the user's commits: subjects of 72 characters or
fewer, descriptive subjects (three or more words, not "wip"), bodies that explain
beyond trailers, and Conventional Commits. Subjects are measured in memory and
never reported. Tools and Git are tracked, not yet scored.

Files are read only for supported languages, the CI, build, and agent instruction
files the practices need, and extensions the user includes (`--include-ext`, or
saved by `init`). The secrets scanner runs on the same files, so secrets in
excluded config files are not caught; this is the user's choice to widen.
A Google API key is not reported when it is the `apiKey` of a Firebase web
config (with `authDomain` and `messagingSenderId` or `appId` within ten lines):
Firebase web keys ship to every browser by design and access is governed by
Firebase Security Rules. Any other Google key is reported.

### Measurement limits

- Authorship comes from commits matching the user's verified GitHub emails; the
  attested tiers count only signed commits.
- AI-written code cannot be told apart from the code itself. Two sources bound it.
  The share is never scored or counted against anyone, and publishing it is a
  separate opt-in; the AI engineering category scores how the assisted code holds
  up:
  - **Commit trailers** (every scan): owned lines from commits whose
    `Co-authored-by` trailers name a coding tool by its email domain or identity
    (`@anthropic.com`, `@openai.com`, `@cursor.com`, the GitHub `Copilot` user, or
    "(aider)" in the author name). Works for any tool and machine, but marks whole
    commits, so it overstates the line share, and trailers can be turned off.
  - **Sessions** (`--sessions`): owned lines matching lines agents added in local
    sessions in the same file. Line-level, but a lower bound: inline completions,
    pasted code, and deleted transcripts are missing.

  On the author's projects (2026-10-26) the two gave 29% (sessions) and 53%
  (trailers) of 136,794 owned code lines.
- Each language also reports its authorship split: the share of owned lines that
  are AI-assisted (agent-written in sessions or from AI-marked commits) and the
  findings rate for those lines versus the rest. Strengths and weaknesses still
  cover all owned code, since the user is responsible for what they commit; the
  split shows whose code a result reflects. On the author's projects, TypeScript
  was 75% AI-assisted with 5.37 findings per 1,000 AI-assisted lines against 1.36
  for the rest, while Go was 53% with similar rates (2.97 and 3.28).
- Prompting signals depend on task type; a bug with an evident symptom needs little
  specification, so signals are grouped by task type.
- Architecture and clean code involve judgment; model-assisted review is optional,
  runs with the user's own or employer-approved model provider, and is labeled.

### Prompting validation

Rules ship pre-validated; users never calibrate anything. A prompt-text signal
becomes a graded rule only if tasks with the signal turn out measurably better than
tasks without it. `urfluency validate [path]` prints that comparison from local
sessions, as counts only: corrections, follow-up prompts, reads and searches before
the agent's first edit, and whether code changes were checked.

Run of 2026-10-26: one author, 25 sessions (12 Claude Code, 2 Codex, 11 Copilot
Chat, of which 6 were empty), 345 tasks.

| Signal in the prompt | With / without: corrections | Searches before first edit | Changes checked |
| --- | --- | --- | --- |
| Concrete context (files, symbols, errors) | 2% / 1% | 3.2 / 4.0 | 39% / 65% |
| Acceptance criteria | 0% / 2% | 5.2 / 3.5 | 70% / 60% |
| Asks for verification | 2% / 1% | 3.1 / 4.0 | 45% / 64% |

None of the three predicted better outcomes, so none is graded. Asking for
verification did not even predict that checks ran: projects whose agent
instructions require checks ran them unprompted. Verification is therefore graded
on what ran, not on what the prompt said. Limits: one author, whose sessions are
mostly long conversations where follow-ups inherit context; corrections are rare
(about 1%) and detected by keywords; harder tasks need more searching regardless.
Re-run validation across opted-in users' aggregate counts before reconsidering
these signals.

### Deep review (local model, experimental; drafted, not built)

Some prompting skills need reading, not counting: whether intent was clear,
whether the right context was given, whether the user managed the agent well. A
deep review asks a model to judge them with the rubric in
[`internal/review/prompt.md`](internal/review/prompt.md), returning JSON that
matches [`internal/review/schema.json`](internal/review/schema.json).

- **Division of labor.** urFluency measures outcomes (checks run, corrections,
  findings on agent-written lines) and code quality itself, and hands them to the
  model as facts. The model judges four competencies only: intent, context,
  verification, and agent management, in bands (strong, solid, developing, focus
  area, not assessable), per session and overall. No overall score, rank, or
  comparison with other people.
- **Local by default.** It runs only on explicit request, against a model runtime
  on the same machine (an OpenAI-compatible endpoint on `localhost`, such as Ollama
  or LM Studio), or through an installed agent CLI the user names (see
  [Review agents](#review-agents---review)); urFluency never bundles a model or
  holds a provider credential. Before
  calling, the CLI removes injected editor blocks, redacts anything the secrets
  scanner matches, truncates each user turn to 2,000 characters and each agent
  excerpt to 400, and includes only sessions for the scanned repositories (at most
  20, most recent first). The full result stays local; at most the overall bands
  are published, with their provenance (see [Upload format](#upload-format)).
- **Injection.** Sessions and repository files are untrusted. The rubric treats the
  packet as data, and the result flags sessions that appear to address the
  evaluator.
- **Versioning.** Results record the rubric version and model; trends compare only
  results with the same pair.
- **Validation before release**, the same rule as other prompting rules:
  1. Repeatability: the same packets reviewed twice by the same model agree on at
     least 80% of session bands, and two different models on at least 70%.
  2. Prediction: per-session bands track urFluency's outcomes, for example
     sessions banded `focus_area` for intent show more corrections and follow-ups,
     and `strong` verification sessions show more checked changes.
  Competencies that fail either test are not shown. Token use and cost are measured
  on the first validation run.

### Design review (local model, experimental; drafted, not built)

Static rules cover idioms, type safety, error handling, reliability, and security.
Design choices, test quality, and production practices need reading. A design
review asks a model to judge them per technology with the rubric in
[`internal/review/design.md`](internal/review/design.md), returning JSON that
matches [`internal/review/design.schema.json`](internal/review/design.schema.json).

- **Authored code only.** Excerpts are labeled by authorship (sessions and commit
  trailers). The model judges only lines the user wrote without an agent; accepted
  AI code is context, never evidence of skill. Too little authored code yields
  "not assessable", never a low band.
- **Local by default.** It runs only per repository on explicit request, against a
  model runtime on the same machine or an agent CLI the user names (see
  [Review agents](#review-agents---review)). Excerpts come from owned
  lines only, protected files are never read, secrets are redacted, and each
  technology is limited to a sampled set of excerpts.
- **Complements, never replaces, the deterministic metrics** in
  [Design metrics](#design-metrics-deterministic). Small local models are often too
  inconsistent to pass validation; until one does, model results are shown locally
  only and never enter scores or profiles.
- **No duplicate measurement.** Static findings are passed in as facts; the model
  judges three competencies (design, testing, production practices) in bands, with
  file and line references, and no overall score or rank.
- **Validation before release:** the repeatability thresholds of the deep review,
  and prediction against history: files whose design or testing is banded
  `developing` or `focus_area` should see more later bug-fix commits than files
  banded `strong`.

### Review agents (`--review`; drafted, not built)

`--review <agent>` runs the deep and design reviews through a coding agent CLI the
user already installed and signed in to. urFluency starts the agent in its
non-interactive mode, in an empty temporary folder with no tools, and reads its
JSON answer; the agent uses its own sign-in, so
urFluency never asks for, reads, stores, or passes an API key or token, and never
installs an agent. `urfluency doctor` reports which agents are installed.

| `--review` | Agent CLI | Non-interactive call | Sign-in it uses | Status |
| --- | --- | --- | --- | --- |
| `local` | Ollama or LM Studio | OpenAI-compatible endpoint on `localhost` | None | Default; code stays on the machine |
| `claude` | Claude Code (`claude`) | `claude -p <packet> --tools "" --strict-mcp-config --disable-slash-commands --no-session-persistence --output-format json`, run in the empty folder | `claude` login (Claude subscription) or the user's API key | Planned; passed the canary test on Claude Code 2.1.308 |
| `codex` | Codex CLI (`codex`) | `codex exec -C <empty dir> --skip-git-repo-check --sandbox read-only --ephemeral --ignore-user-config --json --disable shell_tool --disable unified_exec --disable apps --disable plugins --disable browser_use --disable in_app_browser --disable computer_use --disable view_image --disable tool_suggest -c 'web_search="disabled"' <packet>` with stdin closed | `codex login` (ChatGPT plan) or the user's API key | Planned; passed the canary test on Codex CLI 0.163.0 |
| `copilot` | GitHub Copilot CLI (`copilot`) | `copilot -C <empty dir> -p <packet> --available-tools <unknown name> --no-custom-instructions --disable-builtin-mcps --no-ask-user --disable-client-telemetry --output-format json` | `copilot login` (keychain), or `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN`, then `gh` | Planned; call checked against Copilot CLI 1.0.104 |

Copilot Chat in VS Code and Copilot inline completions have no command-line entry
point, so only Copilot CLI can review.

- **Hosted agents send code off the machine.** Every agent except `local` sends the
  review packet (redacted excerpts and session signals) to its provider. They are
  never the default, and they are never selected from configuration alone: each run
  needs `--review <agent>` and prints the files and sessions it will send before
  sending. On a work machine, use one only if the employer's agreement with that
  provider covers the code.
- **Missing agents.** If the named agent is not on `PATH`, the scan stops before
  reading anything and prints where to get it for the user's operating system, then
  how to sign in. urFluency never runs an installer. The official page is the
  source of truth; the command is a convenience and is only shown for package
  managers found on the machine (`brew`, `npm`), so the user gets one command they
  can run instead of a list. `doctor` prints the same hint for each agent it does
  not find.

  ```
  urfluency: --review copilot needs GitHub Copilot CLI, which is not installed.
    Install:  brew install --cask copilot-cli
    Docs:     https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/install-copilot-cli
    Then run `copilot login` once, and try again.
  ```

  | Agent | macOS | Linux | Windows | Sign in |
  | --- | --- | --- | --- | --- |
  | `claude` | `brew install --cask claude-code` | `npm install -g @anthropic-ai/claude-code` | `npm install -g @anthropic-ai/claude-code` | `claude`, then `/login` |
  | `codex` | `brew install --cask codex` | `npm install -g @openai/codex` | `npm install -g @openai/codex` | `codex login` |
  | `copilot` | `brew install --cask copilot-cli` | `brew install --cask copilot-cli` or `npm install -g @github/copilot` | `npm install -g @github/copilot` | `copilot login` |
  | `local` (Ollama) | `brew install ollama` | ollama.com/download | ollama.com/download | None; pull a model |
  | `local` (LM Studio) | `brew install --cask lm-studio` | lmstudio.ai | lmstudio.ai | None; load a model |

  Docs links: Claude Code `https://docs.claude.com/en/docs/claude-code/setup`,
  Codex `https://developers.openai.com/codex/cli`, Copilot CLI (above). Without a
  usable package manager the hint shows only the docs link. When npm is the only
  route, the hint names Node.js as a prerequisite. Package names were checked on
  2026-10-27 (Homebrew casks `claude-code`, `codex`, `copilot-cli`, `lm-studio`;
  formula `ollama`; npm packages above) and live in one table in code, covered by a
  test, so they change in one place.
- **Installed but not signed in.** If the agent's first call fails on
  authentication, urFluency prints the sign-in command from the table and stops;
  it never retries with other credentials.
- **No tools, empty folder.** The packet is the whole input: the agent runs in an
  empty temporary folder with every tool removed, so it cannot read files beyond
  the redacted packet, and urFluency rejects output that does not match the
  schema. A read-only sandbox is not enough, because reading is the risk: in
  non-interactive mode Copilot CLI 1.0.104 runs its file-reading tools (`view`,
  `glob`) without asking, and Codex CLI 0.163.0 with `--sandbox read-only` read a
  file outside its working folder with `cat`. Tools must be removed outright:
  Copilot's `--excluded-tools view glob grep bash` still left `read_bash`
  available, while `--available-tools` naming a tool that does not exist left
  none; Claude Code's `--tools ""` left none; Codex needed its tool features
  disabled one by one, and feature names change between releases. Before an
  adapter ships, and after agent upgrades, a canary test puts a file in the folder
  and asks the agent to read it; the adapter passes only if no tool runs and the
  file's contents are not returned.
- **Input.** Copilot CLI ignores stdin when given `-p`, so the packet goes in the
  prompt argument (packets stay well under the operating system's argument limit).
  Each agent's input path is checked the same way.
- **The agent's own sign-in, unchanged.** urFluency runs the agent with the user's
  environment as is and never reads the agent's credential files. Copilot CLI
  prefers a `GH_TOKEN` or `GITHUB_TOKEN` in the environment over its own login, so
  `doctor` warns when one is set. Each Copilot prompt counts as a premium request;
with the model left on `auto`, the model Copilot chose is read from the
`session.auto_mode_resolved` event for provenance.
- **Provenance.** Results record `enhanced_with` with the agent as the runtime
  (for example `"runtime": "codex"`) and the model it reports (see
  [Upload format](#upload-format)), and pass the same validation as local models
  before they are shown.
- **Terms.** urFluency starts the user's own installed agent; it does not reuse a
  subscription sign-in in its own requests. Check each provider's current terms for
  automated use of subscription plans before release.

### Design metrics (deterministic)

The core answers design, testing, and production questions without a model, so
results are fast, repeatable, and never leave the machine:

- **Durability** (built): the SZZ method traces each fix commit's changed lines to
  the commits that introduced them; owned lines from commits at least 90 days old
  are compared by whether a fix changed them within 90 days, split by AI-marked
  commits. This is also the outcome every other design metric is validated against.
- **Cognitive complexity** per function (built): SonarSource's published
  specification on the existing parsers (`go/ast` and tree-sitter), including
  recursion and nested functions, which deepen nesting and count toward the
  enclosing function. Functions above 15, SonarQube's default, are a clean-code
  finding, so they get the authorship split and AI engineering parity like every
  other rule. Tests cover the specification's worked examples and cases adapted
  from codemetrics.
- **Testing** (built): owned test lines per owned code line, per language; and the
  share of code-changing commits (10 or more added code lines) that also change
  tests, split by AI-marked commits. Test files follow each language's conventions
  (`analysis/testpath.go`).
- **Production practices** (built): in repositories where the user owns lines of
  the CI configuration, which practices it runs (tests, lint, format, type checks,
  security scanning), including those delegated to the Makefile or `package.json`.
- **Structure** (not built): import cycles between JavaScript and TypeScript modules.
  Go forbids import cycles, so Go needs no check.

These are reported as "Practices" and shown but not scored until each is validated
against durability. On the author's projects (2026-10-27), 88% of AI-assisted code
changes came with tests against 47% of hand-written ones.

Libraries were evaluated before writing these. `codemetrics` (Go, MIT) computes
both complexity measures across 17 languages, but requires Go 1.27, brings a second
tree-sitter implementation, and was four months old with one maintainer when
checked (2026-10-27), so its tests serve as a cross-check rather than a dependency.
Server platforms such as SonarQube are out of scope for a single local binary. Tools
a project already configures (ESLint, Ruff, golangci-lint) remain optional adapters.

### clean-agent-code integration

urFluency measures whether the practices clean-agent-code defines actually happen:

- **Quality gates.** `gates` names the gates of
  [QUALITY_GATES.md](https://github.com/rburns629/clean-agent-code/blob/main/standards/QUALITY_GATES.md)
  (format, lint, types, build, tests, security) and the commands that run them.
  CI the user helped write is checked against the mandatory gates that apply to
  its languages, and the AI engineering section shows which gates agents ran in
  tasks with edits.
- **Agent instructions.** Repositories where the user wrote AGENTS.md, CLAUDE.md,
  or similar files are counted with what the files cover (verification, secret
  handling, conventions), and AI-assisted work is compared between repositories
  with and without them: real-world evidence on whether written standards help.
- **Standards links.** Rules link to the clean-agent-code document behind them
  (type safety, security, architecture, quality gates); language idiom rules have
  none.
- **Evaluation harness** (planned). clean-agent-code's Docker harness generates
  sessions with known prompt quality and instruction conditions, which gives
  `urfluency validate` controlled data beyond one author's sessions; and
  `urfluency scan --json` can score harness runs deterministically next to its
  model judge. Runs cost API credits and need an explicit budget.

### Prior art

As of 2026-10-27, open-source work splits into two kinds, and none links agent
sessions to the quality of the committed code:

- **Session analyzers** report usage from Claude Code or Codex logs: tokens, cost,
  tool use, and behavioral ratios such as reads per edit (for example
  [session-analyzer](https://github.com/Tomatio13/session-analyzer),
  [claude-session-analyzer](https://github.com/lucemia/claude-session-analyzer),
  [claudelytics](https://github.com/nwiizo/claudelytics)).
- **Prompt and instruction linters** score prompt text or files such as AGENTS.md
  with rules or a trained model (for example
  [reprompt](https://pypi.org/project/reprompt-cli/),
  [Schliff](https://github.com/Zandereins/schliff),
  [prompt-architect](https://www.sourcepulse.org/projects/33953940)). Scoring prompt
  text is the approach urFluency's validation found did not predict outcomes.

urFluency's difference is the join: sessions and commit trailers attribute owned
lines to agents, and those lines are measured with the same analyzers, history, and
outcomes as hand-written code.

## CLI

```
urfluency login                 # GitHub device flow; token in the OS keychain
urfluency scan [path...]        # analyze repos; writes a local report
urfluency report                # show the local report and what would be uploaded
urfluency upload                # send aggregates; prints the exact payload first
urfluency scan --sessions       # also grade agent tasks and report AI-written code
urfluency validate [path]       # maintainers: check prompt signals against outcomes
urfluency logout
```

- `scan` filters to the user's commits, runs language analyzers only on changed
  lines, and never executes project code beyond the language's static tools.
- `upload` always shows the payload and asks for confirmation unless `--yes` is
  passed.
- `scan` finds the user's commits without a list of addresses. Seeds are
  `user.name` and `user.email` as each repository resolves them, the OS account's
  full name, and any `--author`. Detection links through history until nothing
  changes: an address used with a known name joins, and a name used with a known
  address joins when it has at least two words (one-word handles link only as
  seeds). Names compare by first and last word, so middle initials match. Bots,
  `noreply@github.com`, and `root` never link. Unlinked addresses that contain the
  user's last name, initial plus last name, or the letters of a known address are
  listed (top 10) as possible and not counted. A shared name can link a
  namesake's address; the counted addresses are always printed so the user can
  see it. Addresses go to stderr only and never into the report or an upload.
- `init [path]` runs the same detection over the repositories under a path, asks
  about each possible address in a terminal (`--yes` skips the questions), and
  saves confirmed and declined addresses to `settings.json` in the user config
  directory with owner-only permissions. Scans seed detection with the saved
  addresses and drop declined ones from the possible list. Running `init` again
  merges with what is saved. Nothing about repositories is saved.
- `scan` accepts a repository or a folder; for a folder it scans every repository up
  to four levels below it and combines the results. A path inside a repository scans
  that repository. `--exclude` skips folders by name or relative path.
- Folders without Git (history lost, or `git init` with no commits) are scanned
  directly when they hold a manifest, or source files and no other projects; a
  folder of projects with a stray script is a container, not a project. The same
  filters apply as for repositories (dependency and secret folders, files over
  2 MB, binaries), plus what a repository would have left out: the folder's
  `.gitignore` and `.gitattributes`, read through a scratch repository in the
  system temporary directory so nothing is written to the folder, and build output
  (`dist`, `build`, `out`, `coverage`, `target`). Every line counts as the user's, so results are reported in a
  separate section as declared authorship, never merged with attributed results,
  and history measures (AI share, durability, tests with changes) are omitted. This
  relies on honesty, like all self-reported metrics (see Trust tiers).
- Session adapters read local transcripts only with `--sessions`, and only sessions
  that ran in or edited the scanned repositories. Each adapter records prompts, the
  lines the agent added, and its reads, edits, and checks. Formats are undocumented,
  so each adapter is checked against real files with a structure-only read (key
  paths and types, never values) before it ships.

  | Agent | Location | Status |
  | --- | --- | --- |
  | Claude Code | `~/.claude/projects/*/*.jsonl` (`CLAUDE_CONFIG_DIR`) | Supported, checked against real sessions |
  | Codex (OpenAI or local models) | `~/.codex/sessions/**/rollout-*.jsonl` (`CODEX_HOME`) | Supported, including code-mode `exec` scripts; checked against real sessions without patches, so patch parsing is tested on fixtures only. Models from local providers are labeled `lmstudio/<model>` or `ollama/<model>`; this labeling is tested on fixtures only (a local LM Studio run was too slow to complete on the development machine) |
  | GitHub Copilot Chat in VS Code | `<VS Code user dir>/workspaceStorage/*/chatSessions/*.json[l]`, folder from `workspace.json` | Supported for the JSON document and JSONL operation-log formats; checked against real sessions |
  | GitHub Copilot CLI | `~/.copilot/session-state/<id>/events.jsonl` | Not yet: no real sessions to check against |
  | Cursor, Gemini CLI, Aider, Cline, Continue, opencode | Tool-specific | Not yet; local models are covered through the tool that runs them |

  Copilot inline completions are not logged locally, so the AI-written share
  undercounts code from them. Commit trailers give a tool-independent signal
  (see Measurement limits).

  Each prompt is reduced to signals while parsing; its text is never kept,
  printed, or uploaded. Subagent
  transcripts are skipped because an agent wrote those prompts. Grading uses fixed
  local rules, never a model call, so prompts never leave the machine.

### Analyzer architecture

Go comes first, but the CLI must grow to most languages and their frameworks
without a rewrite. Analysis is organized as plug-in analyzers behind one
interface, sharing Git access, ownership, and the rule catalog.

1. **Detection.** Each analyzer declares the languages and frameworks it handles
   and how to detect them: manifests (`go.mod`, `package.json`, `pyproject.toml`,
   `Cargo.toml`, `pom.xml`, `*.csproj`), file extensions, and dependencies that
   identify frameworks (for example React, Django, Spring).
2. **Ownership.** `gitdata` lists the user's lines with `git blame` at the
   analyzed revision, matched to the user's emails. Analyzers receive files plus the
   owned line ranges and report findings; the engine keeps only findings on owned
   lines.
   Blame is most of a scan's cost, so only files that a commit by the user's
   emails ever changed (following later renames) are blamed; files over 2 MB are
   skipped by size, and a file that takes over 60 seconds is skipped and counted.
   Author emails are matched as fixed strings with their angle brackets, so the
   "+" in GitHub noreply addresses is literal. Measured on 98 repositories
   (2026-10-27): 7 minutes before these changes, 42 seconds after.

3. **Analyzer kinds.**
   - **Native analyzers** parse in process. Go uses `go/ast` with `go/types`. Other
     languages use the shared `parsing` layer over the official tree-sitter
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

Built (2026-10-27) as the public `upload` package, which the CLI and the API
both validate with. `urfluency upload` sends a report's `summary` and `combined`
objects with version information:

```json
{
  "format": "1",
  "tool": "0.1.8",
  "report_format": "0.4",
  "catalog": "0.10",
  "model": "0.2-uncalibrated",
  "tier": "self-reported",
  "since": "all",
  "summary": {"headline": "…", "ratings": [], "strengths": []},
  "combined": {"repositories": 94, "languages": [], "design": []}
}
```

`upload.Validate` rejects a payload with fields named like locations, paths,
repositories, commits, emails, or code, or with strings that look like email
addresses, home-directory paths, or commit hashes, so a bug that leaks them fails
on the user's machine and again on the server. The CLI only claims
`self-reported` (or `declared` when nothing came from Git); the service assigns
the tiers it verifies. `--dry-run` prints the exact payload.

The first sketch, kept for the server-side scoring it describes (raw rule counts
so scoring can improve without a CLI release):

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
  "prompting": {"sessions": 18, "tasks_by_type": {"feature": 20, "bug": 12},
    "rules": {"prompt.unverified-change": {"applicable": 26, "missed": 14}}},
  "ai_written": {"owned_lines": 11035, "ai_written_lines": 3120},
  "reviews": [{
    "kind": "design",
    "technology": "go",
    "bands": {"design": "solid", "testing": "strong", "production_practices": "solid"},
    "enhanced_with": {"model": "qwen3-coder-30b", "runtime": "ollama", "rubric": "design-0.1", "validated": false}
  }]
}
```

Anything a model produced carries `enhanced_with`: the model, the runtime (a local
one, or the agent CLI named with `--review`),
the rubric version, and whether that rubric and model pair passed validation. The
service keeps model-assessed results separate from measured ones, labels them with
their provenance, and excludes unvalidated pairs from scores, profiles, and badges.

Counts and volume come from the report's `combined` result: every repository
and folder scanned in the period, with the share traced to Git. The
payload has no repository names, paths, commit hashes, locations, or secret values,
and the CLI prints it for confirmation before sending. The server computes bands and
scores from rule counts and volume, so scoring can improve without a new CLI release,
and every result records the method version.

## Trust tiers

| Tier | Source | Shown as |
| --- | --- | --- |
| **Declared** | Local CLI on folders without Git history | "Declared"; kept apart from attributed results |
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

### Engine and `urfluency check` (built 2026-10-28)

The public `safety` package holds the rules (version 0.1) and runs in both the
CLI and the hosted scanner. Rules are a table of line patterns with a severity,
a confidence, and an explanation, plus manifest checks (npm install scripts and
URL dependencies, `setup.py` install hooks, near-miss names of popular npm and
PyPI packages), file-level combinations (machine details read and sent to a
hard-coded address; fetch and evaluate in one file), committed executables, and
the CLI's secrets scanner. Whole-line comments are skipped; printed messages,
documentation, tests, CI, and container builds are lowered to notes, since they
do not run on install. The verdict is High-risk indicators for any high-severity
finding of medium or high confidence, Review recommended for any other
non-note finding, and No indicators found otherwise. The score starts at 100 and
loses 35, 12, or 4 points per high, medium, or low finding, scaled by confidence
(1, 0.6, 0.3), with each rule counted at most three times and notes at most 10
points in all; high risk caps it at 40.

Calibration (2026-10-28): expressjs/express, psf/requests, spf13/cobra,
nvm-sh/nvm, and Homebrew/install score No indicators found (90–100); a
synthetic malicious repository scores High-risk indicators.

`urfluency check <url|path>` clones with `--depth=1 --no-recurse-submodules`,
hooks pointed at `/dev/null`, symlinks off, LFS and filters off, `file` and `ext`
transports refused, the system and global Git configuration ignored, and no
terminal prompts or credential helpers, under a two-minute timeout and a 500 MB
size limit. Scans read at most 20,000 files of up to 2 MB each and skip `.git`
and installed dependency folders.

### Hosted scanning (decided 2026-10-28, not built)

- **Runner:** Cloud Run jobs running an image built from a pinned urFluency
  release with `git`, as a non-root user. Not Cloud Build, which queues, keeps
  build records with the URL, and cannot restrict egress cheaply; not Homebrew,
  since releases ship static Linux binaries.
- **Identity:** the job's service account has no roles. The job's settings hold
  only the scan ID and a one-time token (stored hashed); it fetches the
  repository from the API and returns the report, each time with that token and
  a Google-signed ID token for its service account. A compromised job has
  nothing to steal.
- **Network:** direct VPC egress with firewall rules allowing only the Git hosts
  and the API.
- **Access:** signed-in users can scan and keep a private history of
  `owner/repo`, verdict, score, and date for public repositories. Without an
  account, three scans per IP address per 24 hours, with Firebase App Check;
  anonymous results are not stored. The URL is never logged, and an anonymous
  scan's record forgets it once the job has it.
- **Flow:** `POST /v1/scans` validates the URL against allowed hosts, reads the
  default branch's commit with Git's smart HTTP protocol (a missing or private
  repository is answered at once, with no job and no quota charge), records the
  scan with a one-hour TTL, and starts the job; the browser polls
  `GET /v1/scans/{id}`, which returns the result once and deletes the record.
  The API deletes finished job executions after each result.
- **Cost bounds:** three anonymous scans per client IP and at most 300 new
  anonymous jobs per day across everyone; past that, anonymous visitors are
  asked to sign in or use `urfluency check`, which runs locally at no cost to
  the service and needs no account. A VPN changes the IP but not the global
  budget. App Check is to follow before the scanner is promoted.
- **No reuse:** reports are not cached, so every anonymous scan is a new job
  within the global budget.

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
  deleted once the report is returned, except that a signed-in user's private
  history keeps `owner/repo`, the verdict, the score, and the date. Users who want to keep a report download it.
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
| `POST /auth/device` | none | Start a CLI device login (RFC 8628) |
| `POST /auth/device/token` | device code | Poll for the upload token |
| `POST /auth/device/approve` | Firebase | Approve or deny a code as the signed-in user |
| `POST /metrics` | upload token | Upload a payload from `urfluency upload` |
| `PUT /me` | Firebase | Set handle, name, and whether the profile is public |
| `GET /users/{handle}` | none | Published profile: categories, languages, tiers, trends |
| `GET /users/{handle}/languages/{language}` | none | Published per-language metrics |
| `GET /me` | Firebase | Account and latest upload |
| `DELETE /me` | Firebase | Delete account and all data |
| `GET /repos/{owner}/{repo}` | none | Published repository metrics |
| `POST /scans` | user token | Submit a public Git URL for a safety scan |
| `GET /scans/{id}` | scan owner, or public if shared | Scan status and report |
| `GET /rules/{id}` | none | Rule catalog entry |

- Sign-in: Firebase Authentication with Google and email (decided 2026-10-27),
  verified by API Gateway. The CLI uses a device flow: `urfluency login` shows a
  code, the user approves it at urfluency.com/connect, and the CLI receives an
  upload-only token that expires after 90 days; only its hash is stored, and the
  CLI keeps it in `credentials.json` beside the settings, readable only by the user.
  Personal API tokens with read-only scopes for portfolio use come later.
- Built in `urfluency-app/api` with an OpenAPI 2.0 spec for API Gateway; the
  remaining endpoints in this table are planned.
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
- Session analysis is opt-in and local; prompt text is reduced to signals during
  parsing and never stored or uploaded. Model-assisted reviews call only a model
  runtime on the same machine; source code and prompt text never leave it. The AI-written share is uploaded only if the
  user opts in to publishing it.
- Secrets are reported by type and location in the local CLI and in a delivered scan
  report only; their values are never printed, stored, or uploaded.
- Threat model to write before launch: forged uploads, token theft, scanner escape,
  prompt injection through scanned code, badge spoofing, and abuse of public
  endpoints.

## Phases

1. **Local CLI:** Go analyzers (pitfalls, type safety, clean code), local report; no
   accounts.
2. **Distribution:** license, history audit, public repository, release workflow,
   Homebrew tap, `urfluency doctor`, and shared packages moved out of `internal/`.
3. **Service and web app** (in `urfluency-app`): Google and email sign-in, upload, dashboard
   with strengths, weaknesses, and focus points; public profiles and user badges;
   API tokens.
4. **Safety scanner:** the engine and `urfluency check` in the public repository;
   hosted scanning of public URLs in isolated workers and safety badges in
   `urfluency-app`.
5. **Prompting:** session adapters for Claude Code, Codex, and Copilot Chat in VS
   Code, outcome-based grading, and AI-written share (local CLI done); Copilot CLI,
   Cursor, and Gemini CLI next. Commit-trailer detection done.
6. **Repository connections:** GitHub App for public repos; Action with OIDC for
   private repos.
7. **More languages and dynamic scanning.**

## Open decisions

- Web stack: server-rendered Go with htmx, or a separate frontend.
- Hosting and the isolation technology for scan workers (containers with gVisor,
  or microVMs).
- Exact score formula per category and the minimum sample for a band.
- Whether model-assisted review is offered in the hosted scanner, and its cost limits.
- Languages after the current twelve, and F# and Visual Basic for .NET.

