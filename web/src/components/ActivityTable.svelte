<script lang="ts">
    import {
        absoluteTime,
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
        worktreeProjects: Map<string, string>;
        worktreeBranches: Map<string, string>;
        onOpenTimeline: (path: string) => void;
    }

    const {
        activities,
        now,
        worktreeIDs,
        worktreeProjects,
        worktreeBranches,
        onOpenTimeline,
    }: Props = $props();

    function openEventTimeline(path: string | undefined): void {
        if (path && worktreeIDs.has(path)) onOpenTimeline(path);
    }
</script>

<div class="activity-table-wrap">
    <table class="activity-table">
        <colgroup>
            <col class="activity-col-time" />
            <col class="activity-col-kind" />
            <col class="activity-col-project" />
            <col class="activity-col-branch" />
            <col class="activity-col-details" />
        </colgroup>
        <thead>
            <tr>
                <th scope="col">When</th>
                <th scope="col">Event</th>
                <th scope="col">Project</th>
                <th scope="col">Branch</th>
                <th scope="col">Details</th>
            </tr>
        </thead>
        <tbody>
            {#each activities.slice(0, 10) as event (event.path ?? event.time)}
                {@const path = event.path}
                {@const project = path ? worktreeProjects.get(path) : undefined}
                {@const branch = path ? worktreeBranches.get(path) : undefined}
                {@const hash = event.hash ? shortHash(event.hash) : ''}
                {@const detailTitle =
                    event.message ||
                    [event.stats ? changeTitle(event.stats) : '', event.hash]
                        .filter(Boolean)
                        .join(' · ')}
                <!-- The branch button provides keyboard access; the row click is
                     a mouse convenience for the whole event. -->
                <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
                <tr
                    class:clickable={path !== undefined && worktreeIDs.has(path)}
                    onclick={() => openEventTimeline(path)}
                >
                    <td class="mono muted" title={absoluteTime(event.time)}>
                        {relativeTime(event.time, now)}
                    </td>
                    <td>
                        <span class={`event event--${event.kind}`}>
                            <span class="event-dot"></span>
                            {event.kind}
                        </span>
                    </td>
                    <td class="mono truncate" title={project ?? ''}>
                        {project ?? '—'}
                    </td>
                    <td colspan="2" class="activity-main-cell">
                        <div class="activity-main">
                            <span class="activity-branch-cell mono truncate" title={path ?? ''}>
                                {#if path && worktreeIDs.has(path)}
                                    <button
                                        type="button"
                                        class="activity-branch"
                                        title={`Open history for ${path}`}
                                        onclick={event => {
                                            event.stopPropagation();
                                            onOpenTimeline(path);
                                        }}
                                    >
                                        {branch ?? '—'}
                                    </button>
                                {:else}
                                    {branch ?? '—'}
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
