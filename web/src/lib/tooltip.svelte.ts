import type { Action } from 'svelte/action';

/** Stable id shared by the rendered tooltip element and `aria-describedby`. */
export const TOOLTIP_ID = 'tachograph-tooltip';

/** Hover tooltips appear after this delay; keyboard focus shows them immediately. */
const HOVER_DELAY = 400;

export const tooltipState = $state<{
    text: string;
    anchor: HTMLElement | null;
    visible: boolean;
}>({ text: '', anchor: null, visible: false });

let timer: ReturnType<typeof setTimeout> | undefined;
let pending: HTMLElement | null = null;
let active: HTMLElement | null = null;
let previousDescribedBy: string | null = null;

function cancelPending(): void {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    pending = null;
}

function restoreDescription(node: HTMLElement): void {
    if (previousDescribedBy === null) node.removeAttribute('aria-describedby');
    else node.setAttribute('aria-describedby', previousDescribedBy);
    previousDescribedBy = null;
}

function describe(node: HTMLElement): void {
    if (active === node) return;
    if (active) restoreDescription(active);
    active = node;
    previousDescribedBy = node.getAttribute('aria-describedby');
    node.setAttribute('aria-describedby', TOOLTIP_ID);
}

function show(node: HTMLElement, text: string): void {
    if (!text.trim()) return;
    cancelPending();
    describe(node);
    tooltipState.text = text;
    tooltipState.anchor = node;
    tooltipState.visible = true;
}

/** Hides the layer. With an anchor, only that trigger's tooltip is dismissed. */
export function hideTooltip(anchor?: HTMLElement): void {
    if (anchor) {
        if (pending === anchor) cancelPending();
        if (active !== anchor) return;
    } else {
        cancelPending();
    }
    if (active) restoreDescription(active);
    active = null;
    tooltipState.visible = false;
    tooltipState.anchor = null;
}

/** Hides the tooltip on pointer, scroll, resize, click and Escape. Returns a cleanup. */
export function dismissOnGlobalEvents(): () => void {
    const hide = (): void => hideTooltip();
    const onKeydown = (event: KeyboardEvent): void => {
        if (event.key === 'Escape') hide();
    };
    window.addEventListener('scroll', hide, true);
    window.addEventListener('resize', hide);
    document.addEventListener('pointerdown', hide, true);
    document.addEventListener('click', hide, true);
    document.addEventListener('keydown', onKeydown);
    return () => {
        window.removeEventListener('scroll', hide, true);
        window.removeEventListener('resize', hide);
        document.removeEventListener('pointerdown', hide, true);
        document.removeEventListener('click', hide, true);
        document.removeEventListener('keydown', onKeydown);
    };
}

/**
 * Attaches the shared tooltip to an element. Native `title` tooltips are
 * removed on mount so the browser never renders a second tooltip: the hint is
 * the layer, the accessible name stays the element's own text or `aria-label`.
 */
export const tooltip: Action<HTMLElement, string> = (node, text) => {
    let value = text;

    function onPointerEnter(event: PointerEvent): void {
        if (event.pointerType === 'touch') return;
        cancelPending();
        pending = node;
        timer = setTimeout(() => {
            timer = undefined;
            if (pending === node) {
                pending = null;
                show(node, value);
            }
        }, HOVER_DELAY);
    }

    function onPointerLeave(): void {
        hideTooltip(node);
    }

    function onFocus(): void {
        if (node.matches(':focus-visible')) show(node, value);
    }

    function onBlur(): void {
        hideTooltip(node);
    }

    node.removeAttribute('title');
    node.addEventListener('pointerenter', onPointerEnter);
    node.addEventListener('pointerleave', onPointerLeave);
    node.addEventListener('focus', onFocus);
    node.addEventListener('blur', onBlur);

    return {
        update(next: string) {
            value = next;
            node.removeAttribute('title');
            if (!next.trim()) hideTooltip(node);
            else if (active === node) tooltipState.text = next;
        },
        destroy() {
            node.removeEventListener('pointerenter', onPointerEnter);
            node.removeEventListener('pointerleave', onPointerLeave);
            node.removeEventListener('focus', onFocus);
            node.removeEventListener('blur', onBlur);
            hideTooltip(node);
        },
    };
};
