<script lang="ts">
    import Icon from './ui/Icon.svelte';
    import { absoluteTime, relativeTime } from '../lib/format';
    import type { ConnectionState, Status } from '../lib/types';

    interface Props {
        status: Status | null;
        connection: ConnectionState;
        connectionTitle: string;
        uptime: string;
        totalSnapshots: number;
        worktreeCount: number;
        projectCount: number;
        rootsCount: number;
        lastSnapshotAt: string;
        lastScanAt: string;
        now: number;
        busy: boolean;
        onToggleRecording: () => void;
        onToggleAutostart: (enabled: boolean) => void;
    }

    const {
        status,
        connection,
        connectionTitle,
        uptime,
        totalSnapshots,
        worktreeCount,
        projectCount,
        rootsCount,
        lastSnapshotAt,
        lastScanAt,
        now,
        busy,
        onToggleRecording,
        onToggleAutostart,
    }: Props = $props();

    const diffLabel = $derived(status?.diffTool === 'difft' ? 'difftastic' : 'git diff');
    const diffTitle = $derived(
        status?.diffTool === 'difft'
            ? 'Structural diff by difftastic'
            : 'Unified diff by git'
    );

    const recorderCounts = $derived.by(() => {
        const counts = { watching: 0, polling: 0, idle: 0 };
        for (const project of status?.projects ?? []) {
            for (const worktree of project.worktrees) {
                const state = worktree.watcher || worktree.state || 'ready';
                if (state === 'watching') counts.watching += 1;
                else if (state === 'polling') counts.polling += 1;
                else if (state === 'idle') counts.idle += 1;
            }
        }
        return counts;
    });

    const connectionClass = $derived(
        connection === 'offline'
            ? 'chip--danger'
            : connection === 'live'
              ? 'chip--ok'
              : 'chip--warn'
    );
</script>

<section class="panel status-panel">
    <div class="panel-head">
        <h2>Status</h2>
        <span class={`chip ${connectionClass}`} title={connectionTitle}>
            <span class="chip-dot"></span>
            {connection}
        </span>
    </div>
    <div class="panel-body">
        <div class="stat-grid">
            <div class="stat">
                <span class="stat-num">{totalSnapshots}</span>
                <span class="stat-label">Snapshots</span>
            </div>
            <div class="stat">
                <span class="stat-num">{worktreeCount}</span>
                <span class="stat-label">Worktrees</span>
            </div>
            <div class="stat">
                <span class="stat-num">{projectCount}</span>
                <span class="stat-label">Projects</span>
            </div>
            <div class="stat">
                <span class="stat-num stat-num--time">
                    {lastSnapshotAt ? relativeTime(lastSnapshotAt, now) : '—'}
                </span>
                <span class="stat-label">Last snapshot</span>
            </div>
        </div>

        <div class="state-strip" aria-label="Recorder states">
            <span class="state-pill" title="watchexec is active for these worktrees">
                <span class="state-dot state-dot--watching"></span>
                <span class="state-count">{recorderCounts.watching}</span>
                <span class="state-name">Watching</span>
            </span>
            <span class="state-pill" title="These worktrees are checked by polling">
                <span class="state-dot state-dot--polling"></span>
                <span class="state-count">{recorderCounts.polling}</span>
                <span class="state-name">Polling</span>
            </span>
            <span class="state-pill" title="Hibernated worktrees poll at the maximum interval">
                <span class="state-dot state-dot--idle"></span>
                <span class="state-count">{recorderCounts.idle}</span>
                <span class="state-name">Idle</span>
            </span>
        </div>

        <details class="details-block">
            <summary class="details-summary">
                <span>Details</span>
                <Icon name="chevron" size={13} class="panel-caret" />
            </summary>
            <div class="kv-list">
                <div class="kv"><span class="k">Version</span><span class="v">{status?.version ?? '—'}</span></div>
                <div class="kv"><span class="k">Uptime</span><span class="v">{uptime}</span></div>
                <div class="kv">
                    <span class="k">Recording</span>
                    <span class="v" class:off={!status?.recording}>
                        {status?.paused ? 'paused' : status?.recording ? 'on' : 'off'}
                    </span>
                </div>
                <div class="kv">
                    <span class="k">Watcher</span>
                    <span
                        class="v"
                        title={status?.watcherEngine === 'watchexec'
                            ? 'watchexec events, polling as fallback'
                            : 'polling only (watchexec not available)'}
                    >
                        {status?.watcherEngine === 'watchexec' ? 'watchexec' : 'poll'}
                    </span>
                </div>
                <div class="kv">
                    <span class="k">Diff</span>
                    <span class="v" title={diffTitle}>{diffLabel}</span>
                </div>
                <div class="kv"><span class="k">Roots</span><span class="v">{rootsCount}</span></div>
                <div class="kv">
                    <span class="k">Last scan</span>
                    <span class="v" title={absoluteTime(lastScanAt)}>
                        {lastScanAt ? relativeTime(lastScanAt, now) : 'never'}
                    </span>
                </div>
            </div>
        </details>

        <button
            type="button"
            class="btn btn--secondary status-action"
            onclick={onToggleRecording}
            disabled={busy || !status}
        >
            <Icon name={status?.paused ? 'play' : 'pause'} size={15} />
            {status?.paused ? 'Resume recording' : 'Pause recording'}
        </button>

        <label class="autostart">
            <input
                type="checkbox"
                checked={status?.autostart ?? false}
                disabled={busy || !status}
                onchange={event => onToggleAutostart(event.currentTarget.checked)}
            />
            <span>Start at login</span>
            <span class="autostart-note">without opening the UI</span>
        </label>
    </div>
</section>

<style>
    .details-block {
        margin-top: 13px;
    }

    .details-summary {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 2px 0;
        list-style: none;
        color: var(--muted);
        font-size: 11.5px;
        font-weight: 550;
        cursor: pointer;
        user-select: none;
    }

    .details-summary::-webkit-details-marker {
        display: none;
    }

    .details-summary::marker {
        content: '';
    }

    .details-summary:hover {
        color: var(--ink);
    }

    .details-block[open] .details-summary {
        margin-bottom: 4px;
    }

    .state-strip {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 8px;
        margin-top: 8px;
    }

    .state-pill {
        display: flex;
        gap: 6px;
        align-items: center;
        min-width: 0;
        padding: 6px 9px;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        background: var(--bg-elevated);
        white-space: nowrap;
    }

    .state-dot {
        width: 6px;
        height: 6px;
        flex: none;
        border-radius: 50%;
        background: currentColor;
    }

    .state-dot--watching {
        color: var(--success);
    }

    .state-dot--polling {
        color: var(--info);
    }

    .state-dot--idle {
        color: var(--faint);
    }

    .state-count {
        color: var(--ink);
        font-size: 13px;
        font-weight: 650;
        font-variant-numeric: tabular-nums;
    }

    .autostart {
        display: flex;
        gap: 7px;
        align-items: center;
        margin-top: 11px;
        color: var(--muted);
        font-size: 11.5px;
        cursor: pointer;
    }

    .autostart input {
        width: 14px;
        height: 14px;
        accent-color: var(--accent);
    }

    .autostart-note {
        color: var(--faint);
        font-size: 10.5px;
    }

    .state-name {
        overflow: hidden;
        color: var(--faint);
        font-size: 9.5px;
        font-weight: 550;
        letter-spacing: 0.06em;
        text-overflow: ellipsis;
        text-transform: uppercase;
    }
</style>
