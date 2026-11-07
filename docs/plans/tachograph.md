# Tachograph — product plan

**Status:** active

**Date:** 2026-11-05

**Scope:** product direction and milestone plan; M2, M3, the automatic lifecycle path, M4a, M4b,
M4c, M4d, M4e, M4f and M5 (recovery) are implemented. M6 (productization) is current:
auto-registration, worktree patterns, autostart and the v0.1.0 release tooling are done;
onboarding, native installers and the optional backup remain.

---

## 1. Product

**Tachograph — the local history recorder for every worktree.**

A tachograph records a journey continuously, without gaps, on a physical chart. Tachograph records the
states between Git commits: it watches every worktree, keeps the timeline local, and makes it
inspectable and restorable without turning the project repository into a museum of snapshot branches.

It also makes a new worktree usable. A fresh worktree is often worthless until local, untracked files
exist — `.env`, `.env.local`, a local config, a toolchain setup. Tachograph treats the appearance of a
worktree as a lifecycle event and can copy those files from the main worktree and run the project's
setup step before the first snapshot.

The name follows the studio's instrument pattern: Boxcar, SignalBox, Watchtower, GaugeMind — a
physical recorder, not another `-keeper`.

---

## 2. Problem

Git records commits, not work. The states that hurt most when lost never became a commit: the
paragraph that was almost right, the refactor that was abandoned, the file overwritten an hour ago.

Existing answers are too narrow:

- JetBrains Local History is excellent, but bound to the IDE.
- `jj` solves it by replacing the workflow and is not a drop-in for a Git/lazygit setup.
- Timers over a repository root are too coarse, and a snapshot branch per repository pollutes the
  graph the history is supposed to stay out of.
- Worktrees multiply the problem: every branch can have its own working directory and its own WIP.

---

## 3. Principles

- **Local-first.** Loopback only, no cloud, no telemetry.
- **Automatic.** No commit discipline required to be protected.
- **Worktree-aware.** One recorder, one history branch and one timeline per working directory.
- **Useful on arrival.** A new worktree gets the local files and setup it needs, not just a recorder.
- **Separate.** The project's branches, index and reflog stay untouched.
- **Inspectable and restorable.** Diffs, per-file history, labels, restore.
- **Tool-friendly.** Orchestrate `git` and `watchexec`; do not reimplement them.
- **Desktop-first.** Tray-resident shell, embedded PWA, no WebView and no CGO.

---

## 4. Surfaces

| Surface | Responsibility |
| --- | --- |
| Tray icon | Lifecycle, open UI, pause/resume, status, quit |
| Local web UI | Roots, projects, worktrees, timelines, diffs, restore, settings |
| CLI | Scriptable access to the same service |
| History store | Git-backed, one store per project, one branch per worktree |

---

## 5. Core concepts

- **Root** — a directory Tachograph scans for repositories and worktrees.
- **Project** — a Git repository, identified by its common Git directory.
- **Worktree** — a working directory attached to a project; the unit of recording.
- **Pattern** — an optional rule for how a project names and places its worktrees.
- **Lifecycle action** — a local setup step (`copy`, `run`) executed once when a worktree appears.
- **Recorder** — the per-worktree pipeline: watch → detect → snapshot.
- **Snapshot** — a commit in the history store, carrying the full worktree tree and context.
- **Timeline** — snapshots of a worktree, newest first, filterable by path.
- **Label** — a named marker (Git tag) on a snapshot.

---

## 6. Architecture

### 6.1 Stack

- Go (CGO-free) for the service, tray, discovery, recorder and HTTP API.
- Svelte 5 + Vite for the UI, embedded into the binary.
- SQLite for metadata (deferred); the history itself stays in Git.
- `watchexec` is the primary trigger; polling is the adaptive fallback (M4c).
- `git` CLI for all repository operations.

### 6.2 Layers

