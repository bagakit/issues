# Issue Core Contracts

This repository builds a standalone issue core first, then layers adapters on
top of it. Karp is one client of that core. It does not own issue truth.

## Package and Surface Split

- `pkg/issuecore`
  - GitHub-shaped issue, comment, timeline, PR-link, context, and dispatch
    types plus the dispatch gateway and recorder contracts used by the shared
    service boundary.
  - Stable service boundary that works for the direct CLI now and future daemon,
    wrapper, or MCP surfaces later.
- `internal/providers/localfile`
  - Default local logical-file issue truth for the standalone issue system.
- `internal/providers/local`
  - Legacy SQLite-backed provider retained for compatibility tests and future
    migration/import work; it is not the default local authority.
- `internal/providers/github`
  - Explicit user-triggered GitHub issue operations behind the same provider
    boundary.
- `internal/app`
  - CLI argument parsing and JSON/text rendering over `pkg/issuecore`.
- `cmd/issues`
  - Thin entrypoint for the standalone `issues` binary.

## Stable v1 Contracts

- The standalone `issues` CLI is the first public contract.
- `issues list|view|create|edit|comment|close|reopen` remain provider-agnostic
  command surfaces over the same core service.
- `issues context <issue>` renders a stable issue-context packet for coding
  agents and future dispatch adapters.
- Context output keeps GitHub-like issue identity and metadata while marking
  issue title, issue body, comment text, timeline payloads, and timeline
  payload previews as untrusted user content.
- `issues record-dispatch <issue>` records externally completed dispatch facts
  back into issue truth, including target group, terminal reuse or creation,
  preserved existing-terminal runtime identity, selected new-terminal runtime,
  outcome, timestamp, and the context packet link used for dispatch.
- Dispatch types in `pkg/issuecore` define target groups with optional existing
  terminal candidates, terminal reuse vs new terminal creation, new-terminal
  runtime selection, dispatch outcome, and the link to the issue context packet
  used for the dispatch.
- `issuecore.Service` also exposes provider-agnostic dispatch target listing
  and dispatch submission. Dispatch submission records returned dispatch facts
  back into issue truth through the provider recorder path when the provider
  supports it. If delivery succeeds but recorder persistence fails,
  `SubmitDispatch` returns the delivered `DispatchResult` together with a
  `PostDeliveryPersistenceError`, and callers must inspect both `result` and
  `error`.

## Adapter Responsibilities

- `issues`
  - Human and script entrypoint for the standalone core.
- Future `gh-issues`
  - Packaging or wrapper surface over the same core contract, not a new domain.
- Future `issuesd`
  - Optional daemon transport for the same commands and JSON shapes.
- Karp
  - Lists issues, lists dispatch targets, chooses existing vs new terminal, and
    submits dispatch requests through the core contract.
  - Reusing an existing terminal must preserve that terminal's current runtime
    identity.
  - Creating a new terminal may include selected coding-agent or runtime
    metadata, but Karp still consumes issue truth from `issuecore`.
- Future MCP
  - Exposes the same issue and context contracts to external agent workflows.

## Provider Boundaries

- Local logical files are authoritative for local issues. SQLite or another DB
  may be retained only as a rebuildable cache, index, or migration input.
- GitHub provider behavior stays explicit and user-triggered. No background
  sync, board semantics, or cross-provider merge layer is part of v1.
- Provider implementations may enrich GitHub-shaped issue data, but they do not
  change the shared issue model or dispatch contract.

## Configuration

- Default local storage:
  - `--local-root <path>`
  - `--store <path>` and `--local-store <path>` as aliases
  - `ISSUES_LOCAL_ROOT`, then `ISSUES_LOCAL_STORE`, then deprecated
    `ISSUES_LOCAL_DB` as environment fallback order
- Deprecated compatibility:
  - `--db` and `--local-db` are accepted as aliases for the local logical file
    root. They no longer select SQLite authority.
- GitHub provider:
  - `--github-token`, `ISSUES_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`
  - `--github-base-url` / `--github-api-url` or `ISSUES_GITHUB_API_URL`
- DB/DSN/profile configuration:
  - v1 has no authoritative DB/DSN/profile-backed issue store.
  - A future DB, DSN, profile, KV, or object backend must implement the
    `pkg/issuecore.LogicalStore` contract or remain a rebuildable cache/index
    or migration input. It must not create a second issue truth.

## Logical File Protocol

- Store manifest:
  - `issues/manifest.json`
  - schema version: `issues.store.manifest.v1`
  - protocol version: `issues.store.protocol.v1`
  - root prefix: `issues`
  - issue id format: lower-case UUIDv7
  - shard rule: prefix shards from issue id with widths `2,2`
- Canonical issue directory:
  - `issues/by-id/<first2>/<next2>/<issue-id>/`
- Canonical records under each issue directory:
  - `issue.md`
  - `comments/000001.md`, `comments/000002.md`, ...
  - `timeline/000001.json`, `timeline/000002.json`, ...
  - `providers/<provider>.json`
  - `pull-requests.json`
  - `dispatch/000001.json`, `dispatch/000002.json`, ...
  - `extensions/<namespace>.json`
- Markdown records use typed frontmatter plus user body text. JSON records use
  strict schema versions and reject unknown fields where schema decoding applies.
- Provider identity records map local issue ids to provider-facing repository,
  number, URL, node id, or external id. Provider identity is lookup metadata,
  not the canonical path.

