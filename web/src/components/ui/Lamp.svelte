<script lang="ts">
    type LampState = 'ok' | 'error' | 'none' | 'watching' | 'polling' | 'idle' | 'paused';

    interface Props {
        state: LampState;
        title: string;
    }

    const { state, title }: Props = $props();
</script>

<span class={`lamp lamp--${state}`} role="img" title={title} aria-label={title}>
    <span class="lamp-core"></span>
</span>

<style>
    .lamp {
        --ring: var(--faint);
        display: inline-grid;
        flex: none;
        width: 14px;
        height: 14px;
        place-items: center;
        border-radius: 50%;
        background: color-mix(in srgb, var(--ring) 12%, transparent);
        color: var(--ring);
    }

    .lamp-core {
        width: 6px;
        height: 6px;
        border: 1.5px solid var(--ring);
        border-radius: 50%;
        background: var(--bg-elevated);
    }

    .lamp--ok,
    .lamp--watching {
        --ring: var(--success);
    }

    .lamp--watching .lamp-core {
        background: var(--ring);
        animation: breathe 1.6s var(--ease) infinite;
    }

    .lamp--polling {
        --ring: var(--info);
    }

    .lamp--idle,
    .lamp--none {
        --ring: var(--line-strong);
    }

    .lamp--paused {
        --ring: var(--warning);
    }

    .lamp--error {
        --ring: var(--danger);
    }

    @keyframes breathe {
        50% {
            opacity: 0.35;
        }
    }

    @media (prefers-reduced-motion: reduce) {
        .lamp--watching .lamp-core {
            animation: none;
        }
    }
</style>
