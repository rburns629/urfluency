# Project context template

Complete applicable fields only. Mark unknowns and assign follow-up ownership; omit
irrelevant fields. Never include credentials or sensitive values. For existing
projects derive conventions from representative source files and established tools,
recording safe reference paths. For new projects state chosen conventions and why.
Record the completed context's location in the project's agent instructions.

| Area | Project facts, safe references, owners, or commands |
| --- | --- |
| Project purpose | Users, outcomes, and scope |
| Languages | Versions and supported language features |
| Frameworks | Versions and existing patterns |
| Runtime environment | OS, runtime, resource constraints |
| Deployment targets | Environments and release responsibilities |
| Architecture | Boundaries and dependency direction |
| Module ownership | Modules and responsible teams |
| Domain terminology | Terms, units, invariants |
| Existing coding conventions | Representative files and conventions |
| Reference implementations | Safe source paths and reasons to follow them |
| Error handling | Error contracts, retries, cancellation |
| Authentication | Identity mechanisms and boundaries |
| Authorization | Resource/tenant policy and enforcement points |
| Data sensitivity | Classification, retention, approved handling; no values |
| Protected paths and value files | Exact paths/globs, classification authority, known non-sensitive examples, and loader/search exclusions; never secret values |
| Trust boundaries | External inputs and validation ownership |
| Type checking | Commands and configuration references |
| Formatting | Check-only command |
| Linting | Check-only command |
| Testing | Commands, prerequisites, safe environments |
| Security tooling | Approved scan scope and redaction |
| CLI operations | Approved tool versions, identities, account/tenant/project/subscription, regions, repositories, Terraform backend/workspace, permitted operations, safe inputs/output, and remote recovery |
| Auto-mode filesystem recovery | Trusted auto-mode signal; isolated backup location; ignore/tracking verification; restore procedure; cleanup approver (no automatic deletion) |
| Performance requirements | Measurable workload and resource budgets |
| Operational requirements | Availability, observability, recovery |
| Compatibility expectations | Supported versions and public contracts |
| Known limitations | Gaps, impact, owners, follow-up |
| Approved architectural exceptions | Rationale, scope, approver, review condition |

Document gate applicability and evidence using [quality gates](../standards/QUALITY_GATES.md).
Record path-specific filesystem approvals separately; context is not blanket permission
to read protected material or overwrite files. Exceptions cannot silently weaken
security, correctness, data integrity, or authorization. Review context when contracts,
requirements, or tooling change.

See the [completed fictional example](PROJECT_CONTEXT_EXAMPLE.md) for one way to map
these fields to a stack. Its commands, identities, and paths are illustrative, not
configuration or authorization for this repository.
