---
title: GitHub-Compatible Issue System First Principles
sop:
  - Before implementing issue-core behavior, read this page and keep Karp as an adapter instead of the owner of issue truth.
  - Treat GitHub provider behavior as research-gated until source-backed provider details have been reviewed.
directives:
  - Complete the whole tracked feature scope; do not stop after the nearest local milestone.
  - Keep command and JSON contracts stable across direct CLI, future daemon, GitHub CLI wrapper, Karp, and MCP adapters.
---

# GitHub-Compatible Issue System First Principles

## Plain Goal

Build a standalone issue system that feels like GitHub Issues to humans and
coding agents, while remaining local-first and reusable outside any one UI.
Karp is a client and dispatch surface. It does not own issue truth.

See `docs/issue-core-contracts.md` for the concrete package, adapter, context,
and dispatch contract split used by the current implementation.

## Background

Karp needs a lightweight way to capture requirements and dispatch them into
terminal work. That need should not create a Karp-private task model. The
durable value is a GitHub-compatible issue core that can serve local issues,
GitHub-backed issues, agent context packets, Karp dispatch, and future adapters.

## First Principles

1. Issue truth comes before UI.
   The system must define and preserve issues, comments, labels, status,
   timeline events, provider identity, and dispatch metadata before any adapter
   is treated as authoritative.

2. GitHub-compatible protocol comes before custom workflow language.
   Commands, fields, and JSON should resemble GitHub Issues and `gh issue`
   semantics where practical. This lets humans and coding agents reuse existing
   expectations instead of learning a private task dialect.

3. The standalone `issues` CLI is the first public contract.
   A GitHub CLI wrapper can exist later, but wrapper packaging is not the core
   premise. The same core must also be callable by Karp, MCP, and future local
   service modes.

4. Local durability comes before remote ambition.
   SQLite should be the authoritative local store. Append-only event or outbox
   records should preserve operation history and make tests, export, provider
   bridges, and failure recovery deterministic.

5. Daemon mode must be transparent.
   Users and agents should see the same commands and JSON whether operations
   run in the command process or through a future `issuesd` process.

6. Adapters must not take ownership of truth.
   Karp, GitHub CLI wrappers, MCP servers, and daemon clients should call the
   same service boundary. None of them should invent a separate issue model.

7. Complete means complete.
   Implementation can be sequenced from core outward, but closeout requires the
   full tracked feature scope: domain, storage, CLI, GitHub provider path,
   context rendering, Karp contract, documentation, tests, and commit-ready
   evidence.

## Requirements

- Implement the project in Go.
- Define GitHub-shaped issue, comment, label, reaction, timeline/event, and PR
  link structures without turning them into Karp-specific task blobs.
- Provide provider interfaces for local and GitHub-backed issues.
- Implement local CRUD and state changes over durable SQLite storage.
- Preserve deterministic export or fixture output for tests and provider
  bridges.
- Expose `issues` commands for create, list, view, update, comment, close,
  reopen, and context rendering.
- Keep JSON output stable enough for agents and future adapters.
- Render prompt-friendly issue context with identity, metadata, body, comments,
  links, dispatch metadata, truncation, and untrusted-content boundaries.
- Define Karp dispatch metadata for target group, terminal reuse or creation,
  selected runtime when creating a new terminal, timestamp, and outcome.
- Preserve an existing terminal's runtime identity when dispatching into it.
- Implement GitHub provider behavior only after source-backed provider research
  confirms operation, auth, rate-limit, idempotency, and PR-linking choices.

## Non-Goals

- Do not build a full work center or global project board in the first release.
- Do not build background bidirectional GitHub sync or conflict resolution in
  the first release.
- Do not add Linear, Jira, squad, profile, or autopilot providers in the first
  release.
- Do not make Karp UI or any adapter the source of truth.
- Do not let GitHub CLI extension packaging determine the internal model.

## Execution Order

1. Scaffold the Go module, domain model, service boundary, provider interfaces,
   and command contract.
2. Implement durable local storage and local issue operations.
3. Expose the standalone `issues` CLI and stable JSON output.
4. Add prompt-friendly context rendering.
5. Define Karp dispatch and client contracts.
6. Refresh GitHub provider research and then implement explicit GitHub
   operations with mockable and dry-run coverage.
7. Finish documentation, tests, verification fixtures, and commit preparation.

This order is a construction sequence, not a reduced scope.

## Review And Recovery Rules

- At every implementation step, review whether the change preserves
  GitHub-compatible issue truth and keeps adapters thin.
- Use bounded reviewer or researcher sub-processes for provider behavior,
  command contract review, context packet review, and final quality checks when
  they can materially improve correctness.
- After context compaction or loop reentry, first reload the shared knowledge
  guidebook, then this page, then the feature tracker state, then current git
  status.
- Resume from the full feature goal, not only the most recent local task.
- Stop only when all tracked tasks are complete and the remaining work is human
  confirmation, credentialed live testing, or merge approval.
