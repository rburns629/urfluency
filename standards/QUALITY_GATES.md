# Quality gates and readiness

Determine applicability from actual languages, frameworks, change scope, project
context, and risk. Do not mandate incompatible tooling. Written compliance and a
successful compile are insufficient evidence of production readiness.

## Required classification

| Level | Checks and applicability |
| --- | --- |
| Mandatory | Existing required formatting, linting, static analysis, type checking, build/compilation, and relevant unit tests where supported; architecture and documentation consistency review for every change. |
| Conditional | Integration and contract tests for changed boundaries; security, dependency vulnerability, and secret scans for relevant code/configuration/dependency changes; compatibility review for public contracts; migration safety for persisted data; concurrency and deployment checks where affected; performance verification for affected hard requirements. |
| Recommended | Additional exploratory, property-based, resilience, or broader end-to-end checks when they add confidence beyond required gates. |

A consuming project's established required scans or tests remain mandatory even if
listed as conditional here. A documentation-only repository can mark language
compilation inapplicable. Explain applicability rather than checking boxes blindly.
Secret scanners must use approved scope and redact findings; they are not permission
to inspect protected contents. Review tool behavior before execution.

## Evidence record

For every applicable gate record check name, classification, command or manual
procedure, scope/revision, environment, status, and relevant evidence:

- Passed: actually executed and satisfied the defined criterion.
- Failed: executed and did not satisfy it; include reason and unresolved impact.
- Not run: applicable but unexecuted; include reason and follow-up needed.
- Not applicable: explain why the check does not apply.

Report failed or unexecuted recommended checks too. Never relabel blocked checks as
inapplicable. Separate automated results from human review. Avoid exposing sensitive
logs. Readiness requires all mandatory and triggered conditional gates to pass, or
explicit owner acceptance of a documented exception with risk, owner, expiry/review
condition, and follow-up. Security and authorization boundaries cannot be silently
waived; do not claim unverified behavior is proven by exception approval.

## CLI change evidence

For changes performed through a CLI, include the classification, exact target,
authorization scope, reviewed preview where available, recovery approach, and narrow
post-operation verification required by [CLI operations](CLI_OPERATIONS.md). Report
unverified remote outcomes explicitly. A successful command or plan alone does not
prove a deployment or infrastructure change is correct.

## CI enforcement

Use the consuming repository's existing tools and reproducible versions. Map
applicable commands to CI jobs, make failures nonzero, and require relevant status
checks through hosting branch protection. Review workflow permissions, immutable
action pins, dependencies, and script side effects. Keep production credentials out
of untrusted changes. Document local commands, prerequisites, and exceptions.

For migrations verify compatibility with old/new application versions, rollback or
forward recovery, backups where needed, and safe rehearsal on approved test data.
For performance compare measured behavior to explicit requirements on representative
workloads. Review architectural consistency and semantic compatibility manually
where automation cannot determine intent.

This repository's validate workflow checks required documents, basic Markdown
hygiene, and local links only. It provides no application-security, semantic-quality,
external-link availability, or consuming-project enforcement guarantee.
