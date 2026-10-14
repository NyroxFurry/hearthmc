<script lang="ts">
  import Modal from './Modal.svelte';
  import { dialogs } from '../lib/dialogs.svelte';

  let typed = $state('');
  let checked = $state(false);
  let value = $state('');
  let open = $state(false);

  $effect(() => {
    const d = dialogs.current;
    if (d) {
      typed = '';
      checked = d.kind === 'confirm' ? !!d.opts.checkboxDefault : false;
      value = d.kind === 'prompt' ? d.opts.value ?? '' : '';
      open = true;
    } else {
      open = false;
    }
  });

  function finish(ok: boolean) {
    const d = dialogs.current;
    if (!d) return;
    dialogs.current = null;
    if (d.kind === 'confirm') d.resolve({ ok, checked });
    else d.resolve(ok ? value : null);
  }
</script>

{#if dialogs.current}
  {@const d = dialogs.current}
  <Modal bind:open title={d.opts.title} size="s" onclose={() => finish(false)}>
    {#if d.kind === 'confirm'}
      <div class="stack tight">
        {#if d.opts.text}<p class="muted">{d.opts.text}</p>{/if}
        {#if d.opts.checkbox}
          <label class="check"><input type="checkbox" bind:checked /> {d.opts.checkbox}</label>
        {/if}
        {#if d.opts.typed}
          <div class="field">
            <label for="confirm-typed">Zur Bestätigung „{d.opts.typed}“ eingeben</label>
            <input id="confirm-typed" class="input" bind:value={typed} autocomplete="off" />
          </div>
        {/if}
      </div>
    {:else}
      <form
        class="field"
        onsubmit={(e) => {
          e.preventDefault();
          finish(true);
        }}
      >
        {#if d.opts.label}<label for="prompt-value">{d.opts.label}</label>{/if}
        <input id="prompt-value" class="input" bind:value placeholder={d.opts.placeholder ?? ''} autocomplete="off" />
        {#if d.opts.hint}<p class="hint">{d.opts.hint}</p>{/if}
      </form>
    {/if}
    {#snippet footer()}
      <button class="btn ghost" onclick={() => finish(false)}>Abbrechen</button>
      {#if d.kind === 'confirm'}
        <button
          class="btn {d.opts.danger ? 'danger' : 'primary'}"
          disabled={!!d.opts.typed && typed !== d.opts.typed}
          onclick={() => finish(true)}
          data-autofocus>{d.opts.confirm ?? 'Bestätigen'}</button
        >
      {:else}
        <button class="btn primary" disabled={!d.opts.optional && !value.trim()} onclick={() => finish(true)}>{d.opts.confirm ?? 'Speichern'}</button>
      {/if}
    {/snippet}
  </Modal>
{/if}
