<script lang="ts">
    import { onMount, tick } from 'svelte';
    import Icon from './ui/Icon.svelte';
    import { parseAnsiLine } from '../lib/ansi';
    import type { AnsiToken } from '../lib/ansi';
    import { absoluteTime, baseName, relativeTime } from '../lib/format';
    import { fetchRange, fetchSnapshot, fetchTimeline, restoreSnapshot, setSnapshotLabel } from '../lib/timeline';
    import type { SnapshotDetail, TimelineEntry } from '../lib/timeline';
    import { resolvedTheme } from '../lib/theme.svelte';
    import type { Worktree } from '../lib/types';

    interface Props {
        worktree: Worktree;
        selectedHash: string | null;
        selectedFrom: string | null;
        selectedFile: string | null;
        onSelect: (hash: string | null) => void;
        onSelectRange: (from: string, to: string) => void;
        onSelectFile: (file: string | null) => void;
        onClose: () => void;
    }

    const {
        worktree,
        selectedHash,
        selectedFrom,
        selectedFile,
        onSelect,
        onSelectRange,
        onSelectFile,
        onClose,
    }: Props = $props();

    let entries = $state<TimelineEntry[]>([]);
    let hasMore = $state(false);
    let loading = $state(false);
    let error = $state<string | null>(null);
    let fileInput = $state('');
    let fileFilter = $state('');
    let detail = $state<SnapshotDetail | null>(null);
    let detailLoading = $state(false);
    let detailError = $state<string | null>(null);
    let restorePath = $state('');
    let restoring = $state(false);
    let restoreMessage = $state<string | null>(null);
    let restoreError = $state<string | null>(null);
    let labelEditing = $state(false);
    let labelDraft = $state('');
    let labelSaving = $state(false);
    let labelError = $state<string | null>(null);
    let labelInput = $state<HTMLInputElement | null>(null);
    let now = $state(Date.now());

    let lastSnapshots = $state<number | null>(null);
    let lastDetailKey = '';
    let anchorHash = $state<string | null>(null);

    interface DiffRow {
        kind: '' | 'add' | 'del' | 'hunk';
        tokens: AnsiToken[];
    }

    const rangeBounds = $derived.by(() => {
        if (!selectedFrom || !selectedHash) return null;
        const first = entries.findIndex(entry => entry.hash === selectedFrom);
        const last = entries.findIndex(entry => entry.hash === selectedHash);
        if (first < 0 || last < 0) return null;
        return { first: Math.min(first, last), last: Math.max(first, last) };
    });

    function lineKind(line: string): DiffRow['kind'] {
        if (line.startsWith('@@')) return 'hunk';
        if (line.startsWith('+') && !line.startsWith('+++')) return 'add';
        if (line.startsWith('-') && !line.startsWith('---')) return 'del';
        return '';
    }

    const diffRows = $derived.by((): DiffRow[] => {
        const text = detail?.diff ?? '';
        if (!text) return [];
        let lines = text.split('\n');
        if (lines.length > 3000) lines = lines.slice(0, 3000);
        if (detail?.diffTool === 'difft' || detail?.diffTool === 'delta') {
            return lines.map(line => ({ kind: '', tokens: parseAnsiLine(line) }));
        }
        return lines.map(line => ({
            kind: lineKind(line),
            tokens: [{ text: line, className: '' }],
        }));
    });
    const diffTruncated = $derived.by(() => {
        if (detail?.truncated) return true;
        return (detail?.diff ?? '').split('\n').length > 3000;
    });

    async function load(reset: boolean): Promise<void> {
        if (loading) return;
        loading = true;
        error = null;
        try {
            const offset = reset ? 0 : entries.length;
            const page = await fetchTimeline(worktree.path, 50, offset, fileFilter);
            entries = reset ? page.entries : [...entries, ...page.entries];
            hasMore = page.hasMore;
        } catch (cause) {
            error = cause instanceof Error ? cause.message : 'could not load the timeline';
        } finally {
            loading = false;
        }
    }

    async function restore(event: SubmitEvent): Promise<void> {
        event.preventDefault();
        const target = restorePath.trim();
        if (!detail || !target || restoring) return;
        if (
            !window.confirm(
                `Restore ${target} from ${detail.hash.slice(0, 8)}? This overwrites local changes in that path.`
            )
        ) {
            return;
        }
        restoring = true;
        restoreError = null;
        restoreMessage = null;
        try {
            const result = await restoreSnapshot(worktree.path, detail.hash, target);
            const count = result.restored.length;
            restoreMessage = `Restored ${count} file${count === 1 ? '' : 's'}${
                result.skipped.length ? `, skipped ${result.skipped.length}` : ''
            }. The next snapshot records the change.`;
        } catch (cause) {
            restoreError = cause instanceof Error ? cause.message : 'could not restore the path';
        } finally {
            restoring = false;
        }
    }

    function select(entry: TimelineEntry, extend: boolean): void {
        if (!extend) {
            anchorHash = entry.hash;
            onSelect(entry.hash);
            return;
        }
        const anchor = anchorHash ?? selectedHash;
        if (!anchor) {
            anchorHash = entry.hash;
            onSelect(entry.hash);
            return;
        }
        const anchorIndex = entries.findIndex(item => item.hash === anchor);
        const targetIndex = entries.findIndex(item => item.hash === entry.hash);
        if (anchorIndex < 0 || targetIndex < 0 || anchorIndex === targetIndex) {
            anchorHash = entry.hash;
            onSelect(entry.hash);
            return;
        }
        // The list is newest first, so the larger index is the older snapshot.
        const older = entries[Math.max(anchorIndex, targetIndex)].hash;
        const newer = entries[Math.min(anchorIndex, targetIndex)].hash;
        onSelectRange(older, newer);
    }

    function applyFilter(event: SubmitEvent): void {
        event.preventDefault();
        fileFilter = fileInput.trim();
        onSelect(null);
        void load(true);
    }

    function clearFilter(): void {
        fileInput = '';
        fileFilter = '';
        onSelect(null);
        void load(true);
    }

    function diffToolLabel(tool: string): string {
        switch (tool) {
            case 'difft':
                return 'difftastic';
            case 'delta':
                return 'delta';
            default:
                return 'git diff';
        }
    }

    function diffToolTitle(tool: string): string {
        switch (tool) {
            case 'difft':
                return 'Structural diff by difftastic';
            case 'delta':
                return 'Syntax-highlighted diff by delta';
            default:
                return 'Unified diff by git';
        }
    }

    function kindLabel(kind: string): string {
        switch (kind) {
            case 'snapshot':
                return 'snap';
            case 'base':
                return 'base';
            case 'archive':
                return 'archive';
            case 'range':
                return 'range';
            default:
                return 'commit';
        }
    }

    function entryTitle(entry: TimelineEntry): string {
        const parts: string[] = [];
        if (entry.kind === 'snapshot' && entry.branch) {
            parts.push(`${entry.branch}@${entry.head}`);
        }
        if (entry.label) parts.push(entry.label);
        parts.push(entry.subject);
        return parts.join(' · ');
    }

    async function startLabelEdit(): Promise<void> {
        labelDraft = detail?.label ?? '';
        labelError = null;
        labelEditing = true;
        await tick();
        labelInput?.focus();
    }

    function cancelLabelEdit(): void {
        labelEditing = false;
        labelError = null;
        labelDraft = detail?.label ?? '';
    }

    async function saveLabel(event: SubmitEvent): Promise<void> {
        event.preventDefault();
        if (!detail || labelSaving) return;
        const hash = detail.hash;
        labelSaving = true;
        labelError = null;
        try {
            const saved = await setSnapshotLabel(worktree.path, hash, labelDraft);
            if (selectedHash !== hash) return;
            detail = { ...detail, label: saved || undefined };
            const entry = entries.find(item => item.hash === hash);
            if (entry) entry.label = saved || undefined;
            labelDraft = saved;
            labelEditing = false;
        } catch (cause) {
            if (selectedHash === hash) {
                labelError =
                    cause instanceof Error ? cause.message : 'could not save the label';
            }
        } finally {
            labelSaving = false;
        }
    }

    onMount(() => {
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        const timer = window.setInterval(() => {
            now = Date.now();
        }, 10000);
        void load(true);
        return () => {
            window.clearInterval(timer);
            document.body.style.overflow = previousOverflow;
        };
    });

    // The selected commit and file live in the URL, so back/forward and a
    // reload land on the same view. The effect also guards against out-of-order
    // responses when the selection changes quickly.
    $effect(() => {
        const hash = selectedHash;
        const from = selectedFrom;
        const file = selectedFile;
        const appearance = resolvedTheme();
        const key = `${from ?? ''}|${hash ?? ''}`;
        if (key !== lastDetailKey) {
            restorePath = '';
            restoreMessage = null;
            restoreError = null;
            labelEditing = false;
            labelDraft = '';
            labelSaving = false;
            labelError = null;
            detail = null;
            lastDetailKey = key;
        }
        detailError = null;
        if (!hash) {
            detail = null;
            detailLoading = false;
            return;
        }
        detailLoading = true;
        let cancelled = false;
        void (async () => {
            try {
                const result = from
                    ? await fetchRange(worktree.path, from, hash, file ?? '', appearance)
                    : await fetchSnapshot(worktree.path, hash, file ?? '', appearance);
                if (!cancelled) detail = result;
            } catch (cause) {
                if (!cancelled) {
                    detailError =
                        cause instanceof Error ? cause.message : 'could not load the snapshot';
                }
            } finally {
                if (!cancelled) detailLoading = false;
            }
        })();
        return () => {
            cancelled = true;
        };
    });

    // A new snapshot for this worktree refreshes the newest page while the
    // drawer is open; older pages are not disturbed.
    $effect(() => {
        const count = worktree.snapshots ?? 0;
        if (lastSnapshots === null) {
            lastSnapshots = count;
            return;
        }
        if (count === lastSnapshots) return;
        lastSnapshots = count;
        void load(true);
    });
