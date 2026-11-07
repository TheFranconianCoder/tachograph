// Single-window guard owned by the PWA itself.
//
// A second window announces itself over a BroadcastChannel. When an already-claimed window answers,
// the newcomer becomes secondary and offers to focus the primary window. No server involvement.
const CHANNEL = 'tachograph-ui';

let channel: BroadcastChannel | null = null;
let claimed = false;

export function claimWindow(onDuplicate: () => void): void {
    if (typeof BroadcastChannel === 'undefined') return;

    channel = new BroadcastChannel(CHANNEL);
    let primarySeen = false;

    channel.onmessage = (event: MessageEvent) => {
        switch (event.data) {
            case 'ping':
                if (claimed) channel?.postMessage('pong');
                break;
            case 'pong':
                primarySeen = true;
                break;
            case 'focus':
                window.focus();
                break;
            case 'standdown':
                claimed = false;
                break;
        }
    };

    channel.postMessage('ping');
    window.setTimeout(() => {
        if (primarySeen) {
            channel?.postMessage('focus');
            onDuplicate();
            return;
        }
        claimed = true;
    }, 250);
}

export function focusPrimaryWindow(): void {
    channel?.postMessage('focus');
}

// The explicit "use this window" fallback: tell the current primary to stand
// down, then take over without opening a third channel.
export function claimAsPrimary(): void {
    channel?.postMessage('standdown');
    claimed = true;
}
