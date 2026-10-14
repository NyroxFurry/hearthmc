<script lang="ts">
  import type { PermInfo, Preset } from '../lib/types';

  let {
    perms,
    presets = [],
    value = $bindable([]),
    allowed = null,
  }: { perms: PermInfo[]; presets?: Preset[]; value: string[]; allowed?: string[] | null } = $props();

  let groups = $derived.by(() => {
    const m = new Map<string, PermInfo[]>();
    for (const p of perms) {
      if (!m.has(p.group)) m.set(p.group, []);
      m.get(p.group)!.push(p);
    }
    return [...m.entries()];
  });

  const can = (k: string) => !allowed || allowed.includes(k);

  function toggle(k: string, on: boolean) {
    value = on ? [...new Set([...value, k])] : value.filter((x) => x !== k);
  }

  function applyPreset(p: Preset) {
    value = p.permissions.filter(can);
  }

  function toggleGroup(items: PermInfo[]) {
    const keys = items.map((i) => i.key).filter(can);
    const all = keys.every((k) => value.includes(k));
    value = all ? value.filter((v) => !keys.includes(v)) : [...new Set([...value, ...keys])];
  }
</script>

<div class="matrix">
  {#if presets.length}
    <div class="presets">
      <span class="small muted">Vorlage:</span>
      {#each presets as p (p.id)}
        <button type="button" class="btn sm" title={p.description} onclick={() => applyPreset(p)}>{p.label}</button>
      {/each}
      <button type="button" class="btn sm ghost" onclick={() => (value = [])}>Keine</button>
    </div>
  {/if}
  <div class="groups">
    {#each groups as [group, items] (group)}
      <fieldset>
        <legend><button type="button" class="glabel" onclick={() => toggleGroup(items)}>{group}</button></legend>
        {#each items as p (p.key)}
          <label class="check" class:off={!can(p.key)} title={p.description}>
            <input
              type="checkbox"
              checked={value.includes(p.key)}
              disabled={!can(p.key)}
              onchange={(e) => toggle(p.key, (e.currentTarget as HTMLInputElement).checked)}
            />
            <span>{p.label}</span>
          </label>
        {/each}
      </fieldset>
    {/each}
  </div>
</div>

<style>
  .presets {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-bottom: 14px;
  }
  .groups {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 12px 16px;
  }
  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  legend {
    padding: 0;
    margin-bottom: 4px;
  }
  .glabel {
    border: none;
    background: none;
    padding: 0;
    font: inherit;
    font-size: 13px;
    font-weight: 650;
    color: var(--text-2);
    cursor: pointer;
  }
  .glabel:hover {
    color: var(--text);
  }
  .check {
    font-size: 13.5px;
  }
  .check.off {
    opacity: 0.45;
  }
</style>
