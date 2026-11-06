<script lang="ts">
    import { absoluteTime, baseName, relativeTime } from '../lib/format';
    import type { Activity } from '../lib/types';

    interface Props {
        activities: Activity[];
        now: number;
    }

    const { activities, now }: Props = $props();
</script>

<div class="activity-table-wrap">
    <table class="activity-table">
        <colgroup>
            <col class="activity-col-time" />
            <col class="activity-col-kind" />
            <col class="activity-col-worktree" />
            <col />
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
                    <td class="mono truncate" title={event.path ?? ''}>
                        {event.path ? baseName(event.path) : '—'}
                    </td>
                    <td class="mono truncate" title={event.message ?? event.hash ?? ''}>
                        {event.message || event.hash || '—'}
                    </td>
                </tr>
            {/each}
        </tbody>
    </table>
</div>
