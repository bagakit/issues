# SOP

This page is generated from optional frontmatter in shared knowledge pages.
Do not hand-edit it directly.

## Update Rules
- Refresh this page with `sh "$BAGAKIT_LIVING_KNOWLEDGE_SKILL_DIR/scripts/bagakit-living-knowledge.sh" index --root .`.
- Add or update route guidance in page frontmatter under `sop:` and optional `directives:`.
- Keep items short, concrete, and scoped to the source page.

## Sources
- shared root: `docs`
- system root: `docs`

## SOP Items

### GitHub-Compatible Issue System First Principles
Source: `docs/issue-system-first-principles.md`
- Before implementing issue-core behavior, read this page and keep Karp as an adapter instead of the owner of issue truth.
- Treat GitHub provider behavior as research-gated until source-backed provider details have been reviewed.
- Directives:
  - Complete the whole tracked feature scope; do not stop after the nearest local milestone.
  - Keep command and JSON contracts stable across direct CLI, future daemon, GitHub CLI wrapper, Karp, and MCP adapters.

### Maintaining Reusable Items
Source: `docs/norms-maintaining-reusable-items.md`
- When one pattern, checklist, or example becomes a stable project default, add or update the matching reusable-items catalog entry in the same change.
- When reusable-items route guidance changes, refresh `docs/must-sop.md` by running `sh "$BAGAKIT_LIVING_KNOWLEDGE_SKILL_DIR/scripts/bagakit-living-knowledge.sh" index --root .`.

### Reusable Items - Knowledge
Source: `docs/notes-reusable-items-knowledge.md`
- Update this catalog when one note, index, or query pattern becomes worth reusing across tasks.
- Keep source-of-truth links current and remove duplicate entries.
