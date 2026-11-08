<script lang="ts">
    import Icon from './ui/Icon.svelte';
    import Lamp from './ui/Lamp.svelte';
    import DirtyMarker from './ui/DirtyMarker.svelte';
    import WorktreeRow from './WorktreeRow.svelte';
    import { lifecycleTitle, progressTitle, relativeTime, watcherTitle } from '../lib/format';
    import type { Project, Worktree } from '../lib/types';

    interface Props {
        project: Project;
        now: number;
        copiedPath: string | null;
        copyPath: (path: string) => void;
        onOpenTimeline: (worktree: Worktree) => void;
    }

    const { project, now, copiedPath, copyPath, onOpenTimeline }: Props = $props();

    // The project header IS the main worktree row. Linked worktrees follow as
    // their own rows, so the main directory does not appear twice.
    const main = $derived(
        project.worktrees.find(worktree => worktree.path === project.mainWorktree) ??
            project.worktrees[0] ??
            null
    );
    const linked = $derived(
        main
            ? project.worktrees.filter(worktree => worktree.path !== main.path)
            : project.worktrees
    );

    const state = $derived(main ? main.watcher || main.state || 'ready' : 'ready');
    const lifecycle = $derived(
        !main || main.setupState === 'none' || !main.setupState
            ? 'none'
            : main.setupState === 'error'
              ? 'error'
              : 'ok'
    );
    const snaps = $derived(main?.snapshots ?? 0);
</script>

<section class="panel project-panel">
    <div class="panel-head project-head" class:project-head--only={linked.length === 0}>
        <DirtyMarker dirty={main?.dirty ?? false} />
        <div class="worktree-row__main">
            <h2
                class="truncate"
                title={main ? `${main.path} · ${project.worktrees.length} worktrees` : project.name}
            >
                {project.name}
            </h2>
            {#if main}
                <span class="worktree-row__branch mono truncate" title={main.branch}>
                    {main.branch}
                </span>
                <button
                    type="button"
                    class="icon-btn icon-btn--inline"
                    class:copied={copiedPath === main.path}
                    title={copiedPath === main.path ? 'Copied' : 'Copy worktree path'}
                    aria-label="Copy worktree path"
                    onclick={() => copyPath(main.path)}
                >
                    <Icon name={copiedPath === main.path ? 'check' : 'copy'} size={13} />
                </button>
            {/if}
            {#if project.historyRepo}
                <button
                    type="button"
                    class="icon-btn icon-btn--inline"
                    class:copied={copiedPath === project.historyRepo}
                    title={copiedPath === project.historyRepo
                        ? 'Copied'
                        : 'Copy history repository path'}
                    aria-label="Copy history repository path"
                    onclick={() => copyPath(project.historyRepo!)}
                >
                    <Icon name={copiedPath === project.historyRepo ? 'check' : 'layers'} size={13} />
                </button>
            {/if}
        </div>

        {#if main}
            <span class="worktree-row__meta mono faint" title={progressTitle(main)}>
                {snaps} {snaps === 1 ? 'snap' : 'snaps'}
                {#if main.lastSnapshot}
                    · {relativeTime(main.lastSnapshot, now)}
                {/if}
                <button
                    type="button"
                    class="icon-btn icon-btn--inline"
                    title={main.id ? 'Open timeline' : 'Timeline unavailable'}
                    aria-label="Open timeline"
                    disabled={!main.id}
                    onclick={() => onOpenTimeline(main)}
                >
                    <Icon name="clock" size={13} />
                </button>
            </span>

            <Lamp state={lifecycle} title={lifecycleTitle(main)} />

            <span class={`worktree-state worktree-state--${state}`} title={watcherTitle(main)}>
                <span class="worktree-state__dot"></span>
                {state}
            </span>
        {:else}
            <span class="project-head__meta mono faint">no worktrees</span>
        {/if}
    </div>

    {#if linked.length > 0}
        <ul class="worktree-list">
            {#each linked as worktree (worktree.path)}
                <WorktreeRow {worktree} {now} {copiedPath} {copyPath} {onOpenTimeline} />
            {/each}
        </ul>
    {/if}
</section>
