// Minimal ANSI SGR parser for the colored diff renderers. Basic colors map
// onto the theme palette; 256-color and truecolor values are preserved as
// inline styles so delta's syntax highlighting survives. Every other escape is
// dropped so no control bytes reach the DOM.

export interface AnsiToken {
    text: string;
    className: string;
    style?: string;
}

interface AnsiStyle {
    fg: string;
    bg: string;
    fgColor: string;
    bgColor: string;
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

const basicPalette = [
    '#000000',
    '#800000',
    '#008000',
    '#808000',
    '#000080',
    '#800080',
    '#008080',
    '#c0c0c0',
    '#808080',
    '#ff0000',
    '#00ff00',
    '#ffff00',
    '#0000ff',
    '#ff00ff',
    '#00ffff',
    '#ffffff',
];

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
    return {
        fg: '',
        bg: '',
        fgColor: '',
        bgColor: '',
        bold: false,
        dim: false,
        italic: false,
        underline: false,
    };
}

function reset(style: AnsiStyle): void {
    style.fg = '';
    style.bg = '';
    style.fgColor = '';
    style.bgColor = '';
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
            const mode = codes[index + 1];
            if (mode === 5) {
                const color = xtermColor(codes[index + 2]);
                if (code === 38) {
                    style.fg = '';
                    style.fgColor = color;
                } else {
                    style.bg = '';
                    style.bgColor = color;
                }
                index += 2;
            } else if (mode === 2) {
                const color = rgbColor(codes[index + 2], codes[index + 3], codes[index + 4]);
                if (code === 38) {
                    style.fg = '';
                    style.fgColor = color;
                } else {
                    style.bg = '';
                    style.bgColor = color;
                }
                index += 4;
            } else {
                index += 1;
            }
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
            style.fgColor = '';
        } else if (code === 49) {
            style.bg = '';
            style.bgColor = '';
        } else if (foreground[code]) {
            style.fg = foreground[code];
            style.fgColor = '';
        } else if (background[code]) {
            style.bg = background[code];
            style.bgColor = '';
        }
    }
}

function append(tokens: AnsiToken[], text: string, style: AnsiStyle): void {
    if (!text) return;
    const className = classNameOf(style);
    const inline = styleOf(style);
    const previous = tokens[tokens.length - 1];
    if (previous && previous.className === className && (previous.style ?? '') === inline) {
        previous.text += text;
    } else {
        tokens.push({ text, className, style: inline || undefined });
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

function styleOf(style: AnsiStyle): string {
    const parts: string[] = [];
    if (style.fgColor) parts.push(`color: ${style.fgColor}`);
    if (style.bgColor) parts.push(`background-color: ${style.bgColor}`);
    return parts.join('; ');
}

function xtermColor(index: number): string {
    if (!Number.isFinite(index) || index < 0 || index > 255) return '';
    if (index < 16) return basicPalette[index];
    if (index < 232) {
        const value = index - 16;
        const red = Math.floor(value / 36);
        const green = Math.floor((value % 36) / 6);
        const blue = value % 6;
        const level = (channel: number): number => (channel === 0 ? 0 : 55 + channel * 40);
        return `rgb(${level(red)} ${level(green)} ${level(blue)})`;
    }
    const level = 8 + (index - 232) * 10;
    return `rgb(${level} ${level} ${level})`;
}

function rgbColor(red: number, green: number, blue: number): string {
    const clamp = (value: number): number =>
        Number.isFinite(value) ? Math.max(0, Math.min(255, Math.round(value))) : 0;
    return `rgb(${clamp(red)} ${clamp(green)} ${clamp(blue)})`;
}
