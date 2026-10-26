# Project context example: fictional task tracker

This completed example describes a fictional repository, not clean-agent-code. The paths,
commands, team roles, identities, and budgets below are illustrative project choices.
They do not exist here and must not be executed or treated as authorization here.
Replace them with observed facts when using the [template](PROJECT_CONTEXT.md).

## Purpose, stack, and ownership

An internal task tracker lets team members manage tasks within their organization.
The web team owns a TypeScript/React frontend in `web/`; the API team owns a
Python/FastAPI service in `api/`. Platform maintainers own Terraform in `infra/`.
The service runs in Linux containers with PostgreSQL on AWS. Runtime and tool
versions are pinned in the consuming project's reviewed manifests and CI.

The API owns domain rules and persistence. The frontend calls its HTTP contract;
it does not implement authorization decisions. Infrastructure does not import app
code. A task belongs to one organization; an assignee must be a member of that
organization. Reference implementations are `api/tasks/service.py` for domain
rules, `api/tasks/routes.py` for boundaries, and `web/src/tasks/client.ts` for API
errors. Follow those patterns rather than adding new architectural layers.

## Contracts, identity, and data

Use structured API errors with a stable code and request ID, without raw exceptions.
Identity comes from the existing OIDC integration. The API checks organization and
resource permissions for every operation; login alone is insufficient. Validate
HTTP input in the API and external responses at the frontend adapter. Preserve
existing field names, optional-value semantics, and error codes. Add compatible API
fields first; coordinate breaking changes across both components.

Task descriptions and user details are private. Use synthetic fixtures and omit
payloads/tokens from logs. Trust boundaries include browsers, identity providers,
HTTP payloads, and database records. Follow project retention rules in deployment
configuration rather than copying production data for testing.

## Protected paths

The maintainers classify `env/`, `.env*`, `infra/private/`, `infra/*.tfvars`, local
Terraform state, saved plans, and `deploy/values-private.yaml` as protected. Exclude
them from agent searches, indexing, loaders, backups, and delegated context unless
specific access is approved. `deploy/values-example.yaml` is a maintainer-reviewed
synthetic example; this classification does not extend to similarly named files.

Classify any new values file through its owner before inspection. No credentials,
account IDs, tokens, or actual private values belong in this context document.

## Verification commands

These fictional package scripts and tool configurations are assumed to have been
reviewed in the example repository. They are not provided by clean-agent-code.

| Component and directory | Commands | Preconditions and scope |
| --- | --- | --- |
| Frontend: `web/` | `npm run format:check`, `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` | Existing scripts use check-only formatting, strict types, synthetic fixtures, and no protected loaders. Build writes generated output only to an approved test workspace. |
| API: `api/` | `ruff format --check .`, `ruff check .`, `mypy .`, `pytest` | Existing pinned tools; configured excludes avoid protected paths. Tests use isolated PostgreSQL with synthetic data; no production credentials. |
| Infrastructure: `infra/` | `terraform fmt -check main.tf` | Explicit reviewed source file only; do not expand the command to protected variable files. |
| Cross-component contracts | `npm run test:contract` from `web/` | Isolated API test instance; verifies response/error compatibility rather than mocking the contract away. |

API integration checks are Not run if isolated PostgreSQL is unavailable. Report
that limitation rather than substituting a successful unit suite. Security tooling
uses the project's configured dependency and secret scanners on approved paths with
redacted output; do not inspect excluded files to satisfy a scan.

## CLI scope, backups, and approvals

Cloud operations default to unapproved. Platform maintainers supply the exact test
account/role, region, backend, and workspace before any cloud query or Terraform
initialization/plan. Planning requires approved state and variable access; applying,
changing IAM, publishing, and production operations each need explicit scope approval.
The approved test target never implies approval for another account or environment.

Auto mode is established only by user confirmation or trusted session settings.
Before an authorized destructive file operation, use the owner's configured isolated
backup location; stop if none is configured. Verify recovery copies and record restore
steps under the [backup procedure](../standards/SECURITY.md#auto-mode-backups-and-cleanup).
Retain them and prompt for cleanup at handoff; deletion needs approval of exact paths.
The project owner approves cleanup. Git history is insufficient for uncommitted data.

## Operations, performance, and limitations

For this fictional project, the task-list endpoint has a p95 latency budget of 300 ms
at 50 concurrent users on the documented synthetic staging dataset. Measure changes
affecting that path; do not optimize unrelated code speculatively. Emit request IDs,
latency, and authorization-failure metrics without task payloads. Platform owns
backup restoration rehearsals and deployment recovery.

Maintain the current web/API contract during rolling deployment and review database
migration compatibility. Load tests do not establish production capacity beyond the
specified workload. No architectural exceptions are currently approved. Revisit
these facts when the stack, contract, or operating requirements change.
