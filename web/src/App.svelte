<script lang="ts">
    import { onMount } from 'svelte';
    import ActivityTable from './components/ActivityTable.svelte';
    import ProjectPanel from './components/ProjectPanel.svelte';
    import RecorderActionsCard from './components/RecorderActionsCard.svelte';
    import RootsCard from './components/RootsCard.svelte';
    import StatusCard from './components/StatusCard.svelte';
    import WorktreeSetupCard from './components/WorktreeSetupCard.svelte';
    import TimelineDrawer from './components/TimelineDrawer.svelte';
    import Topbar from './components/layout/Topbar.svelte';
    import ConfirmDialog from './components/ui/ConfirmDialog.svelte';
    import EmptyState from './components/ui/EmptyState.svelte';
    import Icon from './components/ui/Icon.svelte';
    import { EventStream } from './lib/events/event-stream';
    import { relativeTime } from './lib/format';
    import { currentRoute, navigate, startRouter, timelineURL } from './lib/router';
    import { claimAsPrimary, claimWindow, focusPrimaryWindow } from './lib/single-window';
    import type { Route } from './lib/router';
    import type { ConnectionState, Status, Worktree } from './lib/types';

    let status = $state<Status | null>(null);
    let actionError = $state<string | null>(null);
    let rootInput = $state('');
    let patternInput = $state('');
    let filterQuery = $state('');
    let busy = $state(false);
    let copiedPath = $state<string | null>(null);
    let now = $state(Date.now());
    let connection = $state<ConnectionState>('connecting');
    let statusReceivedAt = $state(Date.now());
    let route = $state<Route>(currentRoute());
    let duplicateWindow = $state(false);

    const stream = new EventStream({
        url: '/api/v1/events',
        onFrame: frame => {
            if (frame.type !== 'status') return;
            status = frame.data as Status;
            statusReceivedAt = Date.now();
        },
        onState: state => {
            connection = state;
        },
    });

    onMount(() => {
        claimWindow(() => {
            duplicateWindow = true;
            window.close();
        });
        const stopRouter = startRouter(next => {
            route = next;
        });
        stream.start();
        const timer = window.setInterval(() => {
            now = Date.now();
        }, 5000);
        const wake = (): void => stream.wake();
        const visibility = (): void => {
            if (document.visibilityState === 'visible') stream.wake();
        };
        window.addEventListener('focus', wake);
        window.addEventListener('online', wake);
        document.addEventListener('visibilitychange', visibility);
        return () => {
            stopRouter();
            window.clearInterval(timer);
            window.removeEventListener('focus', wake);
            window.removeEventListener('online', wake);
            document.removeEventListener('visibilitychange', visibility);
            stream.stop();
        };
    });

    const uptime = $derived.by(() => {
        if (!status) return '—';
        const elapsed = Math.max(0, Math.floor((now - statusReceivedAt) / 1000));
        const total = status.uptimeSeconds + elapsed;
        const hours = Math.floor(total / 3600);
        const minutes = Math.floor((total % 3600) / 60);
        const seconds = total % 60;
        if (hours > 0) return `${hours}h ${minutes}m`;
        if (minutes > 0) return `${minutes}m ${seconds}s`;
        return `${seconds}s`;
    });

    const connectionTitle = $derived.by(() => {
        switch (connection) {
            case 'live':
                return 'Live updates via SSE';
            case 'reconnecting':
                return 'Reconnecting to the local service';
            case 'offline':
                return 'Browser offline';
            default:
                return 'Connecting to the local service';
        }
    });

    const roots = $derived(status?.roots ?? []);
    const projects = $derived(status?.projects ?? []);
    const activities = $derived(status?.activity ?? []);
    const worktreeCount = $derived(projects.reduce((sum, project) => sum + project.worktrees.length, 0));
    const totalSnapshots = $derived(
        projects.reduce(
            (total, project) =>
                total + project.worktrees.reduce((sum, worktree) => sum + (worktree.snapshots ?? 0), 0),
            0
        )
    );

    const lastSnapshotAt = $derived.by(() => {
        const times = projects
            .flatMap(project => project.worktrees.map(worktree => worktree.lastSnapshot))
            .filter((value): value is string => Boolean(value))
            .sort();
        return times.length === 0 ? '' : times[times.length - 1];
    });
    const lastScanAt = $derived(status?.lastScan ?? '');

    // The API already orders projects and worktrees by newest snapshot; the
    // filter only narrows the list and keeps that order.
    const timelineWorktree = $derived.by((): Worktree | null => {
        if (route.name !== 'timeline') return null;
        for (const project of projects) {
            for (const worktree of project.worktrees) {
                if (worktree.id === route.worktreeID) return worktree;
            }
        }
        return null;
    });

    const worktreeIDs = $derived.by(() => {
        const ids = new Map<string, string>();
        for (const project of projects) {
            for (const worktree of project.worktrees) {
                if (worktree.id) ids.set(worktree.path, worktree.id);
            }
        }
        return ids;
    });

    function openTimeline(worktree: Worktree): void {
        if (!worktree.id) return;
        navigate(timelineURL(worktree.id));
    }

    function openActivityTimeline(path: string): void {
        const id = worktreeIDs.get(path);
        if (id) navigate(timelineURL(id));
    }

    function selectSnapshot(hash: string | null): void {
        if (route.name !== 'timeline') return;
        navigate(timelineURL(route.worktreeID, hash));
    }

    function selectRange(from: string, to: string): void {
        if (route.name !== 'timeline') return;
        navigate(timelineURL(route.worktreeID, to, [], from));
    }

    function toggleAll(): void {
        if (route.name !== 'timeline') return;
        if (route.all) {
            navigate(timelineURL(route.worktreeID, null, [], null, false));
            return;
        }
        navigate(timelineURL(route.worktreeID, null, [], null, true));
    }

    function selectFiles(files: string[]): void {
        if (route.name !== 'timeline' || (!route.hash && !route.all)) return;
        navigate(timelineURL(route.worktreeID, route.hash, files, route.from, route.all));
    }

    function closeTimeline(): void {
        navigate('/', true);
    }

    const filteredProjects = $derived.by(() => {
        const query = filterQuery.trim().toLowerCase();
        if (!query) return projects;
        return projects.filter(project => {
            const haystack = [
                project.name,
                project.commonDir,
                project.mainWorktree,
                ...project.worktrees.flatMap(worktree => [
                    worktree.path,
                    worktree.branch,
                    worktree.state,
                    worktree.watcher ?? '',
                    worktree.setup ?? '',
                    worktree.dirty ? 'dirty' : '',
                ]),
            ]
                .join(' ')
                .toLowerCase();
            return haystack.includes(query);
        });
    });

    async function request(path: string, options: RequestInit = {}): Promise<void> {
        const response = await fetch(path, {
            headers: { 'content-type': 'application/json', accept: 'application/json' },
            ...options,
        });
        if (!response.ok) {
            const payload = (await response.json().catch(() => null)) as { error?: string } | null;
            throw new Error(payload?.error ?? `request failed with ${response.status}`);
        }
    }

    async function addRoot(): Promise<void> {
        const path = rootInput.trim();
        if (!path || busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/roots', { method: 'POST', body: JSON.stringify({ path }) });
            rootInput = '';
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not add root';
        } finally {
            busy = false;
        }
    }

    async function removeRoot(path: string): Promise<void> {
        if (busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/roots', { method: 'DELETE', body: JSON.stringify({ path }) });
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not remove root';
        } finally {
            busy = false;
        }
    }

    async function addPattern(): Promise<void> {
        const pattern = patternInput.trim();
        if (!pattern || busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/patterns', {
                method: 'POST',
                body: JSON.stringify({ pattern }),
            });
            patternInput = '';
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not add the pattern';
        } finally {
            busy = false;
        }
    }

    async function removePattern(pattern: string): Promise<void> {
        if (busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/patterns', {
                method: 'DELETE',
                body: JSON.stringify({ pattern }),
            });
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not remove the pattern';
        } finally {
            busy = false;
        }
    }

    async function scanNow(): Promise<void> {
        if (busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/scan', { method: 'POST' });
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not start scan';
        } finally {
            busy = false;
        }
    }

    async function toggleAutostart(enabled: boolean): Promise<void> {
        if (busy) return;
        busy = true;
        actionError = null;
        try {
            await request('/api/v1/autostart', {
                method: 'PUT',
                body: JSON.stringify({ enabled }),
            });
        } catch (cause) {
            actionError =
                cause instanceof Error ? cause.message : 'could not change autostart';
        } finally {
            busy = false;
        }
    }

    async function toggleRecording(): Promise<void> {
        if (busy) return;
        busy = true;
        actionError = null;
        try {
            const path = status?.paused
                ? '/api/v1/recording/resume'
                : '/api/v1/recording/pause';
            await request(path, { method: 'POST' });
        } catch (cause) {
            actionError =
                cause instanceof Error ? cause.message : 'could not change recording state';
        } finally {
            busy = false;
        }
    }

    async function saveWorktreeSetup(copy: string[], overwrite: boolean): Promise<void> {
        await request('/api/v1/lifecycle', {
            method: 'PUT',
            body: JSON.stringify({ copy, overwrite }),
        });
    }

    async function applyWorktreeSetup(): Promise<{
        worktrees: number;
        copied: number;
        errors: string[];
    }> {
        const response = await fetch('/api/v1/lifecycle/apply', {
            method: 'POST',
            headers: { accept: 'application/json' },
        });
        if (!response.ok) {
            const payload = (await response.json().catch(() => null)) as { error?: string } | null;
            throw new Error(payload?.error ?? `request failed with ${response.status}`);
        }
        return (await response.json()) as {
            worktrees: number;
            copied: number;
            errors: string[];
        };
    }

    async function copyPath(path: string): Promise<void> {
        try {
            if (!navigator.clipboard) throw new Error('clipboard is not available');
            await navigator.clipboard.writeText(path);
            copiedPath = path;
            window.setTimeout(() => {
                if (copiedPath === path) copiedPath = null;
            }, 1600);
        } catch (cause) {
            actionError = cause instanceof Error ? cause.message : 'could not copy path';
        }
    }
