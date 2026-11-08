<script lang="ts">
    import Icon from './ui/Icon.svelte';
    import type { Status } from '../lib/types';

    interface Props {
        status: Status | null;
        busy: boolean;
        onToggleRecording: () => void;
        onToggleAutostart: (enabled: boolean) => void;
    }

    const { status, busy, onToggleRecording, onToggleAutostart }: Props = $props();
</script>

<section class="panel">
    <div class="panel-body">
        <button
            type="button"
            class="btn btn--secondary recorder-action"
            onclick={onToggleRecording}
            disabled={busy || !status}
        >
            <Icon name={status?.paused ? 'play' : 'pause'} size={15} />
            {status?.paused ? 'Resume recording' : 'Pause recording'}
        </button>

        <label class="autostart">
            <input
                type="checkbox"
                checked={status?.autostart ?? false}
                disabled={busy || !status}
                onchange={event => onToggleAutostart(event.currentTarget.checked)}
            />
            <span>Start at login</span>
            <span class="autostart-note">without opening the UI</span>
        </label>
    </div>
</section>

<style>
    .recorder-action {
        width: 100%;
    }

    .autostart {
        display: flex;
        gap: 7px;
        align-items: center;
        margin-top: 11px;
        color: var(--muted);
        font-size: 11.5px;
        cursor: pointer;
    }

    .autostart input {
        width: 14px;
        height: 14px;
        accent-color: var(--accent);
    }

    .autostart-note {
        color: var(--faint);
        font-size: 10.5px;
    }
</style>