## Storage, Index, And Migration

- `pkg/issuecore.LogicalStore` is the storage adapter boundary:
  - `Read`
  - `Write`
  - `List`
  - create-only writes and compare-version preconditions
- `internal/providers/localfile` is the default filesystem implementation of
  that logical store for local issues.
- `IssueIndex` is derived state. It reads the manifest plus canonical records,
  computes a source fingerprint, and supports list, filters, sort, text search,
  and provider identity lookup.
- `IssueIndexCache` is optional. Missing, stale, corrupt, invalid, or failed
  cache writes must rebuild from canonical logical files and must not lose issue
  truth.
- Legacy SQLite data is not default authority. Migration or import work should
  read legacy rows as input and write canonical logical records before the
  logical files become the issue truth.
- Local provider-facing pagination keeps issue-number cursor semantics even
  when the derived index uses its own internal offset token.

## Support Matrix

| Surface | v1 status | Notes |
| --- | --- | --- |
| Local logical files | Supported default | Create, list, view, update, comment, close, reopen, context, provider identity, PR links, timeline, comments, and dispatch records round-trip through canonical files. |
| Legacy SQLite provider | Compatibility only | Retained for tests and future migration/import work. Not selected by the default CLI and not authoritative local truth. |
| GitHub REST provider | Explicit remote provider | Supports normal issue list, get, create, update, comment, close, and reopen with mocked coverage and credentialed live testing left to users. |
| GitHub repository text search | Unsupported capability | Returns `unsupported_capability` with `field=search` and `behavior=repository_issue_text_search`. |
| GitHub milestone title lookup | Unsupported capability | Numeric milestone ids are accepted; title lookup returns `unsupported_capability`. |
| GitHub node/external id lookup | Unsupported capability | Use repository plus numeric issue number or an issue URL. |
| Direct GitHub `state_reason` update | Unsupported capability | Use close or reopen state transitions instead. |
| Dispatch submission gateway | Service contract | `issuecore.Service` supports gateway-backed submit and post-delivery persistence. The default CLI records already-completed dispatch facts through `record-dispatch`. |
| Karp, MCP, daemon, `gh` wrapper | Future clients | They must call the same service and JSON contracts rather than owning issue truth. |

## Executable Examples

```bash
ISSUES_ROOT="$(mktemp -d)"

go run ./cmd/issues --local-root "$ISSUES_ROOT" create \
  --repository bagakit/issues \
  --title "Example issue" \
  --body "Body text" \
  --labels bug,local \
  --json

go run ./cmd/issues --local-root "$ISSUES_ROOT" list --state all --json

go run ./cmd/issues --local-root "$ISSUES_ROOT" context \
  --body-max-runes 1000 \
  --comment-max-runes 500 \
  1

go run ./cmd/issues --local-root "$ISSUES_ROOT" record-dispatch \
  --target-group grp-1 \
  --target-group-name "Build" \
  --terminal-mode reuse_existing \
  --terminal-id term-7 \
  --runtime-identity codex/gpt-5 \
  --outcome delivered \
  --context-format prompt \
  --json \
  1
```

## Unsupported Capability Errors

Known-but-unsupported GitHub-compatible fields, flags, interfaces, or behaviors
must return a structured JSON error instead of a generic failure or silent
no-op. The CLI error code is `unsupported_capability` and includes:

- `interface`
- `flag`, `field`, or `behavior` when applicable
- `compatibility_level`
- `reason`
- `suggested_alternative` when one is known

Example shape:

```json
{
  "error": {
    "code": "unsupported_capability",
    "provider": "github",
    "operation": "list",
    "unsupported_capability": {
      "interface": "github_rest",
      "field": "search",
      "behavior": "repository_issue_text_search",
      "compatibility_level": "unsupported",
      "reason": "repository issue list does not provide this provider's GitHub-compatible text search behavior",
      "suggested_alternative": "omit search and filter returned issues locally, or use a future GitHub search-backed provider path"
    }
  }
}
```

## GitHub Provider Source Notes

- The provider follows GitHub REST issue endpoints for repository issue list,
  create, get, update, comment, close, and reopen behavior:
  <https://docs.github.com/en/rest/issues/issues>.
- Issue comments use the repository issue comments endpoint and `per_page`
  pagination:
  <https://docs.github.com/en/rest/issues/comments>.
- Timeline and linked pull request context use the issue timeline endpoint and
  `Link` pagination:
  <https://docs.github.com/en/rest/issues/timeline>.
- Requests send `Authorization: Bearer`, `Accept: application/vnd.github+json`,
  `X-GitHub-Api-Version`, and a stable `User-Agent`, matching GitHub REST API
  versioning and troubleshooting guidance:
  <https://docs.github.com/en/rest/overview/api-versions> and
  <https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api>.

## v1 Non-Goals

- No Karp-private issue or task schema.
- No background GitHub sync or conflict resolution.
- No daemon-only behavior branch. CLI, future daemon, wrappers, and adapters
  should share the same contract.
- No Linear, Jira, or board-style work-center expansion in this repository's
  first release.

## Follow-On Surfaces

- `gh-issues` should remain a wrapper around the standalone package/service
  split.
- `issuesd` should keep command and JSON contracts stable while changing only
  process topology.
- MCP should consume the same context and dispatch types instead of inventing a
  separate packet shape.
