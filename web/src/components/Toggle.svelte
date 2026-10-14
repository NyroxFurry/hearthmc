<script lang="ts">
  let {
    checked = $bindable(false),
    label,
    hint = '',
    disabled = false,
    ariaLabel = '',
    onchange,
  }: { checked: boolean; label: string; hint?: string; disabled?: boolean; ariaLabel?: string; onchange?: (v: boolean) => void } = $props();
  const id = 'tg-' + Math.random().toString(36).slice(2, 8);
</script>

<label class="toggle" class:disabled for={id}>
  {#if label || hint}
    <span class="text">
      <span class="label">{label}</span>
      {#if hint}<span class="hint">{hint}</span>{/if}
    </span>
  {/if}
  <input {id} type="checkbox" bind:checked {disabled} aria-label={ariaLabel || undefined} onchange={() => onchange?.(checked)} />
  <span class="track" aria-hidden="true"><span class="knob"></span></span>
</label>

<style>
  .toggle {
    display: flex;
    align-items: center;
    gap: 14px;
    cursor: pointer;
    padding: 4px 0;
  }
  .toggle.disabled {
    opacity: 0.55;
    cursor: default;
  }
  .text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .label {
    font-size: 14px;
    font-weight: 700;
  }
  input {
    position: absolute;
    opacity: 0;
    width: 1px;
    height: 1px;
  }
  .track {
    flex: none;
    width: 42px;
    height: 24px;
    border-radius: 999px;
    background: var(--surface-3);
    border: 1.5px solid var(--line-2);
    position: relative;
    transition: background-color 0.2s, border-color 0.2s;
  }
  .knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 15px;
    height: 15px;
    border-radius: 50%;
    background: var(--text-2);
    transition: transform 0.25s var(--ease), background-color 0.2s;
  }
  input:checked + .track {
    background: var(--accent);
    border-color: var(--accent);
  }
  input:checked + .track .knob {
    transform: translateX(18px);
    background: var(--accent-ink);
  }
  input:focus-visible + .track {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
