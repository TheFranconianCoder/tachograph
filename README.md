# Tachograph

**The local history recorder for every worktree.**

A tachograph records a journey continuously, without gaps, on a physical chart. Tachograph does the
same for Git worktrees: it records the states between commits, keeps them out of the project's Git
graph, and gives every worktree its own timeline.

> Status: **v0.1.0 — first release** — every worktree has a snapshot timeline with a file filter,
> a coloured per-file diff, file/directory restore and snapshot labels, recorded by watchexec
> events with an adaptive poll fallback. Release binaries are built for Linux, macOS and Windows.

---

## Why

Git records commits, not work. The most valuable states are often the ones that never became a
commit: the paragraph that was almost right, the refactor that was abandoned, the file that was
overwritten an hour ago. JetBrains Local History solves this inside the IDE; `jj` solves it by
replacing the workflow. Tachograph is the desktop-first, worktree-aware recorder that stays out of
both.

- **Automatic** — no commit discipline required to be protected.
- **Worktree-aware** — one recorder and one timeline per worktree.
- **Local** — loopback only, no cloud, no telemetry.
- **Inspectable** — diffs and per-file history.
- **Restorable** — put a file or a whole worktree back without touching the project's Git history.
- **Useful on arrival** — copy local files such as `.env` from the main worktree and run a setup step
  when a new worktree appears.
- **Tool-friendly** — it orchestrates `git`, `watchexec` and `difftastic` instead of reimplementing
  them.

---

## Install