```text
cmd/tachograph/                 CLI and app entry point
internal/app/                   composition root: lifecycle, tray, server, supervisor
internal/api/                   HTTP handlers, JSON payloads, SSE
internal/core/                  domain types and use cases
internal/infrastructure/
  config/                       YAML config, XDG paths
  discovery/                    root scanning, worktree discovery, patterns
  history/                      Git-backed history store and snapshots
  watcher/                      watchexec adapter and supervision
  persistence/                  SQLite metadata
internal/web/                   embedded UI build
web/                            Svelte source
```

### 6.3 Process model

- One long-running process: the tray app.
- One HTTP server on `127.0.0.1` (default `9211`).
- One poll supervisor with a bounded worker pool over all active worktrees; `watchexec` becomes the
  per-worktree trigger in M4c.
- One serialized recorder per worktree: overlapping snapshots for the same path are skipped.
- Discovery on startup, on root changes, and on a slow cadence.

---

## 7. History store

One history repository per project, with one linked-worktree placeholder per project worktree. All
placeholders share the object store.

```text
~/.local/share/tachograph/history/thought-keeper-3c0903a6/
├── repo/                                  history repo, main worktree (admin only)
│   └── .git/                              shared object store
└── worktrees/
    ├── thought-keeper-652d21d7/           placeholder, only .git
    │   └── core.worktree = /path/to/thought-keeper
    └── thought-keeper-feature-x-a1b2c3d4/ placeholder, only .git
        └── core.worktree = /path/to/thought-keeper--feature-x
```

### Registry

The history root is also the registry. On startup Tachograph lists the existing placeholders and
reads their per-worktree `repo/.git/worktrees/<name>/config.worktree` directly — no git process per
known worktree. Known projects appear immediately even when no root is configured; the known list is
published before the root scan. Roots discover new projects and worktrees, and only those new paths
go through `Ensure` and lifecycle actions. Both paths converge on `Ensure`. SQLite is a future cache,
not the source of truth.

### Naming

- Project directory: `<project-slug>-<8 hex>`.
- Worktree directory: `<worktree-slug>-<8 hex>`.
- History branch: `wt/<worktree-dir>`.

The slug comes from the project name or the worktree directory name, sanitized to lowercase letters,
digits and single dashes, capped at 40 characters. The hash is the first eight hex characters of
SHA-1 over the absolute common Git dir (project) or worktree path (worktree).

The hash is the stable identity; the slug is display. This keeps paths readable without giving up
uniqueness, filesystem safety or branch safety. Moving a project or worktree changes the hash and
therefore creates a new history store; migration is deliberately not part of M2.

Rules:

- `extensions.worktreeConfig=true` on the history repo allows `core.worktree` per linked worktree.
- Each placeholder has its own index, HEAD and branch (`wt/<worktree-dir>`); objects are shared.
- Snapshot = `git add -A` + `git commit` through the placeholder, with the message
  `snap: <RFC3339> [<branch>@<head>]`; the base commit is `base: history initialized`.
- The project's `.git` is excluded via the history repo's `info/exclude`.
- The project's `.gitignore` files are respected because the worktree is the real worktree.
- Lifecycle copy targets and common secret patterns are excluded from snapshots (see section 8).
- Removing a project worktree stops its watcher and keeps the history branch as an archive.
- The history store never pushes anywhere by default.

This is the architectural bet and the first thing to prove in the app.

---

## 8. Worktree lifecycle actions

Recording is only half of what a new worktree needs. A fresh worktree is often unusable until local,
untracked files exist: `.env`, `.env.local`, a local config, a toolchain setup. Tachograph treats the
appearance of a worktree as a lifecycle event and runs configured actions once.

### 8.1 Events

- `worktree.created` — a worktree appears (via `git worktree add`, discovery, or `history init`).
- `worktree.removed` — a worktree disappears.
- `worktree.moved` — its path changes.

Detection is discovery-based (startup, root scan, periodic rescan). An optional `post-checkout` hook
in the project can notify a running app immediately after `git worktree add`, so the actions and the
recorder start without waiting for the next scan.

### 8.2 Actions

