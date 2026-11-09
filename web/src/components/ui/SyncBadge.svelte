<script lang="ts">
    interface Props {
        ahead: number;
        behind: number;
        upstream?: string;
    }

    const { ahead, behind, upstream }: Props = $props();

    const title = $derived.by(() => {
        const parts: string[] = [];
        if (ahead > 0) parts.push(`${ahead} ${ahead === 1 ? 'commit' : 'commits'} to push`);
        if (behind > 0) parts.push(`${behind} ${behind === 1 ? 'commit' : 'commits'} to pull`);
        return upstream ? `${parts.join(' · ')} (${upstream})` : parts.join(' · ');
    });
</script>

{#if ahead > 0 || behind > 0}
    <span class="sync-badge mono" role="img" title={title} aria-label={title}>
        {#if ahead > 0}<span>↑{ahead}</span>{/if}
        {#if behind > 0}<span>↓{behind}</span>{/if}
    </span>
{/if}

<style>
    .sync-badge {
        display: inline-flex;
        align-items: baseline;
        flex: none;
        gap: 5px;
        color: var(--warning);
        font-size: 10.5px;
        font-variant-numeric: tabular-nums;
        white-space: nowrap;
    }
</style>
