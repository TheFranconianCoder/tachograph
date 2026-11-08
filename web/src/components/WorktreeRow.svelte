<script lang="ts">
    import Icon from './ui/Icon.svelte';
    import Lamp from './ui/Lamp.svelte';
    import DirtyMarker from './ui/DirtyMarker.svelte';
    import {
        baseName,
        changeTitle,
        formatCount,
        lifecycleTitle,
        progressTitle,
        relativeTime,
        watcherTitle,
    } from '../lib/format';
    import type { Worktree } from '../lib/types';

    interface Props {
        worktree: Worktree;
        now: number;
        copiedPath: string | null;
        copyPath: (path: string) => void;
        onOpenTimeline: (worktree: Worktree) => void;
    }

    const { worktree, now, copiedPath, copyPath, onOpenTimeline }: Props = $props();

    const state = $derived(worktree.watcher || worktree.state || 'ready');
    const lifecycle = $derived(
        worktree.setupState === 'error'
            ? 'error'
            : worktree.setupState === 'ok'
              ? 'ok'
              : 'none'
    );
    const snaps = $derived(worktree.snapshots ?? 0);
</script>

<li class="worktree-row">
    <DirtyMarker dirty={worktree.dirty ?? false} />
    <div class="worktree-row__main">
        <span class="worktree-row__name truncate" title={worktree.path}>
            {baseName(worktree.path)}
        </span>
        <span class="worktree-row__branch mono truncate" title={worktree.branch}>
            {worktree.branch}
        </span>
        <button
            type="button"
            class="icon-btn icon-btn--inline"
            class:copied={copiedPath === worktree.path}
            title={copiedPath === worktree.path ? 'Copied' : 'Copy worktree path'}
            aria-label="Copy worktree path"
            onclick={() => copyPath(worktree.path)}
        >
            <Icon name={copiedPath === worktree.path ? 'check' : 'copy'} size={13} />
        </button>
    </div>

    <span class="worktree-row__meta mono faint" title={progressTitle(worktree)}>
        {#if worktree.stats}
            <span class="worktree-row__stats" title={changeTitle(worktree.stats)}>
                <span class="activity-added">+{formatCount(worktree.stats.added)}</span>
                <span class="activity-deleted">-{formatCount(worktree.stats.deleted)}</span>
            </span>
            <span class="activity-sep">·</span>
        {/if}
        {snaps} {snaps === 1 ? 'snap' : 'snaps'}
        {#if worktree.lastSnapshot}
            · {relativeTime(worktree.lastSnapshot, now)}
        {/if}
        <button
            type="button"
            class="icon-btn icon-btn--inline"
            title={worktree.id ? 'Open timeline' : 'Timeline unavailable'}
            aria-label="Open timeline"
            disabled={!worktree.id}
            onclick={() => onOpenTimeline(worktree)}
        >
            <Icon name="clock" size={13} />
        </button>
    </span>

    <Lamp state={lifecycle} title={lifecycleTitle(worktree)} />

    <span class={`worktree-state worktree-state--${state}`} title={watcherTitle(worktree)}>
        <span class="worktree-state__dot"></span>
        {state}
    </span>
</li>