Initial action set:

- **copy** — copy a file from the source worktree into the new worktree. The default source is the
  project's main worktree. Skip if the target exists unless `overwrite: true`.
- **run** — run a local setup command with the new worktree as working directory, with a timeout and
  captured output.

Example configuration:

```yaml
worktrees:
  on_create:
    - copy:
        path: .env
        optional: true
    - copy:
        path: .env.local
        optional: true
    - run:
        command: task setup
        only_if: Taskfile.yml
        timeout: 5m
```

### 8.3 Rules

- The default source is the main worktree; a project-level config may override it.
- Never overwrite an existing target unless explicitly configured.
- Run actions once per worktree, recorded in the history store:
  `local-history.setupAt` and `local-history.setupHash`.
- If the action config changes, the hash changes; a re-run must be explicit, never silent.
- Actions run before the first snapshot so the worktree is usable.
- **Secret safety:** copy targets are added to the history store's `info/exclude`. `.env`,
  `.env.local`, `.env.*.local`, `*.pem` and `*.key` are excluded by default; `.env.example` stays
  tracked. A copied secret must never end up in a snapshot.
- Failures are recorded and shown in the UI; they do not stop the recorder.
- Actions are local setup only: no network assumptions, no CI/CD.

### 8.4 CLI and UI

- `tachograph worktree setup <path>` — run the `on_create` actions for one worktree.
- `tachograph worktree setup --all` — run for discovered worktrees without a setup marker.
- UI: action results per worktree, with a "Run setup" button and the captured output.

---

## 9. Milestones

### M1 — Shell (done)

- Go service, tray, local HTTP server, embedded Svelte shell.
- Status API, config, CLI (`app`, `open`, `status`, `version`).
- Squircle app/tray icons and PWA manifest.
- Codebook entry and studio pictogram in the studio-page repository.

### M2 — Recorder core (done)

Prove the history store without the UI or the watcher:

- `internal/history`: project/worktree identity, history repository, placeholders, snapshots.
- CLI: `tachograph history init <worktree>` and `tachograph history snapshot <worktree>`.
- Tests against a temporary repository with a linked worktree.
- Assert that the project repository's HEAD, branch and index are untouched.

Implemented in `internal/history`; `store_test.go` covers a linked worktree, ignored files, no-change
detection and project-repository isolation.

### M3 — Discovery and roots (done)

- Root management in config and UI.
- Existing history stores are listed on startup by reading their per-worktree config files, so known
  projects appear without a root and without spawning git per worktree.
- Known projects are published before the root scan; only new worktrees go through `Ensure`.
- Scan roots for new repositories and worktrees.
- Project/worktree list and scan state.
- State is re-scanned on startup and every two minutes; SQLite metadata is deferred until re-scanning
  becomes a bottleneck.

### M3.5 — Worktree lifecycle actions (done, automatic path)

- Copy rules are editable in the **Worktree setup** card: a list of glob patterns relative to the
  main worktree, `.env`/`.env.local`/`.env.*.local` by default, an overwrite toggle, and an
  apply-to-existing action that runs the copy steps again without re-running setup commands.
  `GET/PUT /api/v1/lifecycle` and `POST /api/v1/lifecycle/apply` back the card; the configuration
  remains in `worktrees.on_create`, and glob expansions exclude every concrete copied path from the
  history store.

- Action model (`copy`, `run`) with a per-worktree setup marker.
- Default `.env` / `.env.local` copy from the main worktree.
- Detection via discovery.
- Secret-exclusion rules in the history store.
- Follow-ups: `post-checkout` notification, manual `tachograph worktree setup`, UI "Run setup".

### M4a — Poll recording (done)

- Per-worktree poll schedule with a worker pool; active worktrees poll fast, idle ones back off.
- Lightweight recorders from known stores; `Ensure` only for new worktrees.
- Snapshot on change, no-op polls are free.
- Pause/resume via API, UI and tray; per-worktree watcher state, snapshot count and last snapshot.
- Existing history is seeded from the history branch on startup, so the UI shows progress immediately.
- Activity feed for recent snapshots/errors; copy buttons for the history repo, snapshot worktree and
  real worktree paths.
