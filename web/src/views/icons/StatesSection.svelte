<!--
  Asset states (README §3.7): the variant each state an asset can be in is
  drawn with. An asset in a state is drawn as <its icon>-<variant> when
  the build draws that, as an icon does that opts into an axis with that
  variant, and as its plain icon otherwise; in several, the worst its icon
  has.
-->
<script lang="ts">
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { drawnIn, iconStateNames, type IconStates } from '../../lib/iconStates';

  interface Props {
    states: IconStates;
    /** The names the build draws. */
    names: string[];
    canManage: boolean;
    onsaved: () => void;
  }
  let { states, names, canManage, onsaved }: Props = $props();

  let variants = $state<Record<string, string>>({});
  let error = $state('');
  let status = $state('');

  $effect(() => {
    variants = { ...states.variants };
  });

  function drawn(state: string): string {
    const these = drawnIn(names, (variants[state] ?? '').trim());
    if (!(variants[state] ?? '').trim()) return 'Drawn plain.';
    if (!these.length) return 'No icon is drawn in this variant yet.';
    const shown = these.slice(0, 3).join(', ') + (these.length > 3 ? ', …' : '');
    return `${these.length} icon${these.length === 1 ? '' : 's'}: ${shown}.`;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    const out = Object.fromEntries(states.states.map((st) => [st, (variants[st] ?? '').trim()]));
    try {
      await request('PUT', '/icons/states', { variants: out });
      status = 'The states are saved.';
      onsaved();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="card max-w-3xl" aria-labelledby="states-heading">
  <h2 id="states-heading" class="text-lg font-semibold">Asset states</h2>
  <p class="mb-3 text-sm text-zinc-600 dark:text-zinc-400">
    An asset in one of these states is drawn as its icon's variant for it, such as <span class="font-mono">switch-down</span>, when the build draws one, and as its
    plain icon otherwise. In several, it shows the first, from the top, that its icon has. An icon has variants by opting into an axis, such as the one the
    library's state badges start.
  </p>
  <form class="space-y-3" onsubmit={save}>
    <div class="grid gap-x-4 gap-y-3 sm:grid-cols-[auto_1fr]">
      {#each states.states as st (st)}
        <label class="text-sm font-medium sm:pt-3" for="state-{st}">{iconStateNames[st]}</label>
        <div>
          <input
            id="state-{st}"
            class="input w-48 font-mono"
            bind:value={variants[st]}
            placeholder="none"
            disabled={!canManage}
            aria-describedby="state-{st}-drawn"
          />
          <p id="state-{st}-drawn" class="mt-0.5 text-xs text-zinc-600 dark:text-zinc-400">{drawn(st)}</p>
        </div>
      {/each}
    </div>
    {#if error}<p role="alert" class="text-sm text-red-700 dark:text-red-400">{error}</p>{/if}
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    {#if canManage}
      <div class="flex flex-wrap gap-2">
        <button class="btn-primary">Save the states</button>
        <button type="button" class="btn-secondary" onclick={() => (variants = { ...states.defaults })}>Use bam's defaults</button>
      </div>
    {/if}
  </form>
</section>