</script>

<svelte:window
    onkeydown={(event) => {
        if (event.key === 'Escape') onClose();
    }}
/>

<div class="drawer-backdrop" role="presentation" onclick={onClose}></div>

<div class="drawer" role="dialog" aria-modal="true" aria-label="Worktree timeline">
    <header class="drawer-head">
        <div class="drawer-title">
            <h2>Timeline</h2>
            <p class="drawer-sub mono truncate" title={worktree.path}>
                {baseName(worktree.path)} · {worktree.branch}
            </p>
        </div>
        <button type="button" class="icon-btn" aria-label="Close timeline" onclick={onClose}>
            <Icon name="close" size={18} />
        </button>
    </header>

    <form class="drawer-filter" onsubmit={applyFilter}>
        <div class="search-wrap">
            <Icon name="search" size={15} />
            <input
                class="input input--search"
                type="search"
                placeholder="Filter by file (relative path)…"
                bind:value={fileInput}
            />
        </div>
        <button type="submit" class="btn btn--secondary btn--sm" disabled={loading}>Filter</button>
        {#if fileFilter}
            <button type="button" class="btn btn--ghost btn--sm" onclick={clearFilter}>Clear</button>
        {/if}
    </form>

    <div class="drawer-body">
        <div class="timeline-pane">
            {#if error}
                <p class="notice notice--error">{error}</p>
            {/if}

            {#if entries.length === 0 && !loading}
                <p class="muted timeline-empty">
                    No changes{fileFilter ? ' touch this file' : ' since initialization'} yet.
                </p>
            {:else}
                {#if entries.length > 1}
                    <p class="timeline-hint">Shift-click for range</p>
                {/if}
                <ul class="timeline-list">
                    {#each entries as entry, index (entry.hash)}
                        <li>
                            <button
                                type="button"
                                class="timeline-entry"
                                class:active={selectedHash === entry.hash}
                                class:in-range={rangeBounds !== null &&
                                    index >= rangeBounds.first &&
                                    index <= rangeBounds.last}
                                title={entryTitle(entry)}
                                aria-label={entryTitle(entry)}
                                onclick={event => select(entry, event.shiftKey)}
                            >
                                <span class="timeline-entry__time mono faint">
                                    {relativeTime(entry.time, now)}
                                </span>
                                <span class="timeline-entry__hash mono faint">
                                    {entry.hash.slice(0, 8)}
                                </span>
                            </button>
                        </li>
                    {/each}
                </ul>
            {/if}

            {#if loading}
                <p class="muted timeline-empty">Loading…</p>
            {/if}
            {#if hasMore}
                <button
                    type="button"
                    class="btn btn--ghost btn--sm"
                    disabled={loading}
                    onclick={() => void load(false)}
                >
                    Load more
                </button>
            {/if}
        </div>

        <div class="detail-pane">
            {#if detailLoading && !detail}
                <p class="muted">Loading snapshot…</p>
            {:else if detailError}
                <p class="notice notice--error">{detailError}</p>
            {:else if detail}
                <header class="detail-head">
                    <div class="detail-head__row">
                        <span class={`kind kind--${detail.kind}`}>{kindLabel(detail.kind)}</span>
                        <span class="mono faint" title={detail.hash}>{detail.hash.slice(0, 12)}</span>
                        <span class="faint" title={absoluteTime(detail.time)}>
                            {relativeTime(detail.time, now)}
                        </span>
                        {#if detail.diffTool}
                            <span
                                class="diff-tool mono faint"
                                title={diffToolTitle(detail.diffTool)}
                            >
                                {diffToolLabel(detail.diffTool)}
                            </span>
                        {/if}
                    </div>
                    {#if selectedFrom && selectedHash}
                        <div class="range-bar">
                            <span class="range-chip">
                                {detail?.count
                                    ? `${detail.count} snapshots`
                                    : 'Snapshot range'}
                            </span>
                            <span
                                class="mono faint truncate"
                                title={`${selectedFrom} → ${selectedHash}`}
                            >
                                {selectedFrom.slice(0, 8)} → {selectedHash.slice(0, 8)}
                            </span>
                            <button
                                type="button"
                                class="btn btn--ghost btn--sm"
                                onclick={() => onSelect(selectedHash)}
                            >
                                Single snapshot
                            </button>
                        </div>
                    {/if}
                    <h3>{detail.subject}</h3>
                    {#if detail.parent}
                        <p class="muted mono detail-parent" title={detail.parent}>
                            parent {detail.parent.slice(0, 12)}
                        </p>
                    {/if}
                    {#if !selectedFrom}
                        <div class="label-row">
                            {#if labelEditing}
                                <form class="label-form" onsubmit={saveLabel}>
                                    <input
                                        class="input"
                                        type="text"
                                        maxlength="120"
                                        placeholder="Label for this snapshot"
                                        bind:this={labelInput}
                                        bind:value={labelDraft}
                                        disabled={labelSaving}
                                    />
                                    <button
                                        type="submit"
                                        class="btn btn--secondary btn--sm"
                                        disabled={labelSaving}
                                    >
                                        {labelSaving ? 'Saving…' : 'Save'}
                                    </button>
                                    <button
                                        type="button"
                                        class="btn btn--ghost btn--sm"
                                        disabled={labelSaving}
                                        onclick={cancelLabelEdit}
                                    >
                                        Cancel
                                    </button>
                                </form>
                                {#if labelError}
                                    <p class="notice notice--error label-error">{labelError}</p>
                                {/if}
                            {:else if detail.label}
                                <button
                                    type="button"
                                    class="label-chip"
                                    title="Edit label"
                                    onclick={() => void startLabelEdit()}
                                >
                                    {detail.label}
                                </button>
                            {:else}
                                <button
                                    type="button"
                                    class="btn btn--ghost btn--sm label-add"
                                    onclick={() => void startLabelEdit()}
                                >
                                    Add label
                                </button>
                            {/if}
                        </div>
                    {/if}
                </header>

                {#if detail.files.length > 0}
                    <div class="detail-files">
                        {#each detail.files as file (file.path)}
                            <div class="detail-file" class:active={selectedFile === file.path}>
                                <button
                                    type="button"
                                    class="detail-file__main"
                                    title={selectedFile === file.path
                                        ? selectedFrom
                                            ? 'Show the full range diff'
                                            : 'Show the full snapshot diff'
                                        : 'Show only this file'}
                                    onclick={() =>
                                        onSelectFile(selectedFile === file.path ? null : file.path)}
                                >
                                    <span class={`file-status file-status--${file.status}`}>
                                        {file.status}
                                    </span>
                                    <span class="mono truncate" title={file.path}>{file.path}</span>
                                </button>
                                <button
                                    type="button"
                                    class="icon-btn icon-btn--inline"
                                    title="Use this path for restore"
                                    aria-label={`Use ${file.path} for restore`}
                                    onclick={() => (restorePath = file.path)}
                                >
                                    <Icon name="refresh" size={12} />
                                </button>
                            </div>
                        {/each}
                    </div>
                {:else if detail.kind === 'base'}
                    <p class="muted">Base commit: history initialized.</p>
                {:else if detail.kind === 'archive'}
                    <p class="muted">Archive root: the state before the retention window.</p>
                {:else if detail.kind === 'snapshot'}
                    <p class="muted">No file changes.</p>
                {/if}

                {#if detail.kind !== 'base'}
                    <form class="detail-restore" onsubmit={restore}>
                        <input
                            class="input"
                            type="text"
                            placeholder="File or directory in this snapshot"
                            bind:value={restorePath}
                            disabled={restoring}
                        />
                        <button
                            type="submit"
                            class="btn btn--secondary btn--sm"
                            disabled={restoring || !restorePath.trim()}
                        >
                            {restoring ? 'Restoring…' : 'Restore'}
                        </button>
                    </form>
                    <p class="muted restore-note">
                        Writes the path back into the worktree. Files that are absent from the
                        snapshot are kept.
                    </p>
                    {#if restoreMessage}
                        <p class="notice restore-notice">{restoreMessage}</p>
                    {/if}
                    {#if restoreError}
                        <p class="notice notice--error restore-notice">{restoreError}</p>
                    {/if}
                {/if}

                {#if selectedFile}
                    <div class="diff-scope">
                        <span class="faint">Diff</span>
                        <span class="diff-scope__label mono truncate" title={selectedFile}>
                            {selectedFile}
                        </span>
                        <button
                            type="button"
                            class="btn btn--ghost btn--sm"
                            onclick={() => onSelectFile(null)}
                        >
                            All files
                        </button>
                    </div>
                {/if}
                {#if detailLoading && detail}
                    <p class="muted diff-note">Updating diff…</p>
                {/if}

                {#if detail.diff}
                    <pre class="diff" class:stale={detailLoading}>{#each diffRows as row, index (index)}<span class={row.kind}>{#each row.tokens as token}<span class={token.className} style={token.style}>{token.text}</span>{/each}</span>{/each}</pre>
                    {#if diffTruncated}
                        <p class="muted diff-note">Diff truncated.</p>
                    {/if}
                {:else if selectedFile}
                    <p class="muted diff-note">No textual diff for this file.</p>
                {/if}
            {:else}
                <p class="muted">Select a snapshot to see its files and diff.</p>
            {/if}
        </div>
    </div>
</div>

<style>
    .drawer-backdrop {
        position: fixed;
        inset: 0;
        z-index: 40;
        background: rgb(0 0 0 / 52%);
    }

    /* In the installed PWA the OS draws its window controls over the titlebar
     * area. Keep that strip free so the drawer's close button stays below the
     * controls and the titlebar remains draggable. */
    :global(html[data-wco='1']) .drawer-backdrop,
    :global(html[data-wco='1']) .drawer {
        top: env(titlebar-area-height, 0px);
    }

    .drawer {
        position: fixed;
        top: 0;
        right: 0;
        bottom: 0;
        z-index: 41;
        display: flex;
        flex-direction: column;
        width: min(1180px, 96vw);
        background: var(--bg-elevated);
        border-left: 1px solid var(--line);
        box-shadow: var(--shadow-2);
    }

    .drawer-head {
        display: flex;
        flex: none;
        align-items: flex-start;
        gap: 12px;
        padding: 15px 20px;
        border-bottom: 1px solid var(--line);
    }

    .drawer-title {
        min-width: 0;
        flex: 1;
    }

    .drawer-title h2 {
        margin: 0;
        font-size: 16px;
        font-weight: 650;
        letter-spacing: -0.015em;
    }

    .drawer-sub {
        margin: 2px 0 0;
        color: var(--faint);
        font-size: 10.5px;
    }

    .drawer-filter {
        display: flex;
        flex: none;
        align-items: center;
        gap: 8px;
        padding: 10px 20px;
        border-bottom: 1px solid var(--line);
    }

    .drawer-filter .search-wrap {
        flex: 1;
        margin: 0;
    }

    .drawer-body {
        display: grid;
        flex: 1;
        grid-template-columns: 140px minmax(0, 1fr);
        min-height: 0;
    }

    .timeline-pane {
        min-height: 0;
        padding: 8px 8px 20px;
        overflow-y: auto;
        scrollbar-gutter: stable;
        border-right: 1px solid var(--line);
    }

    .timeline-list {
        display: flex;
        flex-direction: column;
        gap: 2px;
        margin: 0;
        padding: 0;
        list-style: none;
    }

    .timeline-entry {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: 6px;
        align-items: center;
        width: 100%;
        padding: 5px 6px;
        border: 0;
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--ink);
        text-align: left;
        transition: background 0.12s var(--ease);
    }

    .timeline-entry:hover {
        background: var(--bg-hover);
    }

    .timeline-entry.in-range {
        background: var(--accent-soft);
    }

    .timeline-entry.active {
        background: var(--bg-active);
        box-shadow: inset 2px 0 var(--accent);
    }

    .timeline-entry__time {
        font-size: 9.5px;
        font-variant-numeric: tabular-nums;
        white-space: nowrap;
    }

    .timeline-entry__hash {
        color: var(--faint);
        font-size: 9px;
        text-align: right;
    }

    .kind--snapshot {
        color: var(--accent);
    }

    .kind--base,
    .kind--archive {
        color: var(--faint);
    }

    .kind--commit {
        color: var(--info);
    }

    .kind--range {
        color: var(--accent);
    }

    .timeline-empty {
        padding: 10px 4px;
    }

    .timeline-hint {
        margin: 0 0 6px;
        padding: 0 6px;
        color: var(--faint);
        font-size: 9px;
    }

    .detail-pane {
        min-width: 0;
        min-height: 0;
        padding: 16px 20px 32px;
        overflow-y: auto;
        scrollbar-gutter: stable;
    }

    .detail-head__row {
        display: flex;
        align-items: center;
        gap: 10px;
        font-size: 10.5px;
    }

    .detail-head h3 {
        margin: 8px 0 4px;
        font-size: 14px;
        font-weight: 600;
        line-height: 1.35;
        overflow-wrap: anywhere;
    }

    .detail-parent {
        margin: 0 0 12px;
        font-size: 10px;
    }

    .diff-tool {
        margin-left: auto;
        font-size: 9px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
    }

    .range-bar {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        align-items: center;
        margin: 0 0 10px;
        padding: 5px 6px 5px 8px;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        background: var(--bg);
    }

    .range-chip {
        flex: none;
        padding: 1px 7px;
        border-radius: var(--radius-pill);
        background: var(--accent-soft);
        color: var(--accent);
        font-size: 9.5px;
        font-weight: 650;
        text-transform: uppercase;
        letter-spacing: 0.04em;
    }

    .range-bar .mono {
        flex: 1;
        min-width: 0;
        font-size: 9.5px;
    }

    .range-bar .btn {
        flex: none;
    }

    .label-row {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        align-items: center;
        margin: 0 0 12px;
    }

    .label-form {
        display: flex;
        flex: 1;
        gap: 6px;
        align-items: center;
        min-width: 0;
    }

    .label-form .input {
        flex: 1;
        height: 28px;
        font-size: 11px;
    }

    .label-chip {
        max-width: 100%;
        overflow: hidden;
        padding: 2px 8px;
        border: 1px solid transparent;
        border-radius: var(--radius-pill);
        background: var(--accent-soft);
        color: var(--accent);
        font-size: 10.5px;
        font-weight: 600;
        text-overflow: ellipsis;
        white-space: nowrap;
        cursor: pointer;
    }

    .label-chip:hover {
        border-color: var(--accent);
    }

    .label-add {
        padding-left: 0;
    }

    .label-error {
        width: 100%;
        margin: 2px 0 0;
    }

    .detail-files {
        max-height: 220px;
        margin: 12px 0;
        overflow-y: auto;
        scrollbar-gutter: stable;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
    }

    .detail-file {
        display: flex;
        align-items: center;
        border-bottom: 1px solid var(--line);
    }

    .detail-file.active {
        background: var(--bg-active);
        box-shadow: inset 2px 0 var(--accent);
    }

    .detail-file__main {
        display: flex;
        flex: 1;
        gap: 8px;
        align-items: center;
        min-width: 0;
        padding: 4px 8px;
        border: 0;
        background: transparent;
        color: inherit;
        font-size: 10.5px;
        text-align: left;
        cursor: pointer;
    }

    .detail-file__main:hover {
        background: var(--bg-hover);
    }

    .detail-file .icon-btn {
        margin-right: 4px;
    }

    .detail-file:last-child {
        border-bottom: 0;
    }

    .file-status {
        width: 18px;
        flex: none;
        font-family: var(--font-mono);
        font-size: 9.5px;
        font-weight: 650;
    }

    .file-status--A {
        color: var(--success);
    }

    .file-status--M {
        color: var(--accent);
    }

    .file-status--D {
        color: var(--danger);
    }

    .file-status--R {
        color: var(--info);
    }

    .diff-scope {
        display: flex;
        gap: 8px;
        align-items: center;
        margin: 14px 0 0;
        padding: 4px 6px 4px 8px;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        background: var(--bg);
        font-size: 10.5px;
    }

    .diff-scope__label {
        flex: 1;
        min-width: 0;
        color: var(--ink-2);
    }

    .diff {
        max-height: 60vh;
        margin: 12px 0 0;
        padding: 12px;
        overflow-x: scroll;
        overflow-y: auto;
        scrollbar-gutter: stable;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        background: var(--bg);
        color: var(--ink-2);
        font-family: var(--font-mono);
        font-size: 10.5px;
        line-height: 1.5;
    }

    .diff.stale {
        opacity: 0.55;
    }

    .diff > span {
        display: block;
        white-space: pre;
    }

    .diff .add {
        color: var(--success);
    }

    .diff .del {
        color: var(--danger);
    }

    .diff .hunk {
        color: var(--info);
    }

    .diff-note {
        margin-top: 6px;
    }

    .detail-restore {
        display: flex;
        gap: 8px;
        align-items: center;
        margin: 12px 0 4px;
    }

    .detail-restore .input {
        flex: 1;
        height: 30px;
        font-size: 11.5px;
    }

    .restore-note {
        margin: 4px 0 8px;
        font-size: 10.5px;
    }

    .restore-notice {
        margin: 8px 0;
    }

    @media (max-width: 860px) {
        .drawer {
            width: 100vw;
        }

        .drawer-body {
            grid-template-rows: minmax(180px, 40vh) minmax(0, 1fr);
            grid-template-columns: 1fr;
        }

        .timeline-pane {
            border-right: 0;
            border-bottom: 1px solid var(--line);
        }
    }
</style>
