<script lang="ts">
    import Icon from '../ui/Icon.svelte';
    import { cycleTheme, themeIcon, themeLabel } from '../../lib/theme.svelte';
    import type { ConnectionState } from '../../lib/types';

    interface Props {
        connection: ConnectionState;
        connectionTitle: string;
    }

    const { connection, connectionTitle }: Props = $props();
</script>

<header class="topbar">
    <div class="brand">
        <img class="brand-mark" src="/icon.svg" alt="" width="24" height="24" />
        <span class="brand-name">Tachograph</span>
    </div>
    <div class="topbar-actions">
        <span class={`connection connection--${connection}`} title={connectionTitle}>
            <span class="connection__dot"></span>
            {connection}
        </span>
        <button class="icon-btn" title={themeLabel()} aria-label={themeLabel()} onclick={cycleTheme}>
            <Icon name={themeIcon()} size={17} />
        </button>
    </div>
</header>
