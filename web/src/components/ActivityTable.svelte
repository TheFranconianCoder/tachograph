<script lang="ts">
    import {
        absoluteTime,
        baseName,
        changeTitle,
        formatCount,
        relativeTime,
        shortHash,
    } from '../lib/format';
    import type { Activity } from '../lib/types';

    interface Props {
        activities: Activity[];
        now: number;
        worktreeIDs: Map<string, string>;
        onOpenTimeline: (path: string) => void;
    }

    const { activities, now, worktreeIDs, onOpenTimeline }: Props = $props();
</script>

<div class="activity-table-wrap">
    <table class="activity-table">
        <colgroup>
            <col class="activity-col-time" />
            <col class="activity-col-kind" />
            <col class="activity-col-worktree" />
            <col class="activity-col-details" />
        </colgroup>
        <thead>
            <tr>
                <th scope="col">When</th>
                <th scope="col">Event</th>
                <th scope="col">Worktree</th>
                <th scope="col">Details</th>
            </tr>
        </thead>
        <tbody>
            {#each activities.slice(0, 10) as event (event.path ?? event.time)}
                {@const path = event.path}
                {@const hash = event.hash ? shortHash(event.hash) : ''}
                {@const detailTitle =
                    event.message ||
                    [event.stats ? changeTitle(event.stats) : '', event.hash]
                        .filter(Boolean)
                        .join(' · ')}
                <tr>
                    <td class="mono muted" title={absoluteTime(event.time)}>
                        {relativeTime(event.time, now)}
                    </td>
                    <td>
                        <span class={`event event--${event.kind}`}>
                            <span class="event-dot"></span>
                            {event.kind}
                        </span>
                    </td>
                    <td colspan="2" class="activity-main-cell">
                        <div class="activity-main">
                            <span class="activity-worktree-cell mono truncate" title={path ?? ''}>
                                {#if path && worktreeIDs.has(path)}
                                    <button
                                        type="button"
                                        class="activity-worktree"
                                        title={`Open history for ${path}`}
                                        onclick={() => onOpenTimeline(path)}
                                    >
                                        {baseName(path)}
                                    </button>
                                {:else}
                                    {path ? baseName(path) : '—'}
                                {/if}
                            </span>
                            <span class="activity-details mono" title={detailTitle}>
                                {#if event.message}
                                    {event.message}
                                {:else if hash || event.stats}
                                    {#if event.stats}
                                        <span class="activity-added">+{formatCount(event.stats.added)}</span>
                                        <span class="activity-sep"> </span>
                                        <span class="activity-deleted">-{formatCount(event.stats.deleted)}</span>
                                    {/if}
                                    {#if event.stats && hash}<span class="activity-sep"> · </span>{/if}
                                    {#if hash}{hash}{/if}
                                {:else}
                                    —
                                {/if}
                            </span>
                        </div>
                    </td>
                </tr>
            {/each}
        </tbody>
    </table>
</div>
