<script lang="ts">
    import Icon from './ui/Icon.svelte';

    interface Props {
        roots: string[];
        patterns: string[];
        busy: boolean;
        scanning: boolean;
        rootInput: string;
        patternInput: string;
        actionError: string | null;
        scanError: string | null;
        onAdd: () => void;
        onRemove: (path: string) => void;
        onAddPattern: () => void;
        onRemovePattern: (pattern: string) => void;
        onScan: () => void;
    }

    let {
        roots,
        patterns,
        busy,
        scanning,
        rootInput = $bindable(''),
        patternInput = $bindable(''),
        actionError,
        scanError,
        onAdd,
        onRemove,
        onAddPattern,
        onRemovePattern,
        onScan,
    }: Props = $props();

    function submit(event: SubmitEvent): void {
        event.preventDefault();
        onAdd();
    }

    function submitPattern(event: SubmitEvent): void {
        event.preventDefault();
        onAddPattern();
    }
</script>

<details class="panel panel--collapsible">
    <summary class="panel-head">
        <span class="panel-title">Roots</span>
        <span class="count">{roots.length}</span>
        <Icon name="chevron" size={14} class="panel-caret" />
    </summary>
    <div class="panel-body">
        {#if roots.length === 0}
            <p class="muted root-hint">
                No roots configured. A root is a directory Tachograph scans for repositories and
                worktrees.
            </p>
        {:else}
            <ul class="root-list">
                {#each roots as root (root)}
                    <li class="row">
                        <span class="row__main">
                            <span class="row__title mono truncate" title={root}>{root}</span>
                        </span>
                        <span class="row__actions">
                            <button
                                type="button"
                                class="icon-btn"
                                title="Remove root"
                                aria-label="Remove root"
                                onclick={() => onRemove(root)}
                                disabled={busy}
                            >
                                <Icon name="trash" size={15} />
                            </button>
                        </span>
                    </li>
                {/each}
            </ul>
        {/if}

        <form class="root-form" onsubmit={submit}>
            <input
                class="input"
                type="text"
                placeholder="/home/you/Code"
                bind:value={rootInput}
                disabled={busy}
                aria-label="Root path"
            />
            <button type="submit" class="btn btn--primary btn--sm" disabled={busy || !rootInput.trim()}>
                <Icon name="plus" size={14} />
                Add
            </button>
        </form>

        <div class="root-actions">
            <button
                type="button"
                class="btn btn--ghost btn--sm"
                onclick={onScan}
                disabled={busy || scanning}
            >
                <Icon name="refresh" size={14} />
                {scanning ? 'Scanning…' : 'Scan now'}
            </button>
        </div>

        <div class="pattern-block">
            <div class="pattern-head">
                <h3>Worktree patterns</h3>
                <span class="count">{patterns.length}</span>
            </div>
            <p class="muted pattern-hint">
                Globs for worktrees outside the roots, e.g. <code>~/worktrees/*</code>.
            </p>
            {#if patterns.length > 0}
                <ul class="root-list pattern-list">
                    {#each patterns as pattern (pattern)}
                        <li class="row">
                            <span class="row__main">
                                <span class="row__title mono truncate" title={pattern}>
                                    {pattern}
                                </span>
                            </span>
                            <span class="row__actions">
                                <button
                                    type="button"
                                    class="icon-btn"
                                    title="Remove pattern"
                                    aria-label="Remove pattern"
                                    onclick={() => onRemovePattern(pattern)}
                                    disabled={busy}
                                >
                                    <Icon name="trash" size={15} />
                                </button>
                            </span>
                        </li>
                    {/each}
                </ul>
            {/if}
            <form class="root-form" onsubmit={submitPattern}>
                <input
                    class="input"
                    type="text"
                    placeholder="~/worktrees/*"
                    bind:value={patternInput}
                    disabled={busy}
                    aria-label="Worktree pattern"
                />
                <button
                    type="submit"
                    class="btn btn--primary btn--sm"
                    disabled={busy || !patternInput.trim()}
                >
                    <Icon name="plus" size={14} />
                    Add
                </button>
            </form>
        </div>

        {#if actionError}
            <p class="notice notice--error root-notice">{actionError}</p>
        {/if}
        {#if scanError}
            <p class="notice notice--error root-notice">{scanError}</p>
        {/if}
    </div>
</details>

<style>
    .pattern-block {
        margin-top: 14px;
        padding-top: 12px;
        border-top: 1px solid var(--line);
    }

    .pattern-head {
        display: flex;
        gap: 8px;
        align-items: baseline;
        justify-content: space-between;
    }

    .pattern-head h3 {
        margin: 0;
        font-size: 12px;
        font-weight: 650;
        letter-spacing: 0.02em;
    }

    .pattern-hint {
        margin: 2px 0 8px;
        font-size: 11px;
        line-height: 1.45;
    }

    .pattern-hint code {
        font-family: var(--font-mono);
        font-size: 10.5px;
    }

    .pattern-list {
        margin-bottom: 8px;
    }
</style>
