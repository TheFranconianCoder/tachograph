<script lang="ts">
    import { absoluteTime, baseName, relativeTime, shortHash } from '../lib/format';
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
                {@const detail = event.message || (event.hash ? shortHash(event.hash) : '')}
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
                    <td class="mono truncate" title={path ?? ''}>
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
                    </td>
                    <td class="mono truncate" title={event.message ?? event.hash ?? ''}>
                        {detail || '—'}
                    </td>
                </tr>
            {/each}
        </tbody>
    </table>
</div>
