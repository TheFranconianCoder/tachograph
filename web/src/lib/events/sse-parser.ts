/** A small, spec-shaped Server-Sent Events parser.
 *
 *  The browser's EventSource is convenient but opaque: it hides heartbeats, cannot be observed for a
 *  half-open stream, and its built-in retry cannot be coordinated with a catch-up reload. The app
 *  therefore reads the stream with `fetch` and uses this parser.
 */

export interface SSEMessage {
    /** The `event:` field. The Tachograph stream leaves it empty and puts the type in the JSON. */
    event?: string;
    /** The joined `data:` lines. */
    data: string;
    id?: string;
    retry?: number;
    /** A comment line (": ping"). It carries no data but proves the connection is alive. */
    comment?: string;
}

export class SSEParser {
    private buffer = '';
    private data: string[] = [];
    private event = '';
    private id = '';
    private retry: number | undefined;

    push(chunk: string): SSEMessage[] {
        this.buffer += chunk;
        const messages: SSEMessage[] = [];

        for (;;) {
            const newline = this.buffer.indexOf('\n');
            if (newline < 0) break;
            let line = this.buffer.slice(0, newline);
            this.buffer = this.buffer.slice(newline + 1);
            if (line.endsWith('\r')) line = line.slice(0, -1);

            if (line === '') {
                const message = this.dispatch();
                if (message) messages.push(message);
                continue;
            }

            if (line.startsWith(':')) {
                messages.push({ data: '', comment: line.slice(1).trimStart() });
                continue;
            }

            const colon = line.indexOf(':');
            const field = colon >= 0 ? line.slice(0, colon) : line;
            let value = colon >= 0 ? line.slice(colon + 1) : '';
            if (value.startsWith(' ')) value = value.slice(1);

            switch (field) {
                case 'data':
                    this.data.push(value);
                    break;
                case 'event':
                    this.event = value;
                    break;
                case 'id':
                    this.id = value;
                    break;
                case 'retry': {
                    const parsed = Number.parseInt(value, 10);
                    if (Number.isFinite(parsed)) this.retry = parsed;
                    break;
                }
                default:
                    // Unknown fields are ignored, as the spec requires.
                    break;
            }
        }

        return messages;
    }

    /** flush is for a clean end-of-stream: a pending event without a terminating blank line is
     *  discarded, exactly as the EventSource specification says. */
    private dispatch(): SSEMessage | null {
        if (this.data.length === 0) {
            this.event = '';
            return null;
        }
        const message: SSEMessage = {
            data: this.data.join('\n'),
            event: this.event || undefined,
            id: this.id || undefined,
            retry: this.retry,
        };
        this.data = [];
        this.event = '';
        return message;
    }
}
