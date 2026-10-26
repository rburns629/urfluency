# Independent engineering audit

Evaluate actual behavior and evidence against the [standards](standards/README.md)
and the consuming project's context. Following instructions is not proof of quality.
Apply [agent access safeguards](AGENTS.md#mandatory-filesystem-safety) throughout.
An audit authorizes observation, not remediation, execution of untrusted code, secret
inspection, or production access.

## Method

1. Define scope, revision or snapshot, languages, critical assets, trust boundaries,
   applicable standards, and known access limitations.
2. Read permitted documentation, representative source and configuration, and CI.
   Trace important success and failure paths across actual module boundaries.
3. Evaluate architecture, security, data contracts, testing, maintenance burden,
   [consistency and drift](#consistency-and-drift), compatibility, migrations, and
   documented operational/performance requirements.
4. Run only safe, authorized checks after reviewing what commands execute and load.
   Record exact commands, environment, outcomes, and unavailable infrastructure.
5. Separate confirmed defects, suspected risks, and improvements. Seek counterevidence
   and distinguish intentional conventions from demonstrable defects.
6. When a design choice could be intentional, ask the owner for the rationale before
   scoring it as a defect, and record the answer beside the finding. Owner rationale
   can reclassify a design decision; it does not close a defect in how that decision
   was implemented.

## Consistency and drift

Measure drift against the conventions and reference implementations recorded in
[project context](templates/PROJECT_CONTEXT.md). Without a baseline, report the
variants observed rather than declaring one correct. Look for:

- Diverged copies: near-duplicate code whose fixes or behavior differ between copies.
- Convention conformance by module and period: naming, error handling, data access,
  concurrency, logging, and test layout.
- Layering violations and when they appeared.
- Several implementations of one concept, such as pagination, retries, date
  handling, or currency conversion, that behave differently.

Use clone detection, linters, dependency rules, pattern searches, and semantic
review as available, and verify semantic matches manually. When using history,
exclude formatting-only revisions, follow moves and copies, and report by module and
time period rather than by individual contributor.

Style variation alone is not a defect. Report drift with consequences, such as a
fix applied to only one copy, inconsistent behavior, or higher change cost, as
findings; report the rest as observations. Recommend preventing new drift, for
example by failing changes that add convention violations, rather than rewriting
existing code wholesale.

## Report

For each finding record location, evidence, violated contract or standard, impact,
reproduction when safe, confidence, and a focused corrective recommendation.
Prioritize findings by demonstrated impact and likelihood, independently of category
scores. Use the rubric below to explain numerical ratings; never invent a grade
without evidence. Do not expose credentials or sensitive payloads in reproductions.
Include strengths, exceptions, owner-confirmed design decisions with their rationale,
coverage gaps, and the limits of sampled inspection.
Report applicable gates as Passed, Failed, Not run, or Not applicable with reasons
as required by [quality gates](standards/QUALITY_GATES.md).

A clean automated scan is not a clean audit. Do not certify uninspected code or
infer production readiness from absent findings. Propose remediation separately;
obtain required path-specific approval before modifying existing files.

## Numerical scoring rubric

Include a Markdown scorecard in each audit report. Rate the assessed existing code
using integer scores from 0 to 100, where higher means stronger demonstrated quality.
These are review judgments anchored to evidence, not measured probabilities or a
security certification. State the scope, revision, rubric version (2), and audit
limitations before the table. Use the same scope and rubric for later comparisons.

| Score | Meaning within the assessed scope | Required basis |
| --- | --- | --- |
| 0 | Critical defect | Confirmed defect with critical consequences, such as an exploitable authorization bypass or destructive data loss. |
| 1–39 | Major systemic weaknesses | Confirmed high-impact failures across representative paths, with fundamental controls or contracts absent or ineffective. |
| 40–59 | Significant weaknesses | Confirmed material defects or missing required controls; substantial corrective work is needed in the assessed area. |
| 60–79 | Mixed quality | Intended behavior is supported by evidence, but confirmed moderate defects or meaningful maintenance weaknesses remain. |
| 80–94 | Strong with minor issues | Relevant contracts and controls are supported by evidence; remaining confirmed issues have limited impact. |
| 95–100 | Strong in assessed scope | Applicable criteria are supported by review and verification, with no material unresolved defects or verification gaps in that scope. |
| Unassessed | Insufficient evidence | Applicable area could not be inspected or verified enough to support a rating; explain missing access, checks, or evidence. |
| Not applicable | Outside actual requirements | Explain why the category does not apply; do not use this for blocked checks. |

Select the applicable band first. Use its lower bound by default; justify a higher
integer with additional strengths and verification evidence within the same scope.
These bands are reporting conventions, not statistical measurements or universal
pass thresholds. A score of 100 means the top-band criteria are fully supported
within the stated scope, not that the whole repository is defect-free. Scores from
different rubric versions are not directly comparable; re-score the same evidence
under the current rubric when a comparison is needed.

Use the lowest matching band supported by confirmed findings in a category. A
confirmed critical defect makes its affected category 0; do not offset it with good
style or unrelated passing tests. Suspected risks remain identified as suspected,
with confidence and follow-up, rather than being presented as confirmed defects.
If evidence proves a defect but other checks are unavailable, score the demonstrated
weakness and state the incomplete coverage. If evidence cannot support any band,
use Unassessed, not zero. A tool failure alone is not proof of a code defect.

Do not calculate a repository-wide average or percentage: it can conceal a critical
weakness and imply coverage of uninspected code. Numerical scores do not override
[quality gates](standards/QUALITY_GATES.md), readiness blockers, or approval rules.
Do not claim an improved score for a suggested fix until it is implemented and
re-audited. Explain reviewer disagreements with evidence instead of false precision.

## Scorecard format

Use these categories, marking inapplicable or unassessed areas explicitly. Split a
category by component where different coverage or findings would otherwise be hidden.
Replace placeholders with actual evidence; add categories only for concrete needs.

| Category | Score (0–100) or status | Scope and evidence | Findings / rationale | Confidence and gaps |
| --- | --- | --- | --- | --- |
| Correctness and data integrity | Unassessed | Not yet inspected | Pending behavioral and invariant review | State missing evidence |
| Security | Unassessed | Not yet inspected | Pending trust-boundary and authorization review | State missing evidence |
| Architecture | Unassessed | Not yet inspected | Pending boundaries and dependency review | State missing evidence |
| Type safety and data contracts | Unassessed | Not yet inspected | Pending boundary/schema review | State missing evidence |
| Testing and verification | Unassessed | Not yet inspected | Pending test quality and execution review | State missing evidence |
| Readability and maintainability | Unassessed | Not yet inspected | Pending representative source review and drift assessment | State missing evidence |
| Performance and operations | Unassessed | Not yet inspected | Pending documented requirements and measurements | State missing evidence |
| Compatibility and migration safety | Unassessed | Not yet inspected | Pending contract and persistence-change review | State missing evidence |

Follow the scorecard with a findings table and detailed examples. Give each finding
a stable identifier. Severity describes impact/likelihood; confidence describes
strength of evidence. Explain both instead of deriving severity from a category score.

| ID | Severity and impact | Confidence | Location and evidence | Recommended change | Verification needed |
| --- | --- | --- | --- | --- | --- |
| F-01 | State affected users/data and likely consequences | High / Medium / Low, with reason | Safe path/line reference, violated contract, reproduction or review evidence | Focused remedy | Regression, negative-path, integration, or manual checks |

Include an independent checks table using Passed, Failed, Not run, and Not applicable.
Record command/procedure, environment, result evidence, and reasons for failed or
unexecuted checks. A category score is never a substitute for this execution record.

## Example report excerpt

Everything below is fictional and illustrates report structure. The paths, scores,
and findings are not an audit of clean-agent-code. Example snippets are proposed changes,
not applied fixes or proof that verification passed.

Scope: a task-read endpoint and its JSON adapter at fictional revision `example-1`;
rubric version 2. Source review confirmed missing resource authorization and unchecked
external data. No tests were executed. Other modules and deployment were not audited.

| Category | Score (0–100) or status | Scope and evidence | Findings / rationale | Confidence and gaps |
| --- | --- | --- | --- | --- |
| Security | 0 | Task-read endpoint source review and synthetic reproduction | F-01: returns another user's task without a resource permission check; confirmed critical defect | High for this endpoint; other endpoints unassessed |
| Type safety and data contracts | 60 | JSON adapter source review | F-02: external JSON is cast to a trusted model without validation; one confirmed moderate defect, so the 60–79 band's lower bound | High from adapter review; other boundaries unassessed |
| Testing and verification | Unassessed | No test suite inspected or executed | No basis for a quality score | Test quality and execution unknown |
| Performance and operations | Unassessed | No workload or operational evidence available | No performance claim | Requirements and measurements unavailable |

A complete report includes every scorecard category; this excerpt shows selected rows.
The synthetic reproduction in F-01 is a manual exercise, not an executed automated test.

| ID | Severity and impact | Confidence | Location and evidence | Recommended change | Verification needed |
| --- | --- | --- | --- | --- | --- |
| F-01 | Critical: cross-user disclosure of private task data via an ordinary authenticated request | High: reviewed control flow and manually reproduced with synthetic identities | `api/tasks.py:42`; returns a task by ID without checking resource permission | Enforce server-side resource authorization before returning data | Own/other-user, cross-tenant, and policy-error cases |
| F-02 | Medium: malformed upstream JSON can enter the domain model | High: explicit unchecked assertion in the adapter | `web/task.ts:12`; asserts a type without runtime validation | Validate unknown input at the boundary using the established schema mechanism | Valid, null, missing, wrong-type, and invalid-domain-value cases |

### F-01: authorization example

Illustrative Python before:

```python
def read_task(user, task_id):
    return repository.get_task(task_id)
```

Proposed shape after, using the project's existing repository and policy mechanisms:

```python
def read_task(user, task_id):
    task = repository.get_task(task_id)
    authorization.require(user, "task:read", task)
    return task
```

This assumes `require` denies unauthorized access and fails closed on policy errors.
Adapt missing-resource and denial handling to the project's safe error contract;
verify tenant ownership and avoid leaking resource existence or private payloads.
Do not invent a parallel authorization system just to match this example.

### F-02: boundary-validation example

Illustrative TypeScript before:

```typescript
const task = (await response.json()) as Task;
```

Proposed shape after, using an existing runtime schema for the domain contract:

```typescript
const raw: unknown = await response.json();
const task: Task = taskSchema.parse(raw);
```

`taskSchema` must enforce actual field and domain constraints, not merely assert a
type. Handle validation failure through the established error contract without
logging the raw response. The example does not require installing a schema library.

### Example verification record and next actions

| Check | Status | Evidence or reason | Follow-up |
| --- | --- | --- | --- |
| F-01 source review and synthetic manual reproduction | Passed | Missing permission check confirmed in the fictional endpoint | Proposed correction still requires verification |
| Authorization regression tests | Not run | No test execution environment provided | Verify allowed, denied, cross-tenant, and policy-error behavior |
| Adapter boundary tests | Not run | No test execution environment provided | Exercise malformed and valid payloads |
| Deployment/performance assessment | Not run | Environment and workload unavailable | Assess applicable requirements before readiness claims |

Prioritize F-01 before lower-impact improvements. Obtain path-specific approval
before applying either proposal. Retain current scores until the implemented changes
and required checks have been reviewed; unresolved findings and unexecuted gates
prevent an unsupported production-readiness claim.
