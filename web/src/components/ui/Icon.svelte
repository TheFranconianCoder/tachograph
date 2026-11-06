<script lang="ts">
    interface Props {
        name: string;
        size?: number;
        class?: string;
    }

    const { name, size = 16, class: cls = '' }: Props = $props();

    const paths: Record<string, string> = {
        search: 'M8.8 3.2a5.6 5.6 0 1 0 3.9 9.6l3.5 3.5 M8.8 3.2a5.6 5.6 0 0 1 3.9 9.6',
        plus: 'M10 4v12 M4 10h12',
        close: 'M5 5l10 10 M15 5 5 15',
        check: 'M4 10.5 8 14.5 16 5.5',
        copy: 'M7 6.5h9v10H7z M5.5 13.5H4v-10h9v1.5',
        trash: 'M4 5.5h12 M7.5 5.5v-2h5v2 M6 5.5l.7 11h6.6l.7-11',
        refresh: 'M16.5 10a6.5 6.5 0 1 1-1.9-4.6 M16.5 3v4h-4',
        layers:
            'M10 3 3.5 6.5 10 10l6.5-3.5z M3.5 10 10 13.5 16.5 10 M3.5 13.5 10 17l6.5-3.5',
        clock: 'M10 2.8a7.2 7.2 0 1 0 0 14.4 7.2 7.2 0 0 0 0-14.4z M10 5.5V10l3 2',
        warning: 'M10 3 17.5 16H2.5z M10 8v4 M10 14.5h.01',
        info: 'M10 2.8a7.2 7.2 0 1 0 0 14.4 7.2 7.2 0 0 0 0-14.4z M10 9v4 M10 6.5h.01',
        pause: 'M7 4.5v11 M13 4.5v11',
        play: 'M7 4.5 15.5 10 7 15.5z',
        sun: 'M10 6.8a3.2 3.2 0 1 0 0 6.4 3.2 3.2 0 0 0 0-6.4z M10 2v2 M10 16v2 M2 10h2 M16 10h2 M4.3 4.3l1.4 1.4 M14.3 14.3l1.4 1.4 M15.7 4.3l-1.4 1.4 M5.7 14.3l-1.4 1.4',
        moon: 'M16.5 12.3A6.7 6.7 0 0 1 7.7 3.5a6.7 6.7 0 1 0 8.8 8.8z',
        auto: 'M10 2.8a7.2 7.2 0 1 0 0 14.4 7.2 7.2 0 0 0 0-14.4z',
        dot: 'M10 10h.01',
    };

    const filled: Record<string, string> = {
        auto: 'M10 2.8a7.2 7.2 0 0 0 0 14.4z',
    };
</script>

<svg
    class={`ico ${cls}`}
    width={size}
    height={size}
    viewBox="0 0 20 20"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
>
    <path d={paths[name] ?? paths.dot} />
    {#if filled[name]}<path d={filled[name]} fill="currentColor" stroke="none" />{/if}
</svg>

<style>
    .ico {
        flex: none;
        vertical-align: -0.14em;
    }
</style>
