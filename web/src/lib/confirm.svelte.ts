export interface ConfirmRequest {
    title: string;
    message?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
}

export const confirmDialog = $state<{ request: ConfirmRequest | null }>({ request: null });

let resolver: ((confirmed: boolean) => void) | null = null;

/** Asks in the app's own dialog and resolves when the reader answered. */
export function confirm(request: ConfirmRequest): Promise<boolean> {
    resolver?.(false);
    confirmDialog.request = request;
    return new Promise<boolean>(resolve => {
        resolver = resolve;
    });
}

export function settleConfirm(confirmed: boolean): void {
    confirmDialog.request = null;
    const resolve = resolver;
    resolver = null;
    resolve?.(confirmed);
}
