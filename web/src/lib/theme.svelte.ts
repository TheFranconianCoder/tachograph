export type ThemeMode = 'auto' | 'light' | 'dark';

const STORAGE_KEY = 'tachograph-theme';
const ORDER: ThemeMode[] = ['auto', 'light', 'dark'];

function storedMode(): ThemeMode {
    try {
        const value = localStorage.getItem(STORAGE_KEY);
        if (value === 'light' || value === 'dark' || value === 'auto') return value;
    } catch {
        /* storage can be unavailable */
    }
    return 'auto';
}

export const theme = $state<{ mode: ThemeMode }>({ mode: storedMode() });

let systemDark = $state(false);

function apply(mode: ThemeMode): void {
    document.documentElement.dataset.theme = mode;
    try {
        localStorage.setItem(STORAGE_KEY, mode);
    } catch {
        /* storage can be unavailable */
    }
}

export function initTheme(): void {
    apply(theme.mode);
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
        const query = window.matchMedia('(prefers-color-scheme: dark)');
        systemDark = query.matches;
        query.addEventListener('change', event => {
            systemDark = event.matches;
        });
    }
}

/** The effective light/dark appearance, resolving auto against the OS. */
export function resolvedTheme(): 'light' | 'dark' {
    if (theme.mode === 'light' || theme.mode === 'dark') return theme.mode;
    return systemDark ? 'dark' : 'light';
}

export function cycleTheme(): void {
    const next = ORDER[(ORDER.indexOf(theme.mode) + 1) % ORDER.length];
    theme.mode = next;
    apply(next);
}

export function themeLabel(mode: ThemeMode = theme.mode): string {
    switch (mode) {
        case 'light':
            return 'Theme: light';
        case 'dark':
            return 'Theme: dark';
        default:
            return 'Theme: auto';
    }
}

export function themeIcon(mode: ThemeMode = theme.mode): string {
    return mode === 'dark' ? 'moon' : mode === 'light' ? 'sun' : 'auto';
}
