<script lang="ts">
    import type { Snippet } from 'svelte';
    import Icon from './Icon.svelte';

    interface Props {
        icon?: string;
        title: string;
        text?: string;
        children?: Snippet;
    }

    const { icon = 'info', title, text, children }: Props = $props();
</script>

<div class="empty">
    <span class="empty__icon"><Icon name={icon} size={18} /></span>
    <div class="empty__title">{title}</div>
    {#if text}<p class="empty__text">{text}</p>{/if}
    {#if children}{@render children()}{/if}
</div>
