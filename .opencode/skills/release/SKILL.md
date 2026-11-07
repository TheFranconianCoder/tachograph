---
name: Release
description: Cut and publish a GitHub release for tachograph — preflight the branch, cross-build the binaries with the version stamped in, write the release notes, and create the tag plus release with gh.
---

# Release tachograph

Publish a new GitHub release from `main`. The five binaries are built locally and uploaded;
**nothing is committed and no tag is moved**.

## Preconditions

- `mise install` has run in the repository (Go, Node, pnpm, Task, GitHub CLI).
- `gh auth login` has been done for `github.com`.
- The working tree is clean and `main` is level with `origin/main`.

## Steps

### 1. Inspect

From the repository root:

```bash
.opencode/skills/release/scripts/preflight.sh
.opencode/skills/release/scripts/changes.sh
```

`preflight.sh` reports branch, tree, authentication and the last tag; it exits non-zero and names
what is missing. `changes.sh` prints the commit range since the last tag and the commit subjects.

Before publishing, run the checks the release script does not run:

```bash
task test
task web:check
```

If `preflight.sh` fails, fix the cause (push, commit, `gh auth login`) or stop and tell the user —
never work around it.

### 2. Choose the version

Ask the user for `MAJOR.MINOR.PATCH` unless they already gave one. If they did not, propose one from
the commit subjects (Conventional Commits): a breaking change → major, any `feat` → minor, otherwise
patch. State the proposal; do not publish a version the user has not confirmed.

The first release is `0.1.0`.

### 3. Write the notes

Create the directory and write exactly two files:

- `.release/title.txt` — one line, the release title. No version prefix, no period, no Markdown.
- `.release/notes.md` — the release description in Markdown.

Rules:

1. **Title**: catchy but professional. No slang, no exclamation marks, no exaggeration.
2. **Description**: group the changes into categories — `Features`, `Fixes`, `Refactoring`,
   `Performance`, `Documentation` — and keep only the categories that apply. A first release may
   also carry a short **Known limitations** section.
3. **Content only.** No diff links, no commit hashes, no "see the full changelog", no thank-you filler.
4. **Do not invent changes.** Every bullet must come from the commit subjects you read in step 1.
5. **English.** All repository artifacts are English.
6. **User-facing.** Say what changed for the reader of the app, not which package was touched when
   the two differ.

Show the title and the notes to the user before publishing when the user asked to review them; a
plain "release it" authorizes step 4.

### 4. Publish

```bash
.opencode/skills/release/scripts/release.sh 0.1.0
```

The script re-runs the preflight, builds the PWA, cross-builds

- `dist/tachograph-linux-amd64`
- `dist/tachograph-linux-arm64`
- `dist/tachograph-darwin-amd64`
- `dist/tachograph-darwin-arm64`
- `dist/tachograph-windows-amd64.exe`

plus `dist/SHA256SUMS`, stamps `v0.1.0`, the commit and the build time into `internal/version` via
`-ldflags`, verifies the binary reports the tag, and runs `gh release create v0.1.0` with the tag
pointing at `HEAD`. On success it deletes `.release/` and mirrors the tag locally; on failure it
keeps the notes so the same version can be retried.

There are **no GitHub workflows**: the release is built and published from the developer machine.

### 5. Verify

```bash
gh release view v0.1.0
```

Report the release URL and the asset list to the user.

## Rules

- **Never commit or push.** A release tags the commit that is already on `origin/main`; if the work
  is not committed yet, say so and stop.
- **Never delete, move or re-create an existing tag.** The preflight refuses a tag that exists.
- **Never publish from a dirty tree or a feature branch.**
- If the user only wants the notes, stop after step 3.
