<script lang="ts">
    import { confirmDialog, settleConfirm } from '../../lib/confirm.svelte';
    import Modal from './Modal.svelte';

    const request = $derived(confirmDialog.request);

    $effect(() => {
        if (!request) return;
        const onKey = (event: KeyboardEvent): void => {
            if (event.key === 'Enter' && !(event.target instanceof HTMLButtonElement)) {
                event.preventDefault();
                settleConfirm(true);
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    });
</script>

<Modal
    open={!!request}
    title={request?.title ?? ''}
    description={request?.message}
    width="430px"
    onClose={() => settleConfirm(false)}
>
    <div class="dialog-actions">
        <button type="button" class="btn btn--secondary" onclick={() => settleConfirm(false)}>
            {request?.cancelLabel ?? 'Cancel'}
        </button>
        <button
            type="button"
            class="btn {request?.danger ? 'btn--danger' : 'btn--primary'}"
            onclick={() => settleConfirm(true)}
        >
            {request?.confirmLabel ?? 'Confirm'}
        </button>
    </div>
</Modal>

<style>
    .dialog-actions {
        display: flex;
        justify-content: flex-end;
        gap: 7px;
        padding-top: 6px;
    }
</style>