Download the binary for your platform from the
[GitHub releases](https://github.com/TheFranconianCoder/tachograph/releases):

```text
tachograph-linux-amd64
tachograph-linux-arm64
tachograph-darwin-amd64
tachograph-darwin-arm64
tachograph-windows-amd64.exe
SHA256SUMS
```

Linux and macOS:

```bash
curl -LO https://github.com/TheFranconianCoder/tachograph/releases/download/v0.1.0/tachograph-linux-amd64
chmod +x tachograph-linux-amd64
install -m 755 tachograph-linux-amd64 ~/.local/bin/tachograph
tachograph
```

Windows: download `tachograph-windows-amd64.exe` and run it.

Verify the download with `sha256sum -c SHA256SUMS` on Linux or
`shasum -a 256 -c SHA256SUMS` on macOS. The macOS binaries are unsigned;
Gatekeeper may need an explicit allow in System Settings.

The first start opens the UI; add a root such as `~/Code` and the worktrees
below it are recorded. Start at login is available in the status card.

## Build from source

Requirements: Go 1.26+, Node 22+ and pnpm 11+. `watchexec` is optional: when it is installed (PATH or
a mise installation) it becomes the primary trigger; without it the app polls. Set `watcher.engine` to
force one path.

[difftastic](https://difftastic.wilfred.me.uk) (`difft`) is optional and improves the snapshot diff:
it shows structural changes instead of line-based patches. Difftastic's ANSI colours are preserved
and mapped onto the light/dark theme palette, so added and removed tokens stay readable. It is
detected in PATH or a mise installation; `diff.tool` selects `auto`, `git` or `difft`, and
`diff.path` points at a specific binary. Without it, the timeline shows Git's unified diff.

```bash
task build          # build the UI and the static binary
task run            # tray + local service, opens the UI
task run:headless   # service only, no tray, no browser
```

The app listens on `127.0.0.1:9211` by default:

```bash
tachograph status
tachograph open
```

Open the UI, add a root such as `~/Code`, and press **Scan now**.

**Open** from the tray or `tachograph open` respects the installed PWA: Tachograph reads its own
manifest name, finds the browser-written launcher (`--app-id=…`) in the XDG application directories
and starts that. Without a launcher it falls back to the OS URL handler (`xdg-open`, `open` or
`rundll32`).

**Start at login** is available in the status card. Tachograph checks the entry on every start and
refreshes it if the executable or arguments changed; disabling removes it. The login command is
`tachograph app --no-open`, so autostart never opens the browser UI — the app stays in the tray.
Failures are logged and never block the app.

```text
Linux    ~/.config/autostart/tachograph.desktop
macOS    ~/Library/LaunchAgents/dev.tachograph.tray.plist
Windows  HKCU\Software\Microsoft\Windows\CurrentVersion\Run\Tachograph
```

### Discovery and roots

Tachograph starts from the existing history stores, so previously recorded projects appear
immediately — even when no root is configured. Known stores are read directly from their
per-worktree `config.worktree`; no git process is spawned for them. A root scan only adds new
projects: it walks each root for Git repositories, resolves every worktree with `git worktree list`,
ensures its history store and runs the lifecycle actions for worktrees without a setup marker.

**New linked worktrees register themselves.** Tachograph watches the linked-worktree metadata under
each project's common Git directory (every two seconds, debounced), so a `git worktree add` anywhere
appears within a few seconds — even outside the configured roots — and gets its copy rules applied
immediately. Removing a worktree cleans up its placeholder and snapshot branch just as quickly. The
two-minute root scan remains the fallback and is still what discovers brand-new projects.

Projects whose main repository lives outside every root can be reached with **worktree patterns**:
absolute globs or `~/` paths such as `~/worktrees/*`. Every match that is a Git worktree (or
repository) is inspected and merged into the project list. Patterns are editable in the Roots card.

```text
GET    /api/v1/patterns
POST   /api/v1/patterns    {"pattern": "~/worktrees/*"}
DELETE /api/v1/patterns    {"pattern": "~/worktrees/*"}
```

Each worktree row has a copy button for the **real worktree path**; the directory name alone is
shown in the table to keep it narrow.

```bash
curl -X POST http://127.0.0.1:9211/api/v1/roots \
  -H 'content-type: application/json' -d '{"path":"/home/seba/Code"}'
curl -X POST http://127.0.0.1:9211/api/v1/scan
curl http://127.0.0.1:9211/api/v1/projects
```

`GET /api/v1/status` includes roots, projects, worktrees, scan state and errors. Removing a root uses
`DELETE /api/v1/roots` with the same `{"path": ...}` body.

### Worktree setup

The **Worktree setup** card controls which local files are copied from the main worktree into every
new worktree. The defaults are `.env`, `.env.local` and `.env.*.local`; patterns are relative and
support `*`, `?` and `[]` globs. A pattern without a match is skipped, existing files are preserved
unless **Overwrite existing files** is enabled, and every copied path is excluded from the history
store.

**Save** changes the rules for worktrees discovered from then on. **Apply to existing** copies
missing files into the worktrees that are already registered (copy actions only; setup commands are
not re-run). Run commands such as `task setup` stay in `config.yaml`.

```text
GET  /api/v1/lifecycle
PUT  /api/v1/lifecycle          {"copy": [".env", "config/*.yaml"], "overwrite": false}
POST /api/v1/lifecycle/apply
```

### Recording

`watchexec` is the primary trigger. One process per **project** watches all of its worktrees and
emits event batches after `watcher.debounceMs` (default `1500`); each batch triggers a snapshot in the
affected worktree's `wt/...` branch. Grouping keeps the process count low: the event payload carries
absolute paths, and the manager maps each path to its worktree.

Polling runs as the fallback and as a liveness check: an active worktree polls every
`watcher.pollIntervalMs` (default `10000`), an idle one backs off to
`watcher.pollIntervalMaxMs` (default `120000`). A live watcher keeps the poll interval at the
maximum.

Quiet projects hibernate: after `watcher.hibernateAfterMs` (default `300000`, `0` disables) without a
snapshot, the watcher process stops and the project stays on the max poll. The next poll that finds a
change writes the snapshot and starts the watcher again. At app start only projects with recent
activity get a watcher immediately; the last activity comes from
`$XDG_STATE_HOME/tachograph/activity.json`, or from the newest snapshot in the history store when the
state file does not exist yet. A startup grace of the same length keeps those watchers open for up to
five minutes.

The status card shows `Recording: on`, the trigger engine (`watchexec` or `poll`), the diff renderer
(`difftastic` or `git diff`), total snapshots and the last snapshot time. A three-part strip
summarizes the recorder state of all worktrees: **watching**, **polling** and **idle**. Every worktree row shows its state (`watching`, `polling`, `idle`,
`paused`, `error`), `N snap` / `N snaps`, the snapshot age and the current poll interval on hover. The
Activity list keeps one entry per worktree: a new snapshot or error replaces the previous entry for
that worktree. The newest entry per worktree is persisted in the same
`$XDG_STATE_HOME/tachograph/activity.json` and restored on startup, so the table is not empty after a
restart.

The UI is live: it opens one SSE connection to `GET /api/v1/events`, receives a full status snapshot
on connect and after every change, and shows the connection state in the topbar and the status card.
Only one UI window is active. A second window announces itself over a `BroadcastChannel`, focuses the
existing window and closes itself when the browser allows it; otherwise it shows a blocking notice
with **Focus existing window**.
In the installed PWA the topbar uses the **Window Controls Overlay**: it becomes the OS title bar,
reserves the platform window controls and is draggable except for its buttons. Browser tabs keep the
normal topbar.
A heartbeat every 20 s keeps the stream alive; reconnects fetch a fresh snapshot, so no delta is
missed. There is no status polling in the browser.

The interface follows the Thought Keeper design system: warm paper in light, graphite in dark, one
functional accent and semantic colours only for states. The theme starts at `auto` (follow the OS);
the sun/moon button in the topbar cycles auto → light → dark and stores the choice.

Deleted worktrees are removed on the next scan: the placeholder, its snapshot branch and its activity
entry disappear. When it was the project's last worktree, the whole project store (the shared history
repository) is removed too. A stale `git worktree list` entry (shown as `prunable` by Git) is ignored.
Recreating a worktree at the same path starts a fresh history.

### Compaction & retention

Every snapshot is a commit on the worktree's `wt/...` branch; Git deduplicates unchanged content and
packs the rest. A background job additionally thins old snapshots:

- every snapshot for `keepAllFor` (24 h)
- one per hour up to `hourlyFor` (7 days)
- one per day up to `dailyFor` (30 days)
- one per week up to `maxAge` (90 days); `0` keeps the weekly tier forever
- the newest `keepMinimum` (50) commits are always kept, and the tip is never dropped

With `archive: true` the state immediately before the retained window becomes an `archive:` root
commit, so the old state survives even though its intermediate snapshots do not. A rewrite reuses
the original trees and commit dates, so the tip tree—and therefore the next snapshot—is unchanged.
`autoGc` then expires the reflog and prunes the dropped objects. The job runs one minute after start
and then every `interval`.

### Timeline

Every worktree has a timeline from its history branch. The log icon on a worktree row opens a drawer
with the snapshots newest first, a relative file filter and pagination. Selecting a snapshot shows
the changed files and the patch; `base:` and `archive:` roots are labelled. The newest page refreshes
when SSE reports a new snapshot.

```text
GET  /api/v1/timeline?path=<worktree>&limit=50&offset=0&file=<relative path>
GET  /api/v1/snapshot?path=<worktree>&hash=<commit>&file=<relative path>
GET  /api/v1/range?path=<worktree>&from=<older>&to=<newer>&file=<relative path>
PUT  /api/v1/snapshot/label  {"path": "...", "hash": "...", "label": "before refactor"}
POST /api/v1/restore   {"path": "...", "hash": "...", "file": "src/app.go"}
```

The list contains only the relative time and the short hash; the snapshot itself is shown in the
detail pane, where the diff renderer (`difftastic` or `git diff`) is labelled. Every scroll
container reserves its gutter, so a scrollbar appearing—or the drawer locking the page—never shifts
the layout.

Every view has an addressable URL:

```text
/timeline/<worktree-id>                        open the timeline
/timeline/<worktree-id>/<commit>               select one snapshot
/timeline/<worktree-id>/<commit>?file=<path>   select one file's diff
/timeline/<worktree-id>/<newer>?from=<older>   compare a snapshot range
```

The worktree ID is the stable 12-character ID from the status API. Selecting a snapshot pushes a
history entry, so browser back/forward walks the timeline, and a deep link survives a reload (the
server falls back to the single-page app for unknown paths). Clicking a changed file narrows the
diff to that file; the complete file list stays visible and "All files" restores the full patch.
**Shift-click** a second snapshot to compare the contiguous range: the patch spans from the parent
of the older selection to the newer one, so the oldest selected change is included. The range bar
shows the count and both endpoints; "Single snapshot" collapses it again. The refresh icon on a file
row fills the restore path without changing the diff scope. If a link
points at a worktree that no longer exists, the UI says so instead of failing silently.

Every snapshot can carry a short label (up to 120 characters): click **Add label** (or the existing
label chip) in the detail head and save. Labels appear as a chip and in the row tooltip, and they are
kept in `labels.json` inside the project store. Compaction remaps labels onto rewritten commits and
drops the labels of snapshots that no longer exist.

Select a changed file (or type a file or directory path) and press **Restore** in the detail pane.
The snapshot content is written back into the worktree; files that are absent from the snapshot are
kept, submodules are skipped, and the next snapshot records the restore. The diff is capped at 500
files and 256 KiB.

Projects are listed in one column, newest change first. The filter above the list narrows by project
name, path, branch or state. Inside a project the **project header is the main worktree**: it carries
the main branch, snapshot age, timeline button and recorder state. Only the linked worktrees follow
as rows, alphabetically by directory name. The dot at the end of every worktree row is the lifecycle
signal: green = setup actions applied, red = setup failed, hollow = no lifecycle actions recorded
(hover shows the details).

Two copy buttons keep the paths reachable:

- **Project header (layers icon):** the history repository, `history/<project>/repo`.
- **Project header and every worktree row (copy icon):** the real project worktree path. The clock
  icon next to it opens that worktree's timeline.

```bash
curl -X POST http://127.0.0.1:9211/api/v1/recording/pause
curl -X POST http://127.0.0.1:9211/api/v1/recording/resume
curl -X POST http://127.0.0.1:9211/api/v1/recording/poll
```

`watchexec` is auto-detected (PATH, then a mise installation). Force it with `watcher.engine:
watchexec` or disable it with `watcher.engine: poll`; `watcher.watchexecPath` points at a specific
binary.

For debugging, durable writing git commands (`commit`, `config`, `worktree add`, ...) are logged when
the app or the history CLI runs with `--debug`. No-op staging, reads and diffs stay silent:

```bash
task run:debug
# or
go run ./cmd/tachograph app --debug
```

### Recorder core

The history store works without the UI or the watcher:

```bash
tachograph history init /path/to/worktree
tachograph history snapshot /path/to/worktree
tachograph history snapshot /path/to/worktree   # prints "no changes"
```

The first command creates or reuses:

```text
~/.local/share/tachograph/history/thought-keeper-3c0903a6/
├── repo/                                  history repository, shared object store
└── worktrees/thought-keeper-652d21d7/    linked-worktree placeholder
    └── core.worktree = /home/seba/Code/.../thought-keeper
```

`repo/` is the history repository itself (its main worktree is empty; it only carries `.git` and the
shared object store). `worktrees/<name>/` are linked worktrees of that repository, one per real
project worktree; each contains only a `.git` file and points its `core.worktree` at the real
directory. The real project worktree is never duplicated.

Names are `<slug>-<8 hex>`: the slug comes from the project name or the worktree directory, the hash
is the first eight hex characters of SHA-1 over the absolute Git common dir or worktree path. The hash
is the stable identity; the slug is there to keep the paths readable.

Each worktree gets its own branch (`wt/<worktree-dir>`) and index. A snapshot is a commit in that
branch with the message `snap: <RFC3339> [<branch>@<head>]`; the initial commit is
`base: history initialized`. The project's own branches, index and reflog stay untouched. Use
`history.root` in the config or `--data-dir DIR` to keep the history somewhere else.

### Icons

The app icon is a **squircle** — a superellipse, not a rounded rectangle. It is generated from
`web/icons/pictogram.svg` (a copy of `studio-page/design/pictograms/tachograph.svg`) by
`web/scripts/render-icons.mjs`:

```bash
task icons   # regenerate the PWA icons and the tray icon
```

The studio pictogram in the Codebook stays square by design; only the app/tray icon is reframed.

The tray icon carries the aggregate recorder state as a colored badge: gray for **idle** (nothing
is live or recording is paused), blue for **polling**, and green for **watching** (watchexec is
live). Watching wins over polling when worktrees are in both states.

---

## Configuration

The config lives at `~/.config/tachograph/config.yaml` and is created on first run. See
[`config.example.yaml`](config.example.yaml).

```yaml
server:
  port: 9211
ui:
  open: true
autostart: false
watcher:
  engine: auto
  debounceMs: 1500
  pollIntervalMs: 10000
  pollIntervalMaxMs: 120000
  hibernateAfterMs: 300000
  workers: 4
  ignore: [.git, node_modules, dist, build, .cache]
history:
  root: ''            # default: $XDG_DATA_HOME/tachograph/history
  compaction:
    enabled: true
    interval: 1h
    autoGc: true
    keepMinimum: 50
    keepAllFor: 24h
    hourlyFor: 168h
    dailyFor: 720h
    maxAge: 2160h
    archive: true
worktrees:
  patterns:
    - ~/worktrees/*
  on_create:
    - copy:
        path: .env
        optional: true
    - copy:
        path: .env.local
        optional: true
    - copy:
        path: .env.*.local
        optional: true
roots:
  - /home/seba/Code
```

---

## How it will work

- A **root** is a directory that Tachograph scans for repositories and worktrees.
- A **project** is a Git repository with one or more worktrees.
- Each **worktree** gets a recorder; changes become snapshot commits in a separate Git-backed history
  store. `watchexec` is the primary trigger, polling the fallback.
- The history store is one repository per project, with one linked-worktree placeholder and one
  branch per project worktree. The project's own branches, index and reflog stay untouched.
- A new **worktree** can run lifecycle actions: copy local files such as `.env` or `.env.local` from
  the main worktree and run a local setup command before the first snapshot.
- The UI shows the timeline per worktree, filters it by file, shows a read-only snapshot diff and
  restores a file or directory from any snapshot.

The product plan lives at [`docs/plans/tachograph.md`](docs/plans/tachograph.md).

---

## Not implemented yet (0.x)

- Onboarding / first-run guidance
- SQLite metadata (state is re-scanned on startup)
- `post-checkout` notification and manual `worktree setup` command
- Native installers (deb, rpm, MSI, pkg)
- Optional, explicit remote backup (sync and telemetry are non-goals)

---

## Stack

- Go (CGO-free), `github.com/gogpu/systray`
- Svelte 5 + Vite, embedded into the binary
- `git` CLI for repository operations
- `watchexec` as the primary trigger, polling as fallback
- SQLite for metadata (deferred)

---

## License

Tachograph is licensed under the [Mozilla Public License 2.0](LICENSE) (MPL-2.0).

Release notes and binaries are published on the
[GitHub releases](https://github.com/TheFranconianCoder/tachograph/releases) page.
