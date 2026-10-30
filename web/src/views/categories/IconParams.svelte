<!--
  An icon's params bound to what each asset has (README §3.7): each param
  the category's icon has, its own or inherited, set for each asset drawn
  with it. A number param takes a number field's value, so a server with
  2 rack units is drawn 2 units high; a text param takes the asset's name,
  its code or a field's value, written on it. Those with nothing there are
  drawn with the param's default.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Field, IconParamBinding, Rendition } from '../../lib/models';
  import type { Definition } from '../../lib/isolex/syntax';
  import { iconParams, iconTextParams } from '../../lib/icons';
  import { iconset } from '../../lib/iconset.svelte';

  interface Props {
    categoryId: string;
    iconName: string;
    /** What every source defines, for the params the icon has. */
    definitions: Definition[];
    canEdit: boolean;
    /** Which of the category's icons: its screen icon (absent) or its label icon. */
    rendition?: Rendition;
  }
  let { categoryId, iconName, definitions, canEdit, rendition = 'screen' }: Props = $props();
  const path = $derived(`/categories/${encodeURIComponent(categoryId)}/icon/params${rendition === 'screen' ? '' : `?rendition=${rendition}`}`);

  let fields = $state<Field[]>([]);
  let bound = $state<Record<string, string>>({});
  let saved = $state<Record<string, string>>({});
  let error = $state('');
  let status = $state('');
  let saving = $state(false);
  let loaded = $state(false);

  const params = $derived(iconParams(definitions, iconName));
  const texts = $derived(new Set(iconTextParams(definitions, iconName)));
  const numbers = $derived(fields.filter((f) => f.validation_type === 'number'));
  const others = $derived(fields.filter((f) => f.validation_type !== 'number'));
  // a param bound before the icon stopped having it is still listed, to unbind
  const listed = $derived([...params, ...Object.keys(saved).filter((p) => !params.includes(p)).sort()]);
  const changed = $derived(listed.some((p) => (bound[p] ?? '') !== (saved[p] ?? '')));

  onMount(() => {
    void load();
  });

  /** Each param's binding as the form holds it: a field's ID, or name or code. */
  function asMap(list: IconParamBinding[]): Record<string, string> {
    return Object.fromEntries(list.map((b) => [b.param, b.source === 'field' ? b.field_id : b.source]));
  }

  async function load() {
    try {
      const [f, b] = await Promise.all([
        request<{ fields: Field[] }>('GET', '/fields'),
        request<{ params: IconParamBinding[] }>('GET', path),
      ]);
      fields = f.fields ?? [];
      saved = asMap(b.params ?? []);
      bound = { ...saved };
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    saving = true;
    const out = Object.fromEntries(Object.entries(bound).filter(([p, f]) => f && listed.includes(p)));
    try {
      const res = await request<{ params: IconParamBinding[] }>('PUT', path, { params: out });
      saved = asMap(res.params ?? []);
      bound = { ...saved };
      status = 'Saved: each asset is drawn with its own.';
      void iconset.refresh();
    } catch (err) {
      error = messageOf(err);
    } finally {
      saving = false;
    }
  }
</script>

{#if loaded && (listed.length || error)}
  <section class="card mt-3 max-w-2xl" aria-labelledby="icon-params-heading">
    <h3 id="icon-params-heading" class="text-sm font-medium">Parameters</h3>
    <p class="mb-2 text-xs text-zinc-600 dark:text-zinc-400">
      Draw each asset with a param set to what it has: a number param to a number field's value, such as its height from its rack units; a text param to its
      name, its code or a field's value, written on it. An asset with nothing there is drawn with the param's default.
    </p>
    <form class="space-y-2" onsubmit={save}>
      <div class="grid items-center gap-x-3 gap-y-2 sm:grid-cols-[auto_1fr]">
        {#each listed as p (p)}
          <label class="font-mono text-sm" for="icon-param-{p}">{p}{#if texts.has(p)}{' '}<span class="font-sans text-xs text-zinc-600 dark:text-zinc-400">text</span>{/if}</label>
          <select id="icon-param-{p}" class="input-sm w-full max-w-xs" bind:value={bound[p]} disabled={!canEdit}>
            <option value="">Its default</option>
            {#if texts.has(p)}
              <option value="name">The asset's name</option>
              <option value="code">Its code</option>
              {#each others as f (f.id)}<option value={f.id}>{f.label}</option>{/each}
            {:else}
              {#each numbers as f (f.id)}<option value={f.id}>{f.label}</option>{/each}
            {/if}
          </select>
        {/each}
      </div>
      {#if !numbers.length && listed.some((p) => !texts.has(p))}<p class="text-xs text-zinc-600 dark:text-zinc-400">There are no number fields to bind a number param to yet.</p>{/if}
      {#if error}<p role="alert" class="text-sm text-red-700 dark:text-red-400">{error}</p>{/if}
      {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
      {#if canEdit}<button class="btn-secondary" disabled={saving || !changed}>{saving ? 'Saving…' : 'Save the parameters'}</button>{/if}
    </form>
  </section>
{/if}
