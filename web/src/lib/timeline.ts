export interface TimelineEntry {
    hash: string;
    time: string;
    kind: string;
    subject: string;
    branch?: string;
    head?: string;
    label?: string;
}

export interface TimelinePage {
    entries: TimelineEntry[];
    hasMore: boolean;
}

export interface SnapshotFile {
    path: string;
    status: string;
}

export interface SnapshotDetail {
    hash: string;
    parent?: string;
    time: string;
    kind: string;
    subject: string;
    label?: string;
    from?: string;
    count?: number;
    files: SnapshotFile[];
    file?: string;
    diff?: string;
    diffTool?: string;
    truncated?: boolean;
}

async function errorMessage(response: Response): Promise<string> {
    const payload = (await response.json().catch(() => null)) as { error?: string } | null;
    return payload?.error ?? `request failed with ${response.status}`;
}

export async function fetchTimeline(
    path: string,
    limit = 50,
    offset = 0,
    file = ''
): Promise<TimelinePage> {
    const params = new URLSearchParams({
        path,
        limit: String(limit),
        offset: String(offset),
    });
    if (file) params.set('file', file);
    const response = await fetch(`/api/v1/timeline?${params}`, {
        headers: { accept: 'application/json' },
    });
    if (!response.ok) throw new Error(await errorMessage(response));
    return (await response.json()) as TimelinePage;
}

export async function fetchRange(
    path: string,
    from: string,
    to: string,
    file = '',
    theme = 'dark'
): Promise<SnapshotDetail> {
    const params = new URLSearchParams({ path, from, to, theme });
    if (file) params.set('file', file);
    const response = await fetch(`/api/v1/range?${params}`, {
        headers: { accept: 'application/json' },
    });
    if (!response.ok) throw new Error(await errorMessage(response));
    return (await response.json()) as SnapshotDetail;
}

export async function fetchSnapshot(
    path: string,
    hash: string,
    file = '',
    theme = 'dark'
): Promise<SnapshotDetail> {
    const params = new URLSearchParams({ path, hash, theme });
    if (file) params.set('file', file);
    const response = await fetch(`/api/v1/snapshot?${params}`, {
        headers: { accept: 'application/json' },
    });
    if (!response.ok) throw new Error(await errorMessage(response));
    return (await response.json()) as SnapshotDetail;
}

export interface RestoreResult {
    restored: string[];
    skipped: string[];
}

export async function restoreSnapshot(
    path: string,
    hash: string,
    file: string
): Promise<RestoreResult> {
    const response = await fetch('/api/v1/restore', {
        method: 'POST',
        headers: { 'content-type': 'application/json', accept: 'application/json' },
        body: JSON.stringify({ path, hash, file }),
    });
    if (!response.ok) throw new Error(await errorMessage(response));
    return (await response.json()) as RestoreResult;
}

export async function setSnapshotLabel(
    path: string,
    hash: string,
    label: string
): Promise<string> {
    const response = await fetch('/api/v1/snapshot/label', {
        method: 'PUT',
        headers: { 'content-type': 'application/json', accept: 'application/json' },
        body: JSON.stringify({ path, hash, label }),
    });
    if (!response.ok) throw new Error(await errorMessage(response));
    const payload = (await response.json()) as { label?: string };
    return payload.label ?? '';
}
