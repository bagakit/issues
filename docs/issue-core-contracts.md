# Issue Core Contracts

This repository builds a standalone issue core first, then layers adapters on
top of it. Karp is one client of that core. It does not own issue truth.

## Package and Surface Split

- `pkg/issuecore`
  - GitHub-shaped issue, comment, timeline, PR-link, context, and dispatch
    types.
  - Stable service boundary that works for the direct CLI now and future daemon,
    wrapper, or MCP surfaces later.
- `internal/providers/local`
  - Local SQLite-backed issue truth for the standalone issue system.
- `internal/providers/github`
  - Explicit user-triggered GitHub issue operations behind the same provider
    boundary.
- `internal/cli`
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
  issue body and comment text as untrusted user content.
- Dispatch types in `pkg/issuecore` define target groups, terminal reuse vs new
  terminal creation, new-terminal runtime selection, dispatch outcome, and the
  link to the issue context packet used for the dispatch.

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

- Local SQLite storage is authoritative for local issues.
- GitHub provider behavior stays explicit and user-triggered. No background
  sync, board semantics, or cross-provider merge layer is part of v1.
- Provider implementations may enrich GitHub-shaped issue data, but they do not
  change the shared issue model or dispatch contract.

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
