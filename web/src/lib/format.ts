import type { Worktree } from './types';

export const baseName = (path: string): string => path.split(/[\\/]/).pop() ?? path;

export const absoluteTime = (value?: string): string =>
    value ? new Date(value).toLocaleString() : '—';

export function relativeTime(value: string | undefined, now: number): string {
    if (!value) return 'never';
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return 'unknown';
    const seconds = Math.max(0, Math.round((now - parsed) / 1000));
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
}

export function lifecycleTitle(worktree: Worktree): string {
    switch (worktree.setupState) {
        case 'error':
            return worktree.error
                ? `Lifecycle setup failed: ${worktree.error}`
                : 'Lifecycle setup failed';
        case 'none':
            return 'No lifecycle setup recorded for this worktree';
        default:
            return worktree.setup && worktree.setup !== 'setup ok'
                ? `Lifecycle setup applied: ${worktree.setup}`
                : 'Lifecycle setup applied';
    }
}

export function progressTitle(worktree: Worktree): string {
    const parts = [
        worktree.lastSnapshot ? `Last snapshot ${worktree.lastSnapshot}` : 'No snapshot yet',
    ];
    if (worktree.intervalMs) {
        parts.push(`polls every ${Math.round(worktree.intervalMs / 1000)}s`);
    }
    return parts.join(' · ');
}

export function watcherTitle(worktree: Worktree): string {
    if (worktree.watcherError) return worktree.watcherError;
    if (worktree.error) return worktree.error;
    if (worktree.lastSnapshot) return `Last snapshot ${worktree.lastSnapshot}`;
    return 'No snapshot yet';
}
