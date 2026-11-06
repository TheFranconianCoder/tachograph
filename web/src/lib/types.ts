export interface Worktree {
    id?: string;
    path: string;
    branch: string;
    head: string;
    historyRepo?: string;
    placeholder?: string;
    state: string;
    setup?: string;
    setupState?: string;
    error?: string;
    watcher?: string;
    lastSnapshot?: string;
    snapshots?: number;
    intervalMs?: number;
    watcherError?: string;
}

export interface Project {
    name: string;
    commonDir: string;
    mainWorktree: string;
    historyRepo?: string;
    lastChange?: string;
    worktrees: Worktree[];
}

export interface Activity {
    time: string;
    kind: string;
    path?: string;
    message?: string;
    hash?: string;
}

export interface LifecycleCopyRule {
    path: string;
    optional?: boolean;
    overwrite?: boolean;
}

export interface LifecycleRunRule {
    command: string;
    onlyIf?: string;
    timeout?: string;
}

export interface WorktreeSetup {
    copy: LifecycleCopyRule[];
    run: LifecycleRunRule[];
}

export interface Status {
    name: string;
    version: string;
    uptimeSeconds: number;
    recording: boolean;
    paused: boolean;
    watcherEngine?: string;
    diffTool?: string;
    worktreeSetup?: WorktreeSetup;
    worktreePatterns?: string[];
    autostart?: boolean;
    roots: string[];
    projects: Project[];
    activity: Activity[];
    scanning: boolean;
    lastScan?: string;
    scanError?: string;
}

export type ConnectionState = 'connecting' | 'live' | 'reconnecting' | 'offline';

/** One SSE frame; the server currently sends only full `status` snapshots. */
export interface ServerFrame {
    type: string;
    at?: string;
    data?: unknown;
}
