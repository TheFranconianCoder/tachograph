<script lang="ts">
    import Icon from './ui/Icon.svelte';
    import type { WorktreeSetup } from '../lib/types';

    interface Props {
        setup: WorktreeSetup | null;
        onSave: (copy: string[], overwrite: boolean) => Promise<void>;
        onApply: () => Promise<{ worktrees: number; copied: number; errors: string[] }>;
    }

    const { setup, onSave, onApply }: Props = $props();

    const defaults = ['.env', '.env.local', '.env.*.local'];

    let patterns = $state<string[]>([...defaults]);
    let overwrite = $state(false);
    let dirty = $state(false);
    let saving = $state(false);
    let applying = $state(false);
    let error = $state<string | null>(null);
    let message = $state<string | null>(null);
    let lastSignature = '';

    const runRules = $derived(setup?.run ?? []);

    $effect(() => {
        const signature = JSON.stringify(setup ?? null);
        if (signature === lastSignature) return;
        lastSignature = signature;
        if (dirty) return;
        if (setup) {
            patterns = setup.copy.map(rule => rule.path);
            overwrite = setup.copy.some(rule => rule.overwrite);
        } else {
            patterns = [...defaults];
            overwrite = false;
        }
    });

    function markDirty(): void {
        dirty = true;
        error = null;
        message = null;
    }

    function addPattern(): void {
        patterns = [...patterns, ''];
        markDirty();
    }

    function removePattern(index: number): void {
        patterns = patterns.filter((_, position) => position !== index);
        markDirty();
    }

    async function save(): Promise<void> {
        if (saving || applying) return;
        saving = true;
        error = null;
        message = null;
        try {
            const cleaned = Array.from(
                new Set(
                    patterns
                        .map(pattern => pattern.trim())
                        .filter(pattern => pattern.length > 0)
                )
            );
            await onSave(cleaned, overwrite);
            patterns = cleaned;
            dirty = false;
            message = 'Saved. New worktrees use these copy rules.';
        } catch (cause) {
            error = cause instanceof Error ? cause.message : 'could not save the copy rules';
        } finally {
            saving = false;
        }
    }

    async function apply(): Promise<void> {
        if (saving || applying || dirty) return;
        applying = true;
        error = null;
        message = null;
        try {
            const result = await onApply();
            const files = `${result.copied} file${result.copied === 1 ? '' : 's'}`;
            const worktrees = `${result.worktrees} worktree${result.worktrees === 1 ? '' : 's'}`;
            message = `Copied ${files} into ${worktrees}.`;
            if (result.errors.length > 0) {
                error = `${result.errors.length} worktree(s) failed: ${result.errors[0]}`;
            }
        } catch (cause) {
            error = cause instanceof Error ? cause.message : 'could not apply the copy rules';
        } finally {
            applying = false;
        }
    }
</script>

<details class="panel panel--collapsible">
    <summary class="panel-head">
        <span class="panel-title">Worktree setup</span>
        <span class="count">{patterns.length} copies</span>
        <Icon name="chevron" size={14} class="panel-caret" />
    </summary>
    <div class="panel-body">
        <p class="muted setup-hint">
            Files copied from the main worktree into every new worktree. Patterns are relative and
            support <code>*</code>, <code>?</code> and <code>[]</code>; a pattern without a match is
            skipped.
        </p>

        <ul class="setup-list">
            {#each patterns as _, index (index)}
                <li class="setup-row">
                    <input
                        class="input mono"
                        type="text"
                        placeholder=".env or config/*.local"
                        bind:value={patterns[index]}
                        oninput={markDirty}
                        disabled={saving || applying}
                        aria-label={`Copy pattern ${index + 1}`}
                    />
                    <button
                        type="button"
                        class="icon-btn"
                        title="Remove pattern"
                        aria-label="Remove pattern"
                        onclick={() => removePattern(index)}
                        disabled={saving || applying}
                    >
                        <Icon name="trash" size={14} />
                    </button>
                </li>
            {/each}
        </ul>

        {#if patterns.length === 0}
            <p class="muted setup-empty">No files are copied into new worktrees.</p>
        {/if}

        <div class="setup-actions">
            <button
                type="button"
                class="btn btn--ghost btn--sm"
                onclick={addPattern}
                disabled={saving || applying}
            >
                <Icon name="plus" size={14} />
                Add pattern
            </button>
            <label class="setup-check">
                <input
                    type="checkbox"
                    bind:checked={overwrite}
                    onchange={markDirty}
                    disabled={saving || applying}
                />
                Overwrite existing files
            </label>
            <span class="setup-spacer"></span>
            <button
                type="button"
                class="btn btn--ghost btn--sm"
                onclick={apply}
                disabled={saving || applying || dirty}
                title={dirty
                    ? 'Save the copy rules first'
                    : 'Copy missing files into existing worktrees'}
            >
                {applying ? 'Applying…' : 'Apply to existing'}
            </button>
            <button
                type="button"
                class="btn btn--primary btn--sm"
                onclick={save}
                disabled={saving || applying || !dirty}
            >
                {saving ? 'Saving…' : 'Save'}
            </button>
        </div>

        {#if message}
            <p class="notice setup-notice">{message}</p>
        {/if}
        {#if error}
            <p class="notice notice--error setup-notice">{error}</p>
        {/if}

        {#if runRules.length > 0}
            <p class="muted setup-run">
                Setup commands stay in <span class="mono">config.yaml</span>:
                <span class="mono">{runRules.map(rule => rule.command).join(' · ')}</span>
            </p>
        {/if}
    </div>
</details>

<style>
    .setup-hint {
        margin: 0 0 10px;
        max-width: 46rem;
        font-size: 12px;
        line-height: 1.5;
    }

    .setup-hint code {
        font-family: var(--font-mono);
        font-size: 10.5px;
    }

    .setup-list {
        display: flex;
        flex-direction: column;
        gap: 6px;
        margin: 0 0 10px;
        padding: 0;
        list-style: none;
    }

    .setup-row {
        display: flex;
        gap: 6px;
        align-items: center;
    }

    .setup-row .input {
        height: 30px;
        font-size: 12px;
    }

    .setup-empty {
        margin: 0 0 10px;
        font-size: 12px;
    }

    .setup-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        align-items: center;
    }

    .setup-check {
        display: flex;
        gap: 6px;
        align-items: center;
        color: var(--muted);
        font-size: 12px;
        cursor: pointer;
    }

    .setup-spacer {
        flex: 1;
    }

    .setup-check input {
        width: 14px;
        height: 14px;
        accent-color: var(--accent);
    }

    .setup-notice {
        margin: 10px 0 0;
    }

    .setup-run {
        margin: 10px 0 0;
        font-size: 11px;
    }
</style>
