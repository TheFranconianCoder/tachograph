// Minimal ANSI SGR parser for difftastic's coloured inline diff. Only the
// sequences difftastic emits are mapped; every other escape is dropped so no
// control bytes reach the DOM.

export interface AnsiToken {
    text: string;
    className: string;
}

interface AnsiStyle {
    fg: string;
    bg: string;
    bold: boolean;
    dim: boolean;
    italic: boolean;
    underline: boolean;
}

const foreground: Record<number, string> = {
    30: 'black',
    31: 'red',
    32: 'green',
    33: 'yellow',
    34: 'blue',
    35: 'magenta',
    36: 'cyan',
    37: 'white',
    90: 'bright-black',
    91: 'bright-red',
    92: 'bright-green',
    93: 'bright-yellow',
    94: 'bright-blue',
    95: 'bright-magenta',
    96: 'bright-cyan',
    97: 'bright-white',
};

const background: Record<number, string> = {
    40: 'black',
    41: 'red',
    42: 'green',
    43: 'yellow',
    44: 'blue',
    45: 'magenta',
    46: 'cyan',
    47: 'white',
    100: 'black',
    101: 'red',
    102: 'green',
    103: 'yellow',
    104: 'blue',
    105: 'magenta',
    106: 'cyan',
    107: 'white',
};

const escapePattern = /\x1b\[([0-9;]*)m|\x1b\[[0-9;?]*[ -/]*[@-~]/g;

export function parseAnsiLine(line: string): AnsiToken[] {
    if (!line.includes('\x1b')) return [{ text: line, className: '' }];

    const tokens: AnsiToken[] = [];
    const style = emptyStyle();
    let last = 0;
    escapePattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = escapePattern.exec(line)) !== null) {
        if (match.index > last) {
            append(tokens, line.slice(last, match.index), style);
        }
        last = match.index + match[0].length;
        if (match[1] !== undefined) applySgr(style, match[1]);
    }
    if (last < line.length) append(tokens, line.slice(last), style);
    return tokens;
}

function emptyStyle(): AnsiStyle {
    return { fg: '', bg: '', bold: false, dim: false, italic: false, underline: false };
}

function reset(style: AnsiStyle): void {
    style.fg = '';
    style.bg = '';
    style.bold = false;
    style.dim = false;
    style.italic = false;
    style.underline = false;
}

function applySgr(style: AnsiStyle, params: string): void {
    const codes = params === '' ? [0] : params.split(';').map(value => Number.parseInt(value, 10));
    for (let index = 0; index < codes.length; index += 1) {
        const code = codes[index];
        if (!Number.isFinite(code)) continue;

        if (code === 38 || code === 48) {
            // Extended colour: skip its mode and values. The palette stays
            // theme-driven instead of trusting arbitrary RGB values.
            const mode = codes[index + 1];
            index += mode === 5 ? 2 : mode === 2 ? 4 : 1;
            continue;
        }

        if (code === 0) {
            reset(style);
        } else if (code === 1) {
            style.bold = true;
        } else if (code === 2) {
            style.dim = true;
        } else if (code === 3) {
            style.italic = true;
        } else if (code === 4) {
            style.underline = true;
        } else if (code === 22) {
            style.bold = false;
            style.dim = false;
        } else if (code === 23) {
            style.italic = false;
        } else if (code === 24) {
            style.underline = false;
        } else if (code === 39) {
            style.fg = '';
        } else if (code === 49) {
            style.bg = '';
        } else if (foreground[code]) {
            style.fg = foreground[code];
        } else if (background[code]) {
            style.bg = background[code];
        }
    }
}

function append(tokens: AnsiToken[], text: string, style: AnsiStyle): void {
    if (!text) return;
    const className = classNameOf(style);
    const previous = tokens[tokens.length - 1];
    if (previous && previous.className === className) {
        previous.text += text;
    } else {
        tokens.push({ text, className });
    }
}

function classNameOf(style: AnsiStyle): string {
    const classes: string[] = [];
    if (style.fg) classes.push(`ansi-${style.fg}`);
    if (style.bg) classes.push(`ansi-bg-${style.bg}`);
    if (style.bold) classes.push('ansi-bold');
    if (style.dim) classes.push('ansi-dim');
    if (style.italic) classes.push('ansi-italic');
    if (style.underline) classes.push('ansi-underline');
    return classes.join(' ');
}