- Projects in a single column, newest change first, with a text filter; the main worktree is pinned
  to the top of each project and the rest is ordered by activity, newest snapshot first. The worktree
  lifecycle signal is a traffic-light dot (setup applied / failed / none).
- `--debug` logs durable writing git commands; no-op staging, reads and diffs stay out of the log.
- Deleted worktrees are detected on the next scan; placeholder, snapshot branch and activity entry are
  removed, and the project store disappears when it was the last worktree. Stale `git worktree list`
  entries are ignored. Recreating a worktree at the same path starts a fresh history.
- Adaptive polling: `watcher.pollIntervalMs` (10 s) while active, doubling up to
  `watcher.pollIntervalMaxMs` (120 s) while idle. Only the affected worktree changes its cadence.

### M4c — watchexec (done)

- `watchexec` as the primary trigger, one `--only-emit-events` process per **project** watching all
  of its worktrees; events carry absolute paths and are mapped back to the worktree.
- Debounce and ignore rules from `watcher.debounceMs` / `watcher.ignore`; snapshot on event.
- The poll stays as the fallback and liveness check: fast while active, up to
  `watcher.pollIntervalMaxMs` (120 s) while idle; a live watcher keeps the poll at the maximum.
- Hibernation after `watcher.hibernateAfterMs` (default 5 min, `0` disables): the project's watcher
  stops, the worktree state becomes `idle`, and the next poll that finds a change starts the watcher
  again. Startup only starts watchers for recently active projects; the last activity comes from
  `$XDG_STATE_HOME/tachograph/activity.json` (or the newest snapshot in the history store when the
  file is missing), with a five-minute grace window.
- `watcher.engine` (`auto` / `watchexec` / `poll`) and `watcher.watchexecPath`; mise installations
  are discovered when the binary is not on PATH.
- Per-worktree watcher state `watching` / `polling` / `idle` / `paused` / `error`; the UI shows the
  engine and the current poll interval.
- Persist discovered state in SQLite only if re-scanning becomes a bottleneck.

### M4b — Timeline (done)

- Timeline API from the history branch (`git log`): newest first, pagination, relative file filter.
- `GET /api/v1/timeline` and `GET /api/v1/snapshot`; snapshot kinds `snapshot`, `base`, `archive`.
- Timeline drawer per worktree: snapshot list, file filter, changed files and patch, base/archive
  markers. It refreshes the newest page when SSE reports a new snapshot.
- Diffs are read-only and capped (files and bytes); restore is M5.

### M4d — Compaction (done)

- Background retention per project: every snapshot for 24 h, one per hour up to 7 days, one per day
  up to 30 days, one per week up to 90 days; the newest 50 commits and the tip are always kept.
- Rewrites use `git commit-tree`/`update-ref` with the original trees and dates, so the tip and the
  next snapshot stay untouched. `archive: true` keeps the state before the retained window as an
  `archive:` root commit.
- `reflog expire` + `git gc --prune=now` reclaims dropped objects; without a rewrite `git gc --auto`
  packs loose objects. The supervisor counters are resynced after a rewrite.
- Config lives under `history.compaction`; the first run happens one minute after start.

### M4e — Live UI via SSE (done)

- `internal/stream` hub with a single latest-status slot per subscriber; `GET /api/v1/events` sends a
  full status snapshot on connect and after every change, with a 20 s heartbeat.
- Status changes are coalesced over 120 ms, so a scan that snapshots many worktrees produces one
  frame.
- The Svelte client uses a fetch-based EventStream with SSE parsing, exponential reconnect backoff,
  heartbeat watchdog and wake on focus/online; it replaces the 5 s status poll.
- The topbar and the status card show the connection state (`connecting`, `live`, `reconnecting`,
  `offline`).

### M4f — Thought Keeper UI system (done)

