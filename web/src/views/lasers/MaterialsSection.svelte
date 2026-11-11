<script lang="ts">
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { describeMaterial, materialID } from '../../lib/lasers';
  import type { Material } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // A laser's materials (docs/printing.md#materials): its driver's presets,
  // which an admin tunes for this laser alone, and its own.
  let {
    printerID,
    materials,
    canManage,
    onchange,
  }: { printerID: string; materials: Material[]; canManage: boolean; onchange: () => void | Promise<void> } = $props();

  interface Draft {
    id: string;
    name: string;
    power: number;
    speed: number;
    passes: number;
    lpi: number;
    cuts: boolean;
    cutPower: number;
    cutSpeed: number;
    cutPasses: number;
    fromBack: boolean;
    isNew: boolean;
  }

  let editing = $state<Draft | null>(null);
  let error = $state('');

  function edit(m: Material) {
    editing = {
      id: m.id, name: m.name, power: m.engrave.power, speed: m.engrave.speed, passes: m.engrave.passes, lpi: m.engrave.lpi,
      cuts: !!m.cut, cutPower: m.cut?.power ?? 100, cutSpeed: m.cut?.speed ?? 5, cutPasses: m.cut?.passes ?? 1, fromBack: !!m.from_back, isNew: false,
    };
  }

  function add() {
    editing = { id: '', name: '', power: 30, speed: 150, passes: 1, lpi: 254, cuts: false, cutPower: 100, cutSpeed: 5, cutPasses: 1, fromBack: false, isNew: true };
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!editing) return;
    error = '';
    const d = editing;
    const id = d.isNew ? materialID(d.name) : d.id;
    if (d.isNew && materials.some((m) => m.id === id)) {
      error = `This laser already has a material called that (${id}).`;
      return;
    }
    try {
      await request('PUT', `/printers/${encodeURIComponent(printerID)}/materials/${encodeURIComponent(id)}`, {
        name: d.name,
        engrave: { power: Number(d.power), speed: Number(d.speed), passes: Number(d.passes), lpi: Number(d.lpi) },
        cut: d.cuts ? { power: Number(d.cutPower), speed: Number(d.cutSpeed), passes: Number(d.cutPasses) } : null,
        from_back: d.fromBack,
      });
      editing = null;
      await onchange();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(m: Material) {
    error = '';
    try {
      await request('DELETE', `/printers/${encodeURIComponent(printerID)}/materials/${encodeURIComponent(m.id)}`);
      await onchange();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="card space-y-3" aria-labelledby="materials-heading">
  <h2 id="materials-heading" class="text-lg font-semibold">Materials</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    Power is a percentage of the laser’s, and speed in millimeters a second. Lines an inch is the resolution a label is engraved at.
  </p>
  <ErrorMessage {error} />
  <ul class="space-y-2">
    {#each materials as m (m.id)}
      <li class="flex flex-wrap items-start justify-between gap-2 border-t border-zinc-200 pt-2 dark:border-zinc-800">
        <div>
          <p class="font-medium">
            {m.name}
            {#if m.preset}<span class="text-xs text-zinc-600 dark:text-zinc-400">{m.tuned ? ' (preset, tuned)' : ' (preset)'}</span>{/if}
          </p>
          <p class="text-sm text-zinc-600 dark:text-zinc-400">{describeMaterial(m)}.</p>
        </div>
        {#if canManage}
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn-secondary" aria-label="Edit {m.name}" onclick={() => edit(m)}>Edit</button>
            {#if !m.preset}
              <ConfirmButton label="Delete" confirmLabel="Delete {m.name}" question="Delete this material from this laser?" onconfirm={() => remove(m)} />
            {:else if m.tuned}
              <ConfirmButton label="Put back" confirmLabel="Put {m.name} back" question="Put this preset back as the driver has it?" onconfirm={() => remove(m)} />
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ul>

  {#if canManage && !editing}
    <button type="button" class="btn-secondary" onclick={add}>Add a material</button>
  {/if}

  {#if editing}
    <form class="space-y-3 border-t border-zinc-200 pt-3 dark:border-zinc-800" onsubmit={save} aria-label={editing.isNew ? 'New material' : `Edit ${editing.name}`}>
      <div>
        <label class="label" for="material-name">Name</label>
        <input id="material-name" class="input" required maxlength="255" bind:value={editing.name} />
      </div>
      <fieldset class="grid gap-3 sm:grid-cols-4">
        <legend class="label">Engraving</legend>
        <div><label class="label" for="engrave-power">Power (%)</label><input id="engrave-power" class="input" type="number" min="0.1" max="100" step="0.1" required bind:value={editing.power} /></div>
        <div><label class="label" for="engrave-speed">Speed (mm/s)</label><input id="engrave-speed" class="input" type="number" min="0.1" max="10000" step="0.1" required bind:value={editing.speed} /></div>
        <div><label class="label" for="engrave-passes">Passes</label><input id="engrave-passes" class="input" type="number" min="1" max="50" step="1" required bind:value={editing.passes} /></div>
        <div><label class="label" for="engrave-lpi">Lines an inch</label><input id="engrave-lpi" class="input" type="number" min="50" max="1270" step="1" required bind:value={editing.lpi} /></div>
      </fieldset>
      <label class="flex min-h-11 items-center gap-2"><input type="checkbox" bind:checked={editing.cuts} /> This laser cuts it</label>
      {#if editing.cuts}
        <fieldset class="grid gap-3 sm:grid-cols-3">
          <legend class="label">Cutting</legend>
          <div><label class="label" for="cut-power">Power (%)</label><input id="cut-power" class="input" type="number" min="0.1" max="100" step="0.1" required bind:value={editing.cutPower} /></div>
          <div><label class="label" for="cut-speed">Speed (mm/s)</label><input id="cut-speed" class="input" type="number" min="0.1" max="10000" step="0.1" required bind:value={editing.cutSpeed} /></div>
          <div><label class="label" for="cut-passes">Passes</label><input id="cut-passes" class="input" type="number" min="1" max="50" step="1" required bind:value={editing.cutPasses} /></div>
        </fieldset>
      {/if}
      <label class="flex min-h-11 items-center gap-2"><input type="checkbox" bind:checked={editing.fromBack} /> Engraved from the back, as clear acrylic is: its jobs are mirrored</label>
      <div class="flex gap-2">
        <button type="submit" class="btn-secondary">Save the material</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
      </div>
    </form>
  {/if}
</section>
