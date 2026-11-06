import { SSEParser } from './sse-parser';
import type { ConnectionState, ServerFrame } from '../types';

export interface EventStreamOptions {
    url?: string;
    onFrame: (frame: ServerFrame) => void;
    /** Called whenever a stream opens. `reconnected` is true after the first successful connection. */
    onOpen?: (reconnected: boolean) => void;
    onState?: (state: ConnectionState) => void;
    /** If no bytes — data OR heartbeat comment — arrive for this long, the stream is rebuilt. */
    idleTimeoutMs?: number;
}

const MIN_BACKOFF = 900;
const MAX_BACKOFF = 20_000;

/**
 * EventStream is the single SSE connection of the application.
 *
 *  It is intentionally hard to kill:
 *  - exponential backoff with jitter, capped, so a dead service does not spin;
 *  - a heartbeat watchdog, because a half-open TCP connection looks "open" to fetch forever;
 *  - `wake()` for visibility/focus/online events, so returning to the window reconnects immediately;
 *  - a parser that reports heartbeat comments as activity.
 */
export class EventStream {
    private readonly url: string;
    private readonly idleTimeoutMs: number;
    private started = false;
    private stopped = false;
    private controller: AbortController | null = null;
    private hadOpened = false;
    private attempt = 0;
    private lastActivity = 0;
    private watchdog: ReturnType<typeof setInterval> | undefined;
    private sleepTimer: ReturnType<typeof setTimeout> | undefined;
    private sleepResolve: (() => void) | null = null;
    private immediate = false;

    constructor(private readonly options: EventStreamOptions) {
        this.url = options.url ?? '/api/v1/events';
        this.idleTimeoutMs = options.idleTimeoutMs ?? 60_000;
    }

    start(): void {
        if (this.started) return;
        this.started = true;
        this.stopped = false;
        this.hadOpened = false;
        this.attempt = 0;
        void this.loop();
        this.watchdog = setInterval(() => this.checkIdle(), 10_000);
        window.addEventListener('online', this.onOnline);
        window.addEventListener('offline', this.onOffline);
    }

    stop(): void {
        this.stopped = true;
        this.started = false;
        clearInterval(this.watchdog);
        this.watchdog = undefined;
        this.wake();
        this.controller?.abort();
        this.controller = null;
        window.removeEventListener('online', this.onOnline);
        window.removeEventListener('offline', this.onOffline);
    }

    /** wake interrupts a pending backoff sleep. Returning to the window must not wait 20 seconds. */
    wake(): void {
        if (this.sleepTimer !== undefined) clearTimeout(this.sleepTimer);
        this.sleepTimer = undefined;
        const resolve = this.sleepResolve;
        this.sleepResolve = null;
        resolve?.();
    }

    /** restart tears down the current attempt and reconnects now. */
    restart(): void {
        this.immediate = true;
        this.controller?.abort();
        this.wake();
    }

    private onOnline = (): void => {
        this.options.onState?.('connecting');
        this.restart();
    };

    private onOffline = (): void => {
        this.options.onState?.('offline');
        this.controller?.abort();
    };

    private checkIdle(): void {
        if (this.stopped || !this.controller) return;
        if (this.lastActivity > 0 && Date.now() - this.lastActivity > this.idleTimeoutMs) {
            // No heartbeat and no data: the browser still believes the socket is open. Rebuild it.
            this.restart();
        }
    }

    private setState(state: ConnectionState): void {
        this.options.onState?.(state);
    }

    private sleep(ms: number): Promise<void> {
        return new Promise((resolve) => {
            this.sleepResolve = resolve;
            this.sleepTimer = setTimeout(() => {
                this.sleepTimer = undefined;
                this.sleepResolve = null;
                resolve();
            }, ms);
        });
    }

    private backoff(): number {
        const base = Math.min(MAX_BACKOFF, MIN_BACKOFF * 2 ** Math.min(this.attempt, 5));
        const jitter = 0.7 + Math.random() * 0.6;
        this.attempt++;
        return Math.round(base * jitter);
    }

    private async loop(): Promise<void> {
        while (!this.stopped) {
            if (typeof navigator !== 'undefined' && navigator.onLine === false) {
                this.setState('offline');
                await this.sleep(2000);
                continue;
            }

            this.setState(this.hadOpened ? 'reconnecting' : 'connecting');
            this.immediate = false;
            this.controller = new AbortController();
            const controller = this.controller;
            this.lastActivity = Date.now();

            try {
                const response = await fetch(this.url, {
                    headers: { Accept: 'text/event-stream' },
                    cache: 'no-store',
                    signal: controller.signal,
                });
                if (!response.ok || !response.body) {
                    throw new Error(`event stream: ${response.status} ${response.statusText}`);
                }

                this.attempt = 0;
                this.lastActivity = Date.now();
                this.setState('live');
                const reconnected = this.hadOpened;
                this.hadOpened = true;
                this.options.onOpen?.(reconnected);

                await this.read(response.body, controller.signal);
            } catch (error) {
                if (!this.stopped && !(error instanceof DOMException && error.name === 'AbortError')) {
                    // The loop's next turn reports the state and sleeps.
                }
            } finally {
                if (this.controller === controller) this.controller = null;
            }

            if (this.stopped) break;
            if (typeof navigator !== 'undefined' && navigator.onLine === false) {
                this.setState('offline');
                await this.sleep(2000);
                continue;
            }
            this.setState('reconnecting');
            const wait = this.immediate ? 0 : this.backoff();
            this.immediate = false;
            await this.sleep(wait);
        }
    }

    private async read(body: ReadableStream<Uint8Array>, signal: AbortSignal): Promise<void> {
        const reader = body.getReader();
        const decoder = new TextDecoder();
        const parser = new SSEParser();

        try {
            for (;;) {
                if (signal.aborted) throw new DOMException('aborted', 'AbortError');
                const { done, value } = await reader.read();
                if (done) break;
                this.lastActivity = Date.now();
                const text = decoder.decode(value, { stream: true });
                for (const message of parser.push(text)) {
                    if (message.comment !== undefined) continue;
                    if (!message.data) continue;
                    try {
                        this.options.onFrame(JSON.parse(message.data) as ServerFrame);
                    } catch {
                        // One malformed frame must never stop the frames behind it.
                    }
                }
            }
        } finally {
            reader.releaseLock();
        }
        throw new Error('event stream closed');
    }
}