</script>

<div class="shell">
    <Topbar {connection} {connectionTitle} />

    <main class="page">
        <section class="page-head">
            <p class="eyebrow">Local history · Worktrees · Desktop</p>
            <h1>Tachograph</h1>
            <p class="sub">
                The local history recorder for every worktree. Recording is active: watchexec events
                trigger snapshots immediately, with adaptive polling as the fallback.
            </p>
        </section>

        <div class="content">
            <section class="grid">
                <StatusCard
                    {status}
                    {connection}
                    {connectionTitle}
                    {uptime}
                    {totalSnapshots}
                    {worktreeCount}
                    projectCount={projects.length}
                    rootsCount={roots.length}
                    {lastSnapshotAt}
                    {lastScanAt}
                    {now}
                />
                <div class="column-stack">
                    <RootsCard
                        {roots}
                        patterns={status?.worktreePatterns ?? []}
                        {busy}
                        scanning={status?.scanning ?? false}
                        bind:rootInput
                        bind:patternInput
                        {actionError}
                        scanError={status?.scanError ?? null}
                        onAdd={addRoot}
                        onRemove={removeRoot}
                        onAddPattern={addPattern}
                        onRemovePattern={removePattern}
                        onScan={scanNow}
                    />
                    <WorktreeSetupCard
                        setup={status?.worktreeSetup ?? null}
                        onSave={saveWorktreeSetup}
                        onApply={applyWorktreeSetup}
                    />
                    <RecorderActionsCard
                        {status}
                        {busy}
                        onToggleRecording={toggleRecording}
                        onToggleAutostart={toggleAutostart}
                    />
                </div>
            </section>

        <section class="section">
            <div class="section-head">
                <h2>Activity</h2>
                <span class="count">{activities.length} worktrees</span>
            </div>
            {#if activities.length === 0}
                <EmptyState
                    icon="clock"
                    title="No new recorder events since start."
                    text="Existing snapshots are shown in the project cards; new snapshots and errors appear here."
                />
            {:else}
                <ActivityTable
                    {activities}
                    {now}
                    {worktreeIDs}
                    onOpenTimeline={openActivityTimeline}
                />
            {/if}
        </section>

        <section class="section">
            <div class="list-head">
                <div class="head-row">
                    <h2>Projects</h2>
                    <span class="count">
                        {filteredProjects.length} of {projects.length} · {worktreeCount} worktrees
                    </span>
                    <span class="spacer"></span>
                    {#if filterQuery}
                        <button
                            type="button"
                            class="btn btn--ghost btn--sm"
                            onclick={() => (filterQuery = '')}
                        >
                            <Icon name="close" size={14} />
                            Clear
                        </button>
                    {/if}
                </div>
                <div class="search-wrap">
                    <Icon name="search" size={15} />
                    <input
                        class="input input--search"
                        type="search"
                        placeholder="Filter by project, path, branch or state…"
                        bind:value={filterQuery}
                        aria-label="Filter projects"
                    />
                </div>
            </div>

            {#if projects.length === 0}
                <EmptyState
                    icon="layers"
                    title="No projects discovered yet."
                    text="Add a root above and run a scan. Tachograph looks for Git repositories and resolves every worktree with git worktree list."
                />
            {:else if filteredProjects.length === 0}
                <EmptyState
                    icon="search"
                    title={`No projects match “${filterQuery.trim()}”.`}
                    text="The filter checks project name, paths, branches and states."
                />
            {:else}
                <div class="project-list">
                    {#each filteredProjects as project (project.commonDir || project.name)}
                        <ProjectPanel
                            {project}
                            {now}
                            {copiedPath}
                            {copyPath}
                            onOpenTimeline={openTimeline}
                        />
                    {/each}
                </div>
            {/if}
        </section>
        </div>
    </main>

    <footer class="footer">
        <span>Local-first</span>
        <span>127.0.0.1 only</span>
        <span>No telemetry</span>
    </footer>

    {#if duplicateWindow}
        <div
            class="single-window"
            role="alertdialog"
            aria-modal="true"
            aria-label="Tachograph is already open"
        >
            <div class="single-window__card">
                <p class="eyebrow">Single window</p>
                <h2>Tachograph is already open</h2>
                <p class="muted">
                    Only one UI window can be active. Focus the existing window, or close this tab
                    manually.
                </p>
                <div class="single-window__actions">
                    <button
                        type="button"
                        class="btn btn--primary btn--sm"
                        onclick={() => {
                            focusPrimaryWindow();
                            window.close();
                        }}
                    >
                        Focus existing window
                    </button>
                    <button
                        type="button"
                        class="btn btn--ghost btn--sm"
                        onclick={() => {
                            claimAsPrimary();
                            duplicateWindow = false;
                        }}
                    >
                        Use this window
                    </button>
                </div>
            </div>
        </div>
    {/if}

    {#if timelineWorktree}
        {#key timelineWorktree.path}
            <TimelineDrawer
                worktree={timelineWorktree}
                selectedHash={route.name === 'timeline' ? route.hash : null}
                selectedFrom={route.name === 'timeline' ? route.from : null}
                selectedFiles={route.name === 'timeline' ? route.files : []}
                all={route.name === 'timeline' ? route.all : false}
                onSelect={selectSnapshot}
                onSelectRange={selectRange}
                onToggleAll={toggleAll}
                onSelectFiles={selectFiles}
                onClose={closeTimeline}
            />
        {/key}
    {:else if route.name === 'timeline' && status}
        <div class="route-notice" role="alert">
            <h2>Worktree not found</h2>
            <p class="muted">
                This timeline link points to a worktree that is no longer known. It may have been
                removed or renamed.
            </p>
            <button type="button" class="btn btn--secondary btn--sm" onclick={closeTimeline}>
                Back to the overview
            </button>
        </div>
    {/if}
</div>

<ConfirmDialog />

<style>
    .single-window {
        position: fixed;
        inset: 0;
        z-index: 60;
        display: grid;
        place-items: center;
        padding: 20px;
        background: rgb(0 0 0 / 55%);
    }

    .single-window__card {
        width: min(420px, 100%);
        padding: 18px 20px;
        border: 1px solid var(--line);
        border-radius: var(--radius);
        background: var(--bg-elevated);
        box-shadow: var(--shadow-2);
    }

    .single-window__card h2 {
        margin: 4px 0 6px;
        font-size: 16px;
        font-weight: 650;
    }

    .single-window__card p {
        margin: 0 0 14px;
        font-size: 12.5px;
        line-height: 1.55;
    }

    .single-window__actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        justify-content: flex-end;
    }
</style>
