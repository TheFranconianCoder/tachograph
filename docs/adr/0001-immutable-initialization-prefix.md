# 0001 — Keep the initialization prefix out of compaction

**Status:** accepted

**Date:** 2026-11-19

## Context

A linked worktree's history branch starts with `base: history initialized` and a hidden source-copy
window whose end is recorded in `refs/tachograph/init/<worktree-dir>`. Compaction used to treat that
prefix like any other commit: it folded it into an `archive:` root and deleted the init ref.

When the dropped prefix consisted only of the empty base commit, the archive root had an empty tree.
`timelineLowerBound` then treated that root as an initialization marker and hid the leading
addition-only snapshots below it. A worktree that had been idle long enough could lose its visible
commits after compaction.

## Decision

- The prefix from the branch root through the init baseline is immutable. Retention never rewrites,
  drops, or folds it; `keepMinimum` and the age tiers apply to the visible snapshots only.
- A new `archive:` root is parented on the last protected commit, never on the emptied base root.
- The init ref is left untouched while its baseline is on the branch. Only a dangling ref is removed.
- `timelineLowerBound` runs the legacy addition-only fallback only below a `base:` root. An
  `archive:` root is a retained historical state, never an initialization marker.

## Consequences

- Every compacted worktree keeps an exact hidden baseline, so `WorktreeDetail` and the timeline keep
  a correct lower bound.
- Compaction can no longer empty a timeline that has visible changes, even for idle worktrees.
- The initialization commits stay ancestors of the visible branch. A physically separate branch
  would break `merge-base` ancestry and range diffs, so it is deliberately not used.
