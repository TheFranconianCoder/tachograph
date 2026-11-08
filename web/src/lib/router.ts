// Client-side routes. The Go server serves index.html for unknown paths
// without a file extension, so these URLs survive a reload and can be shared.

export type Route =
    | { name: 'home' }
    | {
          name: 'timeline';
          worktreeID: string;
          hash: string | null;
          from: string | null;
          files: string[];
      };

const timelinePattern = /^\/timeline\/([0-9a-f]{6,40})(?:\/([0-9a-f]{7,40}))?\/?$/;

export function parseRoute(pathname: string, search = ''): Route {
    const match = timelinePattern.exec(pathname);
    if (!match) return { name: 'home' };
    const params = new URLSearchParams(search);
    const from = params.get('from');
    const files = params
        .getAll('file')
        .map(value => value.trim())
        .filter(Boolean);
    return {
        name: 'timeline',
        worktreeID: match[1],
        hash: match[2] ?? null,
        from: from && from.trim() ? from : null,
        files,
    };
}

export function timelineURL(
    worktreeID: string,
    hash?: string | null,
    files: string[] = [],
    from?: string | null
): string {
    const base = hash ? `/timeline/${worktreeID}/${hash}` : `/timeline/${worktreeID}`;
    const params = new URLSearchParams();
    if (from) params.set('from', from);
    for (const file of files) params.append('file', file);
    const query = params.toString();
    return query ? `${base}?${query}` : base;
}

export function currentRoute(): Route {
    return parseRoute(window.location.pathname, window.location.search);
}

type Listener = (route: Route) => void;
const listeners = new Set<Listener>();

/** Subscribes to route changes and reports the current route immediately. */
export function startRouter(listener: Listener): () => void {
    listeners.add(listener);
    const handle = (): void => listener(currentRoute());
    window.addEventListener('popstate', handle);
    listener(currentRoute());
    return () => {
        listeners.delete(listener);
        window.removeEventListener('popstate', handle);
    };
}

/** Pushes or replaces a URL and notifies every subscriber. */
export function navigate(url: string, replace = false): void {
    if (window.location.pathname + window.location.search === url) return;
    if (replace) {
        window.history.replaceState(null, '', url);
    } else {
        window.history.pushState(null, '', url);
    }
    for (const listener of listeners) listener(currentRoute());
}