- Token palette for light, dark and auto (warm paper / graphite), semantic state colours and one
  functional accent; the theme toggle lives in the topbar and persists in localStorage.
- Shared element system: buttons, icon buttons, inputs, chips, panels, rows, empty states, notices,
  a fixed-column activity table and lamps for the lifecycle/watcher state.
- `App.svelte` is split into `Topbar`, `StatusCard`, `RootsCard`, `ProjectPanel`, `WorktreeRow`,
  `ActivityTable` and small UI primitives (`Icon`, `Lamp`, `EmptyState`).
- The project header is the main worktree row (branch, snapshot age, timeline, lifecycle, recorder
  state); only linked worktrees follow as separate rows, so the main directory is not repeated.
- Tray/CLI open uses smart open: the embedded manifest supplies the PWA name, the Linux XDG
  application directories are scanned for a Chromium `--app-id=` launcher, and `gio launch` (or
  `gtk-launch`) starts it. No launcher falls back to the OS URL handler.
- Installed PWA: `display_override: window-controls-overlay` plus `viewport-fit=cover`. The topbar
  becomes the OS title bar, reserves `env(titlebar-area-*)` on both sides, and is draggable
  (`-webkit-app-region`) except for buttons and inputs. `data-wco` is set only while the overlay is
  really visible, so a browser tab keeps the normal bar.

### M5 — Recovery (done)

- Diff view (done), with optional difftastic: `diff.tool` is `auto`/`git`/`difft`; structural diff
  when the binary is found, Git unified diff as the fallback. The timeline list is reduced to relative
  time and short hash so the diff gets the space.
- Deep-linkable timeline routes: `/timeline/<worktree-id>`, `/timeline/<worktree-id>/<commit>` and
  `?file=<path>`, with pushState navigation, back/forward support and reload fallback.
- Per-file diff: selecting a changed file scopes the patch to that path while keeping the full file
  list; difftastic and the Git fallback both honour the pathspec.
- Range diff (done): shift-click selects a contiguous snapshot range; `GET /api/v1/range` returns the
  combined files and patch from the parent of the older snapshot to the newer one, with the count,
  difftastic support, file scoping and deep links via `?from=<older>`. Ranges must be linear
  (ancestor check).
- difftastic runs with colour and syntax highlighting; the API keeps the ANSI sequences and the UI
  parses SGR into theme-aware spans (theme variables for red/green/yellow/blue/magenta/cyan).
- The status card reports the resolved diff renderer (`difftastic` or `git diff`), next to the
  watcher engine; `Status.diffTool` exposes `difft` or `git`.
- Single UI window: a `BroadcastChannel` guard makes a second window focus the primary and close
  itself; when the browser blocks `window.close()`, a blocking notice offers "Focus existing window"
  or an explicit takeover. The PWA uses the same guard as a normal browser tab.
- Restore file/directory from a snapshot (done): safe relative paths, symlinks/executable modes,
  submodules skipped, absent files kept, repo lock held, the next snapshot records the restore.
- Snapshot labels (done): `PUT /api/v1/snapshot/label`, stored per project in `labels.json`, keyed
  by full commit hash after resolving short hashes. Labels appear in timeline entries and the
  snapshot detail; compaction remaps them onto rewritten commits and removes labels of dropped
  snapshots. Whitespace is folded and labels are capped at 120 characters.

### M6 — Productization (current)

- Auto-registration (done): every two seconds a cheap poll of `<common-dir>/worktrees` detects
  linked-worktree additions and removals, debounces for 600 ms, waits for a running scan and then
  triggers a full scan. New worktrees get their history store, lifecycle copies and recorder within
  seconds; deleted worktrees disappear just as fast. The two-minute root scan remains the fallback.
- Worktree patterns (done): `worktrees.patterns` holds absolute or `~`-prefixed globs that point at
  worktree directories outside the roots. Discovery expands them on every scan and merges matches by
  common Git dir. The Roots card edits the list (`GET/POST/DELETE /api/v1/patterns`), and unmatched
  patterns are not an error.
