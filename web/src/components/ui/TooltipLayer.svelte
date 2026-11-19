<script lang="ts">
    import { onMount } from 'svelte';

    import { dismissOnGlobalEvents, TOOLTIP_ID, tooltipState } from '../../lib/tooltip.svelte';

    const GAP = 8;
    const MARGIN = 8;

    let node = $state<HTMLDivElement | null>(null);
    let top = $state(0);
    let left = $state(0);
    let above = $state(true);
    let arrowX = $state(0);

    onMount(dismissOnGlobalEvents);

    // Measure after every content change so long text wraps and stays inside
    // the viewport, flipping below when there is not enough room above.
    $effect(() => {
        const text = tooltipState.text;
        const anchor = tooltipState.anchor;
        const visible = tooltipState.visible;
        if (!visible || !anchor || !node || !text) return;

        const vw = window.innerWidth;
        const vh = window.innerHeight;
        const rect = anchor.getBoundingClientRect();
        const width = node.offsetWidth;
        const height = node.offsetHeight;

        const fitsAbove = rect.top - height - GAP >= MARGIN;
        const fitsBelow = vh - rect.bottom - height - GAP >= MARGIN;
        const placeAbove = fitsAbove || !fitsBelow;

        const anchorCenter = rect.left + rect.width / 2;
        const maxLeft = Math.max(MARGIN, vw - width - MARGIN);
        const maxTop = Math.max(MARGIN, vh - height - MARGIN);
        const nextLeft = Math.min(Math.max(anchorCenter - width / 2, MARGIN), maxLeft);
        const nextTop = Math.min(
            Math.max(placeAbove ? rect.top - height - GAP : rect.bottom + GAP, MARGIN),
            maxTop
        );

        above = placeAbove;
        left = nextLeft;
        top = nextTop;
        arrowX = Math.min(Math.max(anchorCenter - nextLeft, 12), Math.max(12, width - 12));
    });
</script>

<div
    bind:this={node}
    id={TOOLTIP_ID}
    class="tooltip"
    class:is-visible={tooltipState.visible}
    class:is-above={above}
    class:is-below={!above}
    role="tooltip"
    style={`top: ${top}px; left: ${left}px; --tooltip-arrow-x: ${arrowX}px`}
>
    {tooltipState.text}
</div>
