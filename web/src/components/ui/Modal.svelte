<script lang="ts">
    import { tick, type Snippet } from 'svelte';
    import Icon from './Icon.svelte';

    interface Props {
        open: boolean;
        title?: string;
        description?: string;
        width?: string;
        onClose: () => void;
        children?: Snippet;
        footer?: Snippet;
    }

    const { open, title, description, width = '460px', onClose, children, footer }: Props = $props();

    let panel: HTMLDivElement | undefined = $state();
    let previousFocus: HTMLElement | null = null;

    $effect(() => {
        if (!open) return;
        previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        void tick().then(() => panel?.focus());

        const onKey = (event: KeyboardEvent): void => {
            if (!open) return;
            if (event.key === 'Escape') {
                event.preventDefault();
                onClose();
                return;
            }
            if (event.key !== 'Tab' || !panel) return;
            const focusable = panel.querySelectorAll<HTMLElement>(
                'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
            );
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        };

        window.addEventListener('keydown', onKey);
        return () => {
            window.removeEventListener('keydown', onKey);
            previousFocus?.focus?.();
        };
    });
</script>

{#if open}
    <div class="overlay">
        <button class="overlay-bg" aria-label="Close" onclick={onClose}></button>
        <div
            class="dialog"
            bind:this={panel}
            role="dialog"
            aria-modal="true"
            aria-label={title ?? 'Dialog'}
            tabindex="-1"
            style="width:min({width}, calc(100vw - 32px))"
        >
            {#if title || description}
                <header class="dialog-head">
                    <div>
                        {#if title}<h2>{title}</h2>{/if}
                        {#if description}<p>{description}</p>{/if}
                    </div>
                    <button type="button" class="icon-btn" title="Close" onclick={onClose}>
                        <Icon name="close" />
                    </button>
                </header>
            {/if}
            <div class="dialog-body">
                {#if children}{@render children()}{/if}
            </div>
            {#if footer}<footer class="dialog-foot">{@render footer()}</footer>{/if}
        </div>
    </div>
{/if}

<style>
    .overlay {
        position: fixed;
        inset: 0;
        z-index: 60;
        display: grid;
        place-items: center;
        padding: 16px;
    }

    .overlay-bg {
        position: absolute;
        inset: 0;
        width: 100%;
        height: 100%;
        border: 0;
        background: rgb(0 0 0 / 38%);
        backdrop-filter: blur(3px);
    }

    .dialog {
        position: relative;
        z-index: 1;
        display: flex;
        flex-direction: column;
        max-height: calc(100dvh - 40px);
        border: 1px solid var(--line);
        border-radius: var(--radius);
        background: var(--bg-elevated);
        box-shadow: var(--shadow-2);
        outline: none;
        animation: dialog-in 0.17s var(--ease);
    }

    .dialog-head {
        display: flex;
        align-items: flex-start;
        gap: 12px;
        padding: 17px 17px 10px 19px;
    }

    .dialog-head h2 {
        margin: 0;
        font-size: 16px;
        font-weight: 650;
        letter-spacing: -0.015em;
    }

    .dialog-head p {
        margin: 4px 0 0;
        color: var(--muted);
        font-size: 12.5px;
        line-height: 1.55;
    }

    .dialog-head .icon-btn {
        margin: -4px -5px 0 auto;
    }

    .dialog-body {
        min-height: 0;
        overflow: auto;
        padding: 4px 19px 18px;
    }

    .dialog-foot {
        display: flex;
        align-items: center;
        gap: 7px;
        padding: 12px 17px;
        border-top: 1px solid var(--line);
    }

    @keyframes dialog-in {
        from {
            opacity: 0;
            transform: translateY(6px) scale(0.99);
        }
    }
</style>