- Autostart (done): the status card toggles a per-user login entry. Every start re-checks and
  refreshes the entry (`app --no-open`, plus `--config` for a non-default config) and removes it when
  disabled. Linux uses an XDG desktop entry, macOS a LaunchAgent, Windows the HKCU Run key. Failures
  are warnings, never fatal.
- Release tooling (done): the local release skill in `.opencode/skills/release` runs a preflight,
  builds Linux, macOS and Windows binaries with the version stamped in, writes `SHA256SUMS`, and
  publishes with `gh release create`. There are no GitHub workflows; notes are drafted in
  `.release/` and removed after a successful publish.
- Onboarding, native installers.
- Optional, explicit remote backup of the history store.

---

## 10. M2 — recorder core (implemented)

Goal: a working, tested recorder core.

```
internal/history/
  store.go       Store, Project, Worktree, Snapshot
  git.go         git command helpers
  store_test.go  integration test on a temp repo + worktree
```

API shape:

```go
type Store struct { Root string }

func DefaultRoot() (string, error)
func New(root string) *Store

func (s *Store) Ensure(ctx context.Context, worktreePath string) (Recorder, error)
func (r Recorder) Snapshot(ctx context.Context) (SnapshotResult, error)
```

`Ensure` resolves:

- worktree root (`git rev-parse --show-toplevel`),
- common Git dir (`git rev-parse --git-common-dir`, absolute),
- project name (basename of the main worktree),
- current branch and HEAD.

It creates or reuses:

- `<root>/<project-dir>/repo` (history repository),
- `<root>/<project-dir>/worktrees/<worktree-dir>` (linked worktree placeholder),
- branch `wt/<worktree-dir>`,
- per-worktree `core.worktree` pointing at the project worktree.

`Snapshot` stages and commits the worktree, returning the commit hash, or `ErrNoChanges`.

CLI:

```text
tachograph history init <path>       ensure and print the history store paths
tachograph history snapshot <path>   take a snapshot and print the commit
```

Acceptance:

- A temp project with a linked worktree produces a history repository and a snapshot.
- A second snapshot without changes returns `ErrNoChanges`.
- A changed file produces a second snapshot.
- The project repository's HEAD, branch, index and reflog are unchanged.
- `go test ./...` and `go vet ./...` pass.

Explicitly out of scope for M2: roots UI, discovery scan, watcher, SQLite, timeline API, restore.

---

## 11. Risks

- **Per-worktree `core.worktree`** is advanced Git; M2 proves it in the app, not just in a shell.
- **`watchexec` availability** at runtime; polling is the fallback and must keep working without it.
- **Polling cost**: a 5 s poll over many worktrees is not free; the interval is configurable, and the
  watchexec trigger reduces the fallback to a liveness check.
- **Tray portability**: Wayland/AppIndicator differences on Linux.
- **Secrets and large files**: unignored files are recorded; ignore rules must be visible and sane.
- **Secrets in lifecycle copies**: `.env` is copied into the worktree; the copy target and common
  secret patterns must be excluded from the history store, or a snapshot (and any future backup)
  contains credentials.
- **Automatic command execution**: `run` actions execute local code. They need explicit config, a
  timeout, captured output and no silent re-runs.
- **Action idempotency**: a re-discovery must not overwrite user edits or run setup again; the setup
  marker and the action-config hash are the guard.
- **Source ambiguity**: the main worktree may not contain the file; `optional` vs required must be
  explicit in the action config.
- **Disk**: shared objects keep duplication low; retention and ignore rules arrive with M5.
- **License**: MPL-2.0 (the target repository ships the LICENSE file).

---

## 12. Non-goals

- No cloud, sync or telemetry.
- No IDE integration; Tachograph is its own UI.
- No replacement for `git` or `jj`.
- No snapshot branch in the project repository.
- No arbitrary CI/CD; lifecycle actions are local worktree setup only.
- No secrets in the history store.
