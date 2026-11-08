<script lang="ts">
    interface Props {
        dirty: boolean;
    }

    const { dirty }: Props = $props();
</script>

<span
    class="dirty-marker"
    class:is-dirty={dirty}
    role={dirty ? 'img' : undefined}
    title={dirty ? 'Uncommitted changes in the worktree' : undefined}
    aria-label={dirty ? 'Uncommitted changes in the worktree' : undefined}
>
    <span class="dirty-marker__dot"></span>
</span>

<style>
    .dirty-marker {
        display: inline-grid;
        flex: none;
        width: 8px;
        height: 8px;
        place-items: center;
    }

    .dirty-marker__dot {
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: var(--warning);
        visibility: hidden;
    }

    .dirty-marker.is-dirty .dirty-marker__dot {
        visibility: visible;
    }
</style>
