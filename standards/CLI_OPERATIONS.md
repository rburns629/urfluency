# CLI operations

## Scope and authority

Apply this policy to AWS CLI, Azure CLI, gcloud, Terraform, Git, GitHub CLI, and other
local or remote command-line tools. Classify a command by its actual effects, inputs,
and outputs, not its name. A list/get/show command is not automatically safe, and a
successful dry run is not approval to execute a change.

Use [filesystem safeguards](../AGENTS.md#mandatory-filesystem-safety),
[security guidance](SECURITY.md), and project context together. Existing explicit
authorization remains valid for the same operation and targets. Auto mode does not
expand authorization. Neither available credentials nor broad IAM permissions grant
user approval. If scope, target, or effects are uncertain, stop the dependent action
and request the missing information; continue unrelated safe work where possible.

## Classify before executing

| Operation | Required handling |
| --- | --- |
| Scoped metadata read | May proceed within the task's authorized scope after target and output checks; exclude secrets, private payloads, and unapproved production access. |
| Sensitive read | Obtain explicit approval for the specific resource/path, purpose, and safe handling before retrieval, export, download, or display. |
| Local mutation | Obtain the operation/path approval required by AGENTS.md, protect existing work, and follow auto-mode backup rules when applicable. |
| Remote mutation or publication | Require explicit authorization identifying environment, resources/repository, operation, and intended scope; prepare a reviewable preview or diff where available. |
| Destructive, privilege-changing, or production operation | Require explicit approval of affected targets and consequences plus an appropriate recovery plan; identify irreversible effects before execution. |

Approval to develop code is not approval to deploy it, change IAM, publish artifacts,
merge a pull request, or delete infrastructure. Do not repeatedly request approval
already given for the same scope. Changed targets, widened permissions, replacement
resources, or materially changed plans require renewed approval.

## Preflight

Before execution, establish the relevant CLI version, trusted executable/wrapper,
working directory, identity, and exact target. Use narrow metadata queries rather
than dumping configuration or environment variables. Review aliases, extensions,
hooks, credential helpers, plugins, scripts, and shell expansion that may introduce
additional behavior. Do not install or upgrade tools or dependencies just to bypass
a missing prerequisite without authorization for those effects.

Confirm applicable account/role, tenant, subscription/project, region/location,
endpoint, environment, repository/remote/branch, and Terraform backend/workspace.
Use explicit supported per-command selectors rather than silently switching global
configuration. Recheck immediately before a mutation and after session/context
changes. Do not use display names alone where stable identifiers are available.
Never retry against another account, region, endpoint, or workspace merely to make
a command succeed. Treat custom endpoints and impersonation as trust-boundary changes.

Assess the whole invocation: pipes, redirects, substitutions, response files, scripts,
recursive options, and subprocesses. Quote arguments using the shell's rules; avoid
constructing executable shell strings from untrusted data. Bound resource sets and
review inventory before bulk actions. Do not use wildcards or all-project/all-repo
operations beyond approved scope.

## Credentials, configuration, and output

Use an approved CLI's configured authentication flow within authorized scope; let
the client handle credentials without extracting or displaying them. This permits
credential use for the approved operation, not independent inspection of credential
stores or token export. Do not read raw credential files, print access tokens, or
create long-lived keys for convenience. Prefer least-privilege, short-lived identity
where supported. Authentication/account switching and persistent configuration
changes must stay within explicit authorization.

Treat secret-manager payloads, connection strings, private logs, state, plans, and
configuration/value files as potentially sensitive. This includes `env/`, `.env*`,
`*.tfvars`, and tool-specific values files when their classification is unknown.
Identify safe inputs from project context; do not open unknown files to determine
whether they contain secrets. Require approval for implicit loading of protected
inputs as well as explicit reads. Scope approval to the CLI's use of those inputs;
it need not authorize displaying them.

Output filtering or redaction after retrieval does not authorize a sensitive read.
Avoid debug/trace modes, shell tracing, full environment dumps, and secret arguments
that can appear in history or process listings. Minimize requested fields and saved
output. Protect approved artifacts from source control, CI artifacts, public paths,
and broad access; follow approved retention and cleanup. Report unexpected exposure
without reproducing values. Do not bypass TLS verification or disable audit/security
controls to complete a command.

## Preview, recovery, and completion

Use supported plans, diffs, what-if operations, or dry runs when useful. Check their
permissions and side effects first; previews may execute code, query live systems,
read protected inputs, or write local artifacts. Where preview is unavailable,
prepare a bounded operation description and verification/recovery procedure.

For local files, follow [auto-mode backups](SECURITY.md#auto-mode-backups-and-cleanup).
For remote changes, identify rollback, snapshots, versioning, or another suitable
recovery mechanism and verify prerequisites. Local backups do not restore deleted
cloud data. If an operation cannot be reversed, explain that limitation and obtain
approval explicitly accepting it. Snapshot/export creation may itself read sensitive
data, incur costs, or mutate resources; obtain the relevant authorization first.

Do not use yes/force/quiet/auto-approve flags to bypass user approval. They may be used
for an already authorized bounded operation only after reviewing their tool-specific
effects; technical flags never supply authorization. Do not disable locks or safety
checks to make progress. Following a timeout or ambiguous error, inspect authorized
status before retrying a mutation; avoid duplicate resources or repeated side effects.

Verify the resulting state with narrow permitted queries. Report target identifiers,
operation, exit status, actual outcome, verification gaps, and retained recovery
artifacts without sensitive values. An exit code alone does not prove the intended
remote state. Prompt for backup cleanup at handoff under the existing procedure;
never automatically delete recovery artifacts, cloud snapshots, or superseded data.

## Tool-specific checks

| Tool | Identity and scope | Particular risks |
| --- | --- | --- |
| AWS CLI | Confirm caller account/role with scoped identity metadata; select approved profile and region explicitly. | Secret/parameter retrieval, object downloads, IAM changes, resource deletion, and sync deletion require effect-based classification. `--dry-run` exists only for supported operations; check the installed command's documentation. |
| Azure CLI | Confirm tenant/subscription using scoped account metadata; specify the approved subscription and resource scope. | Key Vault reads and credential listing can expose secrets. Deployments, role assignments, resource-group deletion, and persistent account changes need relevant approval. ARM what-if is a preview for supported deployments, not a general dry run for every command. |
| gcloud | Confirm active account, configuration, project, and relevant region/zone; use approved explicit selectors. | Secret-version access retrieves payloads. Token printing, service-account key creation, IAM changes, project deletion, and configuration switching need relevant approval. Quiet mode suppresses prompts; it does not grant permission. |
| Terraform | Confirm directory, backend, workspace, provider identities, variables, and approved credential context. | Review providers/modules and external programs before init/plan; initialization can download code and change local files or backend setup. Plans access state and providers; protect plan/state files and implicit variable inputs. |
| Git | Confirm worktree, branch, remote host/repository, and destination ref; inspect scoped status before changes. | Preserve staged, dirty, untracked, and ignored work. Hooks, filters, helpers, and submodules may execute code. Local inspection, fetch, pull, checkout, commit, and push have different effects. |
| GitHub CLI | Confirm host, account, owner/repository, and branch or PR target; use explicit repository selectors where supported. | API methods and arguments determine effects, including GraphQL mutations. PR creation/merge, comments, workflow dispatch, releases, visibility, permissions, and repository deletion require scope-specific authorization. |

For Terraform, planning approval is separate from apply/destroy approval. Review
replacement/deletion actions, provider changes, and the exact artifact or configuration
to be applied. Apply only the approved plan; regenerate and obtain renewed approval
if its effects change or target assumptions no longer hold. State import/removal/move,
state push, backend migration, workspace deletion, and force-unlock require explicit
scope and recovery review. Do not bypass state locks. A sensitive marker is not a
reason to treat saved plans or state as safe to inspect or publish.

For Git, obtain specific approval for discard/reset/clean operations, history rewrites,
branch/tag deletion, and force pushes. A dry-run clean is only an inventory preview.
Check remote/ref and preserve recoverability before an approved rewrite; prefer
lease-protected force pushing where appropriate, but it still requires approval.
Do not bundle unrelated work into a commit, amend another commit without approval,
or assume push authorization also permits a merge, release, or workflow dispatch.

## Verification and reference documentation

Review actual command arguments and targets, authorization records, sanitized results,
and recovery evidence. Where the optional evaluations are adopted, evaluate judgment
through the CLI scenario in `evaluations/security/scenarios.md`.
Technical enforcement requires sandbox, IAM, filesystem, and review controls beyond
these instructions. Consult installed-version documentation; these references explain
specific behaviors, not a universal safe-command allowlist:

- [AWS caller identity](https://docs.aws.amazon.com/cli/latest/reference/sts/get-caller-identity.html)
  and [AWS command help and dry-run support](https://docs.aws.amazon.com/cli/latest/userguide/cli-usage-help.html).
- [Azure account commands](https://learn.microsoft.com/en-us/cli/azure/account)
  and [ARM what-if](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/deploy-what-if).
- [gcloud configurations](https://docs.cloud.google.com/sdk/docs/configurations)
  and [secret-version access](https://docs.cloud.google.com/sdk/gcloud/reference/secrets/versions/access).
- [Terraform plans](https://developer.hashicorp.com/terraform/cli/commands/plan)
  and [sensitive data](https://developer.hashicorp.com/terraform/language/manage-sensitive-data).
- [Git push semantics](https://git-scm.com/docs/git-push)
  and [GitHub API invocation](https://cli.github.com/manual/gh_api).
